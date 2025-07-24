package deviceidprocessor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	apiclient "github.com/flightctl/flightctl/internal/api/client"
	"github.com/flightctl/flightctl/internal/kvstore"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

// Cache entry structure for fleet names
type fleetCacheEntry struct {
	FleetName string    `json:"fleet_name"`
	Timestamp time.Time `json:"timestamp"`
}

type deviceIdProcessor struct {
	flightctlClient *apiclient.ClientWithResponses
	kvStore         kvstore.KVStore
}

func (p *deviceIdProcessor) processMetrics(ctx context.Context, md pmetric.Metrics) (pmetric.Metrics, error) {
	val := ctx.Value("device_fingerprint")
	if valStr, ok := val.(string); ok && valStr != "" {
		// Get the device's owner/fleet from cache or API
		fleetName, err := p.getDeviceFleetWithCache(ctx, valStr)
		if err != nil {
			// If we can't get fleet info, don't add any attributes
			return md, nil
		} else if fleetName != "" {
			// Only add fleet name attribute if device has a fleet owner
			p.addFleetNameAttribute(md, fleetName)
		}
		// If no fleet owner, don't add any attributes
	}
	return md, nil
}

func (p *deviceIdProcessor) getDeviceFleetWithCache(ctx context.Context, deviceFingerprint string) (string, error) {
	// Create cache key for this device
	cacheKey := fmt.Sprintf("deviceid:fleet:%s", deviceFingerprint)

	// Try to get from cache first
	cachedData, err := p.kvStore.Get(ctx, cacheKey)
	if err != nil {
		// Log cache error but continue with API call
		fmt.Printf("Cache get error for device %s: %v\n", deviceFingerprint, err)
	} else if cachedData != nil {
		// Parse cached data
		var entry fleetCacheEntry
		if err := json.Unmarshal(cachedData, &entry); err != nil {
			fmt.Printf("Failed to unmarshal cached data for device %s: %v\n", deviceFingerprint, err)
		} else {
			// Check if cache entry is still valid (24 hour TTL)
			if time.Since(entry.Timestamp) < 24*time.Hour {
				return entry.FleetName, nil
			}
		}
	}

	// Cache miss or expired, fetch from API
	fleetName, err := p.getDeviceFleetFromAPI(ctx, deviceFingerprint)
	if err != nil {
		return "", err
	}

	// Cache the result
	entry := fleetCacheEntry{
		FleetName: fleetName,
		Timestamp: time.Now(),
	}

	cacheData, err := json.Marshal(entry)
	if err != nil {
		fmt.Printf("Failed to marshal cache data for device %s: %v\n", deviceFingerprint, err)
		return fleetName, nil // Return fleet name even if caching fails
	}

	// Store in cache (use SetNX to avoid overwriting if another process already cached it)
	_, err = p.kvStore.SetNX(ctx, cacheKey, cacheData)
	if err != nil {
		fmt.Printf("Failed to cache fleet data for device %s: %v\n", deviceFingerprint, err)
	}

	return fleetName, nil
}

func (p *deviceIdProcessor) getDeviceFleetFromAPI(ctx context.Context, deviceFingerprint string) (string, error) {
	// Check if client is available
	if p.flightctlClient == nil {
		return "", fmt.Errorf("flightctl client not initialized")
	}

	// Get the device from the API
	resp, err := p.flightctlClient.GetDeviceWithResponse(ctx, deviceFingerprint)
	if err != nil {
		return "", fmt.Errorf("failed to get device: %w", err)
	}

	if resp.JSON200 == nil {
		return "", fmt.Errorf("device not found: %s", deviceFingerprint)
	}

	device := resp.JSON200

	// Check if device has an owner (fleet)
	if device.Metadata.Owner != nil && *device.Metadata.Owner != "" {
		// Parse owner in format "Fleet/fleet-name"
		parts := strings.Split(*device.Metadata.Owner, "/")
		if len(parts) == 2 && parts[0] == "Fleet" {
			return parts[1], nil
		}
	}

	// No fleet owner
	return "", nil
}

func (p *deviceIdProcessor) addFleetNameAttribute(md pmetric.Metrics, fleetName string) {
	for i := 0; i < md.ResourceMetrics().Len(); i++ {
		rm := md.ResourceMetrics().At(i)
		for j := 0; j < rm.ScopeMetrics().Len(); j++ {
			sm := rm.ScopeMetrics().At(j)
			for k := 0; k < sm.Metrics().Len(); k++ {
				metric := sm.Metrics().At(k)
				switch metric.Type() {
				case pmetric.MetricTypeGauge:
					for l := 0; l < metric.Gauge().DataPoints().Len(); l++ {
						dp := metric.Gauge().DataPoints().At(l)
						dp.Attributes().PutStr("fleet_name", fleetName)
					}
				case pmetric.MetricTypeSum:
					for l := 0; l < metric.Sum().DataPoints().Len(); l++ {
						dp := metric.Sum().DataPoints().At(l)
						dp.Attributes().PutStr("fleet_name", fleetName)
					}
				case pmetric.MetricTypeHistogram:
					for l := 0; l < metric.Histogram().DataPoints().Len(); l++ {
						dp := metric.Histogram().DataPoints().At(l)
						dp.Attributes().PutStr("fleet_name", fleetName)
					}
				case pmetric.MetricTypeExponentialHistogram:
					for l := 0; l < metric.ExponentialHistogram().DataPoints().Len(); l++ {
						dp := metric.ExponentialHistogram().DataPoints().At(l)
						dp.Attributes().PutStr("fleet_name", fleetName)
					}
				case pmetric.MetricTypeSummary:
					for l := 0; l < metric.Summary().DataPoints().Len(); l++ {
						dp := metric.Summary().DataPoints().At(l)
						dp.Attributes().PutStr("fleet_name", fleetName)
					}
				}
			}
		}
	}
}
