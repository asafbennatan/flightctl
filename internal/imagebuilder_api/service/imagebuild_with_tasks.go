package service

import (
	"context"

	"github.com/flightctl/flightctl/api/v1beta1"
	api "github.com/flightctl/flightctl/api/v1beta1/imagebuilder"
	"github.com/flightctl/flightctl/internal/imagebuilder_api/store"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// ImageBuildWithTasksService handles atomic creation of ImageBuild with optional ImageExport
type ImageBuildWithTasksService interface {
	Create(ctx context.Context, orgId uuid.UUID, req api.ImageBuildWithTasksRequest) (*api.ImageBuildWithTasksResponse, v1beta1.Status)
}

// imageBuildWithTasksService is the concrete implementation
type imageBuildWithTasksService struct {
	store          store.Store
	imageBuildSvc  ImageBuildService
	imageExportSvc ImageExportService
	log            logrus.FieldLogger
}

// NewImageBuildWithTasksService creates a new ImageBuildWithTasksService
func NewImageBuildWithTasksService(s store.Store, imageBuildSvc ImageBuildService, imageExportSvc ImageExportService, log logrus.FieldLogger) ImageBuildWithTasksService {
	return &imageBuildWithTasksService{
		store:          s,
		imageBuildSvc:  imageBuildSvc,
		imageExportSvc: imageExportSvc,
		log:            log,
	}
}

// Create creates an ImageBuild and optionally an ImageExport atomically in a single transaction
func (s *imageBuildWithTasksService) Create(ctx context.Context, orgId uuid.UUID, req api.ImageBuildWithTasksRequest) (*api.ImageBuildWithTasksResponse, v1beta1.Status) {
	var createdBuild *api.ImageBuild
	var createdExport *api.ImageExport
	var status v1beta1.Status

	// If ImageExport is provided, set up the source reference to the ImageBuild
	var imageExport *api.ImageExport
	if req.ImageExport != nil {
		imageExport = req.ImageExport

		// Override source to reference the ImageBuild being created
		imageBuildName := *req.ImageBuild.Metadata.Name
		source := api.ImageBuildRefSource{
			Type:          api.ImageBuildRefSourceTypeImageBuild,
			ImageBuildRef: imageBuildName,
		}
		if err := imageExport.Spec.Source.FromImageBuildRefSource(source); err != nil {
			return nil, StatusInternalServerError("failed to set imageExport source: " + err.Error())
		}
	}

	// Execute in a transaction
	err := s.store.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Create ImageBuild using the existing service
		createdBuild, status = s.imageBuildSvc.CreateWithTx(ctx, tx, orgId, req.ImageBuild)
		if !IsStatusOK(status) {
			return statusToError(status)
		}

		// Create ImageExport if provided using the existing service
		if imageExport != nil {
			// Skip source validation since we just set it to reference the ImageBuild we're creating
			createdExport, status = s.imageExportSvc.CreateWithTx(ctx, tx, orgId, *imageExport, true)
			if !IsStatusOK(status) {
				return statusToError(status)
			}
		}

		return nil
	})

	if err != nil {
		// If we have a status error, return it directly
		if !IsStatusOK(status) {
			return nil, status
		}
		// Otherwise it's a transaction/db error
		return nil, StatusInternalServerError(err.Error())
	}

	return &api.ImageBuildWithTasksResponse{
		ImageBuild:  *createdBuild,
		ImageExport: createdExport,
	}, StatusCreated()
}

// statusToError converts a non-OK status to an error for transaction rollback
type statusError struct {
	status v1beta1.Status
}

func (e *statusError) Error() string {
	return e.status.Message
}

func statusToError(status v1beta1.Status) error {
	return &statusError{status: status}
}
