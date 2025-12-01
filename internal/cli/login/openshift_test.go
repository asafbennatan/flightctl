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

func TestOpenShift_AuthClientCredentialsFlow(t *testing.T) {
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
				"access_token": "openshift-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			},
			expectError: false,
			validateResult: func(t *testing.T, authInfo AuthInfo) {
				assert.Equal(t, "openshift-access-token", authInfo.AccessToken)
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
				assert.Equal(t, "user:full", r.FormValue("scope"))

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.responseStatus)
				_ = json.NewEncoder(w).Encode(tt.responseBody)
			}))
			defer tokenServer.Close()

			// Create OpenShift provider
			spec := api.OpenShiftProviderSpec{
				ProviderType:     api.Openshift,
				AuthorizationUrl: lo.ToPtr("https://openshift.example.com/authorize"),
				TokenUrl:         lo.ToPtr(tokenServer.URL),
				ClientId:         lo.ToPtr("spec-client-id"),
			}

			openshiftProvider := NewOpenShiftConfig(
				api.ObjectMeta{Name: lo.ToPtr("test-openshift")},
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
			authInfo, err := openshiftProvider.Auth()

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

func TestOpenShift_Validate_ClientCredentials(t *testing.T) {
	tests := []struct {
		name         string
		clientID     string
		clientSecret string
		tokenURL     *string
		expectError  bool
		errorMsg     string
	}{
		{
			name:         "valid client credentials",
			clientID:     "test-client-id",
			clientSecret: "test-client-secret",
			tokenURL:     lo.ToPtr("https://openshift.example.com/token"),
			expectError:  false,
		},
		{
			name:         "missing token URL",
			clientID:     "test-client-id",
			clientSecret: "test-client-secret",
			tokenURL:     nil,
			expectError:  true,
			errorMsg:     "token URL is required for client credentials flow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := api.OpenShiftProviderSpec{
				ProviderType:     api.Openshift,
				AuthorizationUrl: lo.ToPtr("https://openshift.example.com/authorize"),
				TokenUrl:         tt.tokenURL,
				ClientId:         lo.ToPtr("spec-client-id"),
			}

			openshiftProvider := NewOpenShiftConfig(
				api.ObjectMeta{Name: lo.ToPtr("test-openshift")},
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

			err := openshiftProvider.Validate(ValidateArgs{
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

func TestOpenShift_AuthFlowPriority(t *testing.T) {
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
			tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseForm()
				assert.Equal(t, tt.expectedFlow, r.FormValue("grant_type"))
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"access_token": "token",
				})
			}))
			defer tokenServer.Close()

			spec := api.OpenShiftProviderSpec{
				ProviderType:     api.Openshift,
				AuthorizationUrl: lo.ToPtr("https://openshift.example.com/authorize"),
				TokenUrl:         lo.ToPtr(tokenServer.URL),
				ClientId:         lo.ToPtr("spec-client-id"),
			}

			openshiftProvider := NewOpenShiftConfig(
				api.ObjectMeta{Name: lo.ToPtr("test-openshift")},
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

			authInfo, err := openshiftProvider.Auth()
			assert.NoError(t, err)
			assert.NotEmpty(t, authInfo.AccessToken)
		})
	}
}
