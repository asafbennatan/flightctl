package errors

import (
	"context"
	"errors"
)

// DeviceNotFoundHandler is a global handler instance
var globalHandler *GlobalDeviceNotFoundHandler

// SetGlobalHandler sets the global device not found handler
func SetGlobalHandler(handler *GlobalDeviceNotFoundHandler) {
	globalHandler = handler
}

// WrapError wraps an error and checks for device 404s
func WrapError(ctx context.Context, err error) error {
	if globalHandler != nil {
		return globalHandler.HandleError(ctx, err)
	}
	return err
}

// IsDeviceNotFound checks if an error is a device not found error
func IsDeviceNotFound(err error) bool {
	return errors.Is(err, ErrDeviceNotFound)
}

// HandleDeviceNotFound is a convenience function for handling device 404s
func HandleDeviceNotFound(ctx context.Context, err error) error {
	if IsDeviceNotFound(err) && globalHandler != nil {
		return globalHandler.HandleError(ctx, err)
	}
	return err
}
