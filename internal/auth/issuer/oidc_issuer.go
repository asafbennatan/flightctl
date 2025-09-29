package issuer

import (
	"context"

	"github.com/flightctl/flightctl/api/v1alpha1"
)

// OIDCIssuer defines the interface for OIDC token issuers
// This handles token issuance only - validation is handled by existing auth modules
type OIDCIssuer interface {
	// Token Issuance (OAuth2/OIDC flows)
	Token(ctx context.Context, req *v1alpha1.TokenRequest) (*v1alpha1.TokenResponse, error)
	UserInfo(ctx context.Context, accessToken string) (*v1alpha1.UserInfoResponse, error)

	// Discovery and Configuration
	GetOpenIDConfiguration(baseURL string) map[string]interface{}
	GetJWKS() (map[string]interface{}, error)
}
