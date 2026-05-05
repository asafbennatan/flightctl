package alert_exporter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/flightctl/flightctl/internal/domain"
	"github.com/flightctl/flightctl/internal/service"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
)

type EventProcessor struct {
	log           *logrus.Logger
	handler       service.Service
	handlers      []AlertTypeHandler
	serviceLookup ServiceLookup
}

func NewEventProcessor(log *logrus.Logger, handler service.Service) *EventProcessor {
	processor := &EventProcessor{
		log:     log,
		handler: handler,
	}

	// Register all handlers
	processor.handlers = []AlertTypeHandler{
		NewCPUHandler(),
		NewMemoryHandler(),
		NewDiskHandler(),
		NewConnectionHandler(),
		NewAppHandler(),
		NewCVEHandler(),
	}

	// Create service lookup
	processor.serviceLookup = &serviceLookupImpl{service: handler}

	return processor
}

// serviceLookupImpl implements ServiceLookup using the service handler
type serviceLookupImpl struct {
	service service.Service
}

func (s *serviceLookupImpl) GetDeviceFleet(ctx context.Context, orgID uuid.UUID, deviceName string) (string, error) {
	device, status := s.service.GetDevice(ctx, orgID, deviceName)
	if status.Code != http.StatusOK {
		return "", fmt.Errorf("failed to get device: %s", status.Message)
	}
	if device.Metadata.Owner == nil || *device.Metadata.Owner == "" {
		return "", nil
	}
	owner := *device.Metadata.Owner
	if strings.HasPrefix(owner, "Fleet/") {
		return strings.TrimPrefix(owner, "Fleet/"), nil
	}
	return "", nil
}

func (e *EventProcessor) ProcessLatestEvents(ctx context.Context, oldCheckpoint *AlertCheckpoint, metrics *ProcessingMetrics) (*AlertCheckpoint, error) {
	if oldCheckpoint == nil {
		return nil, errors.New("checkpoint cannot be nil")
	}

	logger := e.log.WithFields(logrus.Fields{
		"component":            "event_processor",
		"checkpoint_timestamp": oldCheckpoint.Timestamp,
		"existing_handlers":    len(oldCheckpoint.HandlerStates),
	})

	// Get all organizations
	orgs, status := e.handler.ListOrganizations(ctx, domain.ListOrganizationsParams{})
	if status.Code != http.StatusOK {
		logger.WithFields(logrus.Fields{
			"status_code": status.Code,
			"status_msg":  status.Message,
		}).Error("Failed to list organizations")
		return nil, fmt.Errorf("failed to list organizations: %s", status.Message)
	}

	logger.WithField("org_count", len(orgs.Items)).Info("Processing events for organizations")

	// Collect all events across organizations
	allEvents := make([]EnrichedEvent, 0)
	totalPages := 0
	validationErrors := 0

	for _, org := range orgs.Items {
		orgID, err := uuid.Parse(lo.FromPtr(org.Metadata.Name))
		if err != nil {
			logger.WithFields(logrus.Fields{
				"org_id": lo.FromPtr(org.Metadata.Name),
				"error":  err,
			}).Error("Failed to parse organization ID")
			validationErrors++
			continue
		}

		orgLogger := logger.WithFields(logrus.Fields{
			"org_id":           orgID,
			"org_display_name": lo.FromPtrOr(org.Spec.DisplayName, ""),
		})

		events, pages, orgValidationErrors, err := e.collectOrganizationEvents(ctx, orgID, oldCheckpoint.Timestamp, orgLogger)
		if err != nil {
			orgLogger.WithError(err).Error("Failed to collect events for organization")
			continue
		}

		allEvents = append(allEvents, events...)
		totalPages += pages
		validationErrors += orgValidationErrors
	}

	// Enrich events with fleet information (batch lookup)
	e.enrichEventsWithFleetInfo(ctx, allEvents, logger)

	// Group events by handler
	eventsByHandler := e.groupEventsByHandler(allEvents)

	// Process each handler
	newHandlerStates := make(map[string]json.RawMessage)
	allChanges := AlertChanges{}
	alertsCreated := 0
	alertsResolved := 0

	for _, handler := range e.handlers {
		handlerEvents := eventsByHandler[handler.Name()]
		prevState := oldCheckpoint.HandlerStates[handler.Name()]

		changes, newState, err := handler.Process(ctx, handlerEvents, prevState, e.serviceLookup)
		if err != nil {
			logger.WithFields(logrus.Fields{
				"handler": handler.Name(),
				"error":   err,
			}).Error("Handler failed to process events")
			continue
		}

		allChanges.NewAlerts = append(allChanges.NewAlerts, changes.NewAlerts...)
		allChanges.ResolvedAlerts = append(allChanges.ResolvedAlerts, changes.ResolvedAlerts...)
		newHandlerStates[handler.Name()] = newState
	}

	// Build new active alerts map
	newActiveAlerts := e.buildActiveAlerts(allChanges.NewAlerts)

	// Find alerts that were in previous checkpoint but not in current (resolved)
	resolvedAlerts := e.findResolvedAlerts(oldCheckpoint.ActiveAlerts, newActiveAlerts)
	alertsResolved = len(resolvedAlerts)
	alertsCreated = e.countNewAlerts(oldCheckpoint.ActiveAlerts, newActiveAlerts)

	// Fetch the current time from the DB
	timestamp, status := e.handler.GetDatabaseTime(ctx)
	if status.Code != http.StatusOK {
		logger.WithFields(logrus.Fields{
			"status_code": status.Code,
			"status_msg":  status.Message,
		}).Error("Failed to get database time")
		return nil, fmt.Errorf("failed to get DB time: %s", status.Message)
	}

	newCheckpoint := &AlertCheckpoint{
		Version:       CurrentAlertCheckpointVersion,
		Timestamp:     timestamp.Format(time.RFC3339Nano),
		HandlerStates: newHandlerStates,
		ActiveAlerts:  newActiveAlerts,
	}

	logger.WithFields(logrus.Fields{
		"total_events":      len(allEvents),
		"validation_errors": validationErrors,
		"pages_processed":   totalPages,
		"new_timestamp":     newCheckpoint.Timestamp,
		"active_alerts":     len(newActiveAlerts),
		"orgs_processed":    len(orgs.Items),
		"alerts_created":    alertsCreated,
		"alerts_resolved":   alertsResolved,
	}).Info("Event processing completed")

	// Update metrics with results
	metrics.AlertsCreated = alertsCreated
	metrics.AlertsResolved = alertsResolved
	metrics.EventsProcessed = len(allEvents)

	return newCheckpoint, nil
}

// enrichEventsWithFleetInfo enriches events with fleet information via batch lookup
func (e *EventProcessor) enrichEventsWithFleetInfo(ctx context.Context, events []EnrichedEvent, logger *logrus.Entry) {
	// Collect unique (orgID, deviceName) pairs for Device-kind events
	type deviceKey struct {
		orgID      uuid.UUID
		deviceName string
	}
	uniqueDevices := make(map[deviceKey]struct{})

	for _, event := range events {
		if event.Event.InvolvedObject.Kind == string(domain.DeviceKind) {
			key := deviceKey{orgID: event.OrgID, deviceName: event.Event.InvolvedObject.Name}
			uniqueDevices[key] = struct{}{}
		}
	}

	if len(uniqueDevices) == 0 {
		return
	}

	// Batch lookup fleet info for all unique devices
	deviceFleets := make(map[deviceKey]string)
	for key := range uniqueDevices {
		fleet, err := e.serviceLookup.GetDeviceFleet(ctx, key.orgID, key.deviceName)
		if err != nil {
			logger.WithFields(logrus.Fields{
				"org_id":      key.orgID,
				"device_name": key.deviceName,
				"error":       err,
			}).Debug("Failed to get device fleet, treating as fleetless")
			fleet = ""
		}
		deviceFleets[key] = fleet
	}

	logger.WithField("devices_enriched", len(deviceFleets)).Debug("Enriched events with fleet information")

	// Apply fleet info to events
	for i := range events {
		if events[i].Event.InvolvedObject.Kind == string(domain.DeviceKind) {
			key := deviceKey{orgID: events[i].OrgID, deviceName: events[i].Event.InvolvedObject.Name}
			events[i].Fleet = deviceFleets[key]
		}
	}
}

// collectOrganizationEvents collects all events for an organization since the given timestamp
func (e *EventProcessor) collectOrganizationEvents(ctx context.Context, orgID uuid.UUID, timestamp string, logger *logrus.Entry) ([]EnrichedEvent, int, int, error) {
	params := getListEventsParams(timestamp)
	events := make([]EnrichedEvent, 0)
	totalPages := 0
	validationErrors := 0

	for {
		totalPages++
		pageEvents, status := e.handler.ListEvents(ctx, orgID, params)
		if status.Code != http.StatusOK {
			return nil, totalPages, validationErrors, fmt.Errorf("failed to list events: %s", status.Message)
		}

		for _, ev := range pageEvents.Items {
			if ev.Metadata.CreationTimestamp == nil {
				validationErrors++
				continue
			}
			if strings.TrimSpace(ev.InvolvedObject.Name) == "" {
				validationErrors++
				continue
			}
			events = append(events, EnrichedEvent{
				Event: ev,
				OrgID: orgID,
			})
		}

		if pageEvents.Metadata.Continue == nil {
			break
		}
		params.Continue = pageEvents.Metadata.Continue
	}

	return events, totalPages, validationErrors, nil
}

// groupEventsByHandler groups events by the handler that should process them
func (e *EventProcessor) groupEventsByHandler(events []EnrichedEvent) map[string][]EnrichedEvent {
	result := make(map[string][]EnrichedEvent)

	// Build reason -> handler name mapping
	reasonToHandler := make(map[domain.EventReason]string)
	for _, handler := range e.handlers {
		for _, reason := range handler.EventReasons() {
			reasonToHandler[reason] = handler.Name()
		}
	}

	// Group events
	for _, event := range events {
		if handlerName, ok := reasonToHandler[event.Event.Reason]; ok {
			result[handlerName] = append(result[handlerName], event)
		}
	}

	return result
}

// buildActiveAlerts creates the active alerts map from fleet alerts
func (e *EventProcessor) buildActiveAlerts(alerts []FleetAlert) map[string]ActiveAlert {
	result := make(map[string]ActiveAlert)
	for _, alert := range alerts {
		key := e.buildAlertKey(alert)
		result[key] = ActiveAlert{
			AlertKey: key,
			OrgID:    alert.OrgID,
			Fleet:    alert.Fleet,
			Device:   alert.DeviceName,
			Name:     alert.AlertName,
			Severity: alert.Severity,
			StartsAt: alert.StartsAt,
		}
	}
	return result
}

// buildAlertKey creates a unique key for an alert
func (e *EventProcessor) buildAlertKey(alert FleetAlert) string {
	if alert.Fleet != "" {
		return fmt.Sprintf("%s:%s:%s", alert.OrgID, alert.Fleet, alert.AlertName)
	}
	return fmt.Sprintf("%s:%s:%s", alert.OrgID, alert.DeviceName, alert.AlertName)
}

// findResolvedAlerts finds alerts that were active before but are no longer active
func (e *EventProcessor) findResolvedAlerts(oldAlerts, newAlerts map[string]ActiveAlert) []ActiveAlert {
	resolved := make([]ActiveAlert, 0)
	for key, alert := range oldAlerts {
		if _, exists := newAlerts[key]; !exists {
			resolved = append(resolved, alert)
		}
	}
	return resolved
}

// countNewAlerts counts alerts that are new (not in old alerts)
func (e *EventProcessor) countNewAlerts(oldAlerts, newAlerts map[string]ActiveAlert) int {
	count := 0
	for key := range newAlerts {
		if _, exists := oldAlerts[key]; !exists {
			count++
		}
	}
	return count
}

func getListEventsParams(newerThan string) domain.ListEventsParams {
	eventsOfInterest := []domain.EventReason{
		domain.EventReasonDeviceApplicationDegraded,
		domain.EventReasonDeviceApplicationError,
		domain.EventReasonDeviceApplicationHealthy,
		domain.EventReasonDeviceCPUCritical,
		domain.EventReasonDeviceCPUNormal,
		domain.EventReasonDeviceCPUWarning,
		domain.EventReasonDeviceConnected,
		domain.EventReasonDeviceDisconnected,
		domain.EventReasonDeviceMemoryCritical,
		domain.EventReasonDeviceMemoryNormal,
		domain.EventReasonDeviceMemoryWarning,
		domain.EventReasonDeviceDiskCritical,
		domain.EventReasonDeviceDiskNormal,
		domain.EventReasonDeviceDiskWarning,
		domain.EventReasonResourceDeleted,
		domain.EventReasonDeviceDecommissioned,
		domain.EventReasonDeviceVulnerabilityCVEWarning,
		domain.EventReasonDeviceVulnerabilityCVECritical,
		domain.EventReasonDeviceVulnerabilityCVEResolved,
	}

	fieldSelectors := []string{
		fmt.Sprintf("reason in (%s)",
			strings.Join(lo.Map(eventsOfInterest, func(r domain.EventReason, _ int) string {
				return string(r)
			}), ",")),
	}
	if newerThan != "" {
		fieldSelectors = append(fieldSelectors,
			fmt.Sprintf("metadata.creationTimestamp>=%s", newerThan))
	}

	return domain.ListEventsParams{
		Order:         lo.ToPtr(domain.Asc), // Oldest to newest
		FieldSelector: lo.ToPtr(strings.Join(fieldSelectors, ",")),
		Limit:         lo.ToPtr(int32(1000)),
	}
}
