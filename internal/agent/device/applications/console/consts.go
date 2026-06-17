package console

import "time"

const (
	// ConsoleProxyPort is the port the console proxy sidecar listens on.
	// The sidecar is injected by kubevirt-vm-to-pod --add-console-proxy and
	// bridges the VM's serial socket over WebSocket.
	ConsoleProxyPort = 8080

	// ConsoleProxyPath is the WebSocket path served by the console proxy sidecar.
	ConsoleProxyPath = "/console"

	// cleanupDuration is how long inactive sessions are retained before
	// being removed from the inactive list.
	cleanupDuration = 5 * time.Minute
)
