package service

import (
	"context"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	authcommon "github.com/flightctl/flightctl/internal/auth/common"
	"github.com/samber/lo"
)

// AuthToken handles OAuth2 token requests
func (h *ServiceHandler) AuthToken(ctx context.Context, req api.TokenRequest) (*api.TokenResponse, api.Status) {
	if h.oidcIssuer == nil {
		return nil, api.StatusInternalServerError("OIDC issuer not configured")
	}

	tokenResponse, err := h.oidcIssuer.Token(ctx, &req)
	if err != nil {
		return nil, api.StatusBadRequest(err.Error())
	}

	return tokenResponse, api.StatusOK()
}

// AuthUserInfo handles OIDC UserInfo requests
func (h *ServiceHandler) AuthUserInfo(ctx context.Context, accessToken string) (*api.UserInfoResponse, api.Status) {
	if h.oidcIssuer == nil {
		return nil, api.StatusInternalServerError("OIDC issuer not configured")
	}

	userInfoResponse, err := h.oidcIssuer.UserInfo(ctx, accessToken)
	if err != nil {
		return nil, api.StatusUnauthorized(err.Error())
	}

	return userInfoResponse, api.StatusOK()
}

// AuthJWKS handles JWKS requests
func (h *ServiceHandler) AuthJWKS(ctx context.Context) (*api.JWKSResponse, api.Status) {
	if h.oidcIssuer == nil {
		return nil, api.StatusInternalServerError("OIDC issuer not configured")
	}

	jwks, err := h.oidcIssuer.GetJWKS()
	if err != nil {
		return nil, api.StatusInternalServerError(err.Error())
	}

	// The JWKS from the PAM issuer is already in the correct format
	// It returns a map with "keys" containing the actual JWK array
	if keysInterface, ok := jwks["keys"]; ok {
		if keysArray, ok := keysInterface.([]interface{}); ok {
			// Convert the JWK array to the expected struct format
			keys := make([]struct {
				Alg *string `json:"alg,omitempty"`
				E   *string `json:"e,omitempty"`
				Kid *string `json:"kid,omitempty"`
				Kty *string `json:"kty,omitempty"`
				N   *string `json:"n,omitempty"`
				Use *string `json:"use,omitempty"`
			}, len(keysArray))

			for i, keyInterface := range keysArray {
				if keyMap, ok := keyInterface.(map[string]interface{}); ok {
					// Extract values from the JWK map
					if alg, ok := keyMap["alg"].(string); ok {
						keys[i].Alg = &alg
					}
					if e, ok := keyMap["e"].(string); ok {
						keys[i].E = &e
					}
					if kid, ok := keyMap["kid"].(string); ok {
						keys[i].Kid = &kid
					}
					if kty, ok := keyMap["kty"].(string); ok {
						keys[i].Kty = &kty
					}
					if n, ok := keyMap["n"].(string); ok {
						keys[i].N = &n
					}
					if use, ok := keyMap["use"].(string); ok {
						keys[i].Use = &use
					}
				}
			}

			return &api.JWKSResponse{
				Keys: &keys,
			}, api.StatusOK()
		}
	}

	// Fallback: return empty keys if conversion fails
	keys := make([]struct {
		Alg *string `json:"alg,omitempty"`
		E   *string `json:"e,omitempty"`
		Kid *string `json:"kid,omitempty"`
		Kty *string `json:"kty,omitempty"`
		N   *string `json:"n,omitempty"`
		Use *string `json:"use,omitempty"`
	}, 0)

	return &api.JWKSResponse{
		Keys: &keys,
	}, api.StatusOK()
}

// AuthOpenIDConfiguration handles OpenID Connect discovery requests
func (h *ServiceHandler) AuthOpenIDConfiguration(ctx context.Context) (*api.OpenIDConfiguration, api.Status) {
	if h.oidcIssuer == nil {
		return nil, api.StatusInternalServerError("OIDC issuer not configured")
	}

	// Use the UI URL as the base URL for OIDC configuration
	baseURL := h.uiUrl
	if baseURL == "" {
		baseURL = "http://localhost:8080" // fallback
	}

	config := h.oidcIssuer.GetOpenIDConfiguration(baseURL)

	// Helper function to safely get string values
	getString := func(key string) *string {
		if val, ok := config[key].(string); ok {
			return &val
		}
		return nil
	}

	// Helper function to safely get string slice values
	getStringSlice := func(key string) *[]string {
		if val, ok := config[key].([]string); ok {
			return &val
		}
		return nil
	}

	return &api.OpenIDConfiguration{
		Issuer:                            getString("issuer"),
		AuthorizationEndpoint:             getString("authorization_endpoint"),
		TokenEndpoint:                     getString("token_endpoint"),
		UserinfoEndpoint:                  getString("userinfo_endpoint"),
		JwksUri:                           getString("jwks_uri"),
		ResponseTypesSupported:            getStringSlice("response_types_supported"),
		GrantTypesSupported:               getStringSlice("grant_types_supported"),
		ScopesSupported:                   getStringSlice("scopes_supported"),
		ClaimsSupported:                   getStringSlice("claims_supported"),
		IdTokenSigningAlgValuesSupported:  getStringSlice("id_token_signing_alg_values_supported"),
		TokenEndpointAuthMethodsSupported: getStringSlice("token_endpoint_auth_methods_supported"),
	}, api.StatusOK()
}

// AuthAuthorize handles OAuth2 authorization requests
func (h *ServiceHandler) AuthAuthorize(ctx context.Context, params api.AuthAuthorizeParams) (*api.Status, api.Status) {
	// For now, return a simple redirect response
	// In a full OAuth2 implementation, this would redirect to a login page
	// and then redirect back with an authorization code
	return &api.Status{
		Message: "Authorization endpoint - redirect to login page",
	}, api.StatusOK()
}

// GetAuthConfig returns the complete authentication configuration including all available providers
func (h *ServiceHandler) GetAuthConfig(ctx context.Context, authConfig authcommon.AuthConfig) (*api.AuthConfig, api.Status) {
	// Build the list of all available providers
	providers := []api.AuthProviderInfo{}

	// Add the static provider (from config)
	staticProvider := api.AuthProviderInfo{
		Name:        &authConfig.Type,
		DisplayName: &authConfig.Type,
		Type:        (*api.AuthProviderInfoType)(&authConfig.Type),
		AuthUrl:     &authConfig.Url,
		IsDefault:   lo.ToPtr(true),
		IsStatic:    lo.ToPtr(true),
	}
	providers = append(providers, staticProvider)

	// Add dynamic OIDC providers from the database
	// Note: We need to get all OIDC providers without organization context since this is a public endpoint
	// For now, we'll use a system context or handle this differently
	// This is a limitation that needs to be addressed - dynamic providers are org-scoped but this is a global endpoint

	// TODO: Implement proper handling of dynamic OIDC providers
	// Options:
	// 1. Make OIDC provider list endpoint public (no auth required)
	// 2. Use a system/admin context to list all providers
	// 3. Create a separate public endpoint for provider discovery

	conf := api.AuthConfig{
		Providers:            &providers,
		DefaultProvider:      &authConfig.Type,
		OrganizationsEnabled: &authConfig.OrganizationsConfig.Enabled,
	}

	return &conf, api.StatusOK()
}
