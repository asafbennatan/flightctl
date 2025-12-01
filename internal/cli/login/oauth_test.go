package login

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOAuth2ClientCredentialsFlow(t *testing.T) {
	tests := []struct {
		name           string
		clientID       string
		clientSecret   string
		scope          string
		responseStatus int
		responseBody   map[string]interface{}
		expectError    bool
		errorContains  string
		validateResult func(*testing.T, AuthInfo)
	}{
		{
			name:           "successful client credentials flow",
			clientID:       "test-client-id",
			clientSecret:   "test-client-secret",
			scope:          "read write",
			responseStatus: http.StatusOK,
			responseBody: map[string]interface{}{
				"access_token":  "test-access-token",
				"token_type":    "Bearer",
				"expires_in":    3600,
				"refresh_token": "test-refresh-token",
			},
			expectError: false,
			validateResult: func(t *testing.T, authInfo AuthInfo) {
				assert.Equal(t, "test-access-token", authInfo.AccessToken)
				assert.Equal(t, "test-refresh-token", authInfo.RefreshToken)
				assert.NotNil(t, authInfo.ExpiresIn)
				assert.Equal(t, int64(3600), *authInfo.ExpiresIn)
			},
		},
		{
			name:           "successful flow without refresh token",
			clientID:       "test-client-id",
			clientSecret:   "test-client-secret",
			scope:          "read",
			responseStatus: http.StatusOK,
			responseBody: map[string]interface{}{
				"access_token": "test-access-token",
				"token_type":   "Bearer",
				"expires_in":   7200,
			},
			expectError: false,
			validateResult: func(t *testing.T, authInfo AuthInfo) {
				assert.Equal(t, "test-access-token", authInfo.AccessToken)
				assert.Empty(t, authInfo.RefreshToken)
				assert.NotNil(t, authInfo.ExpiresIn)
				assert.Equal(t, int64(7200), *authInfo.ExpiresIn)
			},
		},
		{
			name:           "successful flow with id_token",
			clientID:       "test-client-id",
			clientSecret:   "test-client-secret",
			scope:          "openid",
			responseStatus: http.StatusOK,
			responseBody: map[string]interface{}{
				"access_token": "test-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"id_token":     "test-id-token",
			},
			expectError: false,
			validateResult: func(t *testing.T, authInfo AuthInfo) {
				assert.Equal(t, "test-access-token", authInfo.AccessToken)
				assert.Equal(t, "test-id-token", authInfo.IdToken)
			},
		},
		{
			name:           "empty scope",
			clientID:       "test-client-id",
			clientSecret:   "test-client-secret",
			scope:          "",
			responseStatus: http.StatusOK,
			responseBody: map[string]interface{}{
				"access_token": "test-access-token",
				"token_type":   "Bearer",
			},
			expectError: false,
			validateResult: func(t *testing.T, authInfo AuthInfo) {
				assert.Equal(t, "test-access-token", authInfo.AccessToken)
			},
		},
		{
			name:           "invalid client credentials",
			clientID:       "invalid-client-id",
			clientSecret:   "invalid-secret",
			scope:          "read",
			responseStatus: http.StatusUnauthorized,
			responseBody: map[string]interface{}{
				"error":             "invalid_client",
				"error_description": "Client authentication failed",
			},
			expectError:   true,
			errorContains: "token request failed with status 401",
		},
		{
			name:           "server error",
			clientID:       "test-client-id",
			clientSecret:   "test-client-secret",
			scope:          "read",
			responseStatus: http.StatusInternalServerError,
			responseBody: map[string]interface{}{
				"error": "server_error",
			},
			expectError:   true,
			errorContains: "token request failed with status 500",
		},
		{
			name:           "missing access token in response",
			clientID:       "test-client-id",
			clientSecret:   "test-client-secret",
			scope:          "read",
			responseStatus: http.StatusOK,
			responseBody: map[string]interface{}{
				"token_type": "Bearer",
			},
			expectError:   true,
			errorContains: "no access token in response",
		},
		{
			name:           "expires_in as string",
			clientID:       "test-client-id",
			clientSecret:   "test-client-secret",
			scope:          "read",
			responseStatus: http.StatusOK,
			responseBody: map[string]interface{}{
				"access_token": "test-access-token",
				"expires_in":   "3600",
			},
			expectError: false,
			validateResult: func(t *testing.T, authInfo AuthInfo) {
				assert.Equal(t, "test-access-token", authInfo.AccessToken)
				assert.NotNil(t, authInfo.ExpiresIn)
				assert.Equal(t, int64(3600), *authInfo.ExpiresIn)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request method
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))

				// Parse form data
				err := r.ParseForm()
				require.NoError(t, err)

				// Verify grant type
				assert.Equal(t, "client_credentials", r.FormValue("grant_type"))
				assert.Equal(t, tt.clientID, r.FormValue("client_id"))
				assert.Equal(t, tt.clientSecret, r.FormValue("client_secret"))

				if tt.scope != "" {
					assert.Equal(t, tt.scope, r.FormValue("scope"))
				}

				// Send response
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.responseStatus)
				_ = json.NewEncoder(w).Encode(tt.responseBody)
			}))
			defer server.Close()

			// Call the function
			authInfo, err := oauth2ClientCredentialsFlow(
				server.URL,
				tt.clientID,
				tt.clientSecret,
				tt.scope,
				"",    // caFile
				false, // insecure
			)

			// Verify results
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

func TestOAuth2PasswordFlow(t *testing.T) {
	tests := []struct {
		name           string
		username       string
		password       string
		clientID       string
		scope          string
		responseStatus int
		responseBody   map[string]interface{}
		expectError    bool
		errorContains  string
	}{
		{
			name:           "successful password flow",
			username:       "testuser",
			password:       "testpass",
			clientID:       "test-client-id",
			scope:          "read write",
			responseStatus: http.StatusOK,
			responseBody: map[string]interface{}{
				"access_token": "test-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			},
			expectError: false,
		},
		{
			name:           "invalid credentials",
			username:       "baduser",
			password:       "badpass",
			clientID:       "test-client-id",
			scope:          "read",
			responseStatus: http.StatusUnauthorized,
			responseBody: map[string]interface{}{
				"error":             "invalid_grant",
				"error_description": "Invalid username or password",
			},
			expectError:   true,
			errorContains: "token request failed with status 401",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))

				err := r.ParseForm()
				require.NoError(t, err)

				assert.Equal(t, "password", r.FormValue("grant_type"))
				assert.Equal(t, tt.username, r.FormValue("username"))
				assert.Equal(t, tt.password, r.FormValue("password"))
				assert.Equal(t, tt.clientID, r.FormValue("client_id"))

				if tt.scope != "" {
					assert.Equal(t, tt.scope, r.FormValue("scope"))
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.responseStatus)
				_ = json.NewEncoder(w).Encode(tt.responseBody)
			}))
			defer server.Close()

			authInfo, err := oauth2PasswordFlow(
				server.URL,
				tt.clientID,
				tt.username,
				tt.password,
				tt.scope,
				"",
				false,
			)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, "test-access-token", authInfo.AccessToken)
			}
		})
	}
}

func TestGetExpiresIn(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected *int64
		wantErr  bool
	}{
		{
			name:     "expires_in as float64",
			data:     map[string]interface{}{"expires_in": float64(3600)},
			expected: func() *int64 { v := int64(3600); return &v }(),
			wantErr:  false,
		},
		{
			name:     "expires_in as string",
			data:     map[string]interface{}{"expires_in": "7200"},
			expected: func() *int64 { v := int64(7200); return &v }(),
			wantErr:  false,
		},
		{
			name:     "expires_in missing",
			data:     map[string]interface{}{},
			expected: nil,
			wantErr:  false,
		},
		{
			name:     "expires_in as invalid string",
			data:     map[string]interface{}{"expires_in": "invalid"},
			expected: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := getExpiresIn(tt.data)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.expected == nil {
					assert.Nil(t, result)
				} else {
					require.NotNil(t, result)
					assert.Equal(t, *tt.expected, *result)
				}
			}
		})
	}
}

func TestGetIdToken(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		expected string
	}{
		{
			name:     "id_token as string",
			data:     map[string]interface{}{"id_token": "test-id-token"},
			expected: "test-id-token",
		},
		{
			name:     "id_token as bytes",
			data:     map[string]interface{}{"id_token": []byte("test-id-token")},
			expected: "test-id-token",
		},
		{
			name:     "id_token missing",
			data:     map[string]interface{}{},
			expected: "",
		},
		{
			name:     "id_token as nil",
			data:     map[string]interface{}{"id_token": nil},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getIdToken(tt.data)
			assert.Equal(t, tt.expected, result)
		})
	}
}
