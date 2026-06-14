package transportv1beta1

import (
	convertv1beta1 "github.com/flightctl/flightctl/internal/api/convert/v1beta1"
	"github.com/flightctl/flightctl/internal/api/server"
	"github.com/flightctl/flightctl/internal/auth"
	"github.com/flightctl/flightctl/internal/auth/common"
	"github.com/flightctl/flightctl/internal/console"
	"github.com/flightctl/flightctl/internal/crypto"
	"github.com/flightctl/flightctl/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/sirupsen/logrus"
)

type TransportHandler struct {
	serviceHandler    service.Service
	converter         convertv1beta1.Converter
	authN             common.AuthNMiddleware
	authTokenProxy    *service.AuthTokenProxy
	authUserInfoProxy *service.AuthUserInfoProxy
	authZ             auth.AuthZMiddleware
}

type WebsocketHandler struct {
	ca                       *crypto.CAClient
	log                      logrus.FieldLogger
	consoleSessionManager    *console.ConsoleSessionManager
	appConsoleSessionManager *console.AppConsoleSessionManager
}

// Make sure we conform to servers Transport interface
var _ server.Transport = (*TransportHandler)(nil)

func NewTransportHandler(serviceHandler service.Service, converter convertv1beta1.Converter, authN common.AuthNMiddleware, authTokenProxy *service.AuthTokenProxy, authUserInfoProxy *service.AuthUserInfoProxy, authZ auth.AuthZMiddleware) *TransportHandler {
	return &TransportHandler{
		serviceHandler:    serviceHandler,
		converter:         converter,
		authN:             authN,
		authTokenProxy:    authTokenProxy,
		authUserInfoProxy: authUserInfoProxy,
		authZ:             authZ,
	}
}

func NewWebsocketHandler(ca *crypto.CAClient, log logrus.FieldLogger, consoleSessionManager *console.ConsoleSessionManager, appConsoleSessionManager *console.AppConsoleSessionManager) *WebsocketHandler {
	return &WebsocketHandler{
		ca:                       ca,
		log:                      log,
		consoleSessionManager:    consoleSessionManager,
		appConsoleSessionManager: appConsoleSessionManager,
	}
}

func (h *WebsocketHandler) RegisterRoutes(r chi.Router) {
	if h.consoleSessionManager != nil {
		r.Get("/ws/v1/devices/{name}/console", h.HandleDeviceConsole)
	}
	if h.appConsoleSessionManager != nil {
		r.Get("/ws/v1/devices/{name}/applications/{appName}/console", h.HandleApplicationConsole)
	}
}
