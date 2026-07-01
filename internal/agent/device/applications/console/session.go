package console

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"

	grpc_v1 "github.com/flightctl/flightctl/api/grpc/v1"
	"github.com/flightctl/flightctl/pkg/log"
)

// DialFunc opens a bidirectional connection to a VM console socket.
// The production implementation runs `podman exec -i nc -U <socket>` inside the compute container.
// Tests may inject a mock via NewVMSerialSession.
type DialFunc func(containerName string) (io.ReadWriteCloser, error)

// bridgeConn copies data bidirectionally between conn and streamClient until
// either side closes or ctx is canceled. label is used only for debug logging.
func bridgeConn(ctx context.Context, label string, conn io.ReadWriteCloser, streamClient grpc_v1.RouterService_StreamClient, logger *log.PrefixLogger) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	// conn → gRPC stream
	go func() {
		defer wg.Done()
		defer cancel()
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				logger.Debugf("%s→gRPC: %d bytes", label, n)
				if sendErr := streamClient.Send(&grpc_v1.StreamRequest{Payload: buf[:n]}); sendErr != nil {
					logger.Debugf("send to gRPC stream failed: %v", sendErr)
					return
				}
			}
			if err != nil {
				logger.Debugf("%s connection read error: %v", label, err)
				return
			}
		}
	}()

	// gRPC stream → conn
	go func() {
		defer wg.Done()
		defer cancel()
		for {
			msg, err := streamClient.Recv()
			if err == io.EOF || (msg != nil && msg.Closed) {
				return
			}
			if err != nil {
				logger.Debugf("recv from gRPC stream failed: %v", err)
				return
			}
			if len(msg.Payload) > 0 {
				logger.Debugf("gRPC→%s: %d bytes", label, len(msg.Payload))
				remaining := msg.Payload
				for len(remaining) > 0 {
					n, writeErr := conn.Write(remaining)
					if writeErr != nil {
						logger.Debugf("write to %s connection failed: %v", label, writeErr)
						return
					}
					remaining = remaining[n:]
				}
			}
		}
	}()

	wg.Wait()
}

// vmSerialSession implements Session for VM serial console.
// Created by PodmanMonitor.resolveConsole for AppTypeVm + "serial".
type vmSerialSession struct {
	containerName string
	dialFn        DialFunc
	log           *log.PrefixLogger
}

// NewVMSerialSession returns a Session that bridges the VM's serial socket to
// the gRPC stream. dialFn must not be nil; the production implementation is
// provided by PodmanMonitor.resolveConsole.
func NewVMSerialSession(containerName string, dialFn DialFunc, log *log.PrefixLogger) Session {
	return &vmSerialSession{
		containerName: containerName,
		dialFn:        dialFn,
		log:           log,
	}
}

// Run implements Session. It dials the container and bridges the connection to
// the gRPC stream.
func (s *vmSerialSession) Run(ctx context.Context, streamClient grpc_v1.RouterService_StreamClient) {
	s.log.Debugf("vm serial console session started for container %s", s.containerName)
	defer s.log.Debugf("vm serial console session finished for container %s", s.containerName)

	conn, err := s.dialFn(s.containerName)
	if err != nil {
		sendErrorOverStream(streamClient, fmt.Sprintf("failed to connect to serial console for %s: %v", s.containerName, err))
		return
	}
	defer conn.Close()

	// Send an initial CR to wake up agetty, which waits for the first character
	// before displaying the login prompt (baud-rate detection on real hardware).
	// Done asynchronously so that a slow or synchronous connection (e.g. net.Pipe
	// in tests) does not block bridge startup.
	go func() {
		if _, err := conn.Write([]byte("\r")); err != nil {
			s.log.Debugf("failed to send initial CR to serial console: %v", err)
		}
	}()

	bridgeConn(ctx, "serial", conn, streamClient, s.log)
}

// vmVNCSession implements Session for VM VNC console.
// Created by PodmanMonitor.resolveConsole for AppTypeVm + "vnc".
type vmVNCSession struct {
	containerName string
	dialFn        DialFunc
	log           *log.PrefixLogger
}

// NewVMVNCSession returns a Session that bridges the VM's VNC socket to
// the gRPC stream. dialFn must not be nil; the production implementation is
// provided by PodmanMonitor.resolveConsole.
func NewVMVNCSession(containerName string, dialFn DialFunc, log *log.PrefixLogger) Session {
	return &vmVNCSession{
		containerName: containerName,
		dialFn:        dialFn,
		log:           log,
	}
}

// Run implements Session. It dials the container and bridges the VNC connection
// to the gRPC stream. No initial byte is sent — VNC clients initiate the
// RFB handshake themselves.
func (s *vmVNCSession) Run(ctx context.Context, streamClient grpc_v1.RouterService_StreamClient) {
	s.log.Debugf("vm vnc console session started for container %s", s.containerName)
	defer s.log.Debugf("vm vnc console session finished for container %s", s.containerName)

	conn, err := s.dialFn(s.containerName)
	if err != nil {
		sendErrorOverStream(streamClient, fmt.Sprintf("failed to connect to VNC console for %s: %v", s.containerName, err))
		return
	}
	defer conn.Close()

	bridgeConn(ctx, "vnc", conn, streamClient, s.log)
}

// Ensure net.Conn satisfies io.ReadWriteCloser so existing test helpers that
// return net.Pipe() remain compatible with bridgeConn's signature.
var _ io.ReadWriteCloser = (net.Conn)(nil)
