package issuer

import (
	"os/user"

	"github.com/flightctl/flightctl/internal/auth/authn"
	"github.com/flightctl/flightctl/internal/config"
)

// PAMOIDCProvider represents a PAM-based OIDC issuer
type PAMOIDCProvider struct {
	jwtGenerator     *authn.JWTGenerator
	pamService       string
	config           *config.LinuxIssuerAuth
	pamAuthenticator PAMAuthenticator
}

// PAMAuthenticator interface for PAM authentication and user lookup
type PAMAuthenticator interface {
	Authenticate(service, username, password string) error
	LookupUser(username string) (*user.User, error)
	GetUserGroups(systemUser *user.User) ([]string, error)
}
