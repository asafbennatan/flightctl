package alert_exporter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flightctl/flightctl/internal/domain"
)

const (
	CVEHandlerName         = "cve"
	FleetCVECriticalAlert  = "FleetVulnerabilityCritical"
	FleetCVEWarningAlert   = "FleetVulnerabilityWarning"
	DeviceCVECriticalAlert = "DeviceVulnerabilityCritical"
	DeviceCVEWarningAlert  = "DeviceVulnerabilityWarning"
)

// CVEHandlerState tracks CVE-specific state where multiple CVEs can affect a device.
// Devices are organized by org -> fleet -> device. Fleetless devices use empty string as fleet key.
type CVEHandlerState struct {
	Devices map[OrgID]map[FleetName]map[DeviceName]*DeviceCVEInfo `json:"devices,omitempty"`
}

// DeviceCVEInfo tracks CVE counts for a single device
type DeviceCVEInfo struct {
	CriticalCVEs map[CVEID]time.Time `json:"criticalCves,omitempty"`
	WarningCVEs  map[CVEID]time.Time `json:"warningCves,omitempty"`
}

// NewCVEHandlerState creates an initialized CVEHandlerState
func NewCVEHandlerState() *CVEHandlerState {
	return &CVEHandlerState{
		Devices: make(map[OrgID]map[FleetName]map[DeviceName]*DeviceCVEInfo),
	}
}

// GetOrCreateDeviceInfo gets or creates CVE info for a device (use empty string for fleetless).
// If the device exists in a different fleet, it is moved to the new fleet (handles fleet migration).
func (s *CVEHandlerState) GetOrCreateDeviceInfo(orgID OrgID, fleet FleetName, deviceName DeviceName) *DeviceCVEInfo {
	// Check if device exists in a different fleet and move it
	if s.Devices[orgID] != nil {
		for existingFleet, devices := range s.Devices[orgID] {
			if existingFleet != fleet {
				if existingInfo, exists := devices[deviceName]; exists {
					// Move to new fleet
					delete(devices, deviceName)
					if len(devices) == 0 {
						delete(s.Devices[orgID], existingFleet)
					}
					// Initialize new fleet and add device
					if s.Devices[orgID][fleet] == nil {
						s.Devices[orgID][fleet] = make(map[DeviceName]*DeviceCVEInfo)
					}
					s.Devices[orgID][fleet][deviceName] = existingInfo
					// Ensure maps are initialized (may be nil after JSON unmarshal)
					if existingInfo.CriticalCVEs == nil {
						existingInfo.CriticalCVEs = make(map[CVEID]time.Time)
					}
					if existingInfo.WarningCVEs == nil {
						existingInfo.WarningCVEs = make(map[CVEID]time.Time)
					}
					return existingInfo
				}
			}
		}
	}

	// Create new device info if not found
	if s.Devices[orgID] == nil {
		s.Devices[orgID] = make(map[FleetName]map[DeviceName]*DeviceCVEInfo)
	}
	if s.Devices[orgID][fleet] == nil {
		s.Devices[orgID][fleet] = make(map[DeviceName]*DeviceCVEInfo)
	}
	if s.Devices[orgID][fleet][deviceName] == nil {
		s.Devices[orgID][fleet][deviceName] = &DeviceCVEInfo{
			CriticalCVEs: make(map[CVEID]time.Time),
			WarningCVEs:  make(map[CVEID]time.Time),
		}
	}
	info := s.Devices[orgID][fleet][deviceName]
	if info.CriticalCVEs == nil {
		info.CriticalCVEs = make(map[CVEID]time.Time)
	}
	if info.WarningCVEs == nil {
		info.WarningCVEs = make(map[CVEID]time.Time)
	}
	return info
}

// RemoveDevice removes a device from all tracking (used for device deletion)
func (s *CVEHandlerState) RemoveDevice(orgID OrgID, deviceName DeviceName) {
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

// CleanupEmptyDevices removes devices that have no CVEs tracked
func (s *CVEHandlerState) CleanupEmptyDevices() {
	for orgID, fleets := range s.Devices {
		for fleet, devices := range fleets {
			for deviceName, info := range devices {
				if len(info.CriticalCVEs) == 0 && len(info.WarningCVEs) == 0 {
					delete(devices, deviceName)
				}
			}
			if len(devices) == 0 {
				delete(fleets, fleet)
			}
		}
		if len(fleets) == 0 {
			delete(s.Devices, orgID)
		}
	}
}

// CVEHandler processes CVE-related events and generates fleet-level alerts
type CVEHandler struct{}

// NewCVEHandler creates a new CVE handler
func NewCVEHandler() *CVEHandler {
	return &CVEHandler{}
}

func (h *CVEHandler) Name() string {
	return CVEHandlerName
}

func (h *CVEHandler) EventReasons() []domain.EventReason {
	return []domain.EventReason{
		domain.EventReasonDeviceVulnerabilityCVECritical,
		domain.EventReasonDeviceVulnerabilityCVEWarning,
		domain.EventReasonDeviceVulnerabilityCVEResolved,
	}
}

func (h *CVEHandler) Process(
	ctx context.Context,
	events []EnrichedEvent,
	prevStateRaw json.RawMessage,
	lookup ServiceLookup,
) (AlertChanges, json.RawMessage, error) {
	var state *CVEHandlerState
	if prevStateRaw != nil {
		state = &CVEHandlerState{}
		if err := json.Unmarshal(prevStateRaw, state); err != nil {
			state = NewCVEHandlerState()
		}
	} else {
		state = NewCVEHandlerState()
	}

	for _, enrichedEvent := range events {
		h.processEvent(ctx, enrichedEvent, state, lookup)
	}

	// Cleanup devices that no longer have any CVEs
	state.CleanupEmptyDevices()

	changes := h.generateAlerts(state)

	newStateRaw, err := json.Marshal(state)
	if err != nil {
		return AlertChanges{}, nil, fmt.Errorf("failed to marshal CVE handler state: %w", err)
	}

	return changes, newStateRaw, nil
}

func (h *CVEHandler) processEvent(
	_ context.Context,
	enrichedEvent EnrichedEvent,
	state *CVEHandlerState,
	_ ServiceLookup,
) {
	event := enrichedEvent.Event

	if event.InvolvedObject.Kind != string(domain.DeviceKind) {
		return
	}

	cveID := h.extractCVEID(event)
	if cveID == "" {
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

	deviceInfo := state.GetOrCreateDeviceInfo(orgID, fleet, deviceName)

	switch event.Reason {
	case domain.EventReasonDeviceVulnerabilityCVECritical:
		delete(deviceInfo.WarningCVEs, cveID)
		deviceInfo.CriticalCVEs[cveID] = startsAt

	case domain.EventReasonDeviceVulnerabilityCVEWarning:
		if _, isCritical := deviceInfo.CriticalCVEs[cveID]; !isCritical {
			deviceInfo.WarningCVEs[cveID] = startsAt
		}

	case domain.EventReasonDeviceVulnerabilityCVEResolved:
		delete(deviceInfo.CriticalCVEs, cveID)
		delete(deviceInfo.WarningCVEs, cveID)
	}
}

func (h *CVEHandler) extractCVEID(event domain.Event) CVEID {
	if event.Details == nil {
		return ""
	}
	dd, err := event.Details.AsDeviceVulnerabilityCveDetails()
	if err != nil {
		return ""
	}
	return CVEID(dd.CveId)
}

func (h *CVEHandler) generateAlerts(state *CVEHandlerState) AlertChanges {
	var changes AlertChanges

	for orgID, fleets := range state.Devices {
		for fleet, devices := range fleets {
			if IsFleetless(fleet) {
				for deviceName, info := range devices {
					hasCritical := len(info.CriticalCVEs) > 0
					hasWarning := len(info.WarningCVEs) > 0

					if hasCritical {
						var startsAt time.Time
						for _, t := range info.CriticalCVEs {
							if startsAt.IsZero() || t.Before(startsAt) {
								startsAt = t
							}
						}
						changes.NewAlerts = append(changes.NewAlerts, FleetAlert{
							OrgID:      string(orgID),
							Fleet:      "",
							AlertName:  DeviceCVECriticalAlert,
							Severity:   "critical",
							DeviceName: string(deviceName),
							Summary:    fmt.Sprintf("Device %s has %d critical vulnerabilities", deviceName, len(info.CriticalCVEs)),
							StartsAt:   startsAt,
						})
					}

					if hasWarning && !hasCritical {
						var startsAt time.Time
						for _, t := range info.WarningCVEs {
							if startsAt.IsZero() || t.Before(startsAt) {
								startsAt = t
							}
						}
						changes.NewAlerts = append(changes.NewAlerts, FleetAlert{
							OrgID:      string(orgID),
							Fleet:      "",
							AlertName:  DeviceCVEWarningAlert,
							Severity:   "warning",
							DeviceName: string(deviceName),
							Summary:    fmt.Sprintf("Device %s has %d warning vulnerabilities", deviceName, len(info.WarningCVEs)),
							StartsAt:   startsAt,
						})
					}
				}
				continue
			}

			criticalDeviceCount := 0
			warningDeviceCount := 0
			var criticalStartsAt, warningStartsAt time.Time

			for _, info := range devices {
				hasCritical := len(info.CriticalCVEs) > 0
				hasWarning := len(info.WarningCVEs) > 0

				if hasCritical {
					criticalDeviceCount++
					for _, t := range info.CriticalCVEs {
						if criticalStartsAt.IsZero() || t.Before(criticalStartsAt) {
							criticalStartsAt = t
						}
					}
				}

				if hasWarning && !hasCritical {
					warningDeviceCount++
					for _, t := range info.WarningCVEs {
						if warningStartsAt.IsZero() || t.Before(warningStartsAt) {
							warningStartsAt = t
						}
					}
				}
			}

			if criticalDeviceCount > 0 {
				changes.NewAlerts = append(changes.NewAlerts, FleetAlert{
					OrgID:       string(orgID),
					Fleet:       string(fleet),
					AlertName:   FleetCVECriticalAlert,
					Severity:    "critical",
					DeviceCount: criticalDeviceCount,
					Summary:     fmt.Sprintf("Fleet %s has %d devices with critical vulnerabilities", fleet, criticalDeviceCount),
					StartsAt:    criticalStartsAt,
				})
			}
			if warningDeviceCount > 0 {
				changes.NewAlerts = append(changes.NewAlerts, FleetAlert{
					OrgID:       string(orgID),
					Fleet:       string(fleet),
					AlertName:   FleetCVEWarningAlert,
					Severity:    "warning",
					DeviceCount: warningDeviceCount,
					Summary:     fmt.Sprintf("Fleet %s has %d devices with warning vulnerabilities", fleet, warningDeviceCount),
					StartsAt:    warningStartsAt,
				})
			}
		}
	}

	return changes
}
