package remote_access_server

import (
	"context"
	"testing"

	pb "github.com/flightctl/flightctl/api/grpc/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// stubStreamServer is a minimal implementation of pb.RouterService_StreamServer
// used in unit tests that do not need real transport behaviour.
type stubStreamServer struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *stubStreamServer) Send(*pb.StreamResponse) error    { return nil }
func (s *stubStreamServer) Recv() (*pb.StreamRequest, error) { return nil, nil }
func (s *stubStreamServer) Context() context.Context         { return s.ctx }
func (s *stubStreamServer) SendMsg(interface{}) error        { return nil }
func (s *stubStreamServer) RecvMsg(interface{}) error        { return nil }
func (s *stubStreamServer) SetHeader(metadata.MD) error      { return nil }
func (s *stubStreamServer) SendHeader(metadata.MD) error     { return nil }
func (s *stubStreamServer) SetTrailer(metadata.MD)           {}

func TestServerStream_MissingMetadata(t *testing.T) {
	t.Parallel()
	srv := &Server{}
	stream := &stubStreamServer{ctx: context.Background()}

	err := srv.Stream(stream)
	if err == nil {
		t.Fatal("When Stream is called without metadata it should return an error")
	}
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", code)
	}
}

func TestServerStream_MissingSessionID(t *testing.T) {
	t.Parallel()
	srv := &Server{}
	md := metadata.New(map[string]string{})
	ctx := metadata.NewIncomingContext(context.Background(), md)
	stream := &stubStreamServer{ctx: ctx}

	err := srv.Stream(stream)
	if err == nil {
		t.Fatal("When Stream is called without session ID it should return an error")
	}
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", code)
	}
}
