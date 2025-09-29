package authz

import (
	"context"
	"fmt"
	"os/user"

	"github.com/flightctl/flightctl/internal/auth/common"
)

// LinuxAuthZ implements role-based authorization using system groups
type LinuxAuthZ struct {
	// Group to role mapping configuration
	groupRoleMap map[string]string
}

// Role definitions based on K8s RBAC roles
const (
	RoleAdmin     = "admin"     // Full access to all resources
	RoleOperator  = "operator"  // Manage devices, fleets, resourcesyncs
	RoleViewer    = "viewer"    // Read-only access to devices, fleets, resourcesyncs
	RoleInstaller = "installer" // Limited access for device installation
)

// Default group to role mapping
var defaultGroupRoleMap = map[string]string{
	"flightctl-admin":     RoleAdmin,
	"flightctl-operator":  RoleOperator,
	"flightctl-viewer":    RoleViewer,
	"flightctl-installer": RoleInstaller,
	"wheel":               RoleAdmin,    // Traditional Unix admin group
	"sudo":                RoleAdmin,    // Sudo users get admin access
	"adm":                 RoleOperator, // System administration group
}

// Resource permissions based on K8s RBAC roles
var resourcePermissions = map[string]map[string][]string{
	RoleAdmin: {
		"devices":       {"*"},
		"fleets":        {"*"},
		"repositories":  {"*"},
		"resourcesyncs": {"*"},
		"organizations": {"*"},
		"events":        {"*"},
	},
	RoleOperator: {
		"devices":       {"get", "list", "create", "update", "patch", "delete"},
		"fleets":        {"get", "list", "create", "update", "patch", "delete"},
		"resourcesyncs": {"get", "list", "create", "update", "patch", "delete"},
		"repositories":  {"get", "list"},
		"events":        {"get", "list"},
	},
	RoleViewer: {
		"devices":       {"get", "list"},
		"fleets":        {"get", "list"},
		"resourcesyncs": {"get", "list"},
		"repositories":  {"get", "list"},
		"events":        {"get", "list"},
	},
	RoleInstaller: {
		"devices":      {"get", "list"},
		"fleets":       {"get", "list"},
		"repositories": {"get", "list"},
	},
}

func NewLinuxAuthZ() *LinuxAuthZ {
	return &LinuxAuthZ{
		groupRoleMap: defaultGroupRoleMap,
	}
}

// NewLinuxAuthZWithMapping creates a LinuxAuthZ with custom group to role mapping
func NewLinuxAuthZWithMapping(groupRoleMap map[string]string) *LinuxAuthZ {
	// Merge with defaults, custom mapping takes precedence
	mapping := make(map[string]string)
	for k, v := range defaultGroupRoleMap {
		mapping[k] = v
	}
	for k, v := range groupRoleMap {
		mapping[k] = v
	}

	return &LinuxAuthZ{
		groupRoleMap: mapping,
	}
}

func (l LinuxAuthZ) CheckPermission(ctx context.Context, resource string, op string) (bool, error) {
	// Get identity from context (set by authentication middleware)
	identity, err := common.GetIdentity(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get identity from context: %w", err)
	}

	// Get user's roles from their system groups
	userRoles, err := l.getUserRoles(identity.GetUsername())
	if err != nil {
		return false, fmt.Errorf("failed to get user roles: %w", err)
	}

	// Check if user has permission for the requested resource and operation
	return l.hasPermission(userRoles, resource, op), nil
}

// getUserRoles determines user roles based on their system groups
func (l LinuxAuthZ) getUserRoles(username string) ([]string, error) {
	// Get system user
	systemUser, err := user.Lookup(username)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup user %s: %w", username, err)
	}

	// Root user always has admin role
	if systemUser.Uid == "0" {
		return []string{RoleAdmin}, nil
	}

	// Get user's groups
	groupIds, err := systemUser.GroupIds()
	if err != nil {
		return nil, fmt.Errorf("failed to get group IDs for user %s: %w", username, err)
	}

	var roles []string
	roleSet := make(map[string]bool) // Use set to avoid duplicates

	// Map groups to roles
	for _, groupId := range groupIds {
		group, err := user.LookupGroupId(groupId)
		if err != nil {
			// Skip groups that can't be looked up (e.g., deleted groups)
			continue
		}

		// Check if this group maps to a role
		if role, exists := l.groupRoleMap[group.Name]; exists {
			if !roleSet[role] {
				roles = append(roles, role)
				roleSet[role] = true
			}
		}
	}

	// If no roles found, default to viewer (read-only access)
	if len(roles) == 0 {
		roles = []string{RoleViewer}
	}

	return roles, nil
}

// hasPermission checks if the user's roles allow the requested operation
func (l LinuxAuthZ) hasPermission(userRoles []string, resource string, op string) bool {
	for _, role := range userRoles {
		if permissions, exists := resourcePermissions[role]; exists {
			if resourcePerms, exists := permissions[resource]; exists {
				// Check if role has permission for this operation
				for _, allowedOp := range resourcePerms {
					if allowedOp == "*" || allowedOp == op {
						return true
					}
				}
			}
		}
	}
	return false
}

// GetUserRoles returns the roles for a given username (useful for debugging/testing)
func (l LinuxAuthZ) GetUserRoles(username string) ([]string, error) {
	return l.getUserRoles(username)
}

// GetGroupRoleMapping returns the current group to role mapping
func (l LinuxAuthZ) GetGroupRoleMapping() map[string]string {
	// Return a copy to prevent external modification
	mapping := make(map[string]string)
	for k, v := range l.groupRoleMap {
		mapping[k] = v
	}
	return mapping
}
