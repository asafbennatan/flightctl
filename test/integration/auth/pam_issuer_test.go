//go:build linux

package auth_test

import (
	"context"

	pamapi "github.com/flightctl/flightctl/api/v1beta1/pam-issuer"
	"github.com/flightctl/flightctl/internal/auth/oidc/pam"
	"github.com/flightctl/flightctl/internal/config"
	"github.com/flightctl/flightctl/internal/config/ca"
	fccrypto "github.com/flightctl/flightctl/internal/crypto"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
)

var _ = Describe("PAM Issuer Integration Tests", func() {
	var (
		ctx      context.Context
		provider *pam.PAMOIDCProvider
		caClient *fccrypto.CAClient
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Create test CA client
		cfg := ca.NewDefault(GinkgoT().TempDir())
		var err error
		caClient, _, err = fccrypto.EnsureCA(cfg)
		Expect(err).ToNot(HaveOccurred())

		// Create PAM issuer with real components (no mocks for integration test)
		config := &config.PAMOIDCIssuer{
			Issuer:       "https://test.example.com",
			Scopes:       []string{"openid", "profile", "email"},
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			RedirectURIs: []string{"https://example.com/callback"},
			PAMService:   "other", // Use 'other' PAM service for authentication
		}

		provider, err = pam.NewPAMOIDCProvider(caClient, config)
		Expect(err).ToNot(HaveOccurred())
		Expect(provider).ToNot(BeNil())
	})

	AfterEach(func() {
		if provider != nil {
			provider.Close()
		}
	})

	Context("PAM Issuer Integration", func() {
		It("should provide OpenID Configuration", func() {
			config, err := provider.GetOpenIDConfiguration()
			Expect(err).ToNot(HaveOccurred())

			Expect(config).ToNot(BeNil())
			Expect(config.Issuer).ToNot(BeNil())
			Expect(*config.Issuer).To(Equal("https://test.example.com"))
			Expect(config.ScopesSupported).ToNot(BeNil())
			Expect(*config.ScopesSupported).To(Equal([]string{"openid", "profile", "email"}))
			Expect(config.ResponseTypesSupported).ToNot(BeNil())
			Expect(*config.ResponseTypesSupported).To(ContainElement("code"))
			Expect(config.GrantTypesSupported).ToNot(BeNil())
			Expect(*config.GrantTypesSupported).To(ContainElements("authorization_code", "refresh_token", "client_credentials"))
			// Verify PKCE support is advertised (only S256, not plain)
			Expect(config.CodeChallengeMethodsSupported).ToNot(BeNil())
			Expect(*config.CodeChallengeMethodsSupported).To(ContainElement(
				pamapi.OpenIDConfigurationCodeChallengeMethodsSupportedS256,
			))
			Expect(*config.CodeChallengeMethodsSupported).To(HaveLen(1))
		})

		It("should provide JWKS endpoint", func() {
			jwks, err := provider.GetJWKS()
			Expect(err).ToNot(HaveOccurred())
			Expect(jwks).ToNot(BeNil())
			Expect(jwks.Keys).ToNot(BeNil())
		})

		It("should handle authorization code flow with real PAM", func() {
			// This test would require actual PAM setup and real user authentication
			// For now, we'll test the interface compliance and basic functionality

			authParams := &pamapi.AuthAuthorizeParams{
				ClientId:     "test-client",
				RedirectUri:  "https://example.com/callback",
				ResponseType: pamapi.Code,
				State:        lo.ToPtr("test-state"),
			}

			// This will return a login form since no session is established
			authResp, err := provider.Authorize(ctx, authParams)
			Expect(err).ToNot(HaveOccurred())
			Expect(authResp).ToNot(BeNil())
			Expect(authResp.Type).To(Equal(pam.AuthorizeResponseTypeHTML))
			Expect(authResp.Content).To(ContainSubstring("<!DOCTYPE html>"))
			// The login form contains standard HTML elements
			Expect(authResp.Content).To(Or(ContainSubstring("login"), ContainSubstring("Login")))
		})

		It("should handle token validation", func() {
			// Test with invalid token
			_, err := provider.UserInfo(ctx, "invalid-token")
			Expect(err).To(HaveOccurred())
			oauth2Err, ok := pamapi.IsOAuth2Error(err)
			Expect(ok).To(BeTrue())
			Expect(oauth2Err.Code).To(Equal(pamapi.OAuth2ErrorError("invalid_token")))
		})

		It("should handle unsupported grant types", func() {
			tokenReq := &pamapi.TokenRequest{
				GrantType: "unsupported_grant_type",
			}

			_, err := provider.Token(ctx, tokenReq)
			Expect(err).To(HaveOccurred())
			oauth2Err, ok := pamapi.IsOAuth2Error(err)
			Expect(ok).To(BeTrue())
			Expect(oauth2Err.Code).To(Equal(pamapi.UnsupportedGrantType))
		})

		It("should handle missing required fields in token request", func() {
			// Test missing code for authorization_code grant
			tokenReq := &pamapi.TokenRequest{
				GrantType: pamapi.AuthorizationCode,
				ClientId:  lo.ToPtr("test-client"),
			}

			_, err := provider.Token(ctx, tokenReq)
			Expect(err).To(HaveOccurred())
			oauth2Err, ok := pamapi.IsOAuth2Error(err)
			Expect(ok).To(BeTrue())
			Expect(oauth2Err.Code).To(Equal(pamapi.InvalidRequest))
		})

		It("should handle invalid client credentials", func() {
			tokenReq := &pamapi.TokenRequest{
				GrantType: pamapi.AuthorizationCode,
				Code:      lo.ToPtr("valid-code"),
				ClientId:  lo.ToPtr("wrong-client"),
			}

			_, err := provider.Token(ctx, tokenReq)
			Expect(err).To(HaveOccurred())
			oauth2Err, ok := pamapi.IsOAuth2Error(err)
			Expect(ok).To(BeTrue())
			Expect(oauth2Err.Code).To(Equal(pamapi.InvalidClient))
		})

		It("should implement OIDCIssuer interface", func() {
			// Test all interface methods exist and can be called
			// Token method
			tokenReq := &pamapi.TokenRequest{
				GrantType: "unsupported",
			}
			_, err := provider.Token(ctx, tokenReq)
			Expect(err).To(HaveOccurred())
			oauth2Err, ok := pamapi.IsOAuth2Error(err)
			Expect(ok).To(BeTrue())
			Expect(oauth2Err).ToNot(BeNil())

			// UserInfo method
			_, err = provider.UserInfo(ctx, "invalid-token")
			Expect(err).To(HaveOccurred())
			oauth2Err, ok = pamapi.IsOAuth2Error(err)
			Expect(ok).To(BeTrue())
			Expect(oauth2Err).ToNot(BeNil())

			// GetOpenIDConfiguration method
			oidcConfig, err := provider.GetOpenIDConfiguration()
			Expect(err).ToNot(HaveOccurred())
			Expect(oidcConfig).ToNot(BeNil())
			Expect(oidcConfig.Issuer).ToNot(BeNil())

			// GetJWKS method
			jwks, err := provider.GetJWKS()
			Expect(err).ToNot(HaveOccurred())
			Expect(jwks).ToNot(BeNil())
		})
	})

	Context("Client Credentials Flow Integration", func() {
		It("should issue access token with valid client credentials", func() {
			tokenReq := &pamapi.TokenRequest{
				GrantType:    pamapi.ClientCredentials,
				ClientId:     lo.ToPtr("test-client"),
				ClientSecret: lo.ToPtr("test-secret"),
				Scope:        lo.ToPtr("profile email"),
			}

			response, err := provider.Token(ctx, tokenReq)
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			Expect(response.AccessToken).ToNot(BeEmpty())
			Expect(response.TokenType).To(Equal(pamapi.Bearer))
			Expect(response.ExpiresIn).ToNot(BeNil())
			Expect(*response.ExpiresIn).To(Equal(3600))
			Expect(response.RefreshToken).To(BeNil())
			Expect(response.IdToken).To(BeNil())
		})

		It("should reject client credentials with wrong secret", func() {
			tokenReq := &pamapi.TokenRequest{
				GrantType:    pamapi.ClientCredentials,
				ClientId:     lo.ToPtr("test-client"),
				ClientSecret: lo.ToPtr("wrong-secret"),
			}

			_, err := provider.Token(ctx, tokenReq)
			Expect(err).To(HaveOccurred())
			oauth2Err, ok := pamapi.IsOAuth2Error(err)
			Expect(ok).To(BeTrue())
			Expect(oauth2Err.Code).To(Equal(pamapi.InvalidClient))
		})

		It("should reject client credentials with wrong client_id", func() {
			tokenReq := &pamapi.TokenRequest{
				GrantType:    pamapi.ClientCredentials,
				ClientId:     lo.ToPtr("wrong-client"),
				ClientSecret: lo.ToPtr("test-secret"),
			}

			_, err := provider.Token(ctx, tokenReq)
			Expect(err).To(HaveOccurred())
			oauth2Err, ok := pamapi.IsOAuth2Error(err)
			Expect(ok).To(BeTrue())
			Expect(oauth2Err.Code).To(Equal(pamapi.InvalidClient))
		})

		It("should generate valid JWT token for client credentials", func() {
			tokenReq := &pamapi.TokenRequest{
				GrantType:    pamapi.ClientCredentials,
				ClientId:     lo.ToPtr("test-client"),
				ClientSecret: lo.ToPtr("test-secret"),
			}

			response, err := provider.Token(ctx, tokenReq)
			Expect(err).ToNot(HaveOccurred())

			// Verify token can be used with UserInfo endpoint
			userInfo, err := provider.UserInfo(ctx, response.AccessToken)
			Expect(err).ToNot(HaveOccurred())
			Expect(userInfo).ToNot(BeNil())
			Expect(userInfo.Sub).To(Equal("test-client"))
			Expect(*userInfo.PreferredUsername).To(Equal("test-client"))
			Expect(userInfo.Roles).ToNot(BeNil())
			Expect(*userInfo.Roles).To(BeEmpty())
		})
	})

	Context("Client Credentials Flow - Public Client", func() {
		var publicProvider *pam.PAMOIDCProvider

		BeforeEach(func() {
			// Create PAM issuer WITHOUT ClientSecret (public client)
			config := &config.PAMOIDCIssuer{
				Issuer:       "https://test.example.com",
				Scopes:       []string{"openid", "profile", "email"},
				ClientID:     "public-client",
				ClientSecret: "", // No secret = public client
				RedirectURIs: []string{"https://example.com/callback"},
				PAMService:   "other",
			}

			var err error
			publicProvider, err = pam.NewPAMOIDCProvider(caClient, config)
			Expect(err).ToNot(HaveOccurred())
			Expect(publicProvider).ToNot(BeNil())
		})

		AfterEach(func() {
			if publicProvider != nil {
				publicProvider.Close()
			}
		})

		It("should not advertise client_credentials in OpenID configuration", func() {
			config, err := publicProvider.GetOpenIDConfiguration()
			Expect(err).ToNot(HaveOccurred())
			Expect(config).ToNot(BeNil())
			Expect(config.GrantTypesSupported).ToNot(BeNil())
			Expect(*config.GrantTypesSupported).To(ContainElements("authorization_code", "refresh_token"))
			Expect(*config.GrantTypesSupported).ToNot(ContainElement("client_credentials"))
		})

		It("should reject client_credentials flow for public client", func() {
			tokenReq := &pamapi.TokenRequest{
				GrantType:    pamapi.ClientCredentials,
				ClientId:     lo.ToPtr("public-client"),
				ClientSecret: lo.ToPtr("any-secret"),
			}

			_, err := publicProvider.Token(ctx, tokenReq)
			Expect(err).To(HaveOccurred())
			oauth2Err, ok := pamapi.IsOAuth2Error(err)
			Expect(ok).To(BeTrue())
			Expect(oauth2Err.Code).To(Equal(pamapi.UnsupportedGrantType))
		})
	})

})
