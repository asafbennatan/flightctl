package remote_access_server

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	pb "github.com/flightctl/flightctl/api/grpc/v1"
	apiserver "github.com/flightctl/flightctl/internal/api_server"
	fcmiddleware "github.com/flightctl/flightctl/internal/api_server/middleware"
	"github.com/flightctl/flightctl/internal/auth"
	"github.com/flightctl/flightctl/internal/auth/authn"
	"github.com/flightctl/flightctl/internal/config"
	"github.com/flightctl/flightctl/internal/console"
	"github.com/flightctl/flightctl/internal/consts"
	"github.com/flightctl/flightctl/internal/crypto"
	"github.com/flightctl/flightctl/internal/domain"
	"github.com/flightctl/flightctl/internal/service"
	"github.com/flightctl/flightctl/internal/store"
	transportv1beta1 "github.com/flightctl/flightctl/internal/transport/v1beta1"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	grpcAuth "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/auth"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Server provides the flightctl-remote-access service: a WebSocket HTTP server
// and a gRPC RouterService that bridges agent streams to active AppConsoleSessions.
type Server struct {
	pb.UnimplementedRouterServiceServer
	log            logrus.FieldLogger
	cfg            *config.Config
	grpcServer     *grpc.Server
	httpListener   net.Listener
	agentListener  net.Listener
	agentTLSConfig *tls.Config
	pendingStreams  *sync.Map
	// httpHandler serves port 3444: user-facing WebSocket console (API-like middleware).
	httpHandler http.Handler
	// identityMapper maps authenticated identities to DB organisations; must be Start()ed in Run().
	identityMapper *service.IdentityMapper
}

// storeAppConsoleService adapts store.Device to console.AppConsoleDeviceService.
type storeAppConsoleService struct {
	deviceStore store.Device
}

func (s *storeAppConsoleService) GetDevice(ctx context.Context, orgId uuid.UUID, name string) (*domain.Device, domain.Status) {
	result, err := s.deviceStore.Get(ctx, orgId, name)
	return result, service.StoreErrorToApiStatus(err, false, domain.DeviceKind, &name)
}

func (s *storeAppConsoleService) UpdateDevice(ctx context.Context, orgId uuid.UUID, name string, device domain.Device, fieldsToUnset []string) (*domain.Device, error) {
	return s.deviceStore.Update(ctx, orgId, &device, fieldsToUnset, false, nil, nil)
}

// New creates a Server with the required dependencies. The db store, KV-backed
// console.RenderedVersionPublisher, and auth config are needed for annotation management and auth enforcement.
func New(
	log logrus.FieldLogger,
	cfg *config.Config,
	ca *crypto.CAClient,
	serverCerts *crypto.TLSCertificateConfig,
	dataStore store.Store,
	publisher console.RenderedVersionPublisher,
	multiAuth *authn.MultiAuth,
) (*Server, error) {
	tlsConfig, agentTLSConfig, err := crypto.TLSConfigForServer(ca.GetCABundleX509(), serverCerts)
	if err != nil {
		return nil, err
	}

	httpListener, err := fcmiddleware.NewTLSListener(cfg.Service.Address, tlsConfig)
	if err != nil {
		return nil, err
	}

	// Plain TCP listener — ServeTLS is called in Run() so that Go's net/http
	// stack configures HTTP/2 (ALPN "h2") automatically, which gRPC requires.
	agentListener, err := net.Listen("tcp", cfg.Service.AgentEndpointAddress)
	if err != nil {
		_ = httpListener.Close()
		return nil, err
	}

	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainStreamInterceptor(grpcAuth.StreamServerInterceptor(fcmiddleware.GrpcAuthMiddleware)),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle: 15 * time.Minute,
			Time:              2 * time.Minute,
			Timeout:           20 * time.Second,
		}),
	)

	// Identity mapper: created here, started in Run() to manage its lifecycle with the context.
	orgProvisioner := service.NewOrgProvisioner(dataStore, log)
	identityMapper := service.NewIdentityMapper(dataStore, orgProvisioner, log)
	identityMappingMiddleware := fcmiddleware.NewIdentityMappingMiddleware(identityMapper, log)

	s := &Server{
		log:            log,
		cfg:            cfg,
		grpcServer:     grpcServer,
		httpListener:   httpListener,
		agentListener:  agentListener,
		agentTLSConfig: agentTLSConfig,
		pendingStreams:  &sync.Map{},
		identityMapper: identityMapper,
	}
	pb.RegisterRouterServiceServer(grpcServer, s)

	svc := &storeAppConsoleService{deviceStore: dataStore.Device()}
	appConsoleMgr := console.NewAppConsoleSessionManager(svc, log, s, publisher)
	ws := transportv1beta1.NewWebsocketHandler(ca, log, nil, appConsoleMgr)

	authZ, err := auth.InitMultiAuthZ(cfg, log)
	if err != nil {
		return nil, err
	}

	// Port 3444: user-facing WebSocket console.
	// Middleware mirrors flightctl-api: AuthN → IdentityMapping → OrgExtraction → AuthZ.
	r := chi.NewRouter()
	if multiAuth != nil {
		r.Use(auth.CreateAuthNMiddleware(multiAuth, log))
	}
	r.Use(identityMappingMiddleware.MapIdentityToDB)
	r.Use(fcmiddleware.ExtractAndValidateOrg(fcmiddleware.QueryOrgIDExtractor, log))
	r.Use(auth.CreateAuthZMiddleware(authZ, log))
	ws.RegisterRoutes(r)
	s.httpHandler = otelhttp.NewHandler(r, "remote-access-http-server")

	return s, nil
}

// Run starts both listeners concurrently and blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	s.identityMapper.Start()
	defer s.identityMapper.Stop()

	// Port 7444: agent gRPC endpoint.
	// HTTP fallback returns 404 — agents connect via gRPC only; gRPC auth is
	// handled by the GrpcAuthMiddleware interceptor on the grpcServer.
	agentSrv := fcmiddleware.NewHTTPServerWithTLSContext(
		grpcMuxHandlerFunc(s.grpcServer, http.NotFoundHandler(), s.log),
		s.log,
		s.cfg.Service.AgentEndpointAddress,
		s.cfg,
	)

	// Port 3444: user-facing WebSocket console.
	httpSrv := fcmiddleware.NewHTTPServer(s.httpHandler, s.log, s.cfg.Service.Address, s.cfg)

	go func() {
		s.log.Printf("Remote-access agent listener on %s", s.agentListener.Addr())
		agentSrv.TLSConfig = s.agentTLSConfig
		if err := agentSrv.ServeTLS(s.agentListener, "", ""); err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, http.ErrServerClosed) {
			s.log.Errorf("agent listener error: %v", err)
		}
	}()

	go func() {
		s.log.Printf("Remote-access HTTP listener on %s", s.httpListener.Addr())
		if err := httpSrv.Serve(s.httpListener); err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, http.ErrServerClosed) {
			s.log.Errorf("HTTP listener error: %v", err)
		}
	}()

	<-ctx.Done()
	s.log.Println("Shutdown signal received:", ctx.Err())
	ctxTimeout, cancel := context.WithTimeout(context.Background(), apiserver.GracefulShutdownTimeout)
	defer cancel()
	_ = agentSrv.Shutdown(ctxTimeout)
	_ = httpSrv.Shutdown(ctxTimeout)
	return nil
}

// StartSession registers an AppConsoleSession so the next gRPC Stream() call from
// the agent can rendezvous with it.
func (s *Server) StartSession(session *console.AppConsoleSession) error {
	s.log.Infof("app console session %s registered for device %s app %s", session.UUID, session.DeviceName, session.AppName)
	s.pendingStreams.Store(session.UUID, session)
	return nil
}

// CloseSession removes a previously registered AppConsoleSession.
func (s *Server) CloseSession(session *console.AppConsoleSession) error {
	s.log.Infof("app console session %s removed for device %s app %s", session.UUID, session.DeviceName, session.AppName)
	s.pendingStreams.Delete(session.UUID)
	return nil
}

// Stream implements pb.RouterServiceServer. When the agent connects it reads the
// x-session-id gRPC metadata key, looks up the matching AppConsoleSession, sends
// the selected protocol to ProtocolCh, and forwards bytes bidirectionally.
func (s *Server) Stream(stream pb.RouterService_StreamServer) error {
	ctx := stream.Context()
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.InvalidArgument, "missing metadata")
	}

	sessionIDs := md.Get(consts.GrpcSessionIDKey)
	if len(sessionIDs) != 1 {
		return status.Error(codes.InvalidArgument, "missing "+consts.GrpcSessionIDKey)
	}
	sessionID := sessionIDs[0]

	val, loaded := s.pendingStreams.LoadAndDelete(sessionID)
	if !loaded {
		s.log.Warnf("agent connected to unknown session %s", sessionID)
		return status.Error(codes.NotFound, "session not found: "+sessionID)
	}

	session, ok := val.(*console.AppConsoleSession)
	if !ok {
		return status.Error(codes.Internal, "invalid session type for "+sessionID)
	}

	selectedProtocols := md.Get(consts.GrpcSelectedProtocolKey)
	if len(selectedProtocols) != 1 {
		close(session.ProtocolCh)
		return status.Error(codes.InvalidArgument, "missing "+consts.GrpcSelectedProtocolKey)
	}
	select {
	case session.ProtocolCh <- selectedProtocols[0]:
	default:
		return status.Error(codes.DeadlineExceeded, "session no longer waiting for protocol negotiation")
	}

	s.log.Infof("agent connected to app console session %s for device %s app %s, bridging streams",
		sessionID, session.DeviceName, session.AppName)
	return s.forwardChannels(ctx, stream, session)
}

func (s *Server) forwardChannels(ctx context.Context, stream pb.RouterService_StreamServer, session *console.AppConsoleSession) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go s.pipeStreamToChannel(ctx, stream, session.RecvCh)
	return s.pipeChannelToStream(ctx, session.SendCh, stream)
}

func (s *Server) pipeStreamToChannel(ctx context.Context, stream pb.RouterService_StreamServer, ch chan []byte) {
	defer close(ch)
	for {
		msg, err := stream.Recv()
		if err != nil {
			if ctx.Err() != nil {
				s.log.Debug("app console stream context closed")
			} else {
				s.log.Debugf("app console stream recv error: %v", err)
			}
			return
		}
		ch <- msg.GetPayload()
		if msg.GetClosed() {
			s.log.Debug("app console stream closed by agent")
			return
		}
	}
}

func (s *Server) pipeChannelToStream(ctx context.Context, ch chan []byte, stream pb.RouterService_StreamServer) error {
	for {
		select {
		case <-ctx.Done():
			s.log.Debug("app console channel context closed")
			_ = stream.Send(&pb.StreamResponse{Payload: []byte{}, Closed: true})
			return io.EOF
		case payload, ok := <-ch:
			if !ok {
				s.log.Debug("app console send channel closed")
				_ = stream.Send(&pb.StreamResponse{Payload: []byte{}, Closed: true})
				return io.EOF
			}
			if err := stream.Send(&pb.StreamResponse{Payload: payload}); err != nil {
				s.log.Debugf("app console stream send error: %v", err)
				return err
			}
		}
	}
}

// grpcMuxHandlerFunc routes incoming requests to grpcServer (gRPC) or
// httpHandler (HTTP) based on the Content-Type header.
func grpcMuxHandlerFunc(grpcServer *grpc.Server, httpHandler http.Handler, log logrus.FieldLogger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && r.Header.Get("Content-Type") == "application/grpc" {
			type rwTimeoutSetter interface {
				SetReadDeadline(time.Time) error
				SetWriteDeadline(time.Time) error
			}
			if rtw, ok := w.(rwTimeoutSetter); ok {
				if err := rtw.SetReadDeadline(time.Time{}); err != nil {
					log.Errorf("setting gRPC read deadline: %v", err)
				}
				if err := rtw.SetWriteDeadline(time.Time{}); err != nil {
					log.Errorf("setting gRPC write deadline: %v", err)
				}
			} else {
				log.Error("cannot set gRPC deadline")
			}
			grpcServer.ServeHTTP(w, r)
		} else {
			httpHandler.ServeHTTP(w, r)
		}
	})
}
