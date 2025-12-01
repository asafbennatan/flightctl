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

func TestOAuth2_AuthClientCredentialsFlow(t *testing.T) {
	tests := []struct {
		name           string
		clientID       string
		clientSecret   string
		responseStatus int
		responseBody   map[string]interface{}
		expectError    bool
		errorContains  string
		validateResult func(*testing.T, AuthInfo)
	}{
		{
			name:           "successful client credentials authentication",
			clientID:       "test-client-id",
			clientSecret:   "test-client-secret",
			responseStatus: http.StatusOK,
			responseBody: map[string]interface{}{
				"access_token":  "oauth2-access-token",
				"token_type":    "Bearer",
				"expires_in":    3600,
				"refresh_token": "oauth2-refresh-token",
			},
			expectError: false,
			validateResult: func(t *testing.T, authInfo AuthInfo) {
				assert.Equal(t, "oauth2-access-token", authInfo.AccessToken)
				assert.Equal(t, "oauth2-refresh-token", authInfo.RefreshToken)
				assert.Equal(t, TokenToUseAccessToken, authInfo.TokenToUse)
				assert.NotNil(t, authInfo.ExpiresIn)
				assert.Equal(t, int64(3600), *authInfo.ExpiresIn)
			},
		},
		{
			name:           "authentication failure",
			clientID:       "bad-client-id",
			clientSecret:   "bad-secret",
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
			// Create test token server
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

			// Create OAuth2 provider
			spec := api.OAuth2ProviderSpec{
				ProviderType:     api.Oauth2,
				AuthorizationUrl: "https://oauth2.example.com/authorize",
				TokenUrl:         tokenServer.URL,
				UserinfoUrl:      "https://oauth2.example.com/userinfo",
				ClientId:         "spec-client-id",
				Scopes:           lo.ToPtr([]string{"read", "write"}),
			}

			oauth2Provider := NewOAuth2Config(
				api.ObjectMeta{Name: lo.ToPtr("test-oauth2")},
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
			authInfo, err := oauth2Provider.Auth()

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

func TestOAuth2_Validate_ClientCredentials(t *testing.T) {
	tests := []struct {
		name         string
		clientID     string
		clientSecret string
		expectError  bool
		errorMsg     string
	}{
		{
			name:         "valid client credentials",
			clientID:     "test-client-id",
			clientSecret: "test-client-secret",
			expectError:  false,
		},
		{
			name:         "missing provider name",
			clientID:     "test-client-id",
			clientSecret: "test-client-secret",
			expectError:  true,
			errorMsg:     "provider name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := api.OAuth2ProviderSpec{
				ProviderType:     api.Oauth2,
				AuthorizationUrl: "https://oauth2.example.com/authorize",
				TokenUrl:         "https://oauth2.example.com/token",
				UserinfoUrl:      "https://oauth2.example.com/userinfo",
				ClientId:         "spec-client-id",
				Scopes:           lo.ToPtr([]string{"read"}),
			}

			metadata := api.ObjectMeta{}
			if tt.name != "missing provider name" {
				metadata.Name = lo.ToPtr("test-oauth2")
			}

			oauth2Provider := NewOAuth2Config(
				metadata,
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

			err := oauth2Provider.Validate(ValidateArgs{
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

func TestOAuth2_AuthFlowPriority(t *testing.T) {
	tests := []struct {
		name             string
		clientID         string
		clientSecret     string
		username         string
		password         string
		web              bool
		expectedFlow     string
		setupTokenServer func() *httptest.Server
	}{
		{
			name:         "client credentials flow has priority over password flow",
			clientID:     "client-id",
			clientSecret: "client-secret",
			username:     "user",
			password:     "pass",
			expectedFlow: "client_credentials",
			setupTokenServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_ = r.ParseForm()
					// Verify it's client credentials, not password flow
					assert.Equal(t, "client_credentials", r.FormValue("grant_type"))
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"access_token": "token",
					})
				}))
			},
		},
		{
			name:         "password flow when no client credentials",
			username:     "user",
			password:     "pass",
			expectedFlow: "password",
			setupTokenServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_ = r.ParseForm()
					assert.Equal(t, "password", r.FormValue("grant_type"))
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"access_token": "token",
					})
				}))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenServer := tt.setupTokenServer()
			defer tokenServer.Close()

			spec := api.OAuth2ProviderSpec{
				ProviderType:     api.Oauth2,
				AuthorizationUrl: "https://oauth2.example.com/authorize",
				TokenUrl:         tokenServer.URL,
				UserinfoUrl:      "https://oauth2.example.com/userinfo",
				ClientId:         "spec-client-id",
				Scopes:           lo.ToPtr([]string{"read"}),
			}

			oauth2Provider := NewOAuth2Config(
				api.ObjectMeta{Name: lo.ToPtr("test-oauth2")},
				spec,
				"",
				false,
				"https://api.example.com",
				8080,
				tt.username,
				tt.password,
				tt.clientID,
				tt.clientSecret,
				tt.web,
			)

			authInfo, err := oauth2Provider.Auth()
			assert.NoError(t, err)
			assert.NotEmpty(t, authInfo.AccessToken)
		})
	}
}

func TestOAuth2_AuthPasswordFlow(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		assert.Equal(t, "password", r.FormValue("grant_type"))
		assert.Equal(t, "testuser", r.FormValue("username"))
		assert.Equal(t, "testpass", r.FormValue("password"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "password-flow-token",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	spec := api.OAuth2ProviderSpec{
		ProviderType:     api.Oauth2,
		AuthorizationUrl: "https://oauth2.example.com/authorize",
		TokenUrl:         tokenServer.URL,
		UserinfoUrl:      "https://oauth2.example.com/userinfo",
		ClientId:         "test-client-id",
		Scopes:           lo.ToPtr([]string{"read"}),
	}

	oauth2Provider := NewOAuth2Config(
		api.ObjectMeta{Name: lo.ToPtr("test-oauth2")},
		spec,
		"",
		false,
		"https://api.example.com",
		8080,
		"testuser",
		"testpass",
		"",
		"",
		false,
	)

	authInfo, err := oauth2Provider.Auth()
	assert.NoError(t, err)
	assert.Equal(t, "password-flow-token", authInfo.AccessToken)
	assert.Equal(t, TokenToUseAccessToken, authInfo.TokenToUse)
}
