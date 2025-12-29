package store

import (
	"context"

	flightctlstore "github.com/flightctl/flightctl/internal/store"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// Store is the imagebuilder-specific store interface
type Store interface {
	ImageBuild() flightctlstore.ImageBuild
	RunMigrations(ctx context.Context) error
	Ping() error
	Close() error
}

// ImageBuilderStore is the concrete implementation of the imagebuilder Store interface
type ImageBuilderStore struct {
	imageBuild flightctlstore.ImageBuild
	db         *gorm.DB
	log        logrus.FieldLogger
}

// NewStore creates a new imagebuilder store
func NewStore(db *gorm.DB, log logrus.FieldLogger) Store {
	return &ImageBuilderStore{
		imageBuild: flightctlstore.NewImageBuild(db, log),
		db:         db,
		log:        log,
	}
}

// ImageBuild returns the ImageBuild store
func (s *ImageBuilderStore) ImageBuild() flightctlstore.ImageBuild {
	return s.imageBuild
}

// RunMigrations runs the imagebuilder-specific migrations
func (s *ImageBuilderStore) RunMigrations(ctx context.Context) error {
	return s.imageBuild.InitialMigration(ctx)
}

// Ping checks database connectivity
func (s *ImageBuilderStore) Ping() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}

// Close closes the database connection
func (s *ImageBuilderStore) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
