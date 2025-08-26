package publisher

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/agent/client"
	"github.com/flightctl/flightctl/internal/agent/device/errors"
	"github.com/flightctl/flightctl/pkg/log"
	"github.com/flightctl/flightctl/pkg/ring_buffer"
	"k8s.io/apimachinery/pkg/util/wait"
)

type Subscription = *ring_buffer.RingBuffer[*v1alpha1.Device]

func NewSubscription() Subscription {
	return ring_buffer.NewRingBuffer[*v1alpha1.Device](3)
}

// ReEnrollmentCallback defines the callback function signature for re-enrollment
type ReEnrollmentCallback func(ctx context.Context) error

type Publisher interface {
	Run(ctx context.Context, wg *sync.WaitGroup)
	Subscribe() Subscription
	SetClient(client.Management)
	SetReEnrollmentCallback(ReEnrollmentCallback)
}

type publisher struct {
	managementClient     client.Management
	deviceName           string
	subscribers          []Subscription
	lastKnownVersion     string
	interval             time.Duration
	stopped              atomic.Bool
	log                  *log.PrefixLogger
	backoff              wait.Backoff
	mu                   sync.Mutex
	reEnrollmentCallback ReEnrollmentCallback
}

func New(deviceName string,
	interval time.Duration,
	backoff wait.Backoff,
	log *log.PrefixLogger) Publisher {
	return &publisher{
		deviceName: deviceName,
		interval:   interval,
		backoff:    backoff,
		log:        log,
	}
}

func (n *publisher) getRenderedFromManagementAPIWithRetry(
	ctx context.Context,
	renderedVersion string,
	rendered *v1alpha1.Device,
) (bool, error) {
	params := &v1alpha1.GetRenderedDeviceParams{}
	if renderedVersion != "" {
		params.KnownRenderedVersion = &renderedVersion
	}

	resp, statusCode, err := n.managementClient.GetRenderedDevice(ctx, n.deviceName, params)
	if err != nil {
		return false, fmt.Errorf("%w: %w", errors.ErrGettingDeviceSpec, err)
	}

	switch statusCode {
	case http.StatusOK:
		if resp == nil {
			// 200 OK but response is nil
			return false, errors.ErrNilResponse
		}
		*rendered = *resp
		return true, nil

	case http.StatusNoContent, http.StatusConflict:
		// instead of treating it as an error indicate that no new content is available
		return true, errors.ErrNoContent

	default:
		// For 5xx errors, the management client will already handle infinite retry
		// For other unexpected status codes, return error
		if statusCode >= 500 && statusCode < 600 {
			// This should not happen as the management client handles 5xx retries
			return false, fmt.Errorf("%w: unexpected 5xx status code %d (should be handled by retry logic)", errors.ErrGettingDeviceSpec, statusCode)
		}
		return false, fmt.Errorf("%w: unexpected status code %d", errors.ErrGettingDeviceSpec, statusCode)
	}
}

func (n *publisher) Subscribe() Subscription {
	n.mu.Lock()
	defer n.mu.Unlock()
	sub := NewSubscription()
	n.subscribers = append(n.subscribers, sub)
	if n.stopped.Load() {
		sub.Stop()
	}
	return sub
}

func (n *publisher) SetClient(client client.Management) {
	n.managementClient = client
}

func (n *publisher) SetReEnrollmentCallback(callback ReEnrollmentCallback) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.reEnrollmentCallback = callback
}

func (n *publisher) handleDeviceNotFound(ctx context.Context) {
	n.mu.Lock()
	callback := n.reEnrollmentCallback
	n.mu.Unlock()

	if callback == nil {
		n.log.Error("Device not found but no re-enrollment callback is set")
		return
	}

	n.log.Info("Initiating device re-enrollment due to 404 error")
	if err := callback(ctx); err != nil {
		n.log.Errorf("Re-enrollment failed: %v", err)
		return
	}

	n.log.Info("Re-enrollment completed successfully, resuming normal operation")
}

func (n *publisher) pollAndPublish(ctx context.Context) {
	if n.stopped.Load() {
		n.log.Debug("Publisher is stopped, skipping poll")
		return
	}

	newDesired := &v1alpha1.Device{}

	startTime := time.Now()
	err := wait.ExponentialBackoff(n.backoff, func() (bool, error) {
		return n.getRenderedFromManagementAPIWithRetry(ctx, n.lastKnownVersion, newDesired)
	})

	// log slow calls
	duration := time.Since(startTime)
	if duration > time.Minute {
		n.log.Debugf("Dialing management API took: %v", duration)
	}
	if err != nil {
		if errors.Is(err, errors.ErrNoContent) || errors.IsTimeoutError(err) {
			n.log.Debug("No new template version from management service")
			return
		}

		// Check for device not found error and trigger re-enrollment
		if errors.Is(err, errors.ErrDeviceNotFound) {
			n.log.Warn("Device not found on server, triggering re-enrollment")
			n.handleDeviceNotFound(ctx)
			return
		}

		n.log.Errorf("Received non-retryable error from management service: %v", err)
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	n.lastKnownVersion = newDesired.Version()

	// notify all subscribers of the new device spec
	for _, sub := range n.subscribers {
		if err := sub.Push(newDesired); err != nil {
			n.log.Errorf("Failed to notify subscriber: %v", err)
		}
	}
}

func (n *publisher) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	defer n.stop()
	n.log.Debug("Starting publisher")
	ticker := time.NewTicker(n.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			n.log.Debug("Publisher context done")
			return
		case <-ticker.C:
			n.pollAndPublish(ctx)
		}
	}
}

func (n *publisher) stop() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.stopped.Store(true)
	for _, sub := range n.subscribers {
		sub.Stop()
	}
}
