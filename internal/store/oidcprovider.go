package store

import (
	"context"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/store/model"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type OIDCProvider interface {
	InitialMigration(ctx context.Context) error

	Create(ctx context.Context, orgId uuid.UUID, oidcProvider *api.OIDCProvider, eventCallback EventCallback) (*api.OIDCProvider, error)
	Update(ctx context.Context, orgId uuid.UUID, oidcProvider *api.OIDCProvider, eventCallback EventCallback) (*api.OIDCProvider, error)
	CreateOrUpdate(ctx context.Context, orgId uuid.UUID, oidcProvider *api.OIDCProvider, eventCallback EventCallback) (*api.OIDCProvider, bool, error)
	Get(ctx context.Context, orgId uuid.UUID, name string) (*api.OIDCProvider, error)
	List(ctx context.Context, orgId uuid.UUID, listParams ListParams) (*api.OIDCProviderList, error)
	Delete(ctx context.Context, orgId uuid.UUID, name string, eventCallback EventCallback) error
	UpdateStatus(ctx context.Context, orgId uuid.UUID, resource *api.OIDCProvider, eventCallback EventCallback) (*api.OIDCProvider, error)

	// Used by domain metrics
	Count(ctx context.Context, orgId uuid.UUID, listParams ListParams) (int64, error)
	CountByOrg(ctx context.Context, orgId *uuid.UUID) ([]CountByOrgResult, error)
}

type OIDCProviderStore struct {
	dbHandler           *gorm.DB
	log                 logrus.FieldLogger
	genericStore        *GenericStore[*model.OIDCProvider, model.OIDCProvider, api.OIDCProvider, api.OIDCProviderList]
	eventCallbackCaller EventCallbackCaller
}

// Make sure we conform to OIDCProvider interface
var _ OIDCProvider = (*OIDCProviderStore)(nil)

func NewOIDCProvider(db *gorm.DB, log logrus.FieldLogger) OIDCProvider {
	genericStore := NewGenericStore[*model.OIDCProvider, model.OIDCProvider, api.OIDCProvider, api.OIDCProviderList](
		db,
		log,
		model.NewOIDCProviderFromApiResource,
		(*model.OIDCProvider).ToApiResource,
		model.OIDCProvidersToApiResource,
	)

	return &OIDCProviderStore{
		dbHandler:           db,
		log:                 log,
		genericStore:        genericStore,
		eventCallbackCaller: CallEventCallback(api.OIDCProviderKind, log),
	}
}

func (s *OIDCProviderStore) InitialMigration(ctx context.Context) error {
	return s.dbHandler.WithContext(ctx).AutoMigrate(&model.OIDCProvider{})
}

func (s *OIDCProviderStore) Create(ctx context.Context, orgId uuid.UUID, resource *api.OIDCProvider, eventCallback EventCallback) (*api.OIDCProvider, error) {
	provider, err := s.genericStore.Create(ctx, orgId, resource)
	s.eventCallbackCaller(ctx, eventCallback, orgId, lo.FromPtr(resource.Metadata.Name), nil, provider, true, err)
	return provider, err
}

func (s *OIDCProviderStore) Update(ctx context.Context, orgId uuid.UUID, resource *api.OIDCProvider, eventCallback EventCallback) (*api.OIDCProvider, error) {
	newProvider, oldProvider, err := s.genericStore.Update(ctx, orgId, resource, nil, true, nil)
	s.eventCallbackCaller(ctx, eventCallback, orgId, lo.FromPtr(resource.Metadata.Name), oldProvider, newProvider, false, err)
	return newProvider, err
}

func (s *OIDCProviderStore) CreateOrUpdate(ctx context.Context, orgId uuid.UUID, resource *api.OIDCProvider, eventCallback EventCallback) (*api.OIDCProvider, bool, error) {
	newProvider, oldProvider, created, err := s.genericStore.CreateOrUpdate(ctx, orgId, resource, nil, true, nil)
	s.eventCallbackCaller(ctx, eventCallback, orgId, lo.FromPtr(resource.Metadata.Name), oldProvider, newProvider, created, err)
	return newProvider, created, err
}

func (s *OIDCProviderStore) Get(ctx context.Context, orgId uuid.UUID, name string) (*api.OIDCProvider, error) {
	return s.genericStore.Get(ctx, orgId, name)
}

func (s *OIDCProviderStore) List(ctx context.Context, orgId uuid.UUID, listParams ListParams) (*api.OIDCProviderList, error) {
	return s.genericStore.List(ctx, orgId, listParams)
}

func (s *OIDCProviderStore) getDB(ctx context.Context) *gorm.DB {
	return s.dbHandler.WithContext(ctx)
}

func (s *OIDCProviderStore) Delete(ctx context.Context, orgId uuid.UUID, name string, eventCallback EventCallback) error {
	deleted, err := s.genericStore.Delete(ctx, model.OIDCProvider{Resource: model.Resource{OrgID: orgId, Name: name}})
	if deleted && eventCallback != nil {
		s.eventCallbackCaller(ctx, eventCallback, orgId, name, nil, nil, false, nil)
	}
	return err
}

func (s *OIDCProviderStore) UpdateStatus(ctx context.Context, orgId uuid.UUID, resource *api.OIDCProvider, eventCallback EventCallback) (*api.OIDCProvider, error) {
	return s.genericStore.UpdateStatus(ctx, orgId, resource)
}

func (s *OIDCProviderStore) Count(ctx context.Context, orgId uuid.UUID, listParams ListParams) (int64, error) {
	query, err := ListQuery(&model.OIDCProvider{}).Build(ctx, s.getDB(ctx), orgId, listParams)
	if err != nil {
		return 0, err
	}
	var oidcProvidersCount int64
	if err := query.Count(&oidcProvidersCount).Error; err != nil {
		return 0, ErrorFromGormError(err)
	}
	return oidcProvidersCount, nil
}

func (s *OIDCProviderStore) CountByOrg(ctx context.Context, orgId *uuid.UUID) ([]CountByOrgResult, error) {
	var query *gorm.DB
	var err error

	if orgId != nil {
		query, err = ListQuery(&model.OIDCProvider{}).BuildNoOrder(ctx, s.getDB(ctx), *orgId, ListParams{})
	} else {
		// When orgId is nil, we don't filter by org_id
		query = s.getDB(ctx).Model(&model.OIDCProvider{})
	}

	if err != nil {
		return nil, err
	}

	var results []CountByOrgResult
	if err := query.Select("org_id, COUNT(*) as count").Group("org_id").Scan(&results).Error; err != nil {
		return nil, ErrorFromGormError(err)
	}

	return results, nil
}
