package alert_exporter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flightctl/flightctl/internal/domain"
)

const (
	MemoryHandlerName         = "memory"
	FleetMemoryCriticalAlert  = "FleetMemoryCritical"
	FleetMemoryWarningAlert   = "FleetMemoryWarning"
	DeviceMemoryCriticalAlert = "DeviceMemoryCritical"
	DeviceMemoryWarningAlert  = "DeviceMemoryWarning"
)

// MemoryHandler processes memory-related events and generates fleet-level alerts
type MemoryHandler struct{}

// NewMemoryHandler creates a new Memory handler
func NewMemoryHandler() *MemoryHandler {
	return &MemoryHandler{}
}

func (h *MemoryHandler) Name() string {
	return MemoryHandlerName
}

func (h *MemoryHandler) EventReasons() []domain.EventReason {
	return []domain.EventReason{
		domain.EventReasonDeviceMemoryCritical,
		domain.EventReasonDeviceMemoryWarning,
		domain.EventReasonDeviceMemoryNormal,
	}
}

func (h *MemoryHandler) Process(
	ctx context.Context,
	events []EnrichedEvent,
	prevStateRaw json.RawMessage,
	lookup ServiceLookup,
) (AlertChanges, json.RawMessage, error) {
	var state *BinaryHandlerState
	if prevStateRaw != nil {
		state = &BinaryHandlerState{}
		if err := json.Unmarshal(prevStateRaw, state); err != nil {
			state = NewBinaryHandlerState()
		}
	} else {
		state = NewBinaryHandlerState()
	}

	for _, enrichedEvent := range events {
		h.processEvent(ctx, enrichedEvent, state, lookup)
	}

	changes := h.generateAlerts(state)

	newStateRaw, err := json.Marshal(state)
	if err != nil {
		return AlertChanges{}, nil, fmt.Errorf("failed to marshal Memory handler state: %w", err)
	}

	return changes, newStateRaw, nil
}

func (h *MemoryHandler) processEvent(
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
	case domain.EventReasonDeviceMemoryCritical:
		state.SetDeviceAlert(orgID, fleet, deviceName, string(domain.EventReasonDeviceMemoryCritical), startsAt)
	case domain.EventReasonDeviceMemoryWarning:
		state.SetDeviceAlert(orgID, fleet, deviceName, string(domain.EventReasonDeviceMemoryWarning), startsAt)
	case domain.EventReasonDeviceMemoryNormal:
		state.ClearDeviceAlert(orgID, fleet, deviceName)
	}
}

func (h *MemoryHandler) generateAlerts(state *BinaryHandlerState) AlertChanges {
	var changes AlertChanges

	for orgID, fleets := range state.Devices {
		for fleet, devices := range fleets {
			if IsFleetless(fleet) {
				for deviceName, info := range devices {
					alertName := DeviceMemoryCriticalAlert
					severity := "critical"
					if info.Reason == string(domain.EventReasonDeviceMemoryWarning) {
						alertName = DeviceMemoryWarningAlert
						severity = "warning"
					}
					changes.NewAlerts = append(changes.NewAlerts, FleetAlert{
						OrgID:      string(orgID),
						Fleet:      "",
						AlertName:  alertName,
						Severity:   severity,
						DeviceName: string(deviceName),
						Summary:    fmt.Sprintf("Device %s memory %s", deviceName, severity),
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
				case string(domain.EventReasonDeviceMemoryCritical):
					criticalCount++
					if criticalStartsAt.IsZero() || info.StartsAt.Before(criticalStartsAt) {
						criticalStartsAt = info.StartsAt
					}
				case string(domain.EventReasonDeviceMemoryWarning):
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
					AlertName:   FleetMemoryCriticalAlert,
					Severity:    "critical",
					DeviceCount: criticalCount,
					Summary:     fmt.Sprintf("Fleet %s has %d devices with memory critical", fleet, criticalCount),
					StartsAt:    criticalStartsAt,
				})
			}
			if warningCount > 0 {
				changes.NewAlerts = append(changes.NewAlerts, FleetAlert{
					OrgID:       string(orgID),
					Fleet:       string(fleet),
					AlertName:   FleetMemoryWarningAlert,
					Severity:    "warning",
					DeviceCount: warningCount,
					Summary:     fmt.Sprintf("Fleet %s has %d devices with memory warning", fleet, warningCount),
					StartsAt:    warningStartsAt,
				})
			}
		}
	}

	return changes
}
