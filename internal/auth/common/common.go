package common

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/flightctl/flightctl/internal/consts"
)

const (
	AuthHeader string = "Authorization"
)

const (
	AuthTypeK8s  = "k8s"
	AuthTypeOIDC = "OIDC"
	AuthTypeAAP  = "AAPGateway"
)

type AuthConfig struct {
	Type                string
	Url                 string
	OrganizationsConfig AuthOrganizationsConfig
}

type AuthOrganizationsConfig struct {
	Enabled bool
	// OrganizationAssignment defines how users are assigned to organizations
	OrganizationAssignment *OrganizationAssignment
}

// OrganizationAssignment defines how users are assigned to organizations
type OrganizationAssignment struct {
	Type string `json:"type"` // "static", "dynamic", or "perUser"

	// Static assignment fields
	OrganizationName *string `json:"organizationName,omitempty"`

	// Dynamic assignment fields
	ClaimPath               *string `json:"claimPath,omitempty"`
	OrganizationNameMapping *string `json:"organizationNameMapping,omitempty"`

	// Per-user assignment fields
	OrganizationNamePrefix *string `json:"organizationNamePrefix,omitempty"`
	OrganizationNameSuffix *string `json:"organizationNameSuffix,omitempty"`
}

type Identity interface {
	GetUsername() string
	GetUID() string
	GetOrganizations() []string
	GetRoles() []string
}

type AuthNMiddleware interface {
	GetAuthToken(r *http.Request) (string, error)
	ValidateToken(ctx context.Context, token string) error
	GetIdentity(ctx context.Context, token string) (Identity, error)
	GetAuthConfig() AuthConfig
}

type BaseIdentity struct {
	username      string
	uID           string
	organizations []string
	roles         []string
}

// Ensure BaseIdentity implements Identity
var _ Identity = (*BaseIdentity)(nil)

func NewBaseIdentity(username string, uID string, organizations []string, roles []string) *BaseIdentity {
	return &BaseIdentity{
		username:      username,
		uID:           uID,
		organizations: append([]string(nil), organizations...),
		roles:         roles,
	}
}

func (i *BaseIdentity) GetUsername() string {
	return i.username
}

func (i *BaseIdentity) SetUsername(username string) {
	i.username = username
}

func (i *BaseIdentity) GetUID() string {
	return i.uID
}

func (i *BaseIdentity) SetUID(uID string) {
	i.uID = uID
}

func (i *BaseIdentity) GetOrganizations() []string {
	return append([]string(nil), i.organizations...)
}

func (i *BaseIdentity) SetOrganizations(organizations []string) {
	i.organizations = append([]string(nil), organizations...)
}

func (i *BaseIdentity) GetRoles() []string {
	return append([]string(nil), i.roles...)
}

func (i *BaseIdentity) SetRoles(roles []string) {
	i.roles = append([]string(nil), roles...)
}

func GetIdentity(ctx context.Context) (Identity, error) {
	identityVal := ctx.Value(consts.IdentityCtxKey)
	if identityVal == nil {
		return nil, fmt.Errorf("failed to get identity from context")
	}
	identity, ok := identityVal.(Identity)
	if !ok {
		return nil, fmt.Errorf("incorrect type of identity in context (got %T)", identityVal)
	}
	return identity, nil
}

func ExtractBearerToken(r *http.Request) (string, error) {
	authHeader := r.Header.Get(AuthHeader)
	if authHeader == "" {
		return "", fmt.Errorf("empty %s header", AuthHeader)
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == authHeader {
		return "", fmt.Errorf("invalid %s header", AuthHeader)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", fmt.Errorf("invalid token")
	}
	return token, nil
}
