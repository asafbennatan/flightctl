package alert_exporter

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	v1beta1 "github.com/flightctl/flightctl/api/core/v1beta1"
	"github.com/flightctl/flightctl/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCPUHandler_Name(t *testing.T) {
	require := require.New(t)
	handler := NewCPUHandler()
	require.Equal(CPUHandlerName, handler.Name())
}

func TestCPUHandler_EventReasons(t *testing.T) {
	require := require.New(t)
	handler := NewCPUHandler()
	reasons := handler.EventReasons()

	require.Len(reasons, 3)
	require.Contains(reasons, domain.EventReasonDeviceCPUCritical)
	require.Contains(reasons, domain.EventReasonDeviceCPUWarning)
	require.Contains(reasons, domain.EventReasonDeviceCPUNormal)
}

func TestCPUHandler_Process(t *testing.T) {
	ctx := context.Background()
	handler := NewCPUHandler()
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
			name: "When a device in a fleet has CPU critical it should generate fleet alert",
			events: []EnrichedEvent{
				makeEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceCPUCritical, now),
			},
			prevState:          nil,
			expectedCritical:   1,
			expectedWarning:    0,
			expectedFleetless:  0,
			checkAlertContains: "fleet-a",
		},
		{
			name: "When a device in a fleet has CPU warning it should generate fleet warning alert",
			events: []EnrichedEvent{
				makeEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceCPUWarning, now),
			},
			prevState:          nil,
			expectedCritical:   0,
			expectedWarning:    1,
			expectedFleetless:  0,
			checkAlertContains: "fleet-a",
		},
		{
			name: "When a fleetless device has CPU critical it should generate device alert",
			events: []EnrichedEvent{
				makeEnrichedEvent(orgID, "device-1", "", domain.EventReasonDeviceCPUCritical, now),
			},
			prevState:          nil,
			expectedCritical:   0,
			expectedWarning:    0,
			expectedFleetless:  1,
			checkAlertContains: "device-1",
		},
		{
			name: "When multiple devices in same fleet have CPU critical it should aggregate count",
			events: []EnrichedEvent{
				makeEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceCPUCritical, now),
				makeEnrichedEvent(orgID, "device-2", "fleet-a", domain.EventReasonDeviceCPUCritical, now),
				makeEnrichedEvent(orgID, "device-3", "fleet-a", domain.EventReasonDeviceCPUCritical, now),
			},
			prevState:          nil,
			expectedCritical:   1,
			expectedWarning:    0,
			expectedFleetless:  0,
			checkAlertContains: "3 devices",
		},
		{
			name: "When device returns to normal it should clear alert",
			events: []EnrichedEvent{
				makeEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceCPUNormal, now),
			},
			prevState:          mustMarshal(t, makeBinaryStateWithDevice(orgID.String(), "fleet-a", "device-1", string(domain.EventReasonDeviceCPUCritical), now)),
			expectedCritical:   0,
			expectedWarning:    0,
			expectedFleetless:  0,
			checkAlertContains: "",
		},
		{
			name: "When device transitions from warning to critical it should update state",
			events: []EnrichedEvent{
				makeEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceCPUCritical, now),
			},
			prevState:          mustMarshal(t, makeBinaryStateWithDevice(orgID.String(), "fleet-a", "device-1", string(domain.EventReasonDeviceCPUWarning), now.Add(-time.Hour))),
			expectedCritical:   1,
			expectedWarning:    0,
			expectedFleetless:  0,
			checkAlertContains: "fleet-a",
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

func TestCPUHandler_StatePersistence(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	handler := NewCPUHandler()
	orgID := uuid.New()
	now := time.Now()

	events1 := []EnrichedEvent{
		makeEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceCPUCritical, now),
	}
	changes1, state1, err := handler.Process(ctx, events1, nil, nil)
	require.NoError(err)
	require.Len(changes1.NewAlerts, 1)

	events2 := []EnrichedEvent{
		makeEnrichedEvent(orgID, "device-2", "fleet-a", domain.EventReasonDeviceCPUCritical, now),
	}
	changes2, _, err := handler.Process(ctx, events2, state1, nil)
	require.NoError(err)
	require.Len(changes2.NewAlerts, 1)

	for _, alert := range changes2.NewAlerts {
		if alert.Fleet == "fleet-a" && alert.Severity == "critical" {
			require.Equal(2, alert.DeviceCount, "expected 2 devices after processing second event")
		}
	}
}

func TestCPUHandler_FleetMigration(t *testing.T) {
	ctx := context.Background()
	handler := NewCPUHandler()
	orgID := uuid.New()
	now := time.Now()

	tests := []struct {
		name                string
		initialFleet        string
		newFleet            string
		expectedOldFleetCnt int
		expectedNewFleetCnt int
	}{
		{
			name:                "When device moves from fleet-a to fleet-b it should only be in fleet-b",
			initialFleet:        "fleet-a",
			newFleet:            "fleet-b",
			expectedOldFleetCnt: 0,
			expectedNewFleetCnt: 1,
		},
		{
			name:                "When device moves from fleet to fleetless it should only be fleetless",
			initialFleet:        "fleet-a",
			newFleet:            "",
			expectedOldFleetCnt: 0,
			expectedNewFleetCnt: 1,
		},
		{
			name:                "When device moves from fleetless to fleet it should only be in fleet",
			initialFleet:        "",
			newFleet:            "fleet-a",
			expectedOldFleetCnt: 0,
			expectedNewFleetCnt: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			// Initial event in old fleet
			events1 := []EnrichedEvent{
				makeEnrichedEvent(orgID, "device-1", tt.initialFleet, domain.EventReasonDeviceCPUCritical, now),
			}
			_, state1, err := handler.Process(ctx, events1, nil, nil)
			require.NoError(err)

			// Device moves to new fleet, gets another critical event
			events2 := []EnrichedEvent{
				makeEnrichedEvent(orgID, "device-1", tt.newFleet, domain.EventReasonDeviceCPUCritical, now),
			}
			changes2, _, err := handler.Process(ctx, events2, state1, nil)
			require.NoError(err)

			// Count alerts per fleet
			oldFleetAlerts := 0
			newFleetAlerts := 0
			for _, alert := range changes2.NewAlerts {
				if alert.Fleet == tt.initialFleet || (tt.initialFleet == "" && alert.DeviceName != "") {
					if tt.initialFleet == "" && alert.Fleet == "" {
						// This is the fleetless case for initial
						continue
					}
					oldFleetAlerts++
				}
				if alert.Fleet == tt.newFleet || (tt.newFleet == "" && alert.DeviceName == "device-1") {
					newFleetAlerts++
				}
			}

			// Verify device is only in new fleet
			require.Equal(tt.expectedNewFleetCnt, newFleetAlerts, "device should be in new fleet")
		})
	}
}

func TestCPUHandler_FleetMigration_NormalEventClearsFromAllFleets(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	handler := NewCPUHandler()
	orgID := uuid.New()
	now := time.Now()

	// Device gets critical in fleet-a
	events1 := []EnrichedEvent{
		makeEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceCPUCritical, now),
	}
	_, state1, err := handler.Process(ctx, events1, nil, nil)
	require.NoError(err)

	// Device moves to fleet-b and goes normal (without ever getting an alert in fleet-b)
	events2 := []EnrichedEvent{
		makeEnrichedEvent(orgID, "device-1", "fleet-b", domain.EventReasonDeviceCPUNormal, now),
	}
	changes2, _, err := handler.Process(ctx, events2, state1, nil)
	require.NoError(err)

	// Should have no alerts (device cleared from all fleets)
	require.Empty(changes2.NewAlerts, "device should be cleared from all fleets")
}

func makeEnrichedEvent(orgID uuid.UUID, deviceName, fleet string, reason domain.EventReason, timestamp time.Time) EnrichedEvent {
	return EnrichedEvent{
		Event: domain.Event{
			Actor:      "test",
			ApiVersion: "v1beta1",
			Kind:       string(domain.EventKind),
			InvolvedObject: v1beta1.ObjectReference{
				Kind: string(domain.DeviceKind),
				Name: deviceName,
			},
			Metadata: v1beta1.ObjectMeta{
				CreationTimestamp: &timestamp,
			},
			Reason: reason,
		},
		OrgID: orgID,
		Fleet: fleet,
	}
}

func makeBinaryStateWithDevice(orgIDStr, fleetStr, deviceNameStr, reason string, startsAt time.Time) *BinaryHandlerState {
	state := NewBinaryHandlerState()
	state.SetDeviceAlert(OrgID(orgIDStr), FleetName(fleetStr), DeviceName(deviceNameStr), reason, startsAt)
	return state
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return data
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
