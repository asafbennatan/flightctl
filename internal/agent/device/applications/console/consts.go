package console

import "time"

const (
	// serialSocketPath is the Unix socket path for the VM's serial console
	// inside the virt-launcher compute container.
	serialSocketPath = "/var/run/kubevirt-private/virt-serial0"

	// socketRetryTimeout is the maximum time to wait for the serial socket
	// to become available (e.g., while the VM is starting).
	socketRetryTimeout = 30 * time.Second

	// socketRetryInterval is the delay between consecutive dial attempts.
	socketRetryInterval = 500 * time.Millisecond

	// cleanupDuration is how long inactive sessions are retained before
	// being removed from the inactive list.
	cleanupDuration = 5 * time.Minute
)
