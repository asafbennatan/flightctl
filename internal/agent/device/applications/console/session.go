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

// DialFunc opens a bidirectional connection to the VM serial console.
// The production implementation (in PodmanMonitor.resolveConsole) dials
// the container's socket directly via /proc/<pid>/root/<socket>.
// Tests may inject a mock via NewVMSerialSession.
type DialFunc func(containerName string) (io.ReadWriteCloser, error)

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
	if _, err := conn.Write([]byte("\r")); err != nil {
		s.log.Debugf("failed to send initial CR to serial console: %v", err)
	}

	s.bridge(ctx, conn, streamClient)
}

// bridge copies data bidirectionally between the serial connection and the gRPC
// stream. It returns when either side closes.
func (s *vmSerialSession) bridge(ctx context.Context, conn io.ReadWriteCloser, streamClient grpc_v1.RouterService_StreamClient) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	// serial connection → gRPC stream
	go func() {
		defer wg.Done()
		defer cancel()
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				s.log.Debugf("serial→gRPC: %d bytes: %q", n, buf[:n])
				if sendErr := streamClient.Send(&grpc_v1.StreamRequest{Payload: buf[:n]}); sendErr != nil {
					s.log.Debugf("send to gRPC stream failed: %v", sendErr)
					return
				}
			}
			if err != nil {
				s.log.Debugf("serial connection read error: %v", err)
				return
			}
		}
	}()

	// gRPC stream → serial connection
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
				s.log.Debugf("gRPC→serial: %d bytes: %q", len(msg.Payload), msg.Payload)
				if _, writeErr := conn.Write(msg.Payload); writeErr != nil {
					s.log.Debugf("write to serial connection failed: %v", writeErr)
					return
				}
			}
		}
	}()

	wg.Wait()
}

// Ensure net.Conn satisfies io.ReadWriteCloser so existing test helpers that
// return net.Pipe() remain compatible with bridge's updated signature.
var _ io.ReadWriteCloser = (net.Conn)(nil)
