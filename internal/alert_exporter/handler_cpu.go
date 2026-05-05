package alert_exporter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flightctl/flightctl/internal/domain"
)

const (
	CPUHandlerName         = "cpu"
	FleetCPUCriticalAlert  = "FleetCPUCritical"
	FleetCPUWarningAlert   = "FleetCPUWarning"
	DeviceCPUCriticalAlert = "DeviceCPUCritical"
	DeviceCPUWarningAlert  = "DeviceCPUWarning"
)

// CPUHandler processes CPU-related events and generates fleet-level alerts
type CPUHandler struct{}

// NewCPUHandler creates a new CPU handler
func NewCPUHandler() *CPUHandler {
	return &CPUHandler{}
}

func (h *CPUHandler) Name() string {
	return CPUHandlerName
}

func (h *CPUHandler) EventReasons() []domain.EventReason {
	return []domain.EventReason{
		domain.EventReasonDeviceCPUCritical,
		domain.EventReasonDeviceCPUWarning,
		domain.EventReasonDeviceCPUNormal,
	}
}

func (h *CPUHandler) Process(
	ctx context.Context,
	events []EnrichedEvent,
	prevStateRaw json.RawMessage,
	lookup ServiceLookup,
) (AlertChanges, json.RawMessage, error) {
	// Deserialize previous state (or init empty)
	var state *BinaryHandlerState
	if prevStateRaw != nil {
		state = &BinaryHandlerState{}
		if err := json.Unmarshal(prevStateRaw, state); err != nil {
			state = NewBinaryHandlerState()
		}
	} else {
		state = NewBinaryHandlerState()
	}

	// Process each event
	for _, enrichedEvent := range events {
		h.processEvent(ctx, enrichedEvent, state, lookup)
	}

	// Generate fleet alerts from state
	changes := h.generateAlerts(state)

	// Serialize new state
	newStateRaw, err := json.Marshal(state)
	if err != nil {
		return AlertChanges{}, nil, fmt.Errorf("failed to marshal CPU handler state: %w", err)
	}

	return changes, newStateRaw, nil
}

func (h *CPUHandler) processEvent(
	_ context.Context,
	enrichedEvent EnrichedEvent,
	state *BinaryHandlerState,
	_ ServiceLookup,
) {
	event := enrichedEvent.Event

	if event.InvolvedObject.Kind != string(domain.DeviceKind) {
		return
	}

	orgID := OrgID(enrichedEvent.OrgID.String())
	fleet := FleetName(enrichedEvent.Fleet)
	deviceName := DeviceName(event.InvolvedObject.Name)

	var startsAt time.Time
	if event.Metadata.CreationTimestamp != nil {
		startsAt = *event.Metadata.CreationTimestamp
	} else {
		startsAt = time.Now()
	}

	switch event.Reason {
	case domain.EventReasonDeviceCPUCritical:
		state.SetDeviceAlert(orgID, fleet, deviceName, string(domain.EventReasonDeviceCPUCritical), startsAt)
	case domain.EventReasonDeviceCPUWarning:
		state.SetDeviceAlert(orgID, fleet, deviceName, string(domain.EventReasonDeviceCPUWarning), startsAt)
	case domain.EventReasonDeviceCPUNormal:
		state.ClearDeviceAlert(orgID, fleet, deviceName)
	}
}

func (h *CPUHandler) generateAlerts(state *BinaryHandlerState) AlertChanges {
	var changes AlertChanges

	for orgID, fleets := range state.Devices {
		for fleet, devices := range fleets {
			if IsFleetless(fleet) {
				for deviceName, info := range devices {
					alertName := DeviceCPUCriticalAlert
					severity := "critical"
					if info.Reason == string(domain.EventReasonDeviceCPUWarning) {
						alertName = DeviceCPUWarningAlert
						severity = "warning"
					}
					changes.NewAlerts = append(changes.NewAlerts, FleetAlert{
						OrgID:      string(orgID),
						Fleet:      "",
						AlertName:  alertName,
						Severity:   severity,
						DeviceName: string(deviceName),
						Summary:    fmt.Sprintf("Device %s CPU %s", deviceName, severity),
						StartsAt:   info.StartsAt,
					})
				}
				continue
			}

			criticalCount := 0
			warningCount := 0
			var criticalStartsAt, warningStartsAt time.Time

			for _, info := range devices {
				switch info.Reason {
				case string(domain.EventReasonDeviceCPUCritical):
					criticalCount++
					if criticalStartsAt.IsZero() || info.StartsAt.Before(criticalStartsAt) {
						criticalStartsAt = info.StartsAt
					}
				case string(domain.EventReasonDeviceCPUWarning):
					warningCount++
					if warningStartsAt.IsZero() || info.StartsAt.Before(warningStartsAt) {
						warningStartsAt = info.StartsAt
					}
				}
			}

			if criticalCount > 0 {
				changes.NewAlerts = append(changes.NewAlerts, FleetAlert{
					OrgID:       string(orgID),
					Fleet:       string(fleet),
					AlertName:   FleetCPUCriticalAlert,
					Severity:    "critical",
					DeviceCount: criticalCount,
					Summary:     fmt.Sprintf("Fleet %s has %d devices with CPU critical", fleet, criticalCount),
					StartsAt:    criticalStartsAt,
				})
			}
			if warningCount > 0 {
				changes.NewAlerts = append(changes.NewAlerts, FleetAlert{
					OrgID:       string(orgID),
					Fleet:       string(fleet),
					AlertName:   FleetCPUWarningAlert,
					Severity:    "warning",
					DeviceCount: warningCount,
					Summary:     fmt.Sprintf("Fleet %s has %d devices with CPU warning", fleet, warningCount),
					StartsAt:    warningStartsAt,
				})
			}
		}
	}

	return changes
}
