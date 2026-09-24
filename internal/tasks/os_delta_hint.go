package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/containers/image/v5/docker/reference"
	"github.com/flightctl/flightctl/internal/config"
	"github.com/flightctl/flightctl/internal/delta_worker/model"
	delta "github.com/flightctl/flightctl/internal/delta_worker/store/deltageneration"
	"github.com/flightctl/flightctl/internal/domain"
	"github.com/flightctl/flightctl/internal/flterrors"
	"github.com/flightctl/flightctl/internal/kvstore"
	"github.com/flightctl/flightctl/internal/oci"
	deviceservice "github.com/flightctl/flightctl/internal/service/device"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
)

const generationMemoTTL = 15 * time.Minute

type generationLookup interface {
	GetDeltaGeneration(ctx context.Context, key delta.GenerationKey, opts ...delta.GenerationGetOption) (*model.DeltaGeneration, error)
}

type generationMemo struct {
	Missing   bool    `json:"missing,omitempty"`
	Status    string  `json:"status,omitempty"`
	DeltaRef  *string `json:"deltaRef,omitempty"`
	SizeBytes *int64  `json:"sizeBytes,omitempty"`
}

func FormatIECBytes(n int64) string {
	if n <= 0 {
		return "0 KiB"
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	val := float64(n) / 1024
	unit := 0
	for unit < len(units)-1 && val >= 1024 {
		val /= 1024
		unit++
	}
	rounded := int64(val + 0.5)
	if rounded == 0 && unit > 0 {
		val *= 1024
		unit--
		rounded = int64(val + 0.5)
	}
	if rounded == 0 {
		rounded = 1
	}
	return fmt.Sprintf("%d %s", rounded, units[unit])
}

func (t DeviceRenderLogic) WithDeltaLookup(lookup delta.Store) DeviceRenderLogic {
	t.deltaLookup = lookup
	return t
}

func ImageRepositoryFromRef(imageRef string) (string, error) {
	named, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return "", err
	}
	return named.Name(), nil
}

func deltaWriteSpec(cfg *config.Config) *domain.OciRepoSpec {
	if cfg == nil || cfg.DeltaGeneration == nil || cfg.DeltaGeneration.DefaultRepository == nil {
		return nil
	}
	defaultRepository, err := cfg.DeltaGeneration.DefaultRepository.OciRepoSpec()
	if err != nil {
		return nil
	}
	return defaultRepository
}

func (t *DeviceRenderLogic) resolveTargetDigest(ctx context.Context, osImage string) (string, error) {
	return oci.CachedImageDigest(ctx, t.kvStore, t.orgId, osImage, func(ctx context.Context) (string, error) {
		return oci.InspectImageDigest(ctx, osImage, deltaWriteSpec(t.cfg))
	})
}

func hintFromGeneration(gen *model.DeltaGeneration, fallbackSize *int64) (deltaImage *string, sizeIEC *string) {
	var sizeBytes *int64
	if gen != nil && gen.SizeBytes != nil {
		sizeBytes = gen.SizeBytes
	} else {
		sizeBytes = fallbackSize
	}
	if sizeBytes != nil {
		sizeIEC = lo.ToPtr(FormatIECBytes(*sizeBytes))
	}
	if gen != nil && gen.Status == model.DeltaGenerationSucceeded && gen.DeltaRef != nil && *gen.DeltaRef != "" {
		deltaImage = gen.DeltaRef
	}
	return deltaImage, sizeIEC
}

func (t *DeviceRenderLogic) resolveOSDeltaHint(ctx context.Context, device *domain.Device, rendered RenderedSpec) *deviceservice.RenderedOSHints {
	if rendered.OsImage == "" {
		return nil
	}
	var fallback *int64
	if t.osManifestSize != nil {
		fallback, _ = t.osManifestSize(ctx, rendered.OsImage)
	}
	repo, err := ImageRepositoryFromRef(rendered.OsImage)
	if err != nil || t.deltaLookup == nil {
		_, size := hintFromGeneration(nil, fallback)
		if size == nil {
			return nil
		}
		return &deviceservice.RenderedOSHints{UpdatedSize: size}
	}
	src := ""
	if device != nil && device.Status != nil {
		src = device.Status.Os.ImageDigest
	}
	if src == "" {
		t.log.Infof("os delta hint skipped device=%s/%s reason=empty-source-digest osImage=%q repo=%s",
			t.orgId, t.event.InvolvedObject.Name, rendered.OsImage, repo)
		_, size := hintFromGeneration(nil, fallback)
		if size == nil {
			return nil
		}
		return &deviceservice.RenderedOSHints{UpdatedSize: size}
	}
	tgt, err := t.resolveTargetDigest(ctx, rendered.OsImage)
	if err != nil {
		t.log.Infof("os delta hint skipped device=%s/%s reason=inspect-target-digest osImage=%q err=%v",
			t.orgId, t.event.InvolvedObject.Name, rendered.OsImage, err)
		_, size := hintFromGeneration(nil, fallback)
		if size == nil {
			return nil
		}
		return &deviceservice.RenderedOSHints{UpdatedSize: size}
	}
	if tgt == "" {
		t.log.Infof("os delta hint skipped device=%s/%s reason=empty-target-digest osImage=%q repo=%s",
			t.orgId, t.event.InvolvedObject.Name, rendered.OsImage, repo)
		_, size := hintFromGeneration(nil, fallback)
		if size == nil {
			return nil
		}
		return &deviceservice.RenderedOSHints{UpdatedSize: size}
	}
	key := delta.GenerationKey{
		OrgID:           t.orgId,
		ImageRepository: repo,
		SourceDigest:    src,
		TargetDigest:    tgt,
	}
	t.log.Infof("os delta hint query device=%s/%s repo=%s sourceDigest=%s targetDigest=%s osImage=%s",
		t.orgId, t.event.InvolvedObject.Name, repo, src, tgt, rendered.OsImage)
	gen, err := t.lookupOSDeltaGeneration(ctx, key)
	if err != nil {
		t.log.Warnf("os delta hint lookup failed device=%s/%s repo=%s sourceDigest=%s targetDigest=%s: %v",
			t.orgId, t.event.InvolvedObject.Name, repo, src, tgt, err)
		_, size := hintFromGeneration(nil, fallback)
		if size == nil {
			return nil
		}
		return &deviceservice.RenderedOSHints{UpdatedSize: size}
	}
	img, size := hintFromGeneration(gen, fallback)
	if gen == nil {
		t.log.Infof("os delta hint miss device=%s/%s repo=%s sourceDigest=%s targetDigest=%s",
			t.orgId, t.event.InvolvedObject.Name, repo, src, tgt)
	} else {
		deltaRef := ""
		if gen.DeltaRef != nil {
			deltaRef = *gen.DeltaRef
		}
		t.log.Infof("os delta hint hit device=%s/%s repo=%s sourceDigest=%s targetDigest=%s status=%s deltaRef=%s sizeBytes=%v",
			t.orgId, t.event.InvolvedObject.Name, repo, src, tgt, gen.Status, deltaRef, gen.SizeBytes)
	}
	if img == nil && size == nil {
		return nil
	}
	return &deviceservice.RenderedOSHints{DeltaImage: img, UpdatedSize: size}
}

func (t *DeviceRenderLogic) lookupOSDeltaGeneration(ctx context.Context, key delta.GenerationKey) (*model.DeltaGeneration, error) {
	statusOption := delta.WithStatus(model.DeltaGenerationSucceeded)
	if t.event.Reason != domain.EventReasonDeltaGenerationCompleted {
		return lookupCachedGeneration(ctx, t.kvStore, t.deltaLookup, key, "", statusOption)
	}

	// Completion changes the answer for a previously cached miss, so invalidate
	// that memo and query the generation store directly before rendering again.
	cacheKey := generationMemoKey(key, "")
	cacheInvalidated := true
	if t.kvStore != nil {
		if err := t.kvStore.Delete(ctx, cacheKey); err != nil {
			cacheInvalidated = false
			t.log.WithError(err).Warnf("failed invalidating OS delta hint memo for device %s/%s", t.orgId, t.event.InvolvedObject.Name)
		}
	}
	if t.deltaLookup == nil {
		return nil, nil
	}
	gen, err := t.deltaLookup.GetDeltaGeneration(ctx, key, statusOption)
	if err != nil {
		if errors.Is(err, flterrors.ErrResourceNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if gen != nil && cacheInvalidated {
		cacheGenerationMemo(ctx, t.kvStore, key, "", generationMemo{
			Status:    gen.Status,
			DeltaRef:  gen.DeltaRef,
			SizeBytes: gen.SizeBytes,
		}, t.log)
	}
	return gen, nil
}

func generationMemoKey(key delta.GenerationKey, ref string) string {
	return fmt.Sprintf("deltaHint/%s/%s/%s/%s/%s", key.OrgID, key.ImageRepository, key.SourceDigest, key.TargetDigest, ref)
}

func lookupCachedGeneration(ctx context.Context, kv kvstore.KVStore, store generationLookup, key delta.GenerationKey, ref string, opts ...delta.GenerationGetOption) (*model.DeltaGeneration, error) {
	if kv != nil {
		raw, err := kv.Get(ctx, generationMemoKey(key, ref))
		if err == nil && len(raw) > 0 {
			var memo generationMemo
			if err := json.Unmarshal(raw, &memo); err == nil {
				return generationFromMemo(key, memo), nil
			}
		}
	}

	if store == nil {
		return nil, nil
	}
	gen, err := store.GetDeltaGeneration(ctx, key, opts...)
	if err != nil {
		if errors.Is(err, flterrors.ErrResourceNotFound) {
			cacheGenerationMemo(ctx, kv, key, ref, generationMemo{Missing: true}, logrus.StandardLogger())
			return nil, nil
		}
		return nil, err
	}
	if gen != nil {
		cacheGenerationMemo(ctx, kv, key, ref, generationMemo{
			Status:    gen.Status,
			DeltaRef:  gen.DeltaRef,
			SizeBytes: gen.SizeBytes,
		}, logrus.StandardLogger())
	}
	return gen, nil
}

func cacheGenerationMemo(ctx context.Context, kv kvstore.KVStore, key delta.GenerationKey, ref string, memo generationMemo, log logrus.FieldLogger) {
	if err := writeGenerationMemo(ctx, kv, key, ref, memo); err != nil {
		log.WithError(err).Warnf("failed caching delta generation memo org=%s repo=%s sourceDigest=%s targetDigest=%s", key.OrgID, key.ImageRepository, key.SourceDigest, key.TargetDigest)
	}
}

func generationFromMemo(key delta.GenerationKey, memo generationMemo) *model.DeltaGeneration {
	if memo.Missing {
		return nil
	}
	return &model.DeltaGeneration{
		OrgID:           key.OrgID,
		ImageRepository: key.ImageRepository,
		SourceDigest:    key.SourceDigest,
		TargetDigest:    key.TargetDigest,
		Status:          memo.Status,
		DeltaRef:        memo.DeltaRef,
		SizeBytes:       memo.SizeBytes,
	}
}

func writeGenerationMemo(ctx context.Context, kv kvstore.KVStore, key delta.GenerationKey, ref string, memo generationMemo) error {
	if kv == nil {
		return nil
	}
	raw, err := json.Marshal(memo)
	if err != nil {
		return err
	}
	cacheKey := generationMemoKey(key, ref)
	if _, err := kv.SetNX(ctx, cacheKey, raw); err != nil {
		return err
	}
	if err := kv.SetExpire(ctx, cacheKey, generationMemoTTL); err != nil {
		if deleteErr := kv.Delete(ctx, cacheKey); deleteErr != nil {
			return errors.Join(fmt.Errorf("set delta generation memo expiration: %w", err), fmt.Errorf("delete memo without expiration: %w", deleteErr))
		}
		return fmt.Errorf("set delta generation memo expiration; deleted memo without expiration: %w", err)
	}
	return nil
}
