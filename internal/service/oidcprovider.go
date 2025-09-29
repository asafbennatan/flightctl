package service

import (
	"context"
	"errors"
	"fmt"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/store/selector"
	"github.com/google/uuid"
)

func (h *ServiceHandler) CreateOIDCProvider(ctx context.Context, oidcProvider api.OIDCProvider) (*api.OIDCProvider, api.Status) {
	orgId := getOrgIdFromContext(ctx)

	// don't set fields that are managed by the service
	oidcProvider.Status = nil
	NilOutManagedObjectMetaProperties(&oidcProvider.Metadata)

	if errs := oidcProvider.Validate(); len(errs) > 0 {
		return nil, api.StatusBadRequest(errors.Join(errs...).Error())
	}

	result, err := h.store.OIDCProvider().Create(ctx, orgId, &oidcProvider, h.callbackOIDCProviderUpdated)
	return result, StoreErrorToApiStatus(err, true, api.OIDCProviderKind, oidcProvider.Metadata.Name)
}

func (h *ServiceHandler) ListOIDCProviders(ctx context.Context, params api.ListOIDCProvidersParams) (*api.OIDCProviderList, api.Status) {
	orgId := getOrgIdFromContext(ctx)

	listParams, status := prepareListParams(params.Continue, params.LabelSelector, params.FieldSelector, params.Limit)
	if status != api.StatusOK() {
		return nil, status
	}

	result, err := h.store.OIDCProvider().List(ctx, orgId, *listParams)
	if err == nil {
		return result, api.StatusOK()
	}

	var se *selector.SelectorError

	switch {
	case selector.AsSelectorError(err, &se):
		return nil, api.StatusBadRequest(se.Error())
	default:
		return nil, api.StatusInternalServerError(err.Error())
	}
}

func (h *ServiceHandler) GetOIDCProvider(ctx context.Context, name string) (*api.OIDCProvider, api.Status) {
	orgId := getOrgIdFromContext(ctx)

	result, err := h.store.OIDCProvider().Get(ctx, orgId, name)
	return result, StoreErrorToApiStatus(err, false, api.OIDCProviderKind, &name)
}

func (h *ServiceHandler) ReplaceOIDCProvider(ctx context.Context, name string, oidcProvider api.OIDCProvider) (*api.OIDCProvider, api.Status) {
	orgId := getOrgIdFromContext(ctx)

	// don't overwrite fields that are managed by the service for external requests
	if !IsInternalRequest(ctx) {
		oidcProvider.Status = nil
		NilOutManagedObjectMetaProperties(&oidcProvider.Metadata)
	}

	if errs := oidcProvider.Validate(); len(errs) > 0 {
		return nil, api.StatusBadRequest(errors.Join(errs...).Error())
	}
	if name != *oidcProvider.Metadata.Name {
		return nil, api.StatusBadRequest("resource name specified in metadata does not match name in path")
	}

	result, created, err := h.store.OIDCProvider().CreateOrUpdate(ctx, orgId, &oidcProvider, h.callbackOIDCProviderUpdated)
	return result, StoreErrorToApiStatus(err, created, api.OIDCProviderKind, &name)
}

func (h *ServiceHandler) DeleteOIDCProvider(ctx context.Context, name string) api.Status {
	orgId := getOrgIdFromContext(ctx)

	err := h.store.OIDCProvider().Delete(ctx, orgId, name, h.callbackOIDCProviderDeleted)
	return StoreErrorToApiStatus(err, false, api.OIDCProviderKind, &name)
}

func (h *ServiceHandler) GetOIDCProviderByIssuer(ctx context.Context, issuer string) (*api.OIDCProvider, api.Status) {
	orgId := getOrgIdFromContext(ctx)

	// Use field selector to find provider by issuer
	fieldSelector := fmt.Sprintf("spec.issuer=%s", issuer)
	listParams, status := prepareListParams(nil, nil, &fieldSelector, nil)
	if status != api.StatusOK() {
		return nil, status
	}

	result, err := h.store.OIDCProvider().List(ctx, orgId, *listParams)
	if err != nil {
		var se *selector.SelectorError
		switch {
		case selector.AsSelectorError(err, &se):
			return nil, api.StatusBadRequest(se.Error())
		default:
			return nil, api.StatusInternalServerError(err.Error())
		}
	}

	// Return the first matching provider (should be only one)
	if result != nil && len(result.Items) > 0 {
		return &result.Items[0], api.StatusOK()
	}

	return nil, api.StatusNotFound("OIDC provider not found for issuer: " + issuer)
}

// callbackOIDCProviderUpdated is the OIDC provider-specific callback that handles OIDC provider update events
func (h *ServiceHandler) callbackOIDCProviderUpdated(ctx context.Context, resourceKind api.ResourceKind, orgId uuid.UUID, name string, oldResource, newResource interface{}, created bool, err error) {
	h.eventHandler.HandleOIDCProviderUpdatedEvents(ctx, resourceKind, orgId, name, oldResource, newResource, created, err)
}

// callbackOIDCProviderDeleted is the OIDC provider-specific callback that handles OIDC provider deletion events
func (h *ServiceHandler) callbackOIDCProviderDeleted(ctx context.Context, resourceKind api.ResourceKind, orgId uuid.UUID, name string, oldResource, newResource interface{}, created bool, err error) {
	h.eventHandler.HandleOIDCProviderDeletedEvents(ctx, resourceKind, orgId, name, oldResource, newResource, created, err)
}
