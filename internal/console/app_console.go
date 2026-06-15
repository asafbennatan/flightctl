package console

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/flightctl/flightctl/internal/consts"
	"github.com/flightctl/flightctl/internal/domain"
	"github.com/flightctl/flightctl/internal/flterrors"
	"github.com/flightctl/flightctl/internal/util"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
)

// AppConsoleSession is the server-side session object bridging the WebSocket handler
// and the gRPC stream from the agent.
type AppConsoleSession struct {
	UUID       string
	OrgId      uuid.UUID
	DeviceName string
	AppName    string
	SendCh     chan []byte
	RecvCh     chan []byte
	ProtocolCh chan string
}

// AppConsoleDeviceService is the narrow interface AppConsoleSessionManager needs,
// avoiding a full service.Service dependency in flightctl-remote-access.
type AppConsoleDeviceService interface {
	GetDevice(ctx context.Context, orgId uuid.UUID, name string) (*domain.Device, domain.Status)
	UpdateDevice(ctx context.Context, orgId uuid.UUID, name string, device domain.Device, fieldsToUnset []string) (*domain.Device, error)
}

// AppConsoleSessionRegistration is implemented by the gRPC server that the agent connects to.
type AppConsoleSessionRegistration interface {
	StartSession(session *AppConsoleSession) error
	CloseSession(session *AppConsoleSession) error
}

// RenderedVersionPublisher publishes rendered version change notifications.
type RenderedVersionPublisher interface {
	Publish(ctx context.Context, orgId uuid.UUID, name string, renderedVersion string) error
}

// AppConsoleSessionManager manages application console sessions using device annotations.
// It mirrors ConsoleSessionManager, keyed on DeviceAnnotationRemoteSession with
// per-appName uniqueness enforcement (stored as a JSON array of DeviceRemoteSession).
type AppConsoleSessionManager struct {
	svc                 AppConsoleDeviceService
	log                 logrus.FieldLogger
	sessionRegistration AppConsoleSessionRegistration
	publisher           RenderedVersionPublisher
}

func NewAppConsoleSessionManager(
	svc AppConsoleDeviceService,
	log logrus.FieldLogger,
	reg AppConsoleSessionRegistration,
	publisher RenderedVersionPublisher,
) *AppConsoleSessionManager {
	return &AppConsoleSessionManager{
		svc:                 svc,
		log:                 log,
		sessionRegistration: reg,
		publisher:           publisher,
	}
}

func (m *AppConsoleSessionManager) modifyAnnotations(ctx context.Context, orgId uuid.UUID, deviceName string, updater func(string) (string, error)) domain.Status {
	var (
		err                 error
		newValue            string
		nextRenderedVersion string
	)
	for i := 0; i != 10; i++ {
		device, status := m.svc.GetDevice(ctx, orgId, deviceName)
		if status.Code != http.StatusOK {
			return status
		}
		device.Metadata.Annotations = lo.ToPtr(util.EnsureMap(lo.FromPtr(device.Metadata.Annotations)))

		annotations := lo.FromPtr(device.Metadata.Annotations)
		if waitingValue, exists := annotations[domain.DeviceAnnotationAwaitingReconnect]; exists && waitingValue == "true" {
			return domain.StatusConflict("Device is awaiting reconnection after restore")
		}
		if pausedValue, exists := annotations[domain.DeviceAnnotationConflictPaused]; exists && pausedValue == "true" {
			return domain.StatusConflict("Device is paused due to conflicts")
		}
		if device.Spec != nil && device.Spec.Decommissioning != nil {
			return domain.StatusConflict("Device is decommissioned")
		}

		value, _ := util.GetFromMap(annotations, domain.DeviceAnnotationRemoteSession)
		newValue, err = updater(value)
		if err != nil {
			var dupErr *duplicateAppSessionError
			if errors.As(err, &dupErr) {
				return domain.StatusConflict(dupErr.Error())
			}
			return domain.StatusInternalServerError(err.Error())
		}
		(*device.Metadata.Annotations)[domain.DeviceAnnotationRemoteSession] = newValue
		nextRenderedVersion, err = domain.GetNextDeviceRenderedVersion(*device.Metadata.Annotations, device.Status)
		if err != nil {
			return domain.StatusInternalServerError(err.Error())
		}
		(*device.Metadata.Annotations)[domain.DeviceAnnotationRenderedVersion] = nextRenderedVersion
		m.log.Infof("About to save annotations %+v", *device.Metadata.Annotations)
		_, err = m.svc.UpdateDevice(context.WithValue(ctx, consts.InternalRequestCtxKey, true), orgId, deviceName, *device, nil)
		if !errors.Is(err, flterrors.ErrResourceVersionConflict) {
			break
		}
	}
	if err == nil {
		if pubErr := m.publisher.Publish(ctx, orgId, deviceName, nextRenderedVersion); pubErr != nil {
			m.log.WithError(pubErr).Errorf("annotation for device %s persisted but rendered-version notification failed", deviceName)
			return domain.StatusInternalServerError(pubErr.Error())
		}
	}
	if err != nil {
		return domain.StatusInternalServerError(err.Error())
	}
	return domain.StatusOK()
}

// addAppSession returns an updater closure that appends a new session entry, returning 409 if
// an entry for appName already exists.
func addAppSession(sessionID, appName, consoleType string) func(string) (string, error) {
	return func(existing string) (string, error) {
		var sessions []domain.DeviceRemoteSession
		if existing != "" {
			if err := json.Unmarshal([]byte(existing), &sessions); err != nil {
				return "", err
			}
		}
		for _, s := range sessions {
			if s.AppName == appName {
				return "", &duplicateAppSessionError{appName: appName}
			}
		}
		sessions = append(sessions, domain.DeviceRemoteSession{
			SessionID:   sessionID,
			AppName:     appName,
			ConsoleType: consoleType,
		})
		b, err := json.Marshal(&sessions)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}

// removeAppSession returns an updater closure that removes the session entry for the given sessionID.
func removeAppSession(sessionID string) func(string) (string, error) {
	return func(existing string) (string, error) {
		if existing == "" {
			return "", nil
		}
		var sessions []domain.DeviceRemoteSession
		if err := json.Unmarshal([]byte(existing), &sessions); err != nil {
			return "", err
		}
		filtered := sessions[:0]
		for _, s := range sessions {
			if s.SessionID != sessionID {
				filtered = append(filtered, s)
			}
		}
		b, err := json.Marshal(&filtered)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}

// duplicateAppSessionError signals a 409 conflict when a session for the same appName already exists.
type duplicateAppSessionError struct {
	appName string
}

func (e *duplicateAppSessionError) Error() string {
	return "console session already active for application " + e.appName
}

// StartSession validates inputs, guards against duplicates via annotation, and registers the session.
func (m *AppConsoleSessionManager) StartSession(ctx context.Context, orgId uuid.UUID, deviceName, appName, consoleType string) (*AppConsoleSession, domain.Status) {
	if appName == "" {
		return nil, domain.StatusBadRequest("appName is required")
	}
	if consoleType == "" {
		return nil, domain.StatusBadRequest("consoleType is required")
	}
	if consoleType != "serial" {
		return nil, domain.StatusBadRequest("invalid consoleType: must be \"serial\"")
	}

	device, status := m.svc.GetDevice(ctx, orgId, deviceName)
	if status.Code != http.StatusOK {
		return nil, status
	}
	if device.Spec != nil && device.Spec.Decommissioning != nil {
		return nil, domain.StatusConflict("Device is decommissioned")
	}
	annotations := util.EnsureMap(lo.FromPtr(device.Metadata.Annotations))
	if waitingValue, exists := annotations[domain.DeviceAnnotationAwaitingReconnect]; exists && waitingValue == "true" {
		return nil, domain.StatusConflict("Device is awaiting reconnection after restore")
	}
	if pausedValue, exists := annotations[domain.DeviceAnnotationConflictPaused]; exists && pausedValue == "true" {
		return nil, domain.StatusConflict("Device is paused due to conflicts")
	}

	// Check for duplicate session for this appName in the current annotation value (fast path).
	if val, ok := annotations[domain.DeviceAnnotationRemoteSession]; ok && val != "" {
		var sessions []domain.DeviceRemoteSession
		if err := json.Unmarshal([]byte(val), &sessions); err == nil {
			for _, s := range sessions {
				if s.AppName == appName {
					return nil, domain.StatusConflict("console session already active for application " + appName)
				}
			}
		}
	}

	session := &AppConsoleSession{
		UUID:       uuid.New().String(),
		OrgId:      orgId,
		DeviceName: deviceName,
		AppName:    appName,
		SendCh:     make(chan []byte, ChannelSize),
		RecvCh:     make(chan []byte, ChannelSize),
		ProtocolCh: make(chan string),
	}

	if status := m.modifyAnnotations(ctx, orgId, deviceName, addAppSession(session.UUID, appName, consoleType)); status.Code != http.StatusOK {
		if _, deviceStatus := m.svc.GetDevice(ctx, orgId, deviceName); deviceStatus.Code != http.StatusOK {
			return nil, deviceStatus
		}
		return nil, status
	}

	if err := m.sessionRegistration.StartSession(session); err != nil {
		m.log.Errorf("Failed to start app console session %s for device %s app %s: %v, rolling back annotation", session.UUID, deviceName, appName, err)
		if annStatus := m.modifyAnnotations(ctx, orgId, deviceName, removeAppSession(session.UUID)); annStatus.Code != http.StatusOK {
			m.log.Errorf("Failed to remove annotation from device %s: %v", deviceName, annStatus)
		}
		return nil, domain.StatusInternalServerError(err.Error())
	}
	return session, domain.StatusOK()
}

// CloseSession removes the annotation entry and unregisters the session.
func (m *AppConsoleSessionManager) CloseSession(ctx context.Context, session *AppConsoleSession) domain.Status {
	closeErr := m.sessionRegistration.CloseSession(session)
	if status := m.modifyAnnotations(ctx, session.OrgId, session.DeviceName, removeAppSession(session.UUID)); status.Code != http.StatusOK {
		return status
	}
	if closeErr != nil {
		return domain.StatusInternalServerError(closeErr.Error())
	}
	return domain.StatusOK()
}
