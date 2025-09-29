//go:build linux

package issuer

import (
	"os/user"

	"github.com/flightctl/flightctl/internal/auth/authn"
	"github.com/flightctl/flightctl/internal/config"
)

// SSSDOIDCProvider represents an SSSD-based OIDC issuer
type SSSDOIDCProvider struct {
	jwtGenerator      *authn.JWTGenerator
	config            *config.LinuxIssuerAuth
	sssdAuthenticator SSSDAuthenticator
}

// SSSDAuthenticator interface for SSSD authentication and user lookup
type SSSDAuthenticator interface {
	Authenticate(username, password string) error
	LookupUser(username string) (*user.User, error)
	GetUserGroups(systemUser *user.User) ([]string, error)
}
