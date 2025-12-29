package service

import (
	"errors"

	api "github.com/flightctl/flightctl/api/v1beta1/imagebuilder"
	"github.com/flightctl/flightctl/internal/flterrors"
	"github.com/flightctl/flightctl/internal/store"
	"github.com/flightctl/flightctl/internal/store/selector"
	"github.com/flightctl/flightctl/internal/util"
	"github.com/samber/lo"
)

const (
	MaxRecordsPerListRequest = 1000
	ImageBuildKind           = "ImageBuild"
)

// NilOutManagedObjectMetaProperties clears fields that are managed by the service
func NilOutManagedObjectMetaProperties(om *api.ObjectMeta) {
	if om == nil {
		return
	}
	om.Generation = nil
	om.Owner = nil
	om.Annotations = nil
	om.CreationTimestamp = nil
	om.DeletionTimestamp = nil
}

// prepareListParams prepares list parameters from request query parameters
func prepareListParams(cont *string, lSelector *string, fSelector *string, limit *int32) (*store.ListParams, api.Status) {
	cnt, err := store.ParseContinueString(cont)
	if err != nil {
		return nil, StatusBadRequest("failed to parse continue parameter: " + err.Error())
	}

	var fieldSelector *selector.FieldSelector
	if fSelector != nil {
		if fieldSelector, err = selector.NewFieldSelector(*fSelector); err != nil {
			return nil, StatusBadRequest("failed to parse field selector: " + err.Error())
		}
	}

	var labelSelector *selector.LabelSelector
	if lSelector != nil {
		if labelSelector, err = selector.NewLabelSelector(*lSelector); err != nil {
			return nil, StatusBadRequest("failed to parse label selector: " + err.Error())
		}
	}

	listParams := &store.ListParams{
		Limit:         int(lo.FromPtr(limit)),
		Continue:      cnt,
		FieldSelector: fieldSelector,
		LabelSelector: labelSelector,
	}
	if listParams.Limit == 0 {
		listParams.Limit = MaxRecordsPerListRequest
	} else if listParams.Limit > MaxRecordsPerListRequest {
		return nil, StatusBadRequest("limit cannot exceed 1000")
	} else if listParams.Limit < 0 {
		return nil, StatusBadRequest("limit cannot be negative")
	}

	return listParams, StatusOK()
}

var badRequestErrors = map[error]bool{
	flterrors.ErrResourceIsNil:                 true,
	flterrors.ErrResourceNameIsNil:             true,
	flterrors.ErrIllegalResourceVersionFormat:  true,
	flterrors.ErrFieldSelectorSyntax:           true,
	flterrors.ErrFieldSelectorParseFailed:      true,
	flterrors.ErrFieldSelectorUnknownSelector:  true,
	flterrors.ErrLabelSelectorSyntax:           true,
	flterrors.ErrLabelSelectorParseFailed:      true,
	flterrors.ErrAnnotationSelectorSyntax:      true,
	flterrors.ErrAnnotationSelectorParseFailed: true,
}

var conflictErrors = map[error]bool{
	flterrors.ErrUpdatingResourceWithOwnerNotAllowed: true,
	flterrors.ErrDuplicateName:                       true,
	flterrors.ErrNoRowsUpdated:                       true,
	flterrors.ErrResourceVersionConflict:             true,
	flterrors.ErrResourceOwnerIsNil:                  true,
}

// StoreErrorToApiStatus converts a store error to an API status
func StoreErrorToApiStatus(err error, created bool, kind string, name *string) api.Status {
	if err == nil {
		if created {
			return StatusCreated()
		}
		return StatusOK()
	}

	switch {
	case errors.Is(err, flterrors.ErrResourceNotFound):
		return StatusResourceNotFound(kind, util.DefaultIfNil(name, "none"))
	case badRequestErrors[err]:
		return StatusBadRequest(err.Error())
	case conflictErrors[err]:
		return StatusConflict(err.Error())
	default:
		return StatusInternalServerError(err.Error())
	}
}

// StatusOK returns a 200 OK status
func StatusOK() api.Status {
	return api.Status{Code: lo.ToPtr(int32(200))}
}

// StatusCreated returns a 201 Created status
func StatusCreated() api.Status {
	return api.Status{Code: lo.ToPtr(int32(201))}
}

// StatusBadRequest returns a 400 Bad Request status with the given message
func StatusBadRequest(message string) api.Status {
	return api.Status{Code: lo.ToPtr(int32(400)), Message: lo.ToPtr(message)}
}

// StatusNotFound returns a 404 Not Found status with the given message
func StatusNotFound(message string) api.Status {
	return api.Status{Code: lo.ToPtr(int32(404)), Message: lo.ToPtr(message)}
}

// StatusResourceNotFound returns a 404 status for a specific resource
func StatusResourceNotFound(kind string, name string) api.Status {
	return api.Status{Code: lo.ToPtr(int32(404)), Message: lo.ToPtr(kind + " " + name + " not found")}
}

// StatusConflict returns a 409 Conflict status with the given message
func StatusConflict(message string) api.Status {
	return api.Status{Code: lo.ToPtr(int32(409)), Message: lo.ToPtr(message)}
}

// StatusInternalServerError returns a 500 Internal Server Error status with the given message
func StatusInternalServerError(message string) api.Status {
	return api.Status{Code: lo.ToPtr(int32(500)), Message: lo.ToPtr(message)}
}

// IsStatusOK returns true if the status code is in the 2xx range
func IsStatusOK(status api.Status) bool {
	code := lo.FromPtr(status.Code)
	return code >= 200 && code < 300
}
