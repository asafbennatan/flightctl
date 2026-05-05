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

func TestCVEHandler_Name(t *testing.T) {
	require := require.New(t)
	handler := NewCVEHandler()
	require.Equal(CVEHandlerName, handler.Name())
}

func TestCVEHandler_EventReasons(t *testing.T) {
	require := require.New(t)
	handler := NewCVEHandler()
	reasons := handler.EventReasons()

	require.Len(reasons, 3)
	require.Contains(reasons, domain.EventReasonDeviceVulnerabilityCVECritical)
	require.Contains(reasons, domain.EventReasonDeviceVulnerabilityCVEWarning)
	require.Contains(reasons, domain.EventReasonDeviceVulnerabilityCVEResolved)
}

func TestCVEHandler_Process(t *testing.T) {
	ctx := context.Background()
	handler := NewCVEHandler()
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
			name: "When a device in a fleet has critical CVE it should generate fleet alert",
			events: []EnrichedEvent{
				makeCVEEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceVulnerabilityCVECritical, "CVE-2024-0001", now),
			},
			prevState:          nil,
			expectedCritical:   1,
			expectedWarning:    0,
			expectedFleetless:  0,
			checkAlertContains: "fleet-a",
		},
		{
			name: "When a device in a fleet has warning CVE it should generate fleet warning alert",
			events: []EnrichedEvent{
				makeCVEEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceVulnerabilityCVEWarning, "CVE-2024-0002", now),
			},
			prevState:          nil,
			expectedCritical:   0,
			expectedWarning:    1,
			expectedFleetless:  0,
			checkAlertContains: "fleet-a",
		},
		{
			name: "When a fleetless device has critical CVE it should generate device alert",
			events: []EnrichedEvent{
				makeCVEEnrichedEvent(orgID, "device-1", "", domain.EventReasonDeviceVulnerabilityCVECritical, "CVE-2024-0001", now),
			},
			prevState:          nil,
			expectedCritical:   0,
			expectedWarning:    0,
			expectedFleetless:  1,
			checkAlertContains: "device-1",
		},
		{
			name: "When multiple devices in same fleet have critical CVEs it should aggregate count",
			events: []EnrichedEvent{
				makeCVEEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceVulnerabilityCVECritical, "CVE-2024-0001", now),
				makeCVEEnrichedEvent(orgID, "device-2", "fleet-a", domain.EventReasonDeviceVulnerabilityCVECritical, "CVE-2024-0002", now),
				makeCVEEnrichedEvent(orgID, "device-3", "fleet-a", domain.EventReasonDeviceVulnerabilityCVECritical, "CVE-2024-0003", now),
			},
			prevState:          nil,
			expectedCritical:   1,
			expectedWarning:    0,
			expectedFleetless:  0,
			checkAlertContains: "3 devices",
		},
		{
			name: "When CVE is resolved it should clear from state",
			events: []EnrichedEvent{
				makeCVEEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceVulnerabilityCVEResolved, "CVE-2024-0001", now),
			},
			prevState:          mustMarshal(t, makeCVEStateWithDevice(orgID.String(), "fleet-a", "device-1", "CVE-2024-0001", true, now)),
			expectedCritical:   0,
			expectedWarning:    0,
			expectedFleetless:  0,
			checkAlertContains: "",
		},
		{
			name: "When device has multiple CVEs and one is resolved it should still alert",
			events: []EnrichedEvent{
				makeCVEEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceVulnerabilityCVEResolved, "CVE-2024-0001", now),
			},
			prevState:          mustMarshal(t, makeCVEStateWithMultipleCVEs(orgID.String(), "fleet-a", "device-1", []string{"CVE-2024-0001", "CVE-2024-0002"}, true, now)),
			expectedCritical:   1,
			expectedWarning:    0,
			expectedFleetless:  0,
			checkAlertContains: "fleet-a",
		},
		{
			name: "When warning CVE is superseded by critical it should only count as critical",
			events: []EnrichedEvent{
				makeCVEEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceVulnerabilityCVECritical, "CVE-2024-0001", now),
			},
			prevState:          mustMarshal(t, makeCVEStateWithDevice(orgID.String(), "fleet-a", "device-1", "CVE-2024-0001", false, now.Add(-time.Hour))),
			expectedCritical:   1,
			expectedWarning:    0,
			expectedFleetless:  0,
			checkAlertContains: "fleet-a",
		},
		{
			name: "When device has only warning CVEs it should generate warning alert",
			events: []EnrichedEvent{
				makeCVEEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceVulnerabilityCVEWarning, "CVE-2024-0001", now),
				makeCVEEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceVulnerabilityCVEWarning, "CVE-2024-0002", now),
			},
			prevState:          nil,
			expectedCritical:   0,
			expectedWarning:    1,
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

func TestCVEHandler_StatePersistence(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	handler := NewCVEHandler()
	orgID := uuid.New()
	now := time.Now()

	events1 := []EnrichedEvent{
		makeCVEEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceVulnerabilityCVECritical, "CVE-2024-0001", now),
	}
	changes1, state1, err := handler.Process(ctx, events1, nil, nil)
	require.NoError(err)
	require.Len(changes1.NewAlerts, 1)

	events2 := []EnrichedEvent{
		makeCVEEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceVulnerabilityCVECritical, "CVE-2024-0002", now),
	}
	changes2, state2, err := handler.Process(ctx, events2, state1, nil)
	require.NoError(err)
	require.Len(changes2.NewAlerts, 1)

	var parsedState CVEHandlerState
	require.NoError(json.Unmarshal(state2, &parsedState))
	deviceInfo := parsedState.Devices[OrgID(orgID.String())][FleetName("fleet-a")][DeviceName("device-1")]
	require.Len(deviceInfo.CriticalCVEs, 2)
	require.Contains(deviceInfo.CriticalCVEs, CVEID("CVE-2024-0001"))
	require.Contains(deviceInfo.CriticalCVEs, CVEID("CVE-2024-0002"))
}

func TestCVEHandler_FleetMigration(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	handler := NewCVEHandler()
	orgID := uuid.New()
	now := time.Now()

	// Device gets CVE in fleet-a
	events1 := []EnrichedEvent{
		makeCVEEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceVulnerabilityCVECritical, "CVE-2024-0001", now),
	}
	_, state1, err := handler.Process(ctx, events1, nil, nil)
	require.NoError(err)

	// Verify device is in fleet-a
	var parsedState1 CVEHandlerState
	require.NoError(json.Unmarshal(state1, &parsedState1))
	require.NotNil(parsedState1.Devices[OrgID(orgID.String())][FleetName("fleet-a")][DeviceName("device-1")])

	// Device moves to fleet-b and gets another CVE
	events2 := []EnrichedEvent{
		makeCVEEnrichedEvent(orgID, "device-1", "fleet-b", domain.EventReasonDeviceVulnerabilityCVECritical, "CVE-2024-0002", now),
	}
	changes2, state2, err := handler.Process(ctx, events2, state1, nil)
	require.NoError(err)

	// Verify device is only in fleet-b with both CVEs preserved
	var parsedState2 CVEHandlerState
	require.NoError(json.Unmarshal(state2, &parsedState2))

	// Should NOT be in fleet-a anymore
	require.Nil(parsedState2.Devices[OrgID(orgID.String())][FleetName("fleet-a")])

	// Should be in fleet-b with both CVEs
	deviceInfo := parsedState2.Devices[OrgID(orgID.String())][FleetName("fleet-b")][DeviceName("device-1")]
	require.NotNil(deviceInfo)
	require.Len(deviceInfo.CriticalCVEs, 2)
	require.Contains(deviceInfo.CriticalCVEs, CVEID("CVE-2024-0001"))
	require.Contains(deviceInfo.CriticalCVEs, CVEID("CVE-2024-0002"))

	// Should only have one fleet alert (for fleet-b)
	fleetBAlerts := 0
	for _, alert := range changes2.NewAlerts {
		if alert.Fleet == "fleet-b" {
			fleetBAlerts++
		}
	}
	require.Equal(1, fleetBAlerts)
}

func TestCVEHandler_FleetMigration_FleetToFleetless(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	handler := NewCVEHandler()
	orgID := uuid.New()
	now := time.Now()

	// Device gets CVE in fleet-a
	events1 := []EnrichedEvent{
		makeCVEEnrichedEvent(orgID, "device-1", "fleet-a", domain.EventReasonDeviceVulnerabilityCVECritical, "CVE-2024-0001", now),
	}
	_, state1, err := handler.Process(ctx, events1, nil, nil)
	require.NoError(err)

	// Device becomes fleetless and gets another CVE
	events2 := []EnrichedEvent{
		makeCVEEnrichedEvent(orgID, "device-1", "", domain.EventReasonDeviceVulnerabilityCVEWarning, "CVE-2024-0002", now),
	}
	changes2, state2, err := handler.Process(ctx, events2, state1, nil)
	require.NoError(err)

	var parsedState2 CVEHandlerState
	require.NoError(json.Unmarshal(state2, &parsedState2))

	// Should NOT be in fleet-a anymore
	require.Nil(parsedState2.Devices[OrgID(orgID.String())][FleetName("fleet-a")])

	// Should be fleetless with both CVEs
	deviceInfo := parsedState2.Devices[OrgID(orgID.String())][FleetName("")][DeviceName("device-1")]
	require.NotNil(deviceInfo)
	require.Len(deviceInfo.CriticalCVEs, 1)
	require.Len(deviceInfo.WarningCVEs, 1)

	// Should have device-level alert (not fleet)
	deviceAlerts := 0
	for _, alert := range changes2.NewAlerts {
		if alert.Fleet == "" && alert.DeviceName == "device-1" {
			deviceAlerts++
		}
	}
	require.Equal(1, deviceAlerts, "should have device-level alert for fleetless device")
}

func makeCVEEnrichedEvent(orgID uuid.UUID, deviceName, fleet string, reason domain.EventReason, cveID string, timestamp time.Time) EnrichedEvent {
	details := v1beta1.EventDetails{}
	_ = details.FromDeviceVulnerabilityCveDetails(v1beta1.DeviceVulnerabilityCveDetails{
		CveId: cveID,
	})

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
			Reason:  reason,
			Details: &details,
		},
		OrgID: orgID,
		Fleet: fleet,
	}
}

func makeCVEStateWithDevice(orgIDStr, fleetStr, deviceNameStr, cveIDStr string, isCritical bool, startsAt time.Time) *CVEHandlerState {
	state := NewCVEHandlerState()
	info := state.GetOrCreateDeviceInfo(OrgID(orgIDStr), FleetName(fleetStr), DeviceName(deviceNameStr))
	cveID := CVEID(cveIDStr)
	if isCritical {
		info.CriticalCVEs[cveID] = startsAt
	} else {
		info.WarningCVEs[cveID] = startsAt
	}
	return state
}

func makeCVEStateWithMultipleCVEs(orgIDStr, fleetStr, deviceNameStr string, cveIDs []string, isCritical bool, startsAt time.Time) *CVEHandlerState {
	state := NewCVEHandlerState()
	info := state.GetOrCreateDeviceInfo(OrgID(orgIDStr), FleetName(fleetStr), DeviceName(deviceNameStr))
	for _, cveIDStr := range cveIDs {
		cveID := CVEID(cveIDStr)
		if isCritical {
			info.CriticalCVEs[cveID] = startsAt
		} else {
			info.WarningCVEs[cveID] = startsAt
		}
	}
	return state
}
