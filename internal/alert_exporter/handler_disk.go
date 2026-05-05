package alert_exporter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flightctl/flightctl/internal/domain"
)

const (
	DiskHandlerName         = "disk"
	FleetDiskCriticalAlert  = "FleetDiskCritical"
	FleetDiskWarningAlert   = "FleetDiskWarning"
	DeviceDiskCriticalAlert = "DeviceDiskCritical"
	DeviceDiskWarningAlert  = "DeviceDiskWarning"
)

// DiskHandler processes disk-related events and generates fleet-level alerts
type DiskHandler struct{}

// NewDiskHandler creates a new Disk handler
func NewDiskHandler() *DiskHandler {
	return &DiskHandler{}
}

func (h *DiskHandler) Name() string {
	return DiskHandlerName
}

func (h *DiskHandler) EventReasons() []domain.EventReason {
	return []domain.EventReason{
		domain.EventReasonDeviceDiskCritical,
		domain.EventReasonDeviceDiskWarning,
		domain.EventReasonDeviceDiskNormal,
	}
}

func (h *DiskHandler) Process(
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
		return AlertChanges{}, nil, fmt.Errorf("failed to marshal Disk handler state: %w", err)
	}

	return changes, newStateRaw, nil
}

func (h *DiskHandler) processEvent(
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
	case domain.EventReasonDeviceDiskCritical:
		state.SetDeviceAlert(orgID, fleet, deviceName, string(domain.EventReasonDeviceDiskCritical), startsAt)
	case domain.EventReasonDeviceDiskWarning:
		state.SetDeviceAlert(orgID, fleet, deviceName, string(domain.EventReasonDeviceDiskWarning), startsAt)
	case domain.EventReasonDeviceDiskNormal:
		state.ClearDeviceAlert(orgID, fleet, deviceName)
	}
}

func (h *DiskHandler) generateAlerts(state *BinaryHandlerState) AlertChanges {
	var changes AlertChanges

	for orgID, fleets := range state.Devices {
		for fleet, devices := range fleets {
			if IsFleetless(fleet) {
				for deviceName, info := range devices {
					alertName := DeviceDiskCriticalAlert
					severity := "critical"
					if info.Reason == string(domain.EventReasonDeviceDiskWarning) {
						alertName = DeviceDiskWarningAlert
						severity = "warning"
					}
					changes.NewAlerts = append(changes.NewAlerts, FleetAlert{
						OrgID:      string(orgID),
						Fleet:      "",
						AlertName:  alertName,
						Severity:   severity,
						DeviceName: string(deviceName),
						Summary:    fmt.Sprintf("Device %s disk %s", deviceName, severity),
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
				case string(domain.EventReasonDeviceDiskCritical):
					criticalCount++
					if criticalStartsAt.IsZero() || info.StartsAt.Before(criticalStartsAt) {
						criticalStartsAt = info.StartsAt
					}
				case string(domain.EventReasonDeviceDiskWarning):
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
					AlertName:   FleetDiskCriticalAlert,
					Severity:    "critical",
					DeviceCount: criticalCount,
					Summary:     fmt.Sprintf("Fleet %s has %d devices with disk critical", fleet, criticalCount),
					StartsAt:    criticalStartsAt,
				})
			}
			if warningCount > 0 {
				changes.NewAlerts = append(changes.NewAlerts, FleetAlert{
					OrgID:       string(orgID),
					Fleet:       string(fleet),
					AlertName:   FleetDiskWarningAlert,
					Severity:    "warning",
					DeviceCount: warningCount,
					Summary:     fmt.Sprintf("Fleet %s has %d devices with disk warning", fleet, warningCount),
					StartsAt:    warningStartsAt,
				})
			}
		}
	}

	return changes
}
