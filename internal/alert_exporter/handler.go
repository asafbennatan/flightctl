package alert_exporter

import (
	"context"
	"encoding/json"
	"time"

	"github.com/flightctl/flightctl/internal/domain"
	"github.com/google/uuid"
)

// Type aliases for map keys to improve readability
type (
	OrgID      string // Organization identifier (UUID string)
	FleetName  string // Fleet name (empty string "" for fleetless devices)
	DeviceName string // Device name
	CVEID      string // CVE identifier (e.g., "CVE-2024-1234")
)

// EnrichedEvent pairs an event with its organization ID and enriched data
type EnrichedEvent struct {
	Event domain.Event
	OrgID uuid.UUID
	Fleet string // enriched: fleet name (empty if fleetless)
}

// AlertTypeHandler processes events for a specific alert type.
// Handlers use a functional pattern: they receive state as input and return
// changes + new state as output, with no side effects.
type AlertTypeHandler interface {
	// Name returns the handler identifier (used for checkpoint storage)
	Name() string

	// EventReasons returns which event reasons this handler processes
	EventReasons() []domain.EventReason

	// Process takes events and previous state, returns changes and new state.
	// This is a pure function: no side effects, state changes are explicit.
	Process(
		ctx context.Context,
		events []EnrichedEvent,
		prevState json.RawMessage,
		lookup ServiceLookup,
	) (AlertChanges, json.RawMessage, error)
}

// AlertChanges represents the alerts to create/resolve from a handler
type AlertChanges struct {
	NewAlerts      []FleetAlert // alerts to send (active)
	ResolvedAlerts []FleetAlert // alerts to resolve (send with endsAt=now)
}

// FleetAlert represents a fleet-level alert
type FleetAlert struct {
	OrgID       string
	Fleet       string // empty string means fleetless (will generate per-device alert)
	AlertName   string
	Severity    string
	DeviceCount int
	Summary     string
	StartsAt    time.Time
	// For fleetless devices, this is the device name
	DeviceName string
	// Additional labels for the alert
	AdditionalLabels map[string]string
}

// ServiceLookup provides read-only access to service data needed by handlers
type ServiceLookup interface {
	// GetDeviceFleet returns the fleet name for a device, or empty string if fleetless
	GetDeviceFleet(ctx context.Context, orgID uuid.UUID, deviceName string) (string, error)
}

// DeviceAlertInfo tracks alert state for a single device in binary alert handlers
type DeviceAlertInfo struct {
	Reason   string    `json:"reason"`
	StartsAt time.Time `json:"startsAt"`
}

// BinaryHandlerState is the common state structure for binary alert handlers
// (CPU, Memory, Disk, Connection, Application).
// Devices are organized by org -> fleet -> device. Fleetless devices use empty string as fleet key.
type BinaryHandlerState struct {
	Devices map[OrgID]map[FleetName]map[DeviceName]DeviceAlertInfo `json:"devices,omitempty"`
}

// NewBinaryHandlerState creates an initialized BinaryHandlerState
func NewBinaryHandlerState() *BinaryHandlerState {
	return &BinaryHandlerState{
		Devices: make(map[OrgID]map[FleetName]map[DeviceName]DeviceAlertInfo),
	}
}

// SetDeviceAlert sets an alert for a device (use empty string for fleetless devices).
// It first removes the device from any other fleet to handle fleet migrations.
func (s *BinaryHandlerState) SetDeviceAlert(orgID OrgID, fleet FleetName, deviceName DeviceName, reason string, startsAt time.Time) {
	// Remove from all other fleets first (handles fleet migration)
	if s.Devices[orgID] != nil {
		for existingFleet := range s.Devices[orgID] {
			if existingFleet != fleet {
				delete(s.Devices[orgID][existingFleet], deviceName)
				if len(s.Devices[orgID][existingFleet]) == 0 {
					delete(s.Devices[orgID], existingFleet)
				}
			}
		}
	}

	// Add to the current fleet
	if s.Devices[orgID] == nil {
		s.Devices[orgID] = make(map[FleetName]map[DeviceName]DeviceAlertInfo)
	}
	if s.Devices[orgID][fleet] == nil {
		s.Devices[orgID][fleet] = make(map[DeviceName]DeviceAlertInfo)
	}
	s.Devices[orgID][fleet][deviceName] = DeviceAlertInfo{
		Reason:   reason,
		StartsAt: startsAt,
	}
}

// ClearDeviceAlert clears an alert for a device from all fleets.
// This handles the case where a device may have moved fleets since the alert was set.
func (s *BinaryHandlerState) ClearDeviceAlert(orgID OrgID, fleet FleetName, deviceName DeviceName) {
	if s.Devices[orgID] == nil {
		return
	}
	// Clear from all fleets (handles fleet migration)
	for existingFleet := range s.Devices[orgID] {
		delete(s.Devices[orgID][existingFleet], deviceName)
		if len(s.Devices[orgID][existingFleet]) == 0 {
			delete(s.Devices[orgID], existingFleet)
		}
	}
	if len(s.Devices[orgID]) == 0 {
		delete(s.Devices, orgID)
	}
}

// RemoveDevice removes a device from all tracking (used for device deletion)
func (s *BinaryHandlerState) RemoveDevice(orgID OrgID, deviceName DeviceName) {
	if s.Devices[orgID] == nil {
		return
	}
	for fleet := range s.Devices[orgID] {
		delete(s.Devices[orgID][fleet], deviceName)
		if len(s.Devices[orgID][fleet]) == 0 {
			delete(s.Devices[orgID], fleet)
		}
	}
	if len(s.Devices[orgID]) == 0 {
		delete(s.Devices, orgID)
	}
}

// IsFleetless returns true if the fleet key represents a fleetless device
func IsFleetless(fleet FleetName) bool {
	return fleet == ""
}
