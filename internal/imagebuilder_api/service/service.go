package service

import (
	"context"
	"errors"
	"time"

	api "github.com/flightctl/flightctl/api/v1beta1/imagebuilder"
	"github.com/flightctl/flightctl/internal/imagebuilder_api/store"
	"github.com/flightctl/flightctl/internal/store/selector"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// ImageBuildService handles business logic for ImageBuild resources
type ImageBuildService interface {
	Create(ctx context.Context, orgId uuid.UUID, imageBuild api.ImageBuild) (*api.ImageBuild, api.Status)
	Update(ctx context.Context, orgId uuid.UUID, name string, imageBuild api.ImageBuild) (*api.ImageBuild, api.Status)
	Get(ctx context.Context, orgId uuid.UUID, name string) (*api.ImageBuild, api.Status)
	List(ctx context.Context, orgId uuid.UUID, params api.ListImageBuildsParams) (*api.ImageBuildList, api.Status)
	Delete(ctx context.Context, orgId uuid.UUID, name string) api.Status
	// Internal methods (not exposed via API)
	UpdateStatus(ctx context.Context, orgId uuid.UUID, imageBuild *api.ImageBuild) (*api.ImageBuild, error)
	UpdateLastSeen(ctx context.Context, orgId uuid.UUID, name string, timestamp time.Time) error
	ListByPhase(ctx context.Context, orgId uuid.UUID, phases []api.ImageBuildPhase) ([]api.ImageBuild, error)
}

// imageBuildService is the concrete implementation of ImageBuildService
type imageBuildService struct {
	store store.Store
	log   logrus.FieldLogger
}

// NewImageBuildService creates a new ImageBuildService
func NewImageBuildService(s store.Store, log logrus.FieldLogger) ImageBuildService {
	return &imageBuildService{
		store: s,
		log:   log,
	}
}

func (s *imageBuildService) Create(ctx context.Context, orgId uuid.UUID, imageBuild api.ImageBuild) (*api.ImageBuild, api.Status) {
	// Don't set fields that are managed by the service
	imageBuild.Status = nil
	NilOutManagedObjectMetaProperties(&imageBuild.Metadata)

	// Validate input
	if errs := s.validate(&imageBuild); len(errs) > 0 {
		return nil, StatusBadRequest(errors.Join(errs...).Error())
	}

	result, err := s.store.ImageBuild().Create(ctx, orgId, &imageBuild)
	return result, StoreErrorToApiStatus(err, true, ImageBuildKind, imageBuild.Metadata.Name)
}

func (s *imageBuildService) Update(ctx context.Context, orgId uuid.UUID, name string, imageBuild api.ImageBuild) (*api.ImageBuild, api.Status) {
	// Don't overwrite fields that are managed by the service
	imageBuild.Status = nil
	NilOutManagedObjectMetaProperties(&imageBuild.Metadata)

	// Validate input
	if errs := s.validate(&imageBuild); len(errs) > 0 {
		return nil, StatusBadRequest(errors.Join(errs...).Error())
	}

	if imageBuild.Metadata.Name == nil || name != *imageBuild.Metadata.Name {
		return nil, StatusBadRequest("resource name specified in metadata does not match name in path")
	}

	result, err := s.store.ImageBuild().Update(ctx, orgId, &imageBuild)
	return result, StoreErrorToApiStatus(err, false, ImageBuildKind, &name)
}

func (s *imageBuildService) Get(ctx context.Context, orgId uuid.UUID, name string) (*api.ImageBuild, api.Status) {
	result, err := s.store.ImageBuild().Get(ctx, orgId, name)
	return result, StoreErrorToApiStatus(err, false, ImageBuildKind, &name)
}

func (s *imageBuildService) List(ctx context.Context, orgId uuid.UUID, params api.ListImageBuildsParams) (*api.ImageBuildList, api.Status) {
	listParams, status := prepareListParams(params.Continue, params.LabelSelector, params.FieldSelector, params.Limit)
	if !IsStatusOK(status) {
		return nil, status
	}

	result, err := s.store.ImageBuild().List(ctx, orgId, *listParams)
	if err == nil {
		return result, StatusOK()
	}

	var se *selector.SelectorError
	switch {
	case selector.AsSelectorError(err, &se):
		return nil, StatusBadRequest(se.Error())
	default:
		return nil, StatusInternalServerError(err.Error())
	}
}

func (s *imageBuildService) Delete(ctx context.Context, orgId uuid.UUID, name string) api.Status {
	err := s.store.ImageBuild().Delete(ctx, orgId, name)
	return StoreErrorToApiStatus(err, false, ImageBuildKind, &name)
}

// Internal methods (not exposed via API)

func (s *imageBuildService) UpdateStatus(ctx context.Context, orgId uuid.UUID, imageBuild *api.ImageBuild) (*api.ImageBuild, error) {
	return s.store.ImageBuild().UpdateStatus(ctx, orgId, imageBuild)
}

func (s *imageBuildService) UpdateLastSeen(ctx context.Context, orgId uuid.UUID, name string, timestamp time.Time) error {
	return s.store.ImageBuild().UpdateLastSeen(ctx, orgId, name, timestamp)
}

func (s *imageBuildService) ListByPhase(ctx context.Context, orgId uuid.UUID, phases []api.ImageBuildPhase) ([]api.ImageBuild, error) {
	return s.store.ImageBuild().ListByPhase(ctx, orgId, phases)
}

// validate performs validation on an ImageBuild resource
func (s *imageBuildService) validate(imageBuild *api.ImageBuild) []error {
	var errs []error

	if imageBuild.Metadata.Name == nil || *imageBuild.Metadata.Name == "" {
		errs = append(errs, errors.New("metadata.name is required"))
	}

	if imageBuild.Spec.Input.RegistryName == "" {
		errs = append(errs, errors.New("spec.input.registryName is required"))
	}
	if imageBuild.Spec.Input.ImageName == "" {
		errs = append(errs, errors.New("spec.input.imageName is required"))
	}
	if imageBuild.Spec.Input.ImageTag == "" {
		errs = append(errs, errors.New("spec.input.imageTag is required"))
	}

	if imageBuild.Spec.Output.RegistryName == "" {
		errs = append(errs, errors.New("spec.output.registryName is required"))
	}
	if imageBuild.Spec.Output.ImageName == "" {
		errs = append(errs, errors.New("spec.output.imageName is required"))
	}
	if imageBuild.Spec.Output.Tag == "" {
		errs = append(errs, errors.New("spec.output.tag is required"))
	}

	return errs
}
