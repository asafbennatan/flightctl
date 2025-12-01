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

func TestAAP_AuthClientCredentialsFlow(t *testing.T) {
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
				"access_token": "aap-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600000000000, // AAP returns nanoseconds
			},
			expectError: false,
			validateResult: func(t *testing.T, authInfo AuthInfo) {
				assert.Equal(t, "aap-access-token", authInfo.AccessToken)
				assert.Equal(t, TokenToUseAccessToken, authInfo.TokenToUse)
				assert.NotNil(t, authInfo.ExpiresIn)
				// Should be converted from nanoseconds to seconds
				assert.Equal(t, int64(3600), *authInfo.ExpiresIn)
			},
		},
		{
			name:           "authentication failure",
			clientID:       "bad-client-id",
			clientSecret:   "bad-secret",
			responseStatus: http.StatusUnauthorized,
			responseBody: map[string]interface{}{
				"error": "invalid_client",
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

			// Create AAP provider
			spec := api.AapProviderSpec{
				ProviderType:     api.Aap,
				AuthorizationUrl: "https://aap.example.com/authorize",
				TokenUrl:         tokenServer.URL,
				ClientId:         "spec-client-id",
				Scopes:           []string{"read", "write"},
			}

			aapProvider := NewAAPOAuth2Config(
				api.ObjectMeta{Name: lo.ToPtr("test-aap")},
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
			authInfo, err := aapProvider.Auth()

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

func TestAAP_Validate_ClientCredentials(t *testing.T) {
	tests := []struct {
		name         string
		clientID     string
		clientSecret string
		tokenURL     string
		expectError  bool
		errorMsg     string
	}{
		{
			name:         "valid client credentials",
			clientID:     "test-client-id",
			clientSecret: "test-client-secret",
			tokenURL:     "https://aap.example.com/token",
			expectError:  false,
		},
		{
			name:         "missing token URL",
			clientID:     "test-client-id",
			clientSecret: "test-client-secret",
			tokenURL:     "",
			expectError:  true,
			errorMsg:     "AAP auth: missing Spec.TokenUrl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := api.AapProviderSpec{
				ProviderType:     api.Aap,
				AuthorizationUrl: "https://aap.example.com/authorize",
				TokenUrl:         tt.tokenURL,
				ClientId:         "spec-client-id",
				Scopes:           []string{"read"},
			}

			aapProvider := NewAAPOAuth2Config(
				api.ObjectMeta{Name: lo.ToPtr("test-aap")},
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

			err := aapProvider.Validate(ValidateArgs{
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

func TestAAP_ExpiresInConversion(t *testing.T) {
	// Test that AAP's nanosecond expires_in is converted to seconds
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "token",
			// AAP returns expires_in in nanoseconds
			"expires_in": 7200000000000, // 7200 seconds in nanoseconds
		})
	}))
	defer tokenServer.Close()

	spec := api.AapProviderSpec{
		ProviderType:     api.Aap,
		AuthorizationUrl: "https://aap.example.com/authorize",
		TokenUrl:         tokenServer.URL,
		ClientId:         "test-client-id",
		Scopes:           []string{"read"},
	}

	aapProvider := NewAAPOAuth2Config(
		api.ObjectMeta{Name: lo.ToPtr("test-aap")},
		spec,
		"",
		false,
		"https://api.example.com",
		8080,
		"",
		"",
		"client-id",
		"client-secret",
		false,
	)

	authInfo, err := aapProvider.authClientCredentialsFlow()
	assert.NoError(t, err)
	assert.NotNil(t, authInfo.ExpiresIn)
	// Should be converted to 7200 seconds
	assert.Equal(t, int64(7200), *authInfo.ExpiresIn)
}
