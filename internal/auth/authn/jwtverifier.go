package authn

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/flightctl/flightctl/internal/auth/common"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

const (
	subClaim = "sub"
)

type TokenIdentity interface {
	common.Identity
	GetClaim(string) (interface{}, bool)
}

// JWTIdentity extends common.Identity with JWT-specific fields
type JWTIdentity struct {
	common.BaseIdentity
	parsedToken jwt.Token
}

// Ensure JWTIdentity implements TokenIdentity
var _ TokenIdentity = (*JWTIdentity)(nil)

func (i *JWTIdentity) GetClaim(claim string) (interface{}, bool) {
	return i.parsedToken.Get(claim)
}

// getValueByPath extracts a value from a JWT token using dot notation path
func getValueByPath(token jwt.Token, path string) (interface{}, bool) {
	if path == "" {
		return nil, false
	}

	// Split the path by dots
	parts := strings.Split(path, ".")

	// Get the first part using token.Get
	value, exists := token.Get(parts[0])
	if !exists || len(parts) == 1 {
		return value, exists
	}

	// If there are more parts, navigate through the nested structure
	current := value
	for i := 1; i < len(parts); i++ {
		if current == nil {
			return nil, false
		}

		// Handle map[string]interface{} case
		if m, ok := current.(map[string]interface{}); ok {
			if val, exists := m[parts[i]]; exists {
				current = val
			} else {
				return nil, false
			}
		} else {
			return nil, false
		}
	}

	return current, true
}

type OIDCAuth struct {
	oidcAuthority         string
	externalOIDCAuthority string
	jwksUri               string
	clientTlsConfig       *tls.Config
	client                *http.Client
	orgConfig             *common.AuthOrganizationsConfig
	usernameClaim         string
	groupsClaim           string
	jwksCache             *jwk.Cache
}

type OIDCServerResponse struct {
	TokenEndpoint string `json:"token_endpoint"`
	JwksUri       string `json:"jwks_uri"`
}

func NewOIDCAuth(oidcAuthority string, externalOIDCAuthority string, clientTlsConfig *tls.Config, orgConfig *common.AuthOrganizationsConfig, usernameClaim string, groupsClaim string) (OIDCAuth, error) {
	oidcAuth := OIDCAuth{
		oidcAuthority:         oidcAuthority,
		externalOIDCAuthority: externalOIDCAuthority,
		clientTlsConfig:       clientTlsConfig,
		client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: clientTlsConfig,
			},
		},
		orgConfig:     orgConfig,
		usernameClaim: usernameClaim,
		groupsClaim:   groupsClaim,
	}

	res, err := oidcAuth.client.Get(fmt.Sprintf("%s/.well-known/openid-configuration", oidcAuthority))
	if err != nil {
		return oidcAuth, err
	}
	oidcResponse := OIDCServerResponse{}
	defer res.Body.Close()
	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return oidcAuth, err
	}
	if err := json.Unmarshal(bodyBytes, &oidcResponse); err != nil {
		return oidcAuth, err
	}
	oidcAuth.jwksUri = oidcResponse.JwksUri

	// Initialize JWKS cache with 15-minute refresh interval
	// This balances performance with key rotation requirements
	oidcAuth.jwksCache = jwk.NewCache(context.Background())
	oidcAuth.jwksCache.Register(oidcAuth.jwksUri, jwk.WithMinRefreshInterval(15*time.Minute))

	return oidcAuth, nil
}

func (o OIDCAuth) ValidateToken(ctx context.Context, token string) error {
	_, err := o.parseAndCreateIdentity(ctx, token)
	return err
}

func (o OIDCAuth) parseAndCreateIdentity(ctx context.Context, token string) (*JWTIdentity, error) {
	var jwkSet jwk.Set
	var err error

	// Get JWK set from cache
	jwkSet, err = o.jwksCache.Get(ctx, o.jwksUri)
	if err != nil {
		return nil, fmt.Errorf("failed to get JWK set from cache: %w", err)
	}

	parsedToken, err := jwt.Parse([]byte(token), jwt.WithKeySet(jwkSet), jwt.WithValidate(true))
	if err != nil {
		// If token validation fails, it might be due to key rotation
		// Try to refresh the cache and retry once (only if cache is available)
		if o.jwksCache != nil {
			if _, refreshErr := o.jwksCache.Refresh(ctx, o.jwksUri); refreshErr == nil {
				// Retry with refreshed keys
				jwkSet, retryErr := o.jwksCache.Get(ctx, o.jwksUri)
				if retryErr == nil {
					parsedToken, retryErr = jwt.Parse([]byte(token), jwt.WithKeySet(jwkSet), jwt.WithValidate(true))
					if retryErr == nil {
						err = nil // Clear the original error
					}
				}
			}
		}

		if err != nil {
			return nil, fmt.Errorf("failed to parse JWT token: %w", err)
		}
	}

	identity := &JWTIdentity{}
	identity.parsedToken = parsedToken

	if sub, exists := parsedToken.Get(subClaim); exists {
		if uid, ok := sub.(string); ok {
			identity.SetUID(uid)
		}
	}

	if o.usernameClaim != "" {
		if username, exists := parsedToken.Get(o.usernameClaim); exists {
			if usernameStr, ok := username.(string); ok {
				identity.SetUsername(usernameStr)
			}
		}
	}

	// Extract roles from JWT
	roles := o.extractRoles(parsedToken)
	identity.SetRoles(roles)

	// Extract organizations from JWT based on org config
	organizations := o.extractOrganizations(parsedToken)
	identity.SetOrganizations(organizations)

	return identity, nil
}

func (o OIDCAuth) GetIdentity(ctx context.Context, token string) (common.Identity, error) {
	identity, err := o.parseAndCreateIdentity(ctx, token)
	if err != nil {
		return nil, err
	}

	return identity, nil
}

func (o OIDCAuth) GetAuthConfig() common.AuthConfig {
	orgConfig := common.AuthOrganizationsConfig{}
	if o.orgConfig != nil {
		orgConfig = *o.orgConfig
	}

	return common.AuthConfig{
		Type:                common.AuthTypeOIDC,
		Url:                 o.externalOIDCAuthority,
		OrganizationsConfig: orgConfig,
	}
}

func (o OIDCAuth) GetAuthToken(r *http.Request) (string, error) {
	return common.ExtractBearerToken(r)
}

// extractRoles extracts roles from multiple possible JWT claims
func (o OIDCAuth) extractRoles(token jwt.Token) []string {
	var roles []string

	// 1. Try configured groups claim first
	if o.groupsClaim != "" {
		if groupsClaim, exists := token.Get(o.groupsClaim); exists {
			if groupsList, ok := groupsClaim.([]interface{}); ok {
				for _, group := range groupsList {
					if groupStr, ok := group.(string); ok {
						roles = append(roles, groupStr)
					}
				}
			}
		}
	}

	return roles
}

// extractOrganizations extracts organization information from JWT token based on org config
func (o OIDCAuth) extractOrganizations(token jwt.Token) []string {
	var organizations []string

	// If no org config or organizations are disabled, return empty
	if o.orgConfig == nil || !o.orgConfig.Enabled || o.orgConfig.OrganizationAssignment == nil {
		return organizations
	}

	assignment := o.orgConfig.OrganizationAssignment

	switch assignment.Type {
	case OrganizationAssignmentTypeStatic:
		// Static assignment: use the configured organization name
		if assignment.OrganizationName != nil && *assignment.OrganizationName != "" {
			organizations = append(organizations, *assignment.OrganizationName)
		}
	case OrganizationAssignmentTypeDynamic:
		// Dynamic assignment: extract from JWT claim
		if assignment.ClaimPath != nil && *assignment.ClaimPath != "" {
			if orgValue, exists := getValueByPath(token, *assignment.ClaimPath); exists {
				if orgStr, ok := orgValue.(string); ok && orgStr != "" {
					organizations = append(organizations, orgStr)
				}
			}
		}
	case OrganizationAssignmentTypePerUser:
		// Per-user assignment: create organization name from username
		username := ""
		if o.usernameClaim != "" {
			if usernameClaim, exists := token.Get(o.usernameClaim); exists {
				if usernameStr, ok := usernameClaim.(string); ok {
					username = usernameStr
				}
			}
		}

		if username != "" {
			orgName := username
			if assignment.OrganizationNamePrefix != nil && *assignment.OrganizationNamePrefix != "" {
				orgName = *assignment.OrganizationNamePrefix + orgName
			}
			if assignment.OrganizationNameSuffix != nil && *assignment.OrganizationNameSuffix != "" {
				orgName = orgName + *assignment.OrganizationNameSuffix
			}
			organizations = append(organizations, orgName)
		}
	}

	return organizations
}
