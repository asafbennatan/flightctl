package device

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/flightctl/flightctl/internal/domain"
	"github.com/flightctl/flightctl/internal/flterrors"
	"github.com/flightctl/flightctl/internal/store"
	"github.com/flightctl/flightctl/internal/store/model"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"gorm.io/gorm"
)

// DeviceApplyFunc mutates current in place. It runs on every CAS attempt against a
// freshly resolved device (or a clone of previous on the first attempt).
type DeviceApplyFunc func(current *domain.Device) error

// deviceWriteExtras carries columns not represented on domain.Device (rendered blobs).
type deviceWriteExtras struct {
	renderedConfig       *string
	renderedApplications *string
	renderedOsImage      *string
	setRenderTimestamp   bool
	serviceConditions    *model.JSONField[model.ServiceConditions]
	skipWrite            bool
}

type deviceApplyInternal func(current *domain.Device, extras *deviceWriteExtras) error

// Mutate loads the named device (or uses previous on the first attempt), runs apply,
// and persists under resource_version CAS. On conflict/deadlock it reloads via Get and
// retries; previous is never reused after the first attempt.
func (s *DeviceStore) Mutate(ctx context.Context, orgId uuid.UUID, name string, previous *domain.Device, apply DeviceApplyFunc, eventCallback store.EventCallback) (*domain.Device, error) {
	return s.mutateInternal(ctx, orgId, name, previous, func(current *domain.Device, _ *deviceWriteExtras) error {
		return apply(current)
	}, eventCallback)
}

func (s *DeviceStore) mutateInternal(ctx context.Context, orgId uuid.UUID, name string, previous *domain.Device, apply deviceApplyInternal, eventCallback store.EventCallback) (*domain.Device, error) {
	var (
		updated  *domain.Device
		oldForCb *domain.Device
		err      error
	)
	attempt := 0
	retryErr := retryUpdate(func() (bool, error) {
		var usePrevious *domain.Device
		if attempt == 0 {
			usePrevious = previous
		}
		attempt++
		var retry bool
		var before *domain.Device
		retry, updated, before, err = s.mutateOnce(ctx, orgId, name, usePrevious, apply)
		if before != nil {
			oldForCb = before
		}
		return retry, err
	})
	s.callEventCallback(ctx, eventCallback, orgId, name, oldForCb, updated, false, retryErr)
	return updated, retryErr
}

func (s *DeviceStore) mutateOnce(ctx context.Context, orgId uuid.UUID, name string, previous *domain.Device, apply deviceApplyInternal) (retry bool, updated, before *domain.Device, err error) {
	var existing model.Device
	var current *domain.Device

	if previous != nil {
		current, err = cloneDevice(previous)
		if err != nil {
			return false, nil, nil, err
		}
		before, err = cloneDevice(previous)
		if err != nil {
			return false, nil, nil, err
		}
		fromPrev, convErr := model.NewDeviceFromApiResource(previous)
		if convErr != nil {
			return false, nil, nil, convErr
		}
		fromPrev.OrgID = orgId
		existing = *fromPrev
	} else {
		existing = model.Device{Resource: model.Resource{OrgID: orgId, Name: name}}
		result := s.getDB(ctx).Take(&existing)
		if result.Error != nil {
			return false, nil, nil, store.ErrorFromGormError(result.Error)
		}
		current, err = existing.ToApiResource()
		if err != nil {
			return false, nil, nil, err
		}
		before, err = cloneDevice(current)
		if err != nil {
			return false, nil, nil, err
		}
	}

	extras := &deviceWriteExtras{}
	if err := apply(current, extras); err != nil {
		return false, nil, before, err
	}
	if extras.skipWrite {
		return false, current, before, nil
	}

	retry, err = s.persistDeviceCAS(ctx, &existing, current, extras)
	if err != nil {
		return retry, nil, before, err
	}
	return false, current, before, nil
}

func (s *DeviceStore) persistDeviceCAS(ctx context.Context, existing *model.Device, current *domain.Device, extras *deviceWriteExtras) (bool, error) {
	fromAPI, err := model.NewDeviceFromApiResource(current)
	if err != nil {
		return false, err
	}
	fromAPI.OrgID = existing.OrgID

	sameSpec := true
	if existing.Spec != nil && fromAPI.Spec != nil {
		sameSpec = fromAPI.HasSameSpecAs(existing)
	} else if (existing.Spec == nil) != (fromAPI.Spec == nil) {
		sameSpec = false
	}
	generation := lo.FromPtr(existing.Generation)
	if !sameSpec {
		generation++
	}

	updates := map[string]interface{}{
		"spec":             fromAPI.Spec,
		"alias":            fromAPI.Alias,
		"labels":           model.MakeJSONMap(fromAPI.Labels),
		"annotations":      model.MakeJSONMap(fromAPI.Annotations),
		"owner":            fromAPI.Owner,
		"generation":       generation,
		"status":           fromAPI.Status,
		"resource_version": gorm.Expr("resource_version + 1"),
	}
	if extras.serviceConditions != nil {
		updates["service_conditions"] = extras.serviceConditions
	} else {
		updates["service_conditions"] = fromAPI.ServiceConditions
	}
	if extras.renderedConfig != nil {
		raw := json.RawMessage(*extras.renderedConfig)
		updates["rendered_config"] = model.MakeJSONField(raw)
	}
	if extras.renderedApplications != nil {
		apps := *extras.renderedApplications
		if strings.TrimSpace(apps) == "" {
			apps = "[]"
		}
		raw := json.RawMessage(apps)
		updates["rendered_applications"] = model.MakeJSONField(raw)
	}
	if extras.renderedOsImage != nil {
		updates["rendered_os"] = model.MakeJSONField(domain.DeviceOsSpec{Image: *extras.renderedOsImage})
	}
	if extras.setRenderTimestamp {
		updates["render_timestamp"] = time.Now()
	}

	result := s.getDB(ctx).Model(existing).Where("resource_version = ?", lo.FromPtr(existing.ResourceVersion)).Updates(updates)
	updateErr := store.ErrorFromGormError(result.Error)
	if updateErr != nil {
		return strings.Contains(updateErr.Error(), "deadlock"), updateErr
	}
	if result.RowsAffected == 0 {
		return true, flterrors.ErrNoRowsUpdated
	}

	current.Metadata.Generation = lo.ToPtr(generation)
	if existing.ResourceVersion != nil {
		current.Metadata.ResourceVersion = lo.ToPtr(strconv.FormatInt(*existing.ResourceVersion+1, 10))
	}
	return false, nil
}

func cloneDevice(d *domain.Device) (*domain.Device, error) {
	if d == nil {
		return nil, nil
	}
	b, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	var out domain.Device
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func parseDeviceResourceVersion(d *domain.Device) (*int64, error) {
	if d == nil || d.Metadata.ResourceVersion == nil {
		return lo.ToPtr(int64(0)), nil
	}
	i, err := strconv.ParseInt(*d.Metadata.ResourceVersion, 10, 64)
	if err != nil {
		return nil, flterrors.ErrIllegalResourceVersionFormat
	}
	return &i, nil
}

// applyDeviceResourceUpdate copies mutable fields from resource onto current, preserving
// nil metadata (unless listed in fieldsToUnset) to match generic update semantics.
func applyDeviceResourceUpdate(current, resource *domain.Device, fieldsToUnset []string) {
	if resource.Spec != nil {
		current.Spec = resource.Spec
	}
	if resource.Status != nil {
		current.Status = resource.Status
	}
	unset := func(field string) bool { return lo.Contains(fieldsToUnset, field) }

	if resource.Metadata.Labels != nil || unset("labels") {
		current.Metadata.Labels = resource.Metadata.Labels
	}
	if resource.Metadata.Annotations != nil || unset("annotations") {
		current.Metadata.Annotations = resource.Metadata.Annotations
	}
	if resource.Metadata.Owner != nil || unset("owner") {
		current.Metadata.Owner = resource.Metadata.Owner
	}
	if resource.Metadata.Generation != nil {
		current.Metadata.Generation = resource.Metadata.Generation
	}
	if resource.Metadata.ResourceVersion != nil {
		current.Metadata.ResourceVersion = resource.Metadata.ResourceVersion
	}
}
