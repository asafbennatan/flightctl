package auth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/flightctl/flightctl/internal/auth/authn"
	"github.com/flightctl/flightctl/internal/auth/authz"
	"github.com/flightctl/flightctl/internal/auth/common"
	"github.com/flightctl/flightctl/internal/config"
	"github.com/flightctl/flightctl/internal/consts"
	"github.com/flightctl/flightctl/internal/org/resolvers"
	"github.com/flightctl/flightctl/pkg/k8sclient"
	"github.com/sirupsen/logrus"
)

const (
	// DisableAuthEnvKey is the environment variable key used to disable auth when developing.
	DisableAuthEnvKey = "FLIGHTCTL_DISABLE_AUTH"
	k8sCACertPath     = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	k8sApiService     = "https://kubernetes.default.svc"
)

type AuthZMiddleware interface {
	CheckPermission(ctx context.Context, resource string, op string) (bool, error)
}

type AuthType string

const (
	AuthTypeNil  AuthType = "nil"
	AuthTypeK8s  AuthType = "k8s"
	AuthTypeOIDC AuthType = "oidc"
	AuthTypeAAP  AuthType = "aap"
)

func GetConfiguredAuthType() AuthType {
	return configuredAuthType
}

var configuredAuthType AuthType

func initK8sAuth(cfg *config.Config, log logrus.FieldLogger) (common.AuthNMiddleware, AuthZMiddleware, error) {
	apiUrl := strings.TrimSuffix(cfg.Auth.K8s.ApiUrl, "/")
	externalOpenShiftApiUrl := strings.TrimSuffix(cfg.Auth.K8s.ExternalOpenShiftApiUrl, "/")
	log.Infof("k8s auth enabled: %s", apiUrl)
	var k8sClient k8sclient.K8SClient
	var err error
	if apiUrl == k8sApiService {
		k8sClient, err = k8sclient.NewK8SClient()
	} else {
		k8sClient, err = k8sclient.NewK8SExternalClient(apiUrl, cfg.Auth.InsecureSkipTlsVerify, cfg.Auth.CACert)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create k8s client: %w", err)
	}
	authZProvider := K8sToK8sAuth{K8sAuthZ: authz.K8sAuthZ{K8sClient: k8sClient, Namespace: cfg.Auth.K8s.RBACNs}}
	authNProvider, err := authn.NewK8sAuthN(k8sClient, externalOpenShiftApiUrl)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create k8s AuthN: %w", err)
	}
	return authNProvider, authZProvider, nil
}

func getTlsConfig(cfg *config.Config) *tls.Config {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: cfg.Auth.InsecureSkipTlsVerify, //nolint:gosec
	}
	if cfg.Auth.CACert != "" {
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM([]byte(cfg.Auth.CACert))
		tlsConfig.RootCAs = caCertPool
	}
	return tlsConfig
}

func getOrgConfig(cfg *config.Config) *common.AuthOrganizationsConfig {
	if cfg.Organizations == nil {
		return &common.AuthOrganizationsConfig{
			Enabled: false,
		}
	}
	return &common.AuthOrganizationsConfig{
		Enabled: cfg.Organizations.Enabled,
	}
}

func initOIDCAuth(cfg *config.Config, log logrus.FieldLogger, orgResolver resolvers.Resolver) (common.AuthNMiddleware, AuthZMiddleware, error) {
	oidcUrl := strings.TrimSuffix(cfg.Auth.OIDC.OIDCAuthority, "/")
	externalOidcUrl := strings.TrimSuffix(cfg.Auth.OIDC.ExternalOIDCAuthority, "/")
	log.Infof("OIDC auth enabled: %s", oidcUrl)
	authZProvider := authz.NewOrgMembershipAuthZ(orgResolver)
	authNProvider, err := authn.NewOIDCAuth(oidcUrl, externalOidcUrl, getTlsConfig(cfg), getOrgConfig(cfg), cfg.Auth.OIDC.UsernameClaim, cfg.Auth.OIDC.GroupsClaim)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create OIDC AuthN: %w", err)
	}
	return authNProvider, authZProvider, nil
}

func initAAPAuth(cfg *config.Config, log logrus.FieldLogger, orgResolver resolvers.Resolver) (common.AuthNMiddleware, AuthZMiddleware, error) {
	gatewayUrl := strings.TrimSuffix(cfg.Auth.AAP.ApiUrl, "/")
	gatewayExternalUrl := strings.TrimSuffix(cfg.Auth.AAP.ExternalApiUrl, "/")
	log.Infof("AAP Gateway auth enabled: %s", gatewayUrl)
	authZProvider := authz.NewOrgMembershipAuthZ(orgResolver)
	authNProvider, err := authn.NewAapGatewayAuth(gatewayUrl, gatewayExternalUrl, getTlsConfig(cfg))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create AAP Gateway AuthN: %w", err)
	}
	return authNProvider, authZProvider, nil
}

func InitAuth(cfg *config.Config, log logrus.FieldLogger, orgResolver resolvers.Resolver) (common.AuthNMiddleware, AuthZMiddleware, error) {
	value, exists := os.LookupEnv(DisableAuthEnvKey)
	if exists && value != "" {
		log.Warnln("Auth disabled")
		configuredAuthType = AuthTypeNil
		authNProvider := NilAuth{}
		authZProvider := NilAuth{}
		return authNProvider, authZProvider, nil
	} else if cfg.Auth != nil {
		var authNProvider common.AuthNMiddleware
		var authZProvider AuthZMiddleware
		var err error
		if cfg.Auth.K8s != nil {
			configuredAuthType = AuthTypeK8s
			authNProvider, authZProvider, err = initK8sAuth(cfg, log)
		} else if cfg.Auth.OIDC != nil {
			configuredAuthType = AuthTypeOIDC
			authNProvider, authZProvider, err = initOIDCAuth(cfg, log, orgResolver)
		} else if cfg.Auth.AAP != nil {
			configuredAuthType = AuthTypeAAP
			authNProvider, authZProvider, err = initAAPAuth(cfg, log, orgResolver)
		}
		if err != nil {
			return nil, nil, err
		}

		if authNProvider == nil {
			return nil, nil, errors.New("no authN provider defined")
		}
		if authZProvider == nil {
			return nil, nil, errors.New("no authZ provider defined")
		}
		return authNProvider, authZProvider, nil
	}

	return nil, nil, errors.New("no auth configuration provided")
}

// InitMultiAuth initializes authentication with support for multiple methods
func InitMultiAuth(cfg *config.Config, log logrus.FieldLogger, orgResolver resolvers.Resolver, oidcProviderService authn.OIDCProviderService) (common.AuthNMiddleware, AuthZMiddleware, error) {
	value, exists := os.LookupEnv(DisableAuthEnvKey)
	if exists && value != "" {
		log.Warnln("Auth disabled")
		configuredAuthType = AuthTypeNil
		authNProvider := NilAuth{}
		authZProvider := NilAuth{}
		return authNProvider, authZProvider, nil
	}

	if cfg.Auth == nil {
		return nil, nil, errors.New("no auth configuration provided")
	}

	// Create TLS config for OIDC provider connections
	tlsConfig := &tls.Config{
		InsecureSkipVerify: false, // Use secure connections by default
	}

	// Create MultiAuth instance
	multiAuth := authn.NewMultiAuth(oidcProviderService, tlsConfig)
	var authZProvider AuthZMiddleware

	// Initialize static authentication methods
	if cfg.Auth.K8s != nil {
		log.Infof("K8s auth enabled: %s", cfg.Auth.K8s.ApiUrl)
		k8sAuthN, k8sAuthZ, err := initK8sAuth(cfg, log)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to initialize K8s auth: %w", err)
		}

		// Add K8s auth with its issuer
		k8sIssuer := "https://kubernetes.default.svc.cluster.local" // Default K8s issuer
		if cfg.Auth.K8s.ApiUrl != "" {
			k8sIssuer = strings.TrimSuffix(cfg.Auth.K8s.ApiUrl, "/")
		}
		multiAuth.AddStaticProvider(k8sIssuer, k8sAuthN)

		// Use K8s authZ as primary (can be overridden by other methods)
		if authZProvider == nil {
			authZProvider = k8sAuthZ
		}
		configuredAuthType = AuthTypeK8s
	}

	if cfg.Auth.OIDC != nil {
		log.Infof("OIDC auth enabled: %s", cfg.Auth.OIDC.OIDCAuthority)
		oidcAuthN, oidcAuthZ, err := initOIDCAuth(cfg, log, orgResolver)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to initialize OIDC auth: %w", err)
		}

		// Add OIDC auth with its issuer
		oidcIssuer := strings.TrimSuffix(cfg.Auth.OIDC.OIDCAuthority, "/")
		multiAuth.AddStaticProvider(oidcIssuer, oidcAuthN)

		// Use OIDC authZ as primary (can be overridden by other methods)
		if authZProvider == nil {
			authZProvider = oidcAuthZ
		}
		configuredAuthType = AuthTypeOIDC
	}

	if cfg.Auth.AAP != nil {
		log.Infof("AAP Gateway auth enabled: %s", cfg.Auth.AAP.ApiUrl)
		aapAuthN, aapAuthZ, err := initAAPAuth(cfg, log, orgResolver)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to initialize AAP auth: %w", err)
		}

		// Add AAP auth (for opaque tokens)
		multiAuth.AddStaticProvider("aap", aapAuthN)

		// Use AAP authZ as primary (can be overridden by other methods)
		if authZProvider == nil {
			authZProvider = aapAuthZ
		}
		configuredAuthType = AuthTypeAAP
	}

	if !multiAuth.HasProviders() {
		return nil, nil, errors.New("no authentication providers configured")
	}

	if authZProvider == nil {
		return nil, nil, errors.New("no authZ provider defined")
	}

	return multiAuth, authZProvider, nil
}

type K8sToK8sAuth struct {
	authz.K8sAuthZ
}

func (o K8sToK8sAuth) CheckPermission(ctx context.Context, resource string, op string) (bool, error) {
	k8sTokenVal := ctx.Value(consts.TokenCtxKey)
	if k8sTokenVal == nil {
		return false, nil
	}
	k8sToken := k8sTokenVal.(string)
	return o.K8sAuthZ.CheckPermission(ctx, k8sToken, resource, op)
}
