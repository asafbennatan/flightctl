package model

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/flterrors"
	"github.com/flightctl/flightctl/internal/util"
	"github.com/samber/lo"
)

type OIDCProvider struct {
	Resource

	// The desired state, stored as opaque JSON object.
	Spec *JSONField[api.OIDCProviderSpec] `gorm:"type:jsonb"`

	// The last reported state, stored as opaque JSON object.
	Status *JSONField[api.OIDCProviderStatus] `gorm:"type:jsonb"`
}

func (o OIDCProvider) String() string {
	val, _ := json.Marshal(o)
	return string(val)
}

func NewOIDCProviderFromApiResource(resource *api.OIDCProvider) (*OIDCProvider, error) {
	if resource == nil || resource.Metadata.Name == nil {
		return &OIDCProvider{}, nil
	}

	status := api.OIDCProviderStatus{Conditions: []api.Condition{}}
	if resource.Status != nil {
		status = *resource.Status
	}
	var resourceVersion *int64
	if resource.Metadata.ResourceVersion != nil {
		i, err := strconv.ParseInt(lo.FromPtr(resource.Metadata.ResourceVersion), 10, 64)
		if err != nil {
			return nil, flterrors.ErrIllegalResourceVersionFormat
		}
		resourceVersion = &i
	}

	return &OIDCProvider{
		Resource: Resource{
			Name:            lo.FromPtr(resource.Metadata.Name),
			Owner:           resource.Metadata.Owner,
			Labels:          lo.FromPtrOr(resource.Metadata.Labels, make(map[string]string)),
			Annotations:     lo.FromPtrOr(resource.Metadata.Annotations, make(map[string]string)),
			Generation:      resource.Metadata.Generation,
			ResourceVersion: resourceVersion,
		},
		Spec:   MakeJSONField(resource.Spec),
		Status: MakeJSONField(status),
	}, nil
}

func OIDCProviderAPIVersion() string {
	return fmt.Sprintf("%s/%s", api.APIGroup, api.OIDCProviderAPIVersion)
}

func (o *OIDCProvider) ToApiResource(opts ...APIResourceOption) (*api.OIDCProvider, error) {
	if o == nil {
		return &api.OIDCProvider{}, nil
	}

	var spec api.OIDCProviderSpec
	if o.Spec != nil {
		spec = o.Spec.Data
	}

	status := api.OIDCProviderStatus{Conditions: []api.Condition{}}
	if o.Status != nil {
		status = o.Status.Data
	}

	return &api.OIDCProvider{
		ApiVersion: OIDCProviderAPIVersion(),
		Kind:       api.OIDCProviderKind,
		Metadata: api.ObjectMeta{
			Name:              lo.ToPtr(o.Name),
			CreationTimestamp: lo.ToPtr(o.CreatedAt.UTC()),
			Labels:            lo.ToPtr(util.EnsureMap(o.Resource.Labels)),
			Annotations:       lo.ToPtr(util.EnsureMap(o.Resource.Annotations)),
			ResourceVersion:   lo.Ternary(o.ResourceVersion != nil, lo.ToPtr(strconv.FormatInt(lo.FromPtr(o.ResourceVersion), 10)), nil),
		},
		Spec:   spec,
		Status: &status,
	}, nil
}

func (o *OIDCProvider) GetKind() string {
	return api.OIDCProviderKind
}

func (o *OIDCProvider) GetStatusAsJson() ([]byte, error) {
	return o.Status.MarshalJSON()
}

func (o *OIDCProvider) HasNilSpec() bool {
	return o.Spec == nil
}

func (o *OIDCProvider) HasSameSpecAs(otherResource any) bool {
	other, ok := otherResource.(*OIDCProvider) // Assert that the other resource is a *OIDCProvider
	if !ok {
		return false
	}
	return reflect.DeepEqual(o.Spec.Data, other.Spec.Data)
}

func OIDCProvidersToApiResource(oidcProviders []OIDCProvider, continueToken *string, resourceVersion *int64) (api.OIDCProviderList, error) {
	items := lo.Map(oidcProviders, func(oidcProvider OIDCProvider, _ int) api.OIDCProvider {
		resource, _ := oidcProvider.ToApiResource()
		return *resource
	})

	return api.OIDCProviderList{
		ApiVersion: OIDCProviderAPIVersion(),
		Kind:       api.OIDCProviderKind,
		Metadata: api.ListMeta{
			Continue: continueToken,
		},
		Items: items,
	}, nil
}
