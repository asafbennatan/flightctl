package console

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	grpc_v1 "github.com/flightctl/flightctl/api/grpc/v1"
	"github.com/flightctl/flightctl/pkg/executer"
	"github.com/flightctl/flightctl/pkg/log"
)

// DialFunc is the injectable Unix socket dialer. nil uses net.Dial("unix", path).
type DialFunc func(socketPath string) (net.Conn, error)

var defaultDialFn DialFunc = func(socketPath string) (net.Conn, error) {
	return net.Dial("unix", socketPath)
}

// vmSerialSession implements Session for VM serial console via a Unix socket
// bridge. Created by PodmanMonitor.resolveConsole for AppTypeVm + "serial".
type vmSerialSession struct {
	containerName string
	executor      executer.Executer
	dialFn        DialFunc
	log           *log.PrefixLogger
}

// NewVMSerialSession returns a Session that bridges the VM's serial socket to
// the gRPC stream. dialFn may be nil (defaults to net.Dial("unix", ...)).
func NewVMSerialSession(containerName string, executor executer.Executer, dialFn DialFunc, log *log.PrefixLogger) Session {
	if dialFn == nil {
		dialFn = defaultDialFn
	}
	return &vmSerialSession{
		containerName: containerName,
		executor:      executor,
		dialFn:        dialFn,
		log:           log,
	}
}

// Run implements Session. It orchestrates: getContainerPID → dialWithRetry → bridge.
func (s *vmSerialSession) Run(ctx context.Context, streamClient grpc_v1.RouterService_StreamClient) {
	s.log.Debugf("vm serial console session started for container %s", s.containerName)
	defer s.log.Debugf("vm serial console session finished for container %s", s.containerName)

	pid, err := s.getContainerPID(ctx)
	if err != nil {
		sendErrorOverStream(streamClient, fmt.Sprintf("failed to get container PID for %s: %v", s.containerName, err))
		return
	}

	socketPath := fmt.Sprintf("/proc/%s/root%s", pid, serialSocketPath)
	conn, err := s.dialWithRetry(ctx, socketPath)
	if err != nil {
		sendErrorOverStream(streamClient, fmt.Sprintf("failed to connect to serial socket for %s: %v", s.containerName, err))
		return
	}
	defer conn.Close()

	s.bridge(ctx, conn, streamClient)
}

// getContainerPID runs "podman inspect --format {{.State.Pid}} {s.containerName}"
// and returns the PID string.
func (s *vmSerialSession) getContainerPID(ctx context.Context) (string, error) {
	stdout, stderr, exitCode := s.executor.ExecuteWithContext(ctx, "podman", "inspect",
		"--format", "{{.State.Pid}}", s.containerName)
	if exitCode != 0 {
		return "", fmt.Errorf("podman inspect failed (exit %d): %s", exitCode, stderr)
	}
	pid := strings.TrimSpace(stdout)
	if _, err := strconv.Atoi(pid); err != nil {
		return "", fmt.Errorf("invalid PID %q from podman inspect: %w", pid, err)
	}
	return pid, nil
}

// dialWithRetry retries dialFn until success or socketRetryTimeout is exceeded.
func (s *vmSerialSession) dialWithRetry(ctx context.Context, socketPath string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, socketRetryTimeout)
	defer cancel()

	for {
		conn, err := s.dialFn(socketPath)
		if err == nil {
			return conn, nil
		}
		s.log.Debugf("failed to dial serial socket %s: %v, retrying...", socketPath, err)

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out waiting for serial socket %s: %w", socketPath, ctx.Err())
		case <-time.After(socketRetryInterval):
		}
	}
}

// bridge copies data bidirectionally between the serial socket and the gRPC
// stream. It returns when either side closes.
func (s *vmSerialSession) bridge(ctx context.Context, conn net.Conn, streamClient grpc_v1.RouterService_StreamClient) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	// serial socket → gRPC stream
	go func() {
		defer wg.Done()
		defer cancel()
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				if sendErr := streamClient.Send(&grpc_v1.StreamRequest{Payload: buf[:n]}); sendErr != nil {
					s.log.Debugf("send to gRPC stream failed: %v", sendErr)
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					s.log.Debugf("serial socket read error: %v", err)
				}
				return
			}
		}
	}()

	// gRPC stream → serial socket
	go func() {
		defer wg.Done()
		defer cancel()
		for {
			msg, err := streamClient.Recv()
			if err == io.EOF || (msg != nil && msg.Closed) {
				return
			}
			if err != nil {
				s.log.Debugf("recv from gRPC stream failed: %v", err)
				return
			}
			if len(msg.Payload) > 0 {
				if _, writeErr := conn.Write(msg.Payload); writeErr != nil {
					s.log.Debugf("write to serial socket failed: %v", writeErr)
					return
				}
			}
		}
	}()

	wg.Wait()
}
