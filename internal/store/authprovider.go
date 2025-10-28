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

type AuthProvider interface {
	InitialMigration(ctx context.Context) error

	Create(ctx context.Context, orgId uuid.UUID, authProvider *api.AuthProvider, eventCallback EventCallback) (*api.AuthProvider, error)
	Update(ctx context.Context, orgId uuid.UUID, authProvider *api.AuthProvider, eventCallback EventCallback) (*api.AuthProvider, error)
	CreateOrUpdate(ctx context.Context, orgId uuid.UUID, authProvider *api.AuthProvider, eventCallback EventCallback) (*api.AuthProvider, bool, error)
	Get(ctx context.Context, orgId uuid.UUID, name string) (*api.AuthProvider, error)
	List(ctx context.Context, orgId uuid.UUID, listParams ListParams) (*api.AuthProviderList, error)
	Delete(ctx context.Context, orgId uuid.UUID, name string, eventCallback EventCallback) error
	UpdateStatus(ctx context.Context, orgId uuid.UUID, resource *api.AuthProvider, eventCallback EventCallback) (*api.AuthProvider, error)
	GetAuthProviderByIssuerAndClientId(ctx context.Context, orgId uuid.UUID, issuer string, clientId string) (*api.AuthProvider, error)
	GetAuthProviderByAuthorizationUrl(ctx context.Context, orgId uuid.UUID, authorizationUrl string) (*api.AuthProvider, error)

	// Used by domain metrics
	Count(ctx context.Context, orgId uuid.UUID, listParams ListParams) (int64, error)
	CountByOrg(ctx context.Context, orgId *uuid.UUID) ([]CountByOrgResult, error)
}

type AuthProviderStore struct {
	dbHandler           *gorm.DB
	log                 logrus.FieldLogger
	genericStore        *GenericStore[*model.AuthProvider, model.AuthProvider, api.AuthProvider, api.AuthProviderList]
	eventCallbackCaller EventCallbackCaller
}

// Make sure we conform to AuthProvider interface
var _ AuthProvider = (*AuthProviderStore)(nil)

func NewAuthProvider(db *gorm.DB, log logrus.FieldLogger) AuthProvider {
	genericStore := NewGenericStore[*model.AuthProvider, model.AuthProvider, api.AuthProvider, api.AuthProviderList](
		db,
		log,
		model.NewAuthProviderFromApiResource,
		(*model.AuthProvider).ToApiResource,
		model.AuthProvidersToApiResource,
	)

	return &AuthProviderStore{
		dbHandler:           db,
		log:                 log,
		genericStore:        genericStore,
		eventCallbackCaller: CallEventCallback(api.AuthProviderKind, log),
	}
}

func (s *AuthProviderStore) InitialMigration(ctx context.Context) error {
	return s.dbHandler.WithContext(ctx).AutoMigrate(&model.AuthProvider{})
}

func (s *AuthProviderStore) Create(ctx context.Context, orgId uuid.UUID, resource *api.AuthProvider, eventCallback EventCallback) (*api.AuthProvider, error) {
	provider, err := s.genericStore.Create(ctx, orgId, resource)
	s.eventCallbackCaller(ctx, eventCallback, orgId, lo.FromPtr(resource.Metadata.Name), nil, provider, true, err)
	return provider, err
}

func (s *AuthProviderStore) Update(ctx context.Context, orgId uuid.UUID, resource *api.AuthProvider, eventCallback EventCallback) (*api.AuthProvider, error) {
	newProvider, oldProvider, err := s.genericStore.Update(ctx, orgId, resource, nil, true, nil)
	s.eventCallbackCaller(ctx, eventCallback, orgId, lo.FromPtr(resource.Metadata.Name), oldProvider, newProvider, false, err)
	return newProvider, err
}

func (s *AuthProviderStore) CreateOrUpdate(ctx context.Context, orgId uuid.UUID, resource *api.AuthProvider, eventCallback EventCallback) (*api.AuthProvider, bool, error) {
	newProvider, oldProvider, created, err := s.genericStore.CreateOrUpdate(ctx, orgId, resource, nil, true, nil)
	s.eventCallbackCaller(ctx, eventCallback, orgId, lo.FromPtr(resource.Metadata.Name), oldProvider, newProvider, created, err)
	return newProvider, created, err
}

func (s *AuthProviderStore) Get(ctx context.Context, orgId uuid.UUID, name string) (*api.AuthProvider, error) {
	return s.genericStore.Get(ctx, orgId, name)
}

func (s *AuthProviderStore) List(ctx context.Context, orgId uuid.UUID, listParams ListParams) (*api.AuthProviderList, error) {
	return s.genericStore.List(ctx, orgId, listParams)
}

func (s *AuthProviderStore) getDB(ctx context.Context) *gorm.DB {
	return s.dbHandler.WithContext(ctx)
}

func (s *AuthProviderStore) Delete(ctx context.Context, orgId uuid.UUID, name string, eventCallback EventCallback) error {
	deleted, err := s.genericStore.Delete(ctx, model.AuthProvider{Resource: model.Resource{OrgID: orgId, Name: name}})
	if deleted && eventCallback != nil {
		s.eventCallbackCaller(ctx, eventCallback, orgId, name, nil, nil, false, nil)
	}
	return err
}

func (s *AuthProviderStore) UpdateStatus(ctx context.Context, orgId uuid.UUID, resource *api.AuthProvider, eventCallback EventCallback) (*api.AuthProvider, error) {
	return s.genericStore.UpdateStatus(ctx, orgId, resource)
}

func (s *AuthProviderStore) Count(ctx context.Context, orgId uuid.UUID, listParams ListParams) (int64, error) {
	query, err := ListQuery(&model.AuthProvider{}).Build(ctx, s.getDB(ctx), orgId, listParams)
	if err != nil {
		return 0, err
	}
	var authProvidersCount int64
	if err := query.Count(&authProvidersCount).Error; err != nil {
		return 0, ErrorFromGormError(err)
	}
	return authProvidersCount, nil
}

func (s *AuthProviderStore) CountByOrg(ctx context.Context, orgId *uuid.UUID) ([]CountByOrgResult, error) {
	var query *gorm.DB
	var err error

	if orgId != nil {
		query, err = ListQuery(&model.AuthProvider{}).BuildNoOrder(ctx, s.getDB(ctx), *orgId, ListParams{})
	} else {
		// When orgId is nil, we don't filter by org_id
		query = s.getDB(ctx).Model(&model.AuthProvider{})
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

func (s *AuthProviderStore) GetAuthProviderByIssuerAndClientId(ctx context.Context, orgId uuid.UUID, issuer string, clientId string) (*api.AuthProvider, error) {
	query := s.getDB(ctx).Model(&model.AuthProvider{}).Where("org_id = ? AND spec->>'issuer' = ? AND spec->>'clientId' = ?", orgId, issuer, clientId)
	var authProvider model.AuthProvider
	if err := query.First(&authProvider).Error; err != nil {
		return nil, ErrorFromGormError(err)
	}
	apiResource, err := authProvider.ToApiResource()
	return apiResource, err
}

func (s *AuthProviderStore) GetAuthProviderByAuthorizationUrl(ctx context.Context, orgId uuid.UUID, authorizationUrl string) (*api.AuthProvider, error) {
	query := s.getDB(ctx).Model(&model.AuthProvider{}).Where("org_id = ? AND spec->>'authorizationUrl' = ?", orgId, authorizationUrl)
	var authProvider model.AuthProvider
	if err := query.First(&authProvider).Error; err != nil {
		return nil, ErrorFromGormError(err)
	}
	apiResource, err := authProvider.ToApiResource()
	return apiResource, err
}
