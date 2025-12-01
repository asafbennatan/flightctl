package login

import (
	"fmt"
	"net/http"

	api "github.com/flightctl/flightctl/api/v1beta1"
	"github.com/openshift/osincli"
)

type OpenShift struct {
	AuthProviderBase[api.OpenShiftProviderSpec]
}

func NewOpenShiftConfig(metadata api.ObjectMeta, spec api.OpenShiftProviderSpec, caFile string, insecure bool, apiServerURL string, callbackPort int, username, password string, clientId, clientSecret string, web bool) *OpenShift {
	return &OpenShift{
		AuthProviderBase: NewAuthProviderBase(
			metadata,
			spec,
			caFile,
			insecure,
			apiServerURL,
			callbackPort,
			username,
			password,
			clientId,
			clientSecret,
			web,
		),
	}
}

func (o *OpenShift) SetInsecureSkipVerify(insecureSkipVerify bool) {
	o.InsecureSkipVerify = insecureSkipVerify
}

func (o *OpenShift) getOAuth2Client(callback string) (*osincli.Client, error) {
	codeVerifier, codeChallenge, err := generatePKCEVerifier()
	if err != nil {
		return nil, fmt.Errorf("failed to generate PKCE parameters: %w", err)
	}

	// Use the API server's token proxy endpoint instead of the OpenShift provider's token endpoint
	tokenProxyURL := getTokenProxyURL(o.ApiServerURL, *o.Metadata.Name)

	config := &osincli.ClientConfig{
		ClientId:                 *o.Spec.ClientId,
		AuthorizeUrl:             *o.Spec.AuthorizationUrl,
		TokenUrl:                 tokenProxyURL,
		ErrorsInStatusCode:       true,
		SendClientSecretInParams: true, // this makes sure we send the client id , the secret is not filled
		RedirectUrl:              callback,
		Scope:                    "user:full",
		CodeVerifier:             codeVerifier,
		CodeChallenge:            codeChallenge,
		CodeChallengeMethod:      "S256",
	}

	client, err := osincli.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create openshift oauth2 client: %w", err)
	}

	tlsConfig, err := getAuthClientTlsConfig(o.CAFile, o.InsecureSkipVerify)
	if err != nil {
		return nil, err
	}

	client.Transport = &http.Transport{
		TLSClientConfig: tlsConfig,
	}

	return client, nil
}

func (o *OpenShift) Auth() (AuthInfo, error) {
	// Use client credentials flow if client ID and secret are provided
	if o.HasClientCredentials() {
		return o.authClientCredentialsFlow()
	}
	// Use password flow if username/password provided and web flag not set
	if o.ShouldUsePasswordFlow() {
		return o.authPasswordFlow()
	}
	// Default to auth code flow
	authInfo, err := oauth2AuthCodeFlow(o.getOAuth2Client, o.CallbackPort)
	if err != nil {
		return AuthInfo{}, err
	}
	authInfo.TokenToUse = TokenToUseAccessToken
	return authInfo, nil
}

func (o *OpenShift) authPasswordFlow() (AuthInfo, error) {
	authInfo, err := oauth2PasswordFlow(*o.Spec.TokenUrl, *o.Spec.ClientId, o.Username, o.Password, "user:full", o.CAFile, o.InsecureSkipVerify)
	if err != nil {
		return AuthInfo{}, err
	}
	authInfo.TokenToUse = TokenToUseAccessToken
	return authInfo, nil
}

func (o *OpenShift) authClientCredentialsFlow() (AuthInfo, error) {
	if o.Spec.TokenUrl == nil {
		return AuthInfo{}, fmt.Errorf("token URL is required for client credentials flow")
	}
	authInfo, err := oauth2ClientCredentialsFlow(*o.Spec.TokenUrl, o.ClientId, o.ClientSecret, "user:full", o.CAFile, o.InsecureSkipVerify)
	if err != nil {
		return AuthInfo{}, err
	}
	authInfo.TokenToUse = TokenToUseAccessToken
	return authInfo, nil
}

func (o *OpenShift) Renew(refreshToken string) (AuthInfo, error) {
	return oauth2RefreshTokenFlow(refreshToken, o.getOAuth2Client)
}

func (o *OpenShift) Validate(args ValidateArgs) error {
	if o.Metadata.Name == nil {
		return fmt.Errorf("provider name is required")
	}
	if o.ApiServerURL == "" {
		return fmt.Errorf("API server URL is required")
	}

	// Client credentials flow validation
	if o.ClientId != "" && o.ClientSecret != "" {
		if o.Spec.TokenUrl == nil {
			return fmt.Errorf("token URL is required for client credentials flow")
		}
		return nil
	}

	// Web-based or password flow validation
	if o.Spec.ClientId == nil {
		return fmt.Errorf("client ID is required")
	}
	if o.Spec.AuthorizationUrl == nil {
		return fmt.Errorf("authorization URL is required")
	}
	if !o.Web && (o.Username == "" || o.Password == "") {
		return fmt.Errorf("username and password are required for password flow (use --web flag for web-based authentication)")
	}
	if !o.Web && o.Username != "" && o.Password != "" && o.Spec.TokenUrl == nil {
		return fmt.Errorf("token URL is required for password flow")
	}
	return nil
}
