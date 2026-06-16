package console

import "time"

const (
	// SerialSocketPath is the Unix socket path for the VM's serial console
	// inside the virt-launcher compute container.
	SerialSocketPath = "/var/run/kubevirt-private/virt-serial0"

	// cleanupDuration is how long inactive sessions are retained before
	// being removed from the inactive list.
	cleanupDuration = 5 * time.Minute
)
