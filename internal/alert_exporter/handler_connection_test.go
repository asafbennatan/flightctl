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

func TestConnectionHandler_Name(t *testing.T) {
	require := require.New(t)
	handler := NewConnectionHandler()
	require.Equal(ConnectionHandlerName, handler.Name())
}

func TestConnectionHandler_EventReasons(t *testing.T) {
	require := require.New(t)
	handler := NewConnectionHandler()
	reasons := handler.EventReasons()

	require.Len(reasons, 2)
	require.Contains(reasons, domain.EventReasonDeviceDisconnected)
	require.Contains(reasons, domain.EventReasonDeviceConnected)
}

func TestConnectionHandler_Process(t *testing.T) {
	ctx := context.Background()
	handler := NewConnectionHandler()
	orgID := uuid.New()
	now := time.Now()

	tests := []struct {
		name               string
		events             []EnrichedEvent
		prevState          json.RawMessage
		expectedAlerts     int
		expectedFleetless  int
		checkAlertContains string
	}{
		{
			name: "When a device in a fleet disconnects it should generate fleet alert",
			events: []EnrichedEvent{
				makeEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceDisconnected, now),
			},
			prevState:          nil,
			expectedAlerts:     1,
			expectedFleetless:  0,
			checkAlertContains: "fleet-a",
		},
		{
			name: "When a fleetless device disconnects it should generate device alert",
			events: []EnrichedEvent{
				makeEnrichedEvent(orgID, "device-1", "", domain.EventReasonDeviceDisconnected, now),
			},
			prevState:          nil,
			expectedAlerts:     0,
			expectedFleetless:  1,
			checkAlertContains: "device-1",
		},
		{
			name: "When multiple devices in same fleet disconnect it should aggregate count",
			events: []EnrichedEvent{
				makeEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceDisconnected, now),
				makeEnrichedEvent(orgID, "device-2", "fleet-a", domain.EventReasonDeviceDisconnected, now),
				makeEnrichedEvent(orgID, "device-3", "fleet-a", domain.EventReasonDeviceDisconnected, now),
			},
			prevState:          nil,
			expectedAlerts:     1,
			expectedFleetless:  0,
			checkAlertContains: "3 disconnected",
		},
		{
			name: "When device reconnects it should clear alert",
			events: []EnrichedEvent{
				makeEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceConnected, now),
			},
			prevState:          mustMarshal(t, makeBinaryStateWithDevice(orgID.String(), "fleet-a", "device-1", string(domain.EventReasonDeviceDisconnected), now)),
			expectedAlerts:     0,
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

			fleetAlertCount := 0
			fleetlessCount := 0

			for _, alert := range changes.NewAlerts {
				if alert.Fleet == "" {
					fleetlessCount++
				} else {
					fleetAlertCount++
				}
			}

			require.Equal(tt.expectedAlerts, fleetAlertCount, "fleet alert count mismatch")
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
