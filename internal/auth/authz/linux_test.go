package authz

import (
	"context"
	"testing"

	"github.com/flightctl/flightctl/internal/auth/common"
	"github.com/flightctl/flightctl/internal/consts"
	"github.com/stretchr/testify/assert"
)

func TestLinuxAuthZ_CheckPermission(t *testing.T) {
	authZ := NewLinuxAuthZ()

	tests := []struct {
		name     string
		username string
		resource string
		op       string
		expected bool
	}{
		{
			name:     "admin user can do anything",
			username: "root",
			resource: "devices",
			op:       "create",
			expected: true,
		},
		{
			name:     "admin user can delete",
			username: "root",
			resource: "fleets",
			op:       "delete",
			expected: true,
		},
		{
			name:     "admin user can access all resources",
			username: "root",
			resource: "organizations",
			op:       "create",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create identity
			identity := common.NewBaseIdentity(tt.username, tt.username, []string{}, []string{})

			// Create context with identity
			ctx := context.WithValue(context.Background(), consts.IdentityCtxKey, identity)

			// Test permission check
			allowed, err := authZ.CheckPermission(ctx, tt.resource, tt.op)

			assert.NoError(t, err)
			assert.Equal(t, tt.expected, allowed)
		})
	}
}

func TestLinuxAuthZ_GetUserRoles(t *testing.T) {
	authZ := NewLinuxAuthZ()

	// Test root user
	roles, err := authZ.GetUserRoles("root")
	assert.NoError(t, err)
	assert.Contains(t, roles, RoleAdmin)
}

func TestLinuxAuthZ_GetGroupRoleMapping(t *testing.T) {
	authZ := NewLinuxAuthZ()

	mapping := authZ.GetGroupRoleMapping()

	// Check that default mappings are present
	assert.Equal(t, RoleAdmin, mapping["flightctl-admin"])
	assert.Equal(t, RoleOperator, mapping["flightctl-operator"])
	assert.Equal(t, RoleViewer, mapping["flightctl-viewer"])
	assert.Equal(t, RoleInstaller, mapping["flightctl-installer"])
	assert.Equal(t, RoleAdmin, mapping["wheel"])
	assert.Equal(t, RoleAdmin, mapping["sudo"])
	assert.Equal(t, RoleOperator, mapping["adm"])
}

func TestLinuxAuthZ_NewLinuxAuthZWithMapping(t *testing.T) {
	customMapping := map[string]string{
		"custom-admin":  RoleAdmin,
		"custom-viewer": RoleViewer,
	}

	authZ := NewLinuxAuthZWithMapping(customMapping)
	mapping := authZ.GetGroupRoleMapping()

	// Check that custom mappings are present
	assert.Equal(t, RoleAdmin, mapping["custom-admin"])
	assert.Equal(t, RoleViewer, mapping["custom-viewer"])

	// Check that default mappings are still present
	assert.Equal(t, RoleAdmin, mapping["flightctl-admin"])
	assert.Equal(t, RoleOperator, mapping["flightctl-operator"])
}
