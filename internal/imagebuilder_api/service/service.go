package service

import (
	"context"

	"github.com/flightctl/flightctl/internal/consts"
	"github.com/flightctl/flightctl/internal/imagebuilder_api/store"
	"github.com/flightctl/flightctl/pkg/queues"
	"github.com/sirupsen/logrus"
)

// Service is the aggregate service interface for the ImageBuilder API.
// It provides access to all sub-services (ImageBuild, ImageExport, and future services).
type Service interface {
	ImageBuild() ImageBuildService
	ImageExport() ImageExportService
	ImagePipeline() ImagePipelineService
}

// service is the concrete implementation of Service
type service struct {
	imageBuild    ImageBuildService
	imageExport   ImageExportService
	imagePipeline ImagePipelineService
}

// NewService creates a new aggregate Service with all sub-services
func NewService(ctx context.Context, s store.Store, queuesProvider queues.Provider, log logrus.FieldLogger) Service {
	// Create queue producer for imagebuild queue
	var queueProducer queues.QueueProducer
	if queuesProvider != nil {
		var err error
		queueProducer, err = queuesProvider.NewQueueProducer(ctx, consts.ImageBuildTaskQueue)
		if err != nil {
			log.WithError(err).Error("failed to create imagebuild queue producer, jobs will not be enqueued")
		}
	}

	imageBuildSvc := NewImageBuildService(s.ImageBuild(), queueProducer, log)
	imageExportSvc := NewImageExportService(s.ImageExport(), s.ImageBuild(), log)
	return &service{
		imageBuild:    imageBuildSvc,
		imageExport:   imageExportSvc,
		imagePipeline: NewImagePipelineService(s.ImagePipeline(), imageBuildSvc, imageExportSvc, log),
	}
}

// ImageBuild returns the ImageBuildService
func (s *service) ImageBuild() ImageBuildService {
	return s.imageBuild
}

// ImageExport returns the ImageExportService
func (s *service) ImageExport() ImageExportService {
	return s.imageExport
}

// ImagePipeline returns the ImagePipelineService
func (s *service) ImagePipeline() ImagePipelineService {
	return s.imagePipeline
}
