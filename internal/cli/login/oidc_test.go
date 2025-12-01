package login

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	api "github.com/flightctl/flightctl/api/v1beta1"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOIDC_AuthClientCredentialsFlow(t *testing.T) {
	tests := []struct {
		name           string
		clientID       string
		clientSecret   string
		scopes         []string
		responseStatus int
		responseBody   map[string]interface{}
		expectError    bool
		errorContains  string
		validateResult func(*testing.T, AuthInfo)
	}{
		{
			name:           "successful client credentials with scopes",
			clientID:       "test-client-id",
			clientSecret:   "test-client-secret",
			scopes:         []string{"profile", "email"},
			responseStatus: http.StatusOK,
			responseBody: map[string]interface{}{
				"access_token": "oidc-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			},
			expectError: false,
			validateResult: func(t *testing.T, authInfo AuthInfo) {
				assert.Equal(t, "oidc-access-token", authInfo.AccessToken)
				// Client credentials flow never issues ID tokens
				assert.Equal(t, "", authInfo.IdToken)
				// Client credentials always uses access token
				assert.Equal(t, TokenToUseAccessToken, authInfo.TokenToUse)
				assert.NotNil(t, authInfo.ExpiresIn)
				assert.Equal(t, int64(3600), *authInfo.ExpiresIn)
			},
		},
		{
			name:           "successful client credentials without scopes",
			clientID:       "test-client-id",
			clientSecret:   "test-client-secret",
			scopes:         []string{},
			responseStatus: http.StatusOK,
			responseBody: map[string]interface{}{
				"access_token": "oidc-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			},
			expectError: false,
			validateResult: func(t *testing.T, authInfo AuthInfo) {
				assert.Equal(t, "oidc-access-token", authInfo.AccessToken)
				// Client credentials always uses access token, regardless of scopes
				assert.Equal(t, TokenToUseAccessToken, authInfo.TokenToUse)
			},
		},
		{
			name:           "authentication failure",
			clientID:       "bad-client-id",
			clientSecret:   "bad-secret",
			scopes:         []string{"openid"},
			responseStatus: http.StatusUnauthorized,
			responseBody: map[string]interface{}{
				"error":             "invalid_client",
				"error_description": "Client authentication failed",
			},
			expectError:   true,
			errorContains: "token request failed with status 401",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tokenServerURL string
			var discoveryServerURL string

			// Create token server
			tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				err := r.ParseForm()
				require.NoError(t, err)

				assert.Equal(t, "client_credentials", r.FormValue("grant_type"))
				assert.Equal(t, tt.clientID, r.FormValue("client_id"))
				assert.Equal(t, tt.clientSecret, r.FormValue("client_secret"))

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.responseStatus)
				_ = json.NewEncoder(w).Encode(tt.responseBody)
			}))
			defer tokenServer.Close()
			tokenServerURL = tokenServer.URL

			// Create OIDC discovery server that returns the token server URL
			discoveryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/.well-known/openid-configuration", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(OIDCDiscoveryResponse{
					Issuer:                discoveryServerURL,
					AuthorizationEndpoint: discoveryServerURL + "/authorize",
					TokenEndpoint:         tokenServerURL,
					JwksUri:               discoveryServerURL + "/jwks",
				})
			}))
			defer discoveryServer.Close()
			discoveryServerURL = discoveryServer.URL

			// Create OIDC provider
			spec := api.OIDCProviderSpec{
				ProviderType: api.Oidc,
				Issuer:       discoveryServerURL,
				ClientId:     "spec-client-id",
				Scopes:       lo.ToPtr(tt.scopes),
			}

			oidcProvider := NewOIDCConfig(
				api.ObjectMeta{Name: lo.ToPtr("test-oidc")},
				spec,
				"",
				false,
				"https://api.example.com",
				8080,
				"",
				"",
				tt.clientID,
				tt.clientSecret,
				false,
			)

			// Test authentication
			authInfo, err := oidcProvider.authClientCredentialsFlow()

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				if tt.validateResult != nil {
					tt.validateResult(t, authInfo)
				}
			}
		})
	}
}

func TestOIDC_Validate_ClientCredentials(t *testing.T) {
	tests := []struct {
		name         string
		clientID     string
		clientSecret string
		issuer       string
		expectError  bool
		errorMsg     string
	}{
		{
			name:         "valid client credentials",
			clientID:     "test-client-id",
			clientSecret: "test-client-secret",
			issuer:       "https://oidc.example.com",
			expectError:  false,
		},
		{
			name:         "missing issuer",
			clientID:     "test-client-id",
			clientSecret: "test-client-secret",
			issuer:       "",
			expectError:  true,
			errorMsg:     "issuer URL is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := api.OIDCProviderSpec{
				ProviderType: api.Oidc,
				Issuer:       tt.issuer,
				ClientId:     "spec-client-id",
				Scopes:       lo.ToPtr([]string{"openid"}),
			}

			oidcProvider := NewOIDCConfig(
				api.ObjectMeta{Name: lo.ToPtr("test-oidc")},
				spec,
				"",
				false,
				"https://api.example.com",
				8080,
				"",
				"",
				tt.clientID,
				tt.clientSecret,
				false,
			)

			err := oidcProvider.Validate(ValidateArgs{
				ApiUrl: "https://api.example.com",
			})

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestOIDC_AuthFlowPriority(t *testing.T) {
	tests := []struct {
		name         string
		clientID     string
		clientSecret string
		username     string
		password     string
		expectedFlow string
	}{
		{
			name:         "client credentials has priority",
			clientID:     "client-id",
			clientSecret: "client-secret",
			username:     "user",
			password:     "pass",
			expectedFlow: "client_credentials",
		},
		{
			name:         "password flow when no client credentials",
			username:     "user",
			password:     "pass",
			expectedFlow: "password",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tokenServerURL string
			var discoveryServerURL string

			tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseForm()
				assert.Equal(t, tt.expectedFlow, r.FormValue("grant_type"))
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"access_token": "token",
				})
			}))
			defer tokenServer.Close()
			tokenServerURL = tokenServer.URL

			// Create OIDC discovery server that returns the token server URL
			discoveryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(OIDCDiscoveryResponse{
					Issuer:                discoveryServerURL,
					AuthorizationEndpoint: discoveryServerURL + "/authorize",
					TokenEndpoint:         tokenServerURL,
					JwksUri:               discoveryServerURL + "/jwks",
				})
			}))
			defer discoveryServer.Close()
			discoveryServerURL = discoveryServer.URL

			spec := api.OIDCProviderSpec{
				ProviderType: api.Oidc,
				Issuer:       discoveryServerURL,
				ClientId:     "spec-client-id",
				Scopes:       lo.ToPtr([]string{"openid"}),
			}

			oidcProvider := NewOIDCConfig(
				api.ObjectMeta{Name: lo.ToPtr("test-oidc")},
				spec,
				"",
				false,
				"https://api.example.com",
				8080,
				tt.username,
				tt.password,
				tt.clientID,
				tt.clientSecret,
				false,
			)

			var authInfo AuthInfo
			var err error

			if tt.clientID != "" && tt.clientSecret != "" {
				authInfo, err = oidcProvider.authClientCredentialsFlow()
			} else {
				authInfo, err = oidcProvider.authPasswordFlow()
			}

			assert.NoError(t, err)
			assert.NotEmpty(t, authInfo.AccessToken)
		})
	}
}
