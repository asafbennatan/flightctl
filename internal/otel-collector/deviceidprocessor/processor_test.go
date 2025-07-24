package deviceidprocessor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

// Mock KV store for testing
type mockKVStore struct {
	data map[string][]byte
}

func (m *mockKVStore) Close() {}
func (m *mockKVStore) SetNX(ctx context.Context, key string, value []byte) (bool, error) {
	m.data[key] = value
	return true, nil
}
func (m *mockKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	if val, exists := m.data[key]; exists {
		return val, nil
	}
	return nil, nil
}
func (m *mockKVStore) GetOrSetNX(ctx context.Context, key string, value []byte) ([]byte, error) {
	if val, exists := m.data[key]; exists {
		return val, nil
	}
	m.data[key] = value
	return value, nil
}
func (m *mockKVStore) DeleteKeysForTemplateVersion(ctx context.Context, key string) error { return nil }
func (m *mockKVStore) DeleteAllKeys(ctx context.Context) error                            { return nil }
func (m *mockKVStore) PrintAllKeys(ctx context.Context)                                   {}

func TestProcessor_ProcessMetrics(t *testing.T) {
	tests := []struct {
		name              string
		deviceFingerprint string
		hasClient         bool
		hasKVStore        bool
		cachedFleet       string
		expectFleet       bool
	}{
		{
			name:              "no device fingerprint",
			deviceFingerprint: "",
			hasClient:         true,
			hasKVStore:        true,
			expectFleet:       false,
		},
		{
			name:              "with device fingerprint, cached fleet",
			deviceFingerprint: "test-device",
			hasClient:         true,
			hasKVStore:        true,
			cachedFleet:       "test-fleet",
			expectFleet:       true,
		},
		{
			name:              "with device fingerprint, no fleet",
			deviceFingerprint: "test-device",
			hasClient:         true,
			hasKVStore:        true,
			expectFleet:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test metrics
			md := pmetric.NewMetrics()
			rm := md.ResourceMetrics().AppendEmpty()
			sm := rm.ScopeMetrics().AppendEmpty()
			metric := sm.Metrics().AppendEmpty()
			metric.SetName("test_metric")
			gauge := metric.SetEmptyGauge()
			dp := gauge.DataPoints().AppendEmpty()
			dp.SetDoubleValue(1.0)

			// Create processor with mock KV store
			p := &deviceIdProcessor{
				flightctlClient: nil, // Will be nil for tests
				kvStore:         &mockKVStore{data: make(map[string][]byte)},
			}

			// Add cached fleet if specified
			if tt.cachedFleet != "" {
				cacheKey := "deviceid:fleet:" + tt.deviceFingerprint
				entry := fleetCacheEntry{
					FleetName: tt.cachedFleet,
					Timestamp: time.Now(),
				}
				cacheData, _ := json.Marshal(entry)
				p.kvStore.SetNX(context.Background(), cacheKey, cacheData)
			}

			// Create context with device fingerprint
			ctx := context.WithValue(context.Background(), "device_fingerprint", tt.deviceFingerprint)

			// Process metrics
			result, err := p.processMetrics(ctx, md)
			require.NoError(t, err)

			// Check results
			if tt.expectFleet {
				// Check that fleet_name attribute was added
				found := false
				for i := 0; i < result.ResourceMetrics().Len(); i++ {
					rm := result.ResourceMetrics().At(i)
					for j := 0; j < rm.ScopeMetrics().Len(); j++ {
						sm := rm.ScopeMetrics().At(j)
						for k := 0; k < sm.Metrics().Len(); k++ {
							metric := sm.Metrics().At(k)
							if metric.Type() == pmetric.MetricTypeGauge {
								for l := 0; l < metric.Gauge().DataPoints().Len(); l++ {
									dp := metric.Gauge().DataPoints().At(l)
									if val, exists := dp.Attributes().Get("fleet_name"); exists {
										assert.Equal(t, tt.cachedFleet, val.Str())
										found = true
									}
								}
							}
						}
					}
				}
				assert.True(t, found, "fleet_name attribute not found")
			} else {
				// Check that no fleet_name attribute was added
				for i := 0; i < result.ResourceMetrics().Len(); i++ {
					rm := result.ResourceMetrics().At(i)
					for j := 0; j < rm.ScopeMetrics().Len(); j++ {
						sm := rm.ScopeMetrics().At(j)
						for k := 0; k < sm.Metrics().Len(); k++ {
							metric := sm.Metrics().At(k)
							if metric.Type() == pmetric.MetricTypeGauge {
								for l := 0; l < metric.Gauge().DataPoints().Len(); l++ {
									dp := metric.Gauge().DataPoints().At(l)
									_, exists := dp.Attributes().Get("fleet_name")
									assert.False(t, exists, "fleet_name attribute should not be present")
								}
							}
						}
					}
				}
			}

			// Verify that device_id attribute is never added
			for i := 0; i < result.ResourceMetrics().Len(); i++ {
				rm := result.ResourceMetrics().At(i)
				for j := 0; j < rm.ScopeMetrics().Len(); j++ {
					sm := rm.ScopeMetrics().At(j)
					for k := 0; k < sm.Metrics().Len(); k++ {
						metric := sm.Metrics().At(k)
						if metric.Type() == pmetric.MetricTypeGauge {
							for l := 0; l < metric.Gauge().DataPoints().Len(); l++ {
								dp := metric.Gauge().DataPoints().At(l)
								_, exists := dp.Attributes().Get("device_id")
								assert.False(t, exists, "device_id attribute should never be present")
							}
						}
					}
				}
			}
		})
	}
}

func TestFleetCacheEntry_MarshalUnmarshal(t *testing.T) {
	entry := fleetCacheEntry{
		FleetName: "test-fleet",
		Timestamp: time.Now(),
	}

	// Marshal
	data, err := json.Marshal(entry)
	require.NoError(t, err)

	// Unmarshal
	var unmarshaled fleetCacheEntry
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	// Verify
	assert.Equal(t, entry.FleetName, unmarshaled.FleetName)
	assert.WithinDuration(t, entry.Timestamp, unmarshaled.Timestamp, time.Second)
}
