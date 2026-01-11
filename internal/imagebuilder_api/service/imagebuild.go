package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/flightctl/flightctl/api/v1beta1"
	api "github.com/flightctl/flightctl/api/v1beta1/imagebuilder"
	"github.com/flightctl/flightctl/internal/imagebuilder_api/store"
	"github.com/flightctl/flightctl/internal/imagebuilder_worker/tasks"
	"github.com/flightctl/flightctl/internal/store/selector"
	"github.com/flightctl/flightctl/pkg/queues"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
)

// ImageBuildService handles business logic for ImageBuild resources
type ImageBuildService interface {
	Create(ctx context.Context, orgId uuid.UUID, imageBuild api.ImageBuild) (*api.ImageBuild, v1beta1.Status)
	Get(ctx context.Context, orgId uuid.UUID, name string) (*api.ImageBuild, v1beta1.Status)
	List(ctx context.Context, orgId uuid.UUID, params api.ListImageBuildsParams) (*api.ImageBuildList, v1beta1.Status)
	Delete(ctx context.Context, orgId uuid.UUID, name string) v1beta1.Status
	// Internal methods (not exposed via API)
	UpdateStatus(ctx context.Context, orgId uuid.UUID, imageBuild *api.ImageBuild) (*api.ImageBuild, error)
	UpdateLastSeen(ctx context.Context, orgId uuid.UUID, name string, timestamp time.Time) error
}

// imageBuildService is the concrete implementation of ImageBuildService
type imageBuildService struct {
	store         store.ImageBuildStore
	queueProducer queues.QueueProducer
	log           logrus.FieldLogger
}

// NewImageBuildService creates a new ImageBuildService
func NewImageBuildService(s store.ImageBuildStore, queueProducer queues.QueueProducer, log logrus.FieldLogger) ImageBuildService {
	return &imageBuildService{
		store:         s,
		queueProducer: queueProducer,
		log:           log,
	}
}

func (s *imageBuildService) Create(ctx context.Context, orgId uuid.UUID, imageBuild api.ImageBuild) (*api.ImageBuild, v1beta1.Status) {
	// Don't set fields that are managed by the service
	imageBuild.Status = nil
	NilOutManagedObjectMetaProperties(&imageBuild.Metadata)

	// Validate input
	if errs := s.validate(&imageBuild); len(errs) > 0 {
		return nil, StatusBadRequest(errors.Join(errs...).Error())
	}

	result, err := s.store.Create(ctx, orgId, &imageBuild)
	if err != nil {
		return result, StoreErrorToApiStatus(err, true, ImageBuildKind, imageBuild.Metadata.Name)
	}

	// Enqueue the build job after successful creation
	if s.queueProducer != nil {
		if err := s.enqueueImageBuildJob(ctx, orgId, result); err != nil {
			s.log.WithError(err).WithField("orgId", orgId).WithField("name", lo.FromPtr(result.Metadata.Name)).Error("failed to enqueue imageBuild job")
			// Don't fail the creation if enqueue fails - the job can be retried later
		}
	}

	return result, StoreErrorToApiStatus(err, true, ImageBuildKind, imageBuild.Metadata.Name)
}

func (s *imageBuildService) Get(ctx context.Context, orgId uuid.UUID, name string) (*api.ImageBuild, v1beta1.Status) {
	result, err := s.store.Get(ctx, orgId, name)
	return result, StoreErrorToApiStatus(err, false, ImageBuildKind, &name)
}

func (s *imageBuildService) List(ctx context.Context, orgId uuid.UUID, params api.ListImageBuildsParams) (*api.ImageBuildList, v1beta1.Status) {
	listParams, status := prepareListParams(params.Continue, params.LabelSelector, params.FieldSelector, params.Limit)
	if !IsStatusOK(status) {
		return nil, status
	}

	result, err := s.store.List(ctx, orgId, *listParams)
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

func (s *imageBuildService) Delete(ctx context.Context, orgId uuid.UUID, name string) v1beta1.Status {
	err := s.store.Delete(ctx, orgId, name)
	return StoreErrorToApiStatus(err, false, ImageBuildKind, &name)
}

// Internal methods (not exposed via API)

func (s *imageBuildService) UpdateStatus(ctx context.Context, orgId uuid.UUID, imageBuild *api.ImageBuild) (*api.ImageBuild, error) {
	return s.store.UpdateStatus(ctx, orgId, imageBuild)
}

func (s *imageBuildService) UpdateLastSeen(ctx context.Context, orgId uuid.UUID, name string, timestamp time.Time) error {
	return s.store.UpdateLastSeen(ctx, orgId, name, timestamp)
}

// validate performs validation on an ImageBuild resource
func (s *imageBuildService) validate(imageBuild *api.ImageBuild) []error {
	var errs []error

	if lo.FromPtr(imageBuild.Metadata.Name) == "" {
		errs = append(errs, errors.New("metadata.name is required"))
	}

	if imageBuild.Spec.Source.Repository == "" {
		errs = append(errs, errors.New("spec.source.repository is required"))
	}
	if imageBuild.Spec.Source.ImageName == "" {
		errs = append(errs, errors.New("spec.source.imageName is required"))
	}
	if imageBuild.Spec.Source.ImageTag == "" {
		errs = append(errs, errors.New("spec.source.imageTag is required"))
	}

	if imageBuild.Spec.Destination.Repository == "" {
		errs = append(errs, errors.New("spec.destination.repository is required"))
	}
	if imageBuild.Spec.Destination.ImageName == "" {
		errs = append(errs, errors.New("spec.destination.imageName is required"))
	}
	if imageBuild.Spec.Destination.Tag == "" {
		errs = append(errs, errors.New("spec.destination.tag is required"))
	}

	// Binding validation is now enforced by the schema:
	// - EarlyBinding requires cert (enforced by schema)
	// - LateBinding has no additional required fields

	return errs
}

// enqueueImageBuildJob enqueues an imageBuild job to the imagebuild-queue
func (s *imageBuildService) enqueueImageBuildJob(ctx context.Context, orgId uuid.UUID, imageBuild *api.ImageBuild) error {
	if imageBuild == nil || imageBuild.Metadata.Name == nil {
		return errors.New("imageBuild or name is nil")
	}

	job := tasks.Job{
		Type:      tasks.TaskTypeImageBuild,
		OrgID:     orgId.String(),
		Name:      lo.FromPtr(imageBuild.Metadata.Name),
		Timestamp: time.Now().UnixMicro(),
	}

	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal job: %w", err)
	}

	if err := s.queueProducer.Enqueue(ctx, payload, job.Timestamp); err != nil {
		return fmt.Errorf("failed to enqueue job: %w", err)
	}

	s.log.WithField("orgId", orgId).WithField("name", job.Name).Info("enqueued imageBuild job")
	return nil
}
