package alert_exporter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flightctl/flightctl/internal/domain"
)

const (
	AppHandlerName         = "application"
	FleetAppErrorAlert     = "FleetApplicationError"
	FleetAppDegradedAlert  = "FleetApplicationDegraded"
	DeviceAppErrorAlert    = "DeviceApplicationError"
	DeviceAppDegradedAlert = "DeviceApplicationDegraded"
)

// AppHandler processes application-related events and generates fleet-level alerts
type AppHandler struct{}

// NewAppHandler creates a new Application handler
func NewAppHandler() *AppHandler {
	return &AppHandler{}
}

func (h *AppHandler) Name() string {
	return AppHandlerName
}

func (h *AppHandler) EventReasons() []domain.EventReason {
	return []domain.EventReason{
		domain.EventReasonDeviceApplicationError,
		domain.EventReasonDeviceApplicationDegraded,
		domain.EventReasonDeviceApplicationHealthy,
	}
}

func (h *AppHandler) Process(
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
		return AlertChanges{}, nil, fmt.Errorf("failed to marshal Application handler state: %w", err)
	}

	return changes, newStateRaw, nil
}

func (h *AppHandler) processEvent(
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
	case domain.EventReasonDeviceApplicationError:
		state.SetDeviceAlert(orgID, fleet, deviceName, string(domain.EventReasonDeviceApplicationError), startsAt)
	case domain.EventReasonDeviceApplicationDegraded:
		state.SetDeviceAlert(orgID, fleet, deviceName, string(domain.EventReasonDeviceApplicationDegraded), startsAt)
	case domain.EventReasonDeviceApplicationHealthy:
		state.ClearDeviceAlert(orgID, fleet, deviceName)
	}
}

func (h *AppHandler) generateAlerts(state *BinaryHandlerState) AlertChanges {
	var changes AlertChanges

	for orgID, fleets := range state.Devices {
		for fleet, devices := range fleets {
			if IsFleetless(fleet) {
				for deviceName, info := range devices {
					alertName := DeviceAppErrorAlert
					severity := "critical"
					if info.Reason == string(domain.EventReasonDeviceApplicationDegraded) {
						alertName = DeviceAppDegradedAlert
						severity = "warning"
					}
					changes.NewAlerts = append(changes.NewAlerts, FleetAlert{
						OrgID:      string(orgID),
						Fleet:      "",
						AlertName:  alertName,
						Severity:   severity,
						DeviceName: string(deviceName),
						Summary:    fmt.Sprintf("Device %s application %s", deviceName, severity),
						StartsAt:   info.StartsAt,
					})
				}
				continue
			}

			errorCount := 0
			degradedCount := 0
			var errorStartsAt, degradedStartsAt time.Time

			for _, info := range devices {
				switch info.Reason {
				case string(domain.EventReasonDeviceApplicationError):
					errorCount++
					if errorStartsAt.IsZero() || info.StartsAt.Before(errorStartsAt) {
						errorStartsAt = info.StartsAt
					}
				case string(domain.EventReasonDeviceApplicationDegraded):
					degradedCount++
					if degradedStartsAt.IsZero() || info.StartsAt.Before(degradedStartsAt) {
						degradedStartsAt = info.StartsAt
					}
				}
			}

			if errorCount > 0 {
				changes.NewAlerts = append(changes.NewAlerts, FleetAlert{
					OrgID:       string(orgID),
					Fleet:       string(fleet),
					AlertName:   FleetAppErrorAlert,
					Severity:    "critical",
					DeviceCount: errorCount,
					Summary:     fmt.Sprintf("Fleet %s has %d devices with application errors", fleet, errorCount),
					StartsAt:    errorStartsAt,
				})
			}
			if degradedCount > 0 {
				changes.NewAlerts = append(changes.NewAlerts, FleetAlert{
					OrgID:       string(orgID),
					Fleet:       string(fleet),
					AlertName:   FleetAppDegradedAlert,
					Severity:    "warning",
					DeviceCount: degradedCount,
					Summary:     fmt.Sprintf("Fleet %s has %d devices with degraded applications", fleet, degradedCount),
					StartsAt:    degradedStartsAt,
				})
			}
		}
	}

	return changes
}
