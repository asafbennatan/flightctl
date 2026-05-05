package alert_exporter

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/flightctl/flightctl/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDiskHandler_Name(t *testing.T) {
	require := require.New(t)
	handler := NewDiskHandler()
	require.Equal(DiskHandlerName, handler.Name())
}

func TestDiskHandler_EventReasons(t *testing.T) {
	require := require.New(t)
	handler := NewDiskHandler()
	reasons := handler.EventReasons()

	require.Len(reasons, 3)
	require.Contains(reasons, domain.EventReasonDeviceDiskCritical)
	require.Contains(reasons, domain.EventReasonDeviceDiskWarning)
	require.Contains(reasons, domain.EventReasonDeviceDiskNormal)
}

func TestDiskHandler_Process(t *testing.T) {
	ctx := context.Background()
	handler := NewDiskHandler()
	orgID := uuid.New()
	now := time.Now()

	tests := []struct {
		name               string
		events             []EnrichedEvent
		prevState          json.RawMessage
		expectedCritical   int
		expectedWarning    int
		expectedFleetless  int
		checkAlertContains string
	}{
		{
			name: "When a device in a fleet has disk critical it should generate fleet alert",
			events: []EnrichedEvent{
				makeEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceDiskCritical, now),
			},
			prevState:          nil,
			expectedCritical:   1,
			expectedWarning:    0,
			expectedFleetless:  0,
			checkAlertContains: "fleet-a",
		},
		{
			name: "When a device in a fleet has disk warning it should generate fleet warning alert",
			events: []EnrichedEvent{
				makeEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceDiskWarning, now),
			},
			prevState:          nil,
			expectedCritical:   0,
			expectedWarning:    1,
			expectedFleetless:  0,
			checkAlertContains: "fleet-a",
		},
		{
			name: "When a fleetless device has disk critical it should generate device alert",
			events: []EnrichedEvent{
				makeEnrichedEvent(orgID, "device-1", "", domain.EventReasonDeviceDiskCritical, now),
			},
			prevState:          nil,
			expectedCritical:   0,
			expectedWarning:    0,
			expectedFleetless:  1,
			checkAlertContains: "device-1",
		},
		{
			name: "When multiple devices in same fleet have disk critical it should aggregate count",
			events: []EnrichedEvent{
				makeEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceDiskCritical, now),
				makeEnrichedEvent(orgID, "device-2", "fleet-a", domain.EventReasonDeviceDiskCritical, now),
			},
			prevState:          nil,
			expectedCritical:   1,
			expectedWarning:    0,
			expectedFleetless:  0,
			checkAlertContains: "2 devices",
		},
		{
			name: "When device returns to normal it should clear alert",
			events: []EnrichedEvent{
				makeEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceDiskNormal, now),
			},
			prevState:          mustMarshal(t, makeBinaryStateWithDevice(orgID.String(), "fleet-a", "device-1", string(domain.EventReasonDeviceDiskCritical), now)),
			expectedCritical:   0,
			expectedWarning:    0,
			expectedFleetless:  0,
			checkAlertContains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			changes, newState, err := handler.Process(ctx, tt.events, tt.prevState, nil)
			require.NoError(err)
			require.NotNil(newState)

			criticalCount := 0
			warningCount := 0
			fleetlessCount := 0

			for _, alert := range changes.NewAlerts {
				if alert.Fleet == "" {
					fleetlessCount++
				} else if alert.Severity == "critical" {
					criticalCount++
				} else if alert.Severity == "warning" {
					warningCount++
				}
			}

			require.Equal(tt.expectedCritical, criticalCount, "critical alert count mismatch")
			require.Equal(tt.expectedWarning, warningCount, "warning alert count mismatch")
			require.Equal(tt.expectedFleetless, fleetlessCount, "fleetless alert count mismatch")

			if tt.checkAlertContains != "" {
				found := false
				for _, alert := range changes.NewAlerts {
					if contains(alert.Summary, tt.checkAlertContains) || alert.Fleet == tt.checkAlertContains || alert.DeviceName == tt.checkAlertContains {
						found = true
						break
					}
				}
				require.True(found, "expected alert to contain %q", tt.checkAlertContains)
			}
		})
	}
}
