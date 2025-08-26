package errors

import (
	"context"
	"errors"
	"sync"

	"github.com/flightctl/flightctl/pkg/log"
)

// GlobalDeviceNotFoundHandler provides centralized handling of device 404 errors
type GlobalDeviceNotFoundHandler struct {
	mu       sync.RWMutex
	callback func(ctx context.Context) error
	log      *log.PrefixLogger
}

// NewGlobalDeviceNotFoundHandler creates a new global handler
func NewGlobalDeviceNotFoundHandler(log *log.PrefixLogger) *GlobalDeviceNotFoundHandler {
	return &GlobalDeviceNotFoundHandler{
		log: log,
	}
}

// SetCallback sets the re-enrollment callback
func (h *GlobalDeviceNotFoundHandler) SetCallback(callback func(ctx context.Context) error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.callback = callback
}

// HandleError checks if an error is a device 404 and triggers re-enrollment
func (h *GlobalDeviceNotFoundHandler) HandleError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}

	// Check if this is a device not found error
	if errors.Is(err, ErrDeviceNotFound) {
		h.mu.RLock()
		callback := h.callback
		h.mu.RUnlock()

		if callback == nil {
			h.log.Error("Device not found but no re-enrollment callback is set")
			return err
		}

		h.log.Warn("Device not found, triggering re-enrollment")
		go func() {
			if reEnrollErr := callback(ctx); reEnrollErr != nil {
				h.log.Errorf("Re-enrollment failed: %v", reEnrollErr)
			} else {
				h.log.Info("Re-enrollment completed successfully")
			}
		}()
	}

	return err
}
