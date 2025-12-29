package store

import (
	"context"
	"time"

	api "github.com/flightctl/flightctl/api/v1beta1/imagebuilder"
	"github.com/flightctl/flightctl/internal/flterrors"
	"github.com/flightctl/flightctl/internal/store/model"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type ImageBuild interface {
	InitialMigration(ctx context.Context) error

	Create(ctx context.Context, orgId uuid.UUID, imageBuild *api.ImageBuild) (*api.ImageBuild, error)
	Update(ctx context.Context, orgId uuid.UUID, imageBuild *api.ImageBuild) (*api.ImageBuild, error)
	Get(ctx context.Context, orgId uuid.UUID, name string) (*api.ImageBuild, error)
	List(ctx context.Context, orgId uuid.UUID, listParams ListParams) (*api.ImageBuildList, error)
	Delete(ctx context.Context, orgId uuid.UUID, name string) error
	UpdateStatus(ctx context.Context, orgId uuid.UUID, imageBuild *api.ImageBuild) (*api.ImageBuild, error)
	UpdateLastSeen(ctx context.Context, orgId uuid.UUID, name string, timestamp time.Time) error
	ListByPhase(ctx context.Context, orgId uuid.UUID, phases []api.ImageBuildPhase) ([]api.ImageBuild, error)
}

type ImageBuildStore struct {
	dbHandler    *gorm.DB
	log          logrus.FieldLogger
	genericStore *GenericStore[*model.ImageBuild, model.ImageBuild, api.ImageBuild, api.ImageBuildList]
}

// Make sure we conform to ImageBuild interface
var _ ImageBuild = (*ImageBuildStore)(nil)

func NewImageBuild(db *gorm.DB, log logrus.FieldLogger) ImageBuild {
	genericStore := NewGenericStore[*model.ImageBuild, model.ImageBuild, api.ImageBuild, api.ImageBuildList](
		db,
		log,
		model.NewImageBuildFromApiResource,
		(*model.ImageBuild).ToApiResource,
		model.ImageBuildsToApiResource,
	)
	return &ImageBuildStore{dbHandler: db, log: log, genericStore: genericStore}
}

func (s *ImageBuildStore) getDB(ctx context.Context) *gorm.DB {
	return s.dbHandler.WithContext(ctx)
}

func (s *ImageBuildStore) InitialMigration(ctx context.Context) error {
	db := s.getDB(ctx)

	if err := db.AutoMigrate(&model.ImageBuild{}); err != nil {
		return err
	}

	// Create GIN index for ImageBuild labels
	if !db.Migrator().HasIndex(&model.ImageBuild{}, "idx_image_builds_labels") {
		if db.Dialector.Name() == "postgres" {
			if err := db.Exec("CREATE INDEX idx_image_builds_labels ON image_builds USING GIN (labels)").Error; err != nil {
				return err
			}
		} else {
			if err := db.Migrator().CreateIndex(&model.ImageBuild{}, "Labels"); err != nil {
				return err
			}
		}
	}

	// Create GIN index for ImageBuild annotations
	if !db.Migrator().HasIndex(&model.ImageBuild{}, "idx_image_builds_annotations") {
		if db.Dialector.Name() == "postgres" {
			if err := db.Exec("CREATE INDEX idx_image_builds_annotations ON image_builds USING GIN (annotations)").Error; err != nil {
				return err
			}
		} else {
			if err := db.Migrator().CreateIndex(&model.ImageBuild{}, "Annotations"); err != nil {
				return err
			}
		}
	}

	// Create index for status->phase queries
	if !db.Migrator().HasIndex(&model.ImageBuild{}, "idx_image_builds_org_phase") {
		if db.Dialector.Name() == "postgres" {
			if err := db.Exec("CREATE INDEX idx_image_builds_org_phase ON image_builds(org_id, (status->>'phase'))").Error; err != nil {
				return err
			}
		}
	}

	// Create index for status->lastSeen queries
	if !db.Migrator().HasIndex(&model.ImageBuild{}, "idx_image_builds_org_lastseen") {
		if db.Dialector.Name() == "postgres" {
			if err := db.Exec("CREATE INDEX idx_image_builds_org_lastseen ON image_builds(org_id, (status->>'lastSeen'))").Error; err != nil {
				return err
			}
		}
	}

	// Create index for spec->output->imageName queries
	if !db.Migrator().HasIndex(&model.ImageBuild{}, "idx_image_builds_output_imagename") {
		if db.Dialector.Name() == "postgres" {
			if err := db.Exec("CREATE INDEX idx_image_builds_output_imagename ON image_builds((spec->'output'->>'imageName'))").Error; err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *ImageBuildStore) Create(ctx context.Context, orgId uuid.UUID, resource *api.ImageBuild) (*api.ImageBuild, error) {
	return s.genericStore.Create(ctx, orgId, resource)
}

func (s *ImageBuildStore) Update(ctx context.Context, orgId uuid.UUID, resource *api.ImageBuild) (*api.ImageBuild, error) {
	newBuild, _, err := s.genericStore.Update(ctx, orgId, resource, nil, true, nil)
	return newBuild, err
}

func (s *ImageBuildStore) Get(ctx context.Context, orgId uuid.UUID, name string) (*api.ImageBuild, error) {
	return s.genericStore.Get(ctx, orgId, name)
}

func (s *ImageBuildStore) List(ctx context.Context, orgId uuid.UUID, listParams ListParams) (*api.ImageBuildList, error) {
	return s.genericStore.List(ctx, orgId, listParams)
}

func (s *ImageBuildStore) Delete(ctx context.Context, orgId uuid.UUID, name string) error {
	_, err := s.genericStore.Delete(ctx, model.ImageBuild{Resource: model.Resource{OrgID: orgId, Name: name}})
	return err
}

func (s *ImageBuildStore) UpdateStatus(ctx context.Context, orgId uuid.UUID, resource *api.ImageBuild) (*api.ImageBuild, error) {
	return s.genericStore.UpdateStatus(ctx, orgId, resource)
}

func (s *ImageBuildStore) UpdateLastSeen(ctx context.Context, orgId uuid.UUID, name string, timestamp time.Time) error {
	db := s.getDB(ctx)
	result := db.Model(&model.ImageBuild{}).
		Where("org_id = ? AND name = ?", orgId, name).
		Update("status", gorm.Expr("jsonb_set(status, '{lastSeen}', to_jsonb(?::text))", timestamp.Format(time.RFC3339)))
	if result.Error != nil {
		return ErrorFromGormError(result.Error)
	}
	if result.RowsAffected == 0 {
		return flterrors.ErrResourceNotFound
	}
	return nil
}

func (s *ImageBuildStore) ListByPhase(ctx context.Context, orgId uuid.UUID, phases []api.ImageBuildPhase) ([]api.ImageBuild, error) {
	db := s.getDB(ctx)
	var builds []model.ImageBuild

	phaseStrings := lo.Map(phases, func(p api.ImageBuildPhase, _ int) string {
		return string(p)
	})

	result := db.Model(&model.ImageBuild{}).
		Where("org_id = ?", orgId).
		Where("status->>'phase' IN ?", phaseStrings).
		Find(&builds)
	if result.Error != nil {
		return nil, ErrorFromGormError(result.Error)
	}

	apiBuilds := make([]api.ImageBuild, len(builds))
	for i, build := range builds {
		apiBuild, err := build.ToApiResource()
		if err != nil {
			return nil, err
		}
		apiBuilds[i] = *apiBuild
	}
	return apiBuilds, nil
}
