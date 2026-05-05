package alert_exporter

import (
	"time"

	"github.com/flightctl/flightctl/internal/config"
	"github.com/sirupsen/logrus"
)

type AlertSender struct {
	log                *logrus.Logger
	alertmanagerClient *AlertmanagerClient
}

func NewAlertSender(log *logrus.Logger, hostname string, port uint, cfg *config.Config) *AlertSender {
	return &AlertSender{
		log:                log,
		alertmanagerClient: NewAlertmanagerClient(hostname, port, log, cfg),
	}
}

func (a *AlertSender) SendAlerts(checkpoint *AlertCheckpoint) error {
	// Convert ActiveAlerts to the format expected by alertmanager
	alerts := a.convertActiveAlertsToAlertInfo(checkpoint.ActiveAlerts)
	return a.alertmanagerClient.SendActiveAlerts(alerts)
}

// convertActiveAlertsToAlertInfo converts the new ActiveAlert format to AlertInfo for alertmanager
func (a *AlertSender) convertActiveAlertsToAlertInfo(activeAlerts map[string]ActiveAlert) []*AlertInfo {
	alerts := make([]*AlertInfo, 0, len(activeAlerts))
	for _, alert := range activeAlerts {
		resourceName := alert.Fleet
		if resourceName == "" {
			resourceName = alert.Device
		}

		alerts = append(alerts, &AlertInfo{
			ResourceName: resourceName,
			ResourceKind: "Fleet",
			OrgID:        alert.OrgID,
			Reason:       alert.Name,
			Summary:      "", // Summary is in the checkpoint's alert state
			StartsAt:     alert.StartsAt,
			EndsAt:       nil, // Active alerts don't have end time
			AdditionalLabels: map[string]string{
				"severity": alert.Severity,
			},
		})
	}
	return alerts
}

// SendResolvedAlerts sends resolution notifications for alerts that are no longer active
func (a *AlertSender) SendResolvedAlerts(resolvedAlerts []ActiveAlert) error {
	if len(resolvedAlerts) == 0 {
		return nil
	}

	alerts := make([]*AlertInfo, 0, len(resolvedAlerts))
	now := time.Now()
	for _, alert := range resolvedAlerts {
		resourceName := alert.Fleet
		if resourceName == "" {
			resourceName = alert.Device
		}

		alerts = append(alerts, &AlertInfo{
			ResourceName: resourceName,
			ResourceKind: "Fleet",
			OrgID:        alert.OrgID,
			Reason:       alert.Name,
			Summary:      "",
			StartsAt:     alert.StartsAt,
			EndsAt:       &now,
			AdditionalLabels: map[string]string{
				"severity": alert.Severity,
			},
		})
	}
	return a.alertmanagerClient.SendActiveAlerts(alerts)
}
