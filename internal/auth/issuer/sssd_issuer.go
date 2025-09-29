//go:build linux

package issuer

import (
	"context"
	"fmt"
	"time"

	"github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/auth/authn"
	"github.com/flightctl/flightctl/internal/auth/common"
	"github.com/flightctl/flightctl/internal/config"
	fccrypto "github.com/flightctl/flightctl/internal/crypto"
	"github.com/samber/lo"
)

// SSSDOIDCProvider handles OIDC-compliant authentication flows using SSSD

// NewSSSDOIDCProvider creates a new SSSD-based OIDC provider
func NewSSSDOIDCProvider(caClient *fccrypto.CAClient, config *config.LinuxIssuerAuth) (*SSSDOIDCProvider, error) {
	return NewSSSDOIDCProviderWithAuthenticator(caClient, config, nil)
}

// NewSSSDOIDCProviderWithAuthenticator creates a new SSSD-based OIDC provider with a custom authenticator
func NewSSSDOIDCProviderWithAuthenticator(caClient *fccrypto.CAClient, config *config.LinuxIssuerAuth, sssdAuth SSSDAuthenticator) (*SSSDOIDCProvider, error) {
	jwtGen, err := authn.NewJWTGenerator(caClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create JWT generator: %w", err)
	}

	// Create default authenticator if none provided
	if sssdAuth == nil {
		sssdAuth, err = NewRealSSSDAuthenticator()
		if err != nil {
			return nil, fmt.Errorf("failed to create SSSD authenticator: %w", err)
		}
	}

	return &SSSDOIDCProvider{
		jwtGenerator:      jwtGen,
		config:            config,
		sssdAuthenticator: sssdAuth,
	}, nil
}

// Token implements OIDCProvider interface - handles OAuth2 token requests
func (s *SSSDOIDCProvider) Token(ctx context.Context, req *v1alpha1.TokenRequest) (*v1alpha1.TokenResponse, error) {
	// Handle different grant types
	switch req.GrantType {
	case v1alpha1.Password:
		return s.handlePasswordGrant(ctx, req)
	case v1alpha1.RefreshToken:
		return s.handleRefreshTokenGrant(ctx, req)
	default:
		return &v1alpha1.TokenResponse{Error: lo.ToPtr("unsupported_grant_type")}, nil
	}
}

// handlePasswordGrant handles the password grant type
func (s *SSSDOIDCProvider) handlePasswordGrant(ctx context.Context, req *v1alpha1.TokenRequest) (*v1alpha1.TokenResponse, error) {
	// Validate required fields for password flow
	if req.Username == nil || req.Password == nil || *req.Username == "" || *req.Password == "" {
		return &v1alpha1.TokenResponse{Error: lo.ToPtr("invalid_request")}, nil
	}

	// Authenticate using SSSD
	if err := s.authenticateWithSSSD(*req.Username, *req.Password); err != nil {
		return &v1alpha1.TokenResponse{Error: lo.ToPtr("invalid_grant")}, nil
	}

	// Get user information from SSSD
	systemUser, err := s.sssdAuthenticator.LookupUser(*req.Username)
	if err != nil {
		return &v1alpha1.TokenResponse{Error: lo.ToPtr("invalid_grant")}, nil
	}

	// Get user groups for roles
	groups, err := s.sssdAuthenticator.GetUserGroups(systemUser)
	if err != nil {
		return &v1alpha1.TokenResponse{Error: lo.ToPtr("server_error")}, nil
	}

	// Create identity for token generation
	identity := common.NewBaseIdentity(*req.Username, *req.Username, []string{}, groups)

	// Generate access token with proper expiry (1 hour)
	accessToken, err := s.jwtGenerator.GenerateTokenWithType(identity, time.Hour, "access_token")
	if err != nil {
		return &v1alpha1.TokenResponse{Error: lo.ToPtr("server_error")}, nil
	}

	// Generate refresh token with longer expiry (7 days)
	refreshToken, err := s.jwtGenerator.GenerateTokenWithType(identity, 7*24*time.Hour, "refresh_token")
	if err != nil {
		return &v1alpha1.TokenResponse{Error: lo.ToPtr("server_error")}, nil
	}

	// Create token response using generated types
	tokenResponse := &v1alpha1.TokenResponse{
		AccessToken:  lo.ToPtr(accessToken),
		TokenType:    lo.ToPtr(v1alpha1.Bearer),
		RefreshToken: lo.ToPtr(refreshToken),
		ExpiresIn:    lo.ToPtr(int(time.Hour.Seconds())),
	}

	return tokenResponse, nil
}

// handleRefreshTokenGrant handles the refresh_token grant type
func (s *SSSDOIDCProvider) handleRefreshTokenGrant(ctx context.Context, req *v1alpha1.TokenRequest) (*v1alpha1.TokenResponse, error) {
	// Validate required fields for refresh token flow
	if req.RefreshToken == nil || *req.RefreshToken == "" {
		return &v1alpha1.TokenResponse{Error: lo.ToPtr("invalid_request")}, nil
	}

	// Validate the refresh token and ensure it's actually a refresh token
	identity, err := s.jwtGenerator.ValidateTokenWithType(*req.RefreshToken, "refresh_token")
	if err != nil {
		return &v1alpha1.TokenResponse{Error: lo.ToPtr("invalid_grant")}, nil
	}

	// Get current user information from SSSD to ensure user still exists
	systemUser, err := s.sssdAuthenticator.LookupUser(identity.GetUsername())
	if err != nil {
		return &v1alpha1.TokenResponse{Error: lo.ToPtr("invalid_grant")}, nil
	}

	// Get current user groups for roles
	groups, err := s.sssdAuthenticator.GetUserGroups(systemUser)
	if err != nil {
		return &v1alpha1.TokenResponse{Error: lo.ToPtr("server_error")}, nil
	}

	// Create updated identity for token generation
	updatedIdentity := common.NewBaseIdentity(identity.GetUsername(), systemUser.Name, []string{}, groups)

	// Generate new access token with proper expiry (1 hour)
	accessToken, err := s.jwtGenerator.GenerateTokenWithType(updatedIdentity, time.Hour, "access_token")
	if err != nil {
		return &v1alpha1.TokenResponse{Error: lo.ToPtr("server_error")}, nil
	}

	// Generate new refresh token with longer expiry (7 days)
	refreshToken, err := s.jwtGenerator.GenerateTokenWithType(updatedIdentity, 7*24*time.Hour, "refresh_token")
	if err != nil {
		return &v1alpha1.TokenResponse{Error: lo.ToPtr("server_error")}, nil
	}

	// Create token response using generated types
	tokenResponse := &v1alpha1.TokenResponse{
		AccessToken:  lo.ToPtr(accessToken),
		TokenType:    lo.ToPtr(v1alpha1.Bearer),
		RefreshToken: lo.ToPtr(refreshToken),
		ExpiresIn:    lo.ToPtr(int(time.Hour.Seconds())),
	}

	return tokenResponse, nil
}

// authenticateWithSSSD authenticates a user using SSSD
func (s *SSSDOIDCProvider) authenticateWithSSSD(username, password string) error {
	return s.sssdAuthenticator.Authenticate(username, password)
}

// UserInfo implements OIDCProvider interface - returns user information
func (s *SSSDOIDCProvider) UserInfo(ctx context.Context, accessToken string) (*v1alpha1.UserInfoResponse, error) {
	// Validate the access token and ensure it's actually an access token
	identity, err := s.jwtGenerator.ValidateTokenWithType(accessToken, "access_token")
	if err != nil {
		return &v1alpha1.UserInfoResponse{Error: lo.ToPtr("invalid_token")}, fmt.Errorf("invalid access token: %w", err)
	}

	// Get user information from SSSD
	systemUser, err := s.sssdAuthenticator.LookupUser(identity.GetUsername())
	if err != nil {
		return &v1alpha1.UserInfoResponse{Error: lo.ToPtr("invalid_token")}, fmt.Errorf("user not found: %w", err)
	}

	// Get user groups for roles
	groups, err := s.sssdAuthenticator.GetUserGroups(systemUser)
	if err != nil {
		return &v1alpha1.UserInfoResponse{Error: lo.ToPtr("server_error")}, fmt.Errorf("failed to get user groups: %w", err)
	}

	// Create user info response
	userInfo := &v1alpha1.UserInfoResponse{
		Sub:               lo.ToPtr(identity.GetUsername()),
		PreferredUsername: lo.ToPtr(identity.GetUsername()),
		Name:              lo.ToPtr(systemUser.Name),
		Email:             lo.ToPtr(""), // Email not available from system user
		EmailVerified:     lo.ToPtr(false),
		Roles:             lo.ToPtr(groups),
		Organizations:     lo.ToPtr([]string{}), // Organizations not implemented yet
	}

	return userInfo, nil
}

// GetOpenIDConfiguration returns the OpenID Connect configuration
func (s *SSSDOIDCProvider) GetOpenIDConfiguration(baseURL string) map[string]interface{} {
	// Use config values if available, otherwise use defaults
	issuer := baseURL
	if s.config != nil && s.config.Issuer != "" {
		issuer = s.config.Issuer
	}

	responseTypes := []string{"code", "token"}
	if s.config != nil && len(s.config.ResponseTypes) > 0 {
		responseTypes = s.config.ResponseTypes
	}

	grantTypes := []string{"password", "refresh_token"}
	if s.config != nil && len(s.config.GrantTypes) > 0 {
		grantTypes = s.config.GrantTypes
	}

	scopes := []string{"openid", "profile", "email", "roles"}
	if s.config != nil && len(s.config.Scopes) > 0 {
		scopes = s.config.Scopes
	}

	return map[string]interface{}{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + "/api/v1/auth/authorize",
		"token_endpoint":                        issuer + "/api/v1/auth/token",
		"userinfo_endpoint":                     issuer + "/api/v1/auth/userinfo",
		"jwks_uri":                              issuer + "/api/v1/auth/jwks",
		"response_types_supported":              responseTypes,
		"grant_types_supported":                 grantTypes,
		"scopes_supported":                      scopes,
		"claims_supported":                      []string{"sub", "preferred_username", "name", "email", "email_verified", "roles", "organizations"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_post"},
	}
}

// GetJWKS returns the JSON Web Key Set
func (s *SSSDOIDCProvider) GetJWKS() (map[string]interface{}, error) {
	// Use the JWT generator's GetJWKS method
	return s.jwtGenerator.GetJWKS()
}
