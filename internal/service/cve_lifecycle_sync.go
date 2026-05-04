package service

import (
	"context"
	"errors"
	"strconv"

	"github.com/flightctl/flightctl/internal/domain"
	"github.com/flightctl/flightctl/internal/store"
)

// ErrInvalidCVECVSSThresholds is returned when criticalThreshold <= warningThreshold.
var ErrInvalidCVECVSSThresholds = errors.New("critical CVSS threshold must be greater than warning CVSS threshold")

// SyncDeviceCVELifecycleEvents runs resolution, Warning supersede, Critical emission, and Warning emission (in that order).
// It is a no-op when the vulnerability finding store or event handler is unavailable.
func (h *ServiceHandler) SyncDeviceCVELifecycleEvents(ctx context.Context, warningThreshold, criticalThreshold float64) error {
	if criticalThreshold <= warningThreshold {
		return ErrInvalidCVECVSSThresholds
	}
	vf := h.store.VulnerabilityFinding()
	if vf == nil {
		return nil
	}
	if h.eventHandler == nil {
		return nil
	}

	resolveRows, err := vf.ListCVEEventResolutionCandidates(ctx, warningThreshold, criticalThreshold)
	if err != nil {
		return err
	}
	for i := range resolveRows {
		ev, berr := buildDeviceCVELifecycleEvent(ctx, resolveRows[i].DeviceName,
			resolveRows[i].CveID, resolveRows[i].ImageDigest, resolveRows[i].ImageRef,
			resolveRows[i].CvssScore, resolveRows[i].Severity, domain.EventReasonDeviceVulnerabilityCVEResolved,
			warningThreshold, criticalThreshold)
		if berr != nil {
			return berr
		}
		h.eventHandler.CreateEvent(ctx, resolveRows[i].OrgID, ev)
	}

	supersedeRows, err := vf.ListOpenWarningSupersedeCVEEventCandidates(ctx, warningThreshold, criticalThreshold)
	if err != nil {
		return err
	}
	for i := range supersedeRows {
		ev, berr := buildDeviceCVELifecycleEventFromCandidate(ctx, supersedeRows[i],
			domain.EventReasonDeviceVulnerabilityCVEResolved, warningThreshold, criticalThreshold)
		if berr != nil {
			return berr
		}
		h.eventHandler.CreateEvent(ctx, supersedeRows[i].OrgID, ev)
	}

	criticalRows, err := vf.ListCriticalCVEEventCandidates(ctx, criticalThreshold)
	if err != nil {
		return err
	}
	for i := range criticalRows {
		ev, berr := buildDeviceCVELifecycleEventFromCandidate(ctx, criticalRows[i],
			domain.EventReasonDeviceVulnerabilityCVECritical, warningThreshold, criticalThreshold)
		if berr != nil {
			return berr
		}
		h.eventHandler.CreateEvent(ctx, criticalRows[i].OrgID, ev)
	}

	warningRows, err := vf.ListWarningCVEEventCandidates(ctx, warningThreshold, criticalThreshold)
	if err != nil {
		return err
	}
	for i := range warningRows {
		ev, berr := buildDeviceCVELifecycleEventFromCandidate(ctx, warningRows[i],
			domain.EventReasonDeviceVulnerabilityCVEWarning, warningThreshold, criticalThreshold)
		if berr != nil {
			return berr
		}
		h.eventHandler.CreateEvent(ctx, warningRows[i].OrgID, ev)
	}

	return nil
}

func buildDeviceCVELifecycleEventFromCandidate(ctx context.Context, row store.CVEEventCandidate, reason domain.EventReason, warningTh, criticalTh float64) (*domain.Event, error) {
	return buildDeviceCVELifecycleEvent(ctx, row.DeviceName, row.CveID, row.ImageDigest, row.ImageRef,
		row.CvssScore, row.Severity, reason, warningTh, criticalTh)
}

func buildDeviceCVELifecycleEvent(ctx context.Context, deviceName, cveID, imageDigest, imageRef string, cvss float64, severity string, reason domain.EventReason, warningTh, criticalTh float64) (*domain.Event, error) {
	details := domain.EventDetails{}
	detail := domain.DeviceVulnerabilityCveDetails{
		DetailType:  domain.DeviceVulnerabilityCveDetailsDetailType("DeviceVulnerabilityCVE"),
		CveId:       cveID,
		ImageRef:    imageRef,
		ImageDigest: imageDigest,
	}
	if err := details.FromDeviceVulnerabilityCveDetails(detail); err != nil {
		return nil, err
	}

	msg := formatCVEDeviceEventMessage(reason, cveID, cvss, severity, imageRef, warningTh, criticalTh)
	return domain.GetBaseEvent(ctx, domain.DeviceKind, deviceName, reason, msg, &details), nil
}

func formatCVEDeviceEventMessage(reason domain.EventReason, cveID string, cvss float64, severity, imageRef string, warningTh, criticalTh float64) string {
	sevLabel := severityLabelForDeviceCVE(cvss, severity, warningTh, criticalTh)
	switch reason {
	case domain.EventReasonDeviceVulnerabilityCVEResolved:
		if imageRef != "" {
			return cveID + " resolved for image " + imageRef
		}
		return cveID + " resolved"
	case domain.EventReasonDeviceVulnerabilityCVECritical, domain.EventReasonDeviceVulnerabilityCVEWarning:
		if imageRef != "" {
			return cveID + " (CVSS " + formatCvssScoreOneDecimal(cvss) + ", " + sevLabel + ") detected on image " + imageRef
		}
		return cveID + " (CVSS " + formatCvssScoreOneDecimal(cvss) + ", " + sevLabel + ") detected"
	default:
		return cveID
	}
}

func severityLabelForDeviceCVE(cvss float64, severity string, warningTh, criticalTh float64) string {
	if cvss >= criticalTh {
		return "Critical"
	}
	if cvss >= warningTh {
		return "Warning"
	}
	if severity != "" {
		return severity
	}
	return "Unknown"
}

func formatCvssScoreOneDecimal(cvss float64) string {
	return strconv.FormatFloat(cvss, 'f', 1, 64)
}
