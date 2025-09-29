//go:build linux

package issuer

import (
	"context"
	"errors"
	"os/user"
	"testing"
	"time"

	"github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/auth/common"
	"github.com/flightctl/flightctl/internal/config"
	"github.com/flightctl/flightctl/internal/config/ca"
	fccrypto "github.com/flightctl/flightctl/internal/crypto"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testUsername = "testuser"
	testPassword = "testpass"
)

// MockSSSDAuthenticator is a simple mock implementation of SSSDAuthenticator for testing
type MockSSSDAuthenticator struct {
	authenticateFunc  func(username, password string) error
	lookupUserFunc    func(username string) (*user.User, error)
	getUserGroupsFunc func(systemUser *user.User) ([]string, error)
}

func (m *MockSSSDAuthenticator) Authenticate(username, password string) error {
	if m.authenticateFunc != nil {
		return m.authenticateFunc(username, password)
	}
	return nil
}

func (m *MockSSSDAuthenticator) LookupUser(username string) (*user.User, error) {
	if m.lookupUserFunc != nil {
		return m.lookupUserFunc(username)
	}
	return nil, errors.New("not implemented")
}

func (m *MockSSSDAuthenticator) GetUserGroups(systemUser *user.User) ([]string, error) {
	if m.getUserGroupsFunc != nil {
		return m.getUserGroupsFunc(systemUser)
	}
	return nil, errors.New("not implemented")
}

// NewMockSSSDAuthenticator creates a mock SSSD authenticator with the specified behavior
func NewMockSSSDAuthenticator(authenticateFunc func(username, password string) error, mockUser *user.User, groups []string) *MockSSSDAuthenticator {
	return &MockSSSDAuthenticator{
		authenticateFunc: authenticateFunc,
		lookupUserFunc: func(username string) (*user.User, error) {
			return mockUser, nil
		},
		getUserGroupsFunc: func(systemUser *user.User) ([]string, error) {
			return groups, nil
		},
	}
}

// Helper function to create a test CA client
func createTestCAClient(t *testing.T) *fccrypto.CAClient {
	t.Helper()
	cfg := ca.NewDefault(t.TempDir())
	caClient, _, err := fccrypto.EnsureCA(cfg)
	require.NoError(t, err)
	return caClient
}

// Helper function to create an SSSD issuer with mock authenticator
func createTestSSSDProvider(t *testing.T, mockAuth *MockSSSDAuthenticator) *SSSDOIDCProvider {
	t.Helper()
	caClient := createTestCAClient(t)
	config := &config.LinuxIssuerAuth{}
	provider, err := NewSSSDOIDCProviderWithAuthenticator(caClient, config, mockAuth)
	require.NoError(t, err)
	return provider
}

func TestNewSSSDOIDCProvider(t *testing.T) {
	caClient := createTestCAClient(t)

	tests := []struct {
		name        string
		caClient    *fccrypto.CAClient
		config      *config.LinuxIssuerAuth
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid config",
			caClient:    caClient,
			config:      &config.LinuxIssuerAuth{},
			expectError: false,
		},
		{
			name:        "nil config",
			caClient:    caClient,
			config:      nil,
			expectError: false, // SSSD doesn't require specific config like PAM service
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewSSSDOIDCProvider(tt.caClient, tt.config)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
				assert.Nil(t, provider)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, provider)
				assert.NotNil(t, provider.jwtGenerator)
				assert.Equal(t, tt.config, provider.config)
			}
		})
	}
}

func TestSSSDOIDCProvider_Token(t *testing.T) {
	// Create a minimal provider for testing
	caClient := createTestCAClient(t)
	config := &config.LinuxIssuerAuth{}
	provider, err := NewSSSDOIDCProvider(caClient, config)
	require.NoError(t, err)

	tests := []struct {
		name        string
		request     *v1alpha1.TokenRequest
		expectError bool
		errorCode   string
	}{
		{
			name: "unsupported grant type",
			request: &v1alpha1.TokenRequest{
				GrantType: "unsupported",
			},
			expectError: true,
			errorCode:   "unsupported_grant_type",
		},
		{
			name: "password grant type - missing username",
			request: &v1alpha1.TokenRequest{
				GrantType: v1alpha1.Password,
				Password:  lo.ToPtr("testpass"),
			},
			expectError: true,
			errorCode:   "invalid_request",
		},
		{
			name: "password grant type - missing password",
			request: &v1alpha1.TokenRequest{
				GrantType: v1alpha1.Password,
				Username:  lo.ToPtr("testuser"),
			},
			expectError: true,
			errorCode:   "invalid_request",
		},
		{
			name: "password grant type - empty username",
			request: &v1alpha1.TokenRequest{
				GrantType: v1alpha1.Password,
				Username:  lo.ToPtr(""),
				Password:  lo.ToPtr("testpass"),
			},
			expectError: true,
			errorCode:   "invalid_request",
		},
		{
			name: "password grant type - empty password",
			request: &v1alpha1.TokenRequest{
				GrantType: v1alpha1.Password,
				Username:  lo.ToPtr("testuser"),
				Password:  lo.ToPtr(""),
			},
			expectError: true,
			errorCode:   "invalid_request",
		},
		{
			name: "refresh token grant type - missing refresh token",
			request: &v1alpha1.TokenRequest{
				GrantType: v1alpha1.RefreshToken,
			},
			expectError: true,
			errorCode:   "invalid_request",
		},
		{
			name: "refresh token grant type - empty refresh token",
			request: &v1alpha1.TokenRequest{
				GrantType:    v1alpha1.RefreshToken,
				RefreshToken: lo.ToPtr(""),
			},
			expectError: true,
			errorCode:   "invalid_request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := provider.Token(context.Background(), tt.request)

			if tt.expectError {
				require.NoError(t, err)
				assert.NotNil(t, response.Error)
				assert.Equal(t, tt.errorCode, *response.Error)
			} else {
				// For successful cases, we'd need to mock the dependencies
				// This is a basic structure test
				assert.NotNil(t, response)
			}
		})
	}
}

func TestSSSDOIDCProvider_GetOpenIDConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		config   *config.LinuxIssuerAuth
		baseURL  string
		expected map[string]interface{}
	}{
		{
			name:    "default configuration",
			config:  &config.LinuxIssuerAuth{},
			baseURL: "https://example.com",
			expected: map[string]interface{}{
				"issuer":                                "https://example.com",
				"authorization_endpoint":                "https://example.com/api/v1/auth/authorize",
				"token_endpoint":                        "https://example.com/api/v1/auth/token",
				"userinfo_endpoint":                     "https://example.com/api/v1/auth/userinfo",
				"jwks_uri":                              "https://example.com/api/v1/auth/jwks",
				"response_types_supported":              []string{"code", "token"},
				"grant_types_supported":                 []string{"password", "refresh_token"},
				"scopes_supported":                      []string{"openid", "profile", "email", "roles"},
				"claims_supported":                      []string{"sub", "preferred_username", "name", "email", "email_verified", "roles", "organizations"},
				"id_token_signing_alg_values_supported": []string{"RS256"},
				"token_endpoint_auth_methods_supported": []string{"client_secret_post"},
			},
		},
		{
			name: "custom configuration with issuer",
			config: &config.LinuxIssuerAuth{
				OIDCIssuerAuth: config.OIDCIssuerAuth{
					Issuer: "https://custom.example.com",
					Scopes: []string{"openid", "profile"},
				},
			},
			baseURL: "https://example.com",
			expected: map[string]interface{}{
				"issuer":                                "https://custom.example.com",
				"authorization_endpoint":                "https://custom.example.com/api/v1/auth/authorize",
				"token_endpoint":                        "https://custom.example.com/api/v1/auth/token",
				"userinfo_endpoint":                     "https://custom.example.com/api/v1/auth/userinfo",
				"jwks_uri":                              "https://custom.example.com/api/v1/auth/jwks",
				"response_types_supported":              []string{"code", "token"},
				"grant_types_supported":                 []string{"password", "refresh_token"},
				"scopes_supported":                      []string{"openid", "profile"},
				"claims_supported":                      []string{"sub", "preferred_username", "name", "email", "email_verified", "roles", "organizations"},
				"id_token_signing_alg_values_supported": []string{"RS256"},
				"token_endpoint_auth_methods_supported": []string{"client_secret_post"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &SSSDOIDCProvider{
				config: tt.config,
			}

			result := provider.GetOpenIDConfiguration(tt.baseURL)

			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSSSDOIDCProvider_GetJWKS(t *testing.T) {
	// Create a minimal provider for testing
	caClient := createTestCAClient(t)
	config := &config.LinuxIssuerAuth{}
	provider, err := NewSSSDOIDCProvider(caClient, config)
	require.NoError(t, err)

	// Test that GetJWKS calls the JWT generator
	result, err := provider.GetJWKS()

	// The actual result depends on the JWT generator implementation
	// We just verify it doesn't error and returns a map
	require.NoError(t, err)
	assert.NotNil(t, result)
	// Verify it has the expected structure for JWKS
	assert.Contains(t, result, "keys")
}

func TestSSSDOIDCProvider_UserInfo(t *testing.T) {
	// Create a minimal provider for testing
	caClient := createTestCAClient(t)
	config := &config.LinuxIssuerAuth{}
	provider, err := NewSSSDOIDCProvider(caClient, config)
	require.NoError(t, err)

	tests := []struct {
		name        string
		accessToken string
		expectError bool
		errorCode   string
	}{
		{
			name:        "invalid token",
			accessToken: "invalid-token",
			expectError: true,
			errorCode:   "invalid_token",
		},
		{
			name:        "empty token",
			accessToken: "",
			expectError: true,
			errorCode:   "invalid_token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := provider.UserInfo(context.Background(), tt.accessToken)

			if tt.expectError {
				require.Error(t, err)
				assert.NotNil(t, response.Error)
				assert.Equal(t, tt.errorCode, *response.Error)
			} else {
				assert.NotNil(t, response)
			}
		})
	}
}

func TestSSSDOIDCProvider_Integration(t *testing.T) {
	// This test demonstrates the full integration flow using real components
	caClient := createTestCAClient(t)
	config := &config.LinuxIssuerAuth{
		OIDCIssuerAuth: config.OIDCIssuerAuth{
			Issuer: "https://test.example.com",
			Scopes: []string{"openid", "profile", "email"},
		},
	}

	provider, err := NewSSSDOIDCProvider(caClient, config)
	require.NoError(t, err)
	assert.NotNil(t, provider)

	// Test OpenID Configuration
	configResult := provider.GetOpenIDConfiguration("https://base.example.com")
	assert.Equal(t, "https://test.example.com", configResult["issuer"])
	assert.Equal(t, []string{"openid", "profile", "email"}, configResult["scopes_supported"])

	// Test JWKS endpoint
	jwksResult, err := provider.GetJWKS()
	require.NoError(t, err)
	assert.Contains(t, jwksResult, "keys")
}

func TestSSSDOIDCProvider_InterfaceCompliance(t *testing.T) {
	// Test that SSSDOIDCProvider implements the OIDCIssuer interface
	caClient := createTestCAClient(t)
	config := &config.LinuxIssuerAuth{}
	provider, err := NewSSSDOIDCProvider(caClient, config)
	require.NoError(t, err)

	// This test ensures the provider implements all required interface methods
	var _ OIDCIssuer = provider

	// Test all interface methods exist and can be called
	ctx := context.Background()

	// Token method
	tokenReq := &v1alpha1.TokenRequest{
		GrantType: "unsupported",
	}
	tokenResp, err := provider.Token(ctx, tokenReq)
	require.NoError(t, err)
	assert.NotNil(t, tokenResp)
	assert.NotNil(t, tokenResp.Error)

	// UserInfo method
	userInfoResp, err := provider.UserInfo(ctx, "invalid-token")
	require.Error(t, err)
	assert.NotNil(t, userInfoResp.Error)

	// GetOpenIDConfiguration method
	oidcConfig := provider.GetOpenIDConfiguration("https://test.com")
	assert.NotNil(t, oidcConfig)
	assert.Contains(t, oidcConfig, "issuer")

	// GetJWKS method
	jwks, err := provider.GetJWKS()
	require.NoError(t, err)
	assert.NotNil(t, jwks)
}

func TestSSSDOIDCProvider_RealTokenFlow(t *testing.T) {
	// Test the actual SSSD issuer token flow with mocked SSSD authentication

	t.Run("invalid_credentials", func(t *testing.T) {
		// Mock SSSD authentication failure
		mockAuth := NewMockSSSDAuthenticator(
			func(username, password string) error {
				return errors.New("authentication failed")
			},
			nil, // No user needed for this test
			nil, // No groups needed for this test
		)
		provider := createTestSSSDProvider(t, mockAuth)

		tokenReq := &v1alpha1.TokenRequest{
			GrantType: v1alpha1.Password,
			Username:  lo.ToPtr("nonexistentuser"),
			Password:  lo.ToPtr("wrongpassword"),
		}

		response, err := provider.Token(context.Background(), tokenReq)
		require.NoError(t, err)
		assert.NotNil(t, response.Error)
		assert.Equal(t, "invalid_grant", *response.Error)
	})

	t.Run("invalid_refresh_token", func(t *testing.T) {
		// Test with invalid refresh token
		mockAuth := NewMockSSSDAuthenticator(nil, nil, nil)
		provider := createTestSSSDProvider(t, mockAuth)

		tokenReq := &v1alpha1.TokenRequest{
			GrantType:    v1alpha1.RefreshToken,
			RefreshToken: lo.ToPtr("invalid-refresh-token"),
		}

		response, err := provider.Token(context.Background(), tokenReq)
		require.NoError(t, err)
		assert.NotNil(t, response.Error)
		assert.Equal(t, "invalid_grant", *response.Error)
	})

	t.Run("successful_sssd_authentication", func(t *testing.T) {
		// Mock successful SSSD authentication with user and groups
		mockUser := &user.User{
			Uid:      "1000",
			Gid:      "1000",
			Username: testUsername,
			Name:     "Test User",
			HomeDir:  "/home/testuser",
		}
		mockAuth := NewMockSSSDAuthenticator(
			func(username, password string) error {
				return nil
			},
			mockUser,
			[]string{"users", "wheel"},
		)
		provider := createTestSSSDProvider(t, mockAuth)

		tokenReq := &v1alpha1.TokenRequest{
			GrantType: v1alpha1.Password,
			Username:  lo.ToPtr(testUsername),
			Password:  lo.ToPtr(testPassword),
		}

		response, err := provider.Token(context.Background(), tokenReq)
		require.NoError(t, err)
		assert.Nil(t, response.Error)

		// Verify successful token response
		require.NotNil(t, response.AccessToken)
		require.NotNil(t, response.RefreshToken)
		require.NotNil(t, response.TokenType)
		assert.Equal(t, v1alpha1.TokenResponseTokenType("Bearer"), *response.TokenType)

		// Verify the access token contains expected claims
		parsedToken, err := jwt.Parse([]byte(*response.AccessToken), jwt.WithValidate(false), jwt.WithVerify(false))
		require.NoError(t, err)

		// Check that the token contains the test user's information
		sub, exists := parsedToken.Get("sub")
		require.True(t, exists)
		assert.Equal(t, testUsername, sub)

		preferredUsername, exists := parsedToken.Get("preferred_username")
		require.True(t, exists)
		assert.Equal(t, testUsername, preferredUsername)

		// Test UserInfo with the generated token
		userInfoResp, err := provider.UserInfo(context.Background(), *response.AccessToken)
		require.NoError(t, err)
		assert.NotNil(t, userInfoResp.Sub)
		assert.Equal(t, testUsername, *userInfoResp.Sub)
	})
}

func TestSSSDOIDCProvider_UserInfoClaims(t *testing.T) {
	// Test that UserInfo returns proper claims structure
	mockUser := &user.User{
		Uid:      "1000",
		Gid:      "1000",
		Username: testUsername,
		Name:     "Test User",
		HomeDir:  "/home/testuser",
	}
	mockAuth := NewMockSSSDAuthenticator(
		func(username, password string) error {
			return nil
		},
		mockUser,
		[]string{"users", "wheel"},
	)
	provider := createTestSSSDProvider(t, mockAuth)

	// Get a token through the SSSD issuer's Token method (real flow)
	tokenReq := &v1alpha1.TokenRequest{
		GrantType: v1alpha1.Password,
		Username:  lo.ToPtr(testUsername),
		Password:  lo.ToPtr(testPassword),
	}

	response, err := provider.Token(context.Background(), tokenReq)
	require.NoError(t, err)
	assert.Nil(t, response.Error)
	require.NotNil(t, response.AccessToken)

	// Test UserInfo with the token generated by SSSD issuer
	userInfoResp, err := provider.UserInfo(context.Background(), *response.AccessToken)
	require.NoError(t, err)
	assert.NotNil(t, userInfoResp)

	// Verify the UserInfo response structure
	assert.NotNil(t, userInfoResp.Sub)
	assert.Equal(t, testUsername, *userInfoResp.Sub)

	assert.NotNil(t, userInfoResp.PreferredUsername)
	assert.Equal(t, testUsername, *userInfoResp.PreferredUsername)

	assert.NotNil(t, userInfoResp.Name)
	// The name should be set by the SSSD issuer from system user lookup
	// It might be the full name or username depending on system configuration

	assert.NotNil(t, userInfoResp.Email)
	// Email might be empty if not available from system user

	assert.NotNil(t, userInfoResp.EmailVerified)
	assert.False(t, *userInfoResp.EmailVerified) // Default to false

	assert.NotNil(t, userInfoResp.Roles)
	roles := *userInfoResp.Roles
	// The roles should come from the system user's groups
	assert.Contains(t, roles, "users")
	assert.Contains(t, roles, "wheel")

	assert.NotNil(t, userInfoResp.Organizations)
	// Organizations should be empty array by default
}

func TestSSSDOIDCProvider_EndToEndFlow(t *testing.T) {
	// Test the complete flow: Token -> UserInfo -> Claims verification
	mockUser := &user.User{
		Uid:      "1000",
		Gid:      "1000",
		Username: testUsername,
		Name:     "Test User",
		HomeDir:  "/home/testuser",
	}
	mockAuth := NewMockSSSDAuthenticator(
		func(username, password string) error {
			return nil
		},
		mockUser,
		[]string{"users", "wheel"},
	)
	provider := createTestSSSDProvider(t, mockAuth)

	// Test the complete authentication flow
	tokenReq := &v1alpha1.TokenRequest{
		GrantType: v1alpha1.Password,
		Username:  lo.ToPtr(testUsername),
		Password:  lo.ToPtr(testPassword),
	}

	response, err := provider.Token(context.Background(), tokenReq)
	require.NoError(t, err)
	assert.Nil(t, response.Error)

	// Verify successful token response
	require.NotNil(t, response.AccessToken)
	require.NotNil(t, response.RefreshToken)
	require.NotNil(t, response.TokenType)
	assert.Equal(t, v1alpha1.TokenResponseTokenType("Bearer"), *response.TokenType)

	// Test 1: Verify access token claims
	parsedToken, err := jwt.Parse([]byte(*response.AccessToken), jwt.WithValidate(false), jwt.WithVerify(false))
	require.NoError(t, err)

	// Verify all expected claims are present
	claims := map[string]interface{}{}
	parsedToken.Walk(context.Background(), jwt.VisitorFunc(func(key string, value interface{}) error {
		claims[key] = value
		return nil
	}))

	// Verify standard claims
	assert.Equal(t, testUsername, claims["sub"])
	assert.Equal(t, testUsername, claims["preferred_username"])
	assert.Equal(t, "access_token", claims["token_type"])
	assert.NotNil(t, claims["exp"])
	assert.NotNil(t, claims["iat"])
	assert.NotNil(t, claims["nbf"])

	// Verify custom claims
	rolesInterface, exists := claims["roles"]
	require.True(t, exists, "roles claim should exist")

	// Handle different possible types for roles
	var roles []string
	switch v := rolesInterface.(type) {
	case []string:
		roles = v
	case []interface{}:
		roles = make([]string, len(v))
		for i, role := range v {
			if roleStr, ok := role.(string); ok {
				roles[i] = roleStr
			}
		}
	default:
		t.Fatalf("Unexpected roles type: %T", v)
	}

	// Verify the specific roles we mocked
	assert.Contains(t, roles, "users")
	assert.Contains(t, roles, "wheel")

	// Test 2: Verify UserInfo response
	userInfoResp, err := provider.UserInfo(context.Background(), *response.AccessToken)
	require.NoError(t, err)
	assert.NotNil(t, userInfoResp.Sub)
	assert.Equal(t, testUsername, *userInfoResp.Sub)

	// Test 3: Verify refresh token structure
	parsedRefreshToken, err := jwt.Parse([]byte(*response.RefreshToken), jwt.WithValidate(false), jwt.WithVerify(false))
	require.NoError(t, err)

	refreshClaims := map[string]interface{}{}
	parsedRefreshToken.Walk(context.Background(), jwt.VisitorFunc(func(key string, value interface{}) error {
		refreshClaims[key] = value
		return nil
	}))

	assert.Equal(t, testUsername, refreshClaims["sub"])
	assert.Equal(t, "refresh_token", refreshClaims["token_type"])
}

func TestSSSDOIDCProvider_TokenValidation(t *testing.T) {
	// Test that generated tokens can be validated by the JWT generator
	caClient := createTestCAClient(t)
	config := &config.LinuxIssuerAuth{}
	provider, err := NewSSSDOIDCProvider(caClient, config)
	require.NoError(t, err)

	// Create a test identity
	identity := common.NewBaseIdentity("testuser", "testuser", []string{}, []string{"admin"})

	// Generate access token
	accessToken, err := provider.jwtGenerator.GenerateTokenWithType(identity, time.Hour, "access_token")
	require.NoError(t, err)

	// Generate refresh token
	refreshToken, err := provider.jwtGenerator.GenerateTokenWithType(identity, 7*24*time.Hour, "refresh_token")
	require.NoError(t, err)

	// Validate access token
	validatedIdentity, err := provider.jwtGenerator.ValidateTokenWithType(accessToken, "access_token")
	require.NoError(t, err)
	assert.Equal(t, "testuser", validatedIdentity.GetUsername())
	assert.Equal(t, "testuser", validatedIdentity.GetUID())

	// Validate refresh token
	validatedRefreshIdentity, err := provider.jwtGenerator.ValidateTokenWithType(refreshToken, "refresh_token")
	require.NoError(t, err)
	assert.Equal(t, "testuser", validatedRefreshIdentity.GetUsername())
	assert.Equal(t, "testuser", validatedRefreshIdentity.GetUID())

	// Test that wrong token type validation fails
	_, err = provider.jwtGenerator.ValidateTokenWithType(accessToken, "refresh_token")
	require.Error(t, err)

	_, err = provider.jwtGenerator.ValidateTokenWithType(refreshToken, "access_token")
	require.Error(t, err)

	// Test that invalid tokens fail validation
	_, err = provider.jwtGenerator.ValidateTokenWithType("invalid-token", "access_token")
	require.Error(t, err)
}
