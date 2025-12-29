package model

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"

	api "github.com/flightctl/flightctl/api/v1beta1/imagebuilder"
	"github.com/flightctl/flightctl/internal/flterrors"
	"github.com/flightctl/flightctl/internal/util"
	"github.com/samber/lo"
)

type ImageBuild struct {
	Resource

	// The desired state, stored as opaque JSON object.
	Spec *JSONField[api.ImageBuildSpec] `gorm:"type:jsonb"`

	// The last reported state, stored as opaque JSON object.
	Status *JSONField[api.ImageBuildStatus] `gorm:"type:jsonb"`
}

func (i ImageBuild) String() string {
	val, _ := json.Marshal(i)
	return string(val)
}

func NewImageBuildFromApiResource(resource *api.ImageBuild) (*ImageBuild, error) {
	if resource == nil || resource.Metadata.Name == nil {
		return &ImageBuild{}, nil
	}

	status := api.ImageBuildStatus{Conditions: &[]api.Condition{}}
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
	return &ImageBuild{
		Resource: Resource{
			Name:            *resource.Metadata.Name,
			Labels:          lo.FromPtrOr(resource.Metadata.Labels, make(map[string]string)),
			Annotations:     lo.FromPtrOr(resource.Metadata.Annotations, make(map[string]string)),
			ResourceVersion: resourceVersion,
		},
		Spec:   MakeJSONField(resource.Spec),
		Status: MakeJSONField(status),
	}, nil
}

func ImageBuildAPIVersion() string {
	return fmt.Sprintf("%s/%s", api.APIGroup, api.ImageBuildAPIVersion)
}

func (i *ImageBuild) ToApiResource(opts ...APIResourceOption) (*api.ImageBuild, error) {
	if i == nil {
		return &api.ImageBuild{}, nil
	}

	var spec api.ImageBuildSpec
	if i.Spec != nil {
		spec = i.Spec.Data
	}

	status := api.ImageBuildStatus{Conditions: &[]api.Condition{}}
	if i.Status != nil {
		status = i.Status.Data
	}

	return &api.ImageBuild{
		ApiVersion: ImageBuildAPIVersion(),
		Kind:       api.ImageBuildKind,
		Metadata: api.ObjectMeta{
			Name:              lo.ToPtr(i.Name),
			CreationTimestamp: lo.ToPtr(i.CreatedAt.UTC()),
			Labels:            lo.ToPtr(util.EnsureMap(i.Resource.Labels)),
			Annotations:       lo.ToPtr(util.EnsureMap(i.Resource.Annotations)),
			ResourceVersion:   lo.Ternary(i.ResourceVersion != nil, lo.ToPtr(strconv.FormatInt(lo.FromPtr(i.ResourceVersion), 10)), nil),
		},
		Spec:   spec,
		Status: &status,
	}, nil
}

func ImageBuildsToApiResource(builds []ImageBuild, cont *string, numRemaining *int64) (api.ImageBuildList, error) {
	imageBuildList := make([]api.ImageBuild, len(builds))
	for i, build := range builds {
		b, err := build.ToApiResource()
		if err != nil {
			return api.ImageBuildList{
				ApiVersion: ImageBuildAPIVersion(),
				Kind:       api.ImageBuildListKind,
				Items:      []api.ImageBuild{},
			}, err
		}
		imageBuildList[i] = *b
	}
	ret := api.ImageBuildList{
		ApiVersion: ImageBuildAPIVersion(),
		Kind:       api.ImageBuildListKind,
		Items:      imageBuildList,
		Metadata:   api.ListMeta{},
	}
	if cont != nil {
		ret.Metadata.Continue = cont
		ret.Metadata.RemainingItemCount = numRemaining
	}
	return ret, nil
}

func (i *ImageBuild) GetKind() string {
	return api.ImageBuildKind
}

func (i *ImageBuild) HasNilSpec() bool {
	return i.Spec == nil
}

func (i *ImageBuild) HasSameSpecAs(otherResource any) bool {
	other, ok := otherResource.(*ImageBuild)
	if !ok {
		return false
	}
	if other == nil {
		return false
	}
	if i.Spec == nil && other.Spec == nil {
		return true
	}
	if (i.Spec == nil && other.Spec != nil) || (i.Spec != nil && other.Spec == nil) {
		return false
	}
	return reflect.DeepEqual(i.Spec.Data, other.Spec.Data)
}

func (i *ImageBuild) GetStatusAsJson() ([]byte, error) {
	return i.Status.MarshalJSON()
}
