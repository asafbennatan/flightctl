package login

import (
	api "github.com/flightctl/flightctl/api/v1beta1"
)

// AuthProviderBase contains common fields shared across all authentication providers
type AuthProviderBase[T any] struct {
	Metadata           api.ObjectMeta
	Spec               T
	CAFile             string
	InsecureSkipVerify bool
	ApiServerURL       string
	CallbackPort       int
	Username           string
	Password           string
	ClientId           string
	ClientSecret       string
	Web                bool
}

// NewAuthProviderBase creates a new base auth provider with common fields
func NewAuthProviderBase[T any](
	metadata api.ObjectMeta,
	spec T,
	caFile string,
	insecure bool,
	apiServerURL string,
	callbackPort int,
	username, password string,
	clientId, clientSecret string,
	web bool,
) AuthProviderBase[T] {
	return AuthProviderBase[T]{
		Metadata:           metadata,
		Spec:               spec,
		CAFile:             caFile,
		InsecureSkipVerify: insecure,
		ApiServerURL:       apiServerURL,
		CallbackPort:       callbackPort,
		Username:           username,
		Password:           password,
		ClientId:           clientId,
		ClientSecret:       clientSecret,
		Web:                web,
	}
}

// HasClientCredentials returns true if both client ID and secret are provided
func (b *AuthProviderBase[T]) HasClientCredentials() bool {
	return b.ClientId != "" && b.ClientSecret != ""
}

// HasPasswordCredentials returns true if both username and password are provided
func (b *AuthProviderBase[T]) HasPasswordCredentials() bool {
	return b.Username != "" && b.Password != ""
}

// ShouldUsePasswordFlow returns true if password flow should be used
func (b *AuthProviderBase[T]) ShouldUsePasswordFlow() bool {
	return b.HasPasswordCredentials() && !b.Web
}
