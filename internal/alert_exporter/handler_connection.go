package alert_exporter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flightctl/flightctl/internal/domain"
)

const (
	ConnectionHandlerName   = "connection"
	FleetDisconnectedAlert  = "FleetDeviceDisconnected"
	DeviceDisconnectedAlert = "DeviceDisconnected"
)

// ConnectionHandler processes connection-related events and generates fleet-level alerts
type ConnectionHandler struct{}

// NewConnectionHandler creates a new Connection handler
func NewConnectionHandler() *ConnectionHandler {
	return &ConnectionHandler{}
}

func (h *ConnectionHandler) Name() string {
	return ConnectionHandlerName
}

func (h *ConnectionHandler) EventReasons() []domain.EventReason {
	return []domain.EventReason{
		domain.EventReasonDeviceDisconnected,
		domain.EventReasonDeviceConnected,
	}
}

func (h *ConnectionHandler) Process(
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
		return AlertChanges{}, nil, fmt.Errorf("failed to marshal Connection handler state: %w", err)
	}

	return changes, newStateRaw, nil
}

func (h *ConnectionHandler) processEvent(
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
	case domain.EventReasonDeviceDisconnected:
		state.SetDeviceAlert(orgID, fleet, deviceName, string(domain.EventReasonDeviceDisconnected), startsAt)
	case domain.EventReasonDeviceConnected:
		state.ClearDeviceAlert(orgID, fleet, deviceName)
	}
}

func (h *ConnectionHandler) generateAlerts(state *BinaryHandlerState) AlertChanges {
	var changes AlertChanges

	for orgID, fleets := range state.Devices {
		for fleet, devices := range fleets {
			if IsFleetless(fleet) {
				for deviceName, info := range devices {
					changes.NewAlerts = append(changes.NewAlerts, FleetAlert{
						OrgID:      string(orgID),
						Fleet:      "",
						AlertName:  DeviceDisconnectedAlert,
						Severity:   "warning",
						DeviceName: string(deviceName),
						Summary:    fmt.Sprintf("Device %s is disconnected", deviceName),
						StartsAt:   info.StartsAt,
					})
				}
				continue
			}

			disconnectedCount := 0
			var disconnectedStartsAt time.Time

			for _, info := range devices {
				if info.Reason == string(domain.EventReasonDeviceDisconnected) {
					disconnectedCount++
					if disconnectedStartsAt.IsZero() || info.StartsAt.Before(disconnectedStartsAt) {
						disconnectedStartsAt = info.StartsAt
					}
				}
			}

			if disconnectedCount > 0 {
				changes.NewAlerts = append(changes.NewAlerts, FleetAlert{
					OrgID:       string(orgID),
					Fleet:       string(fleet),
					AlertName:   FleetDisconnectedAlert,
					Severity:    "warning",
					DeviceCount: disconnectedCount,
					Summary:     fmt.Sprintf("Fleet %s has %d disconnected devices", fleet, disconnectedCount),
					StartsAt:    disconnectedStartsAt,
				})
			}
		}
	}

	return changes
}
