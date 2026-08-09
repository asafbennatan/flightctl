package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/flightctl/flightctl/internal/domain"
	"github.com/flightctl/flightctl/internal/flterrors"
	"github.com/flightctl/flightctl/internal/store"
	"github.com/flightctl/flightctl/internal/store/model"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"gorm.io/gorm"
)

// FleetApplyFunc mutates current in place. It runs on every CAS attempt.
type FleetApplyFunc func(current *domain.Fleet) error

// errMutateSkipWrite tells Mutate to return success without writing (e.g. conditions unchanged).
var errMutateSkipWrite = errors.New("mutate skip write")

// Mutate loads the named fleet (or uses previous on the first attempt), runs apply,
// and persists under resource_version CAS.
func (s *FleetStore) Mutate(ctx context.Context, orgId uuid.UUID, name string, previous *domain.Fleet, apply FleetApplyFunc, eventCallback store.EventCallback) (*domain.Fleet, error) {
	var (
		updated  *domain.Fleet
		oldForCb *domain.Fleet
		err      error
	)
	attempt := 0
	retryErr := retryUpdate(func() (bool, error) {
		var usePrevious *domain.Fleet
		if attempt == 0 {
			usePrevious = previous
		}
		attempt++
		var retry bool
		var before *domain.Fleet
		retry, updated, before, err = s.mutateOnce(ctx, orgId, name, usePrevious, apply)
		if before != nil {
			oldForCb = before
		}
		return retry, err
	})
	s.callEventCallback(ctx, eventCallback, orgId, name, oldForCb, updated, false, retryErr)
	return updated, retryErr
}

func (s *FleetStore) mutateOnce(ctx context.Context, orgId uuid.UUID, name string, previous *domain.Fleet, apply FleetApplyFunc) (retry bool, updated, before *domain.Fleet, err error) {
	var existing model.Fleet
	var current *domain.Fleet

	if previous != nil {
		current, err = cloneFleet(previous)
		if err != nil {
			return false, nil, nil, err
		}
		before, err = cloneFleet(previous)
		if err != nil {
			return false, nil, nil, err
		}
		fromPrev, convErr := model.NewFleetFromApiResource(previous)
		if convErr != nil {
			return false, nil, nil, convErr
		}
		fromPrev.OrgID = orgId
		existing = *fromPrev
	} else {
		existing = model.Fleet{Resource: model.Resource{OrgID: orgId, Name: name}}
		result := s.getDB(ctx).Take(&existing)
		if result.Error != nil {
			return false, nil, nil, store.ErrorFromGormError(result.Error)
		}
		current, err = existing.ToApiResource()
		if err != nil {
			return false, nil, nil, err
		}
		before, err = cloneFleet(current)
		if err != nil {
			return false, nil, nil, err
		}
	}

	if err := apply(current); err != nil {
		if errors.Is(err, errMutateSkipWrite) {
			return false, current, before, nil
		}
		return false, nil, before, err
	}

	retry, err = s.persistFleetCAS(ctx, &existing, current)
	if err != nil {
		return retry, nil, before, err
	}
	return false, current, before, nil
}

func (s *FleetStore) persistFleetCAS(ctx context.Context, existing *model.Fleet, current *domain.Fleet) (bool, error) {
	fromAPI, err := model.NewFleetFromApiResource(current)
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
		"labels":           model.MakeJSONMap(fromAPI.Labels),
		"annotations":      model.MakeJSONMap(fromAPI.Annotations),
		"owner":            fromAPI.Owner,
		"generation":       generation,
		"status":           fromAPI.Status,
		"resource_version": gorm.Expr("resource_version + 1"),
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

func cloneFleet(f *domain.Fleet) (*domain.Fleet, error) {
	if f == nil {
		return nil, nil
	}
	b, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	var out domain.Fleet
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func applyFleetResourceUpdate(current, resource *domain.Fleet, fieldsToUnset []string) {
	current.Spec = resource.Spec
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
