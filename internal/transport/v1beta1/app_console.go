package transportv1beta1

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/flightctl/flightctl/internal/transport"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

func (h *WebsocketHandler) HandleApplicationConsole(w http.ResponseWriter, r *http.Request) {
	deviceName := chi.URLParam(r, "name")
	appName := chi.URLParam(r, "appname")

	h.log.Infof("websocket application console connection requested for device: %s app: %s", deviceName, appName)

	consoleType := r.URL.Query().Get("consoleType")
	if consoleType == "" {
		http.Error(w, "consoleType is required", http.StatusBadRequest)
		return
	}
	if consoleType != "serial" {
		http.Error(w, fmt.Sprintf("invalid consoleType %q: must be \"serial\"", consoleType), http.StatusBadRequest)
		return
	}

	orgId := transport.OrgIDFromContext(r.Context())

	session, status := h.appConsoleSessionManager.StartSession(r.Context(), orgId, deviceName, appName, consoleType)
	if status.Code != http.StatusOK {
		http.Error(w, status.Message, int(status.Code))
		return
	}

	sessionStarted := true
	closeSession := func() {
		if !sessionStarted {
			return
		}
		if closeStatus := h.appConsoleSessionManager.CloseSession(r.Context(), session); closeStatus.Code != http.StatusOK {
			h.log.Errorf("error closing app console session %s for device %s app %s: %v", session.UUID, deviceName, appName, closeStatus.Message)
		}
		sessionStarted = false
	}
	defer closeSession()

	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	var (
		selectedProtocol string
		ok               bool
	)
	select {
	case selectedProtocol, ok = <-session.ProtocolCh:
		if !ok {
			h.log.Errorf("failed selecting protocol for device: %s app: %s", deviceName, appName)
			http.Error(w,
				fmt.Sprintf("failed selecting protocol for device: %s app: %s", deviceName, appName),
				http.StatusInternalServerError)
			return
		}
	case <-timer.C:
		h.log.Errorf("timed out waiting for protocol for device: %s app: %s", deviceName, appName)
		http.Error(w,
			fmt.Sprintf("timed out waiting for protocol for device: %s app: %s", deviceName, appName),
			http.StatusGatewayTimeout)
		return
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
		Subprotocols: []string{selectedProtocol},
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Errorf("failed to upgrade connection to WebSocket for device %s app %s: %v", deviceName, appName, err)
		return
	}

	stopWriter := make(chan struct{})
	wg := sync.WaitGroup{}
	wg.Add(2)

	go func() {
		defer func() {
			close(stopWriter)
			close(session.SendCh)
			wg.Done()
		}()
		for {
			msgType, message, err := conn.ReadMessage()
			if err != nil {
				h.log.Infof("websocket app console session %s closed for device %s app %s: %v", session.UUID, deviceName, appName, err)
				break
			}
			if msgType == websocket.BinaryMessage {
				session.SendCh <- message
			} else {
				h.log.Warningf("received unexpected message type %d from app console websocket session %s for device %s app %s",
					msgType, session.UUID, deviceName, appName)
			}
		}
	}()

	go func() {
		defer func() {
			h.log.Debugf("sending close message to app console websocket for device %s app %s", deviceName, appName)
			if err := conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
				time.Now().Add(time.Second*5),
			); err != nil {
				h.log.Errorf("failed to write close message to app console websocket for session %s: %v", session.UUID, err)
			}
			conn.Close()
			wg.Done()
		}()
		for {
			select {
			case <-stopWriter:
				h.log.Debugf("app console device channel closed for session %s", session.UUID)
				return
			case message, ok := <-session.RecvCh:
				if !ok {
					h.log.Debugf("app console channel from device closed for session %s", session.UUID)
					return
				}
				if err := conn.WriteMessage(websocket.BinaryMessage, message); err != nil {
					h.log.Errorf("failed to write message to app console websocket for device %s app %s: %v", deviceName, appName, err)
					return
				}
			}
		}
	}()

	wg.Wait()
	h.log.Infof("ending app console session %s to device %s app %s", session.UUID, deviceName, appName)
}
