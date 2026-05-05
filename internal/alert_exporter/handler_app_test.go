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

func TestAppHandler_Name(t *testing.T) {
	require := require.New(t)
	handler := NewAppHandler()
	require.Equal(AppHandlerName, handler.Name())
}

func TestAppHandler_EventReasons(t *testing.T) {
	require := require.New(t)
	handler := NewAppHandler()
	reasons := handler.EventReasons()

	require.Len(reasons, 3)
	require.Contains(reasons, domain.EventReasonDeviceApplicationError)
	require.Contains(reasons, domain.EventReasonDeviceApplicationDegraded)
	require.Contains(reasons, domain.EventReasonDeviceApplicationHealthy)
}

func TestAppHandler_Process(t *testing.T) {
	ctx := context.Background()
	handler := NewAppHandler()
	orgID := uuid.New()
	now := time.Now()

	tests := []struct {
		name               string
		events             []EnrichedEvent
		prevState          json.RawMessage
		expectedError      int
		expectedDegraded   int
		expectedFleetless  int
		checkAlertContains string
	}{
		{
			name: "When a device in a fleet has app error it should generate fleet alert",
			events: []EnrichedEvent{
				makeEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceApplicationError, now),
			},
			prevState:          nil,
			expectedError:      1,
			expectedDegraded:   0,
			expectedFleetless:  0,
			checkAlertContains: "fleet-a",
		},
		{
			name: "When a device in a fleet has app degraded it should generate fleet alert",
			events: []EnrichedEvent{
				makeEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceApplicationDegraded, now),
			},
			prevState:          nil,
			expectedError:      0,
			expectedDegraded:   1,
			expectedFleetless:  0,
			checkAlertContains: "fleet-a",
		},
		{
			name: "When a fleetless device has app error it should generate device alert",
			events: []EnrichedEvent{
				makeEnrichedEvent(orgID, "device-1", "", domain.EventReasonDeviceApplicationError, now),
			},
			prevState:          nil,
			expectedError:      0,
			expectedDegraded:   0,
			expectedFleetless:  1,
			checkAlertContains: "device-1",
		},
		{
			name: "When multiple devices in same fleet have app error it should aggregate count",
			events: []EnrichedEvent{
				makeEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceApplicationError, now),
				makeEnrichedEvent(orgID, "device-2", "fleet-a", domain.EventReasonDeviceApplicationError, now),
			},
			prevState:          nil,
			expectedError:      1,
			expectedDegraded:   0,
			expectedFleetless:  0,
			checkAlertContains: "2 devices",
		},
		{
			name: "When device app becomes healthy it should clear alert",
			events: []EnrichedEvent{
				makeEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceApplicationHealthy, now),
			},
			prevState:          mustMarshal(t, makeBinaryStateWithDevice(orgID.String(), "fleet-a", "device-1", string(domain.EventReasonDeviceApplicationError), now)),
			expectedError:      0,
			expectedDegraded:   0,
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

			errorCount := 0
			degradedCount := 0
			fleetlessCount := 0

			for _, alert := range changes.NewAlerts {
				if alert.Fleet == "" {
					fleetlessCount++
				} else if alert.Severity == "critical" {
					errorCount++
				} else if alert.Severity == "warning" {
					degradedCount++
				}
			}

			require.Equal(tt.expectedError, errorCount, "error alert count mismatch")
			require.Equal(tt.expectedDegraded, degradedCount, "degraded alert count mismatch")
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
