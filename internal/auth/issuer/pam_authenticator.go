//go:build linux

package issuer

import (
	"fmt"
	"os/user"

	"github.com/msteinert/pam"
)

// RealPAMAuthenticator implements PAM authentication using the real PAM library
type RealPAMAuthenticator struct{}

// Authenticate performs real PAM authentication
func (r *RealPAMAuthenticator) Authenticate(service, username, password string) error {
	// Create PAM transaction with proper handler
	tx, err := pam.StartFunc(service, username, func(s pam.Style, msg string) (string, error) {
		switch s {
		case pam.PromptEchoOff:
			return password, nil
		case pam.PromptEchoOn:
			return username, nil
		case pam.ErrorMsg:
			return "", fmt.Errorf("PAM error: %s", msg)
		case pam.TextInfo:
			return "", nil
		}
		return "", nil
	})
	if err != nil {
		return fmt.Errorf("PAM initialization failed: %w", err)
	}

	// Authenticate the user
	if err := tx.Authenticate(0); err != nil {
		return fmt.Errorf("PAM authentication failed: %w", err)
	}

	// Check account validity
	if err := tx.AcctMgmt(0); err != nil {
		return fmt.Errorf("PAM account management failed: %w", err)
	}

	return nil
}

// LookupUser looks up a user by username using the system
func (r *RealPAMAuthenticator) LookupUser(username string) (*user.User, error) {
	return user.Lookup(username)
}

// GetUserGroups gets the groups for a system user
func (r *RealPAMAuthenticator) GetUserGroups(systemUser *user.User) ([]string, error) {
	// Get group IDs for the user
	groupIds, err := systemUser.GroupIds()
	if err != nil {
		return nil, fmt.Errorf("failed to get group IDs: %w", err)
	}

	// Convert group IDs to group names
	var groupNames []string
	for _, gid := range groupIds {
		group, err := user.LookupGroupId(gid)
		if err != nil {
			// Skip groups that can't be looked up
			continue
		}
		groupNames = append(groupNames, group.Name)
	}

	return groupNames, nil
}
