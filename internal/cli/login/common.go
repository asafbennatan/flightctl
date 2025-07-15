package login

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
)

type OauthServerResponse struct {
	TokenEndpoint string `json:"token_endpoint"`
	AuthEndpoint  string `json:"authorization_endpoint"`
}

func getAuthClientTlsConfig(authCAFile string, insecureSkipVerify bool) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: insecureSkipVerify, //nolint:gosec
	}

	if authCAFile != "" {
		caData, err := os.ReadFile(authCAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read Auth CA file: %w", err)
		}
		
		// Start with system CAs to ensure compatibility with standard server certificates
		caPool, err := x509.SystemCertPool()
		if err != nil {
			// If system cert pool is not available, create a new one
			caPool = x509.NewCertPool()
		}
		
		// Add custom CAs to the existing pool
		if !caPool.AppendCertsFromPEM(caData) {
			return nil, fmt.Errorf("failed to add custom CA certificates to pool")
		}

		tlsConfig.RootCAs = caPool
	}

	return tlsConfig, nil
}

type AuthInfo struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    *int64
}

type ValidateArgs struct {
	ApiUrl      string
	ClientId    string
	AccessToken string
	Username    string
	Password    string
	Web         bool
}

type AuthProvider interface {
	Auth(web bool, username, password string) (AuthInfo, error)
	Renew(refreshToken string) (AuthInfo, error)
	Validate(args ValidateArgs) error
}

func StrIsEmpty(str string) bool {
	return len(strings.TrimSpace(str)) == 0
}
