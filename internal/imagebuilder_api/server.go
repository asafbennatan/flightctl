package imagebuilder_api

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	api "github.com/flightctl/flightctl/api/v1beta1/imagebuilder"
	fcmiddleware "github.com/flightctl/flightctl/internal/api_server/middleware"
	"github.com/flightctl/flightctl/internal/auth"
	"github.com/flightctl/flightctl/internal/auth/authn"
	"github.com/flightctl/flightctl/internal/config"
	"github.com/flightctl/flightctl/internal/imagebuilder_api/api/server"
	"github.com/flightctl/flightctl/internal/imagebuilder_api/service"
	imagebuilderstore "github.com/flightctl/flightctl/internal/imagebuilder_api/store"
	"github.com/flightctl/flightctl/internal/imagebuilder_api/transport"
	"github.com/flightctl/flightctl/internal/kvstore"
	internalservice "github.com/flightctl/flightctl/internal/service"
	"github.com/flightctl/flightctl/internal/store"
	"github.com/flightctl/flightctl/pkg/queues"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	oapimiddleware "github.com/oapi-codegen/nethttp-middleware"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	gracefulShutdownTimeout = 5 * time.Second
)

// Server represents the ImageBuilder API server
type Server struct {
	log               logrus.FieldLogger
	cfg               *config.Config
	imageBuilderStore imagebuilderstore.Store
	mainStore         store.Store
	kvStore           kvstore.KVStore
	queuesProvider    queues.Provider
	imageBuildService service.ImageBuildService
	authN             *authn.MultiAuth
	authZ             auth.AuthZMiddleware
}

// New returns a new instance of an ImageBuilder API server.
func New(
	log logrus.FieldLogger,
	cfg *config.Config,
	imageBuilderStore imagebuilderstore.Store,
	mainStore store.Store,
	kvStore kvstore.KVStore,
	queuesProvider queues.Provider,
) *Server {
	return &Server{
		log:               log,
		cfg:               cfg,
		imageBuilderStore: imageBuilderStore,
		mainStore:         mainStore,
		kvStore:           kvStore,
		queuesProvider:    queuesProvider,
		imageBuildService: service.NewImageBuildService(imageBuilderStore, log),
	}
}

func oapiErrorHandler(w http.ResponseWriter, message string, statusCode int) {
	http.Error(w, fmt.Sprintf("API Error: %s", message), statusCode)
}

// Run starts the ImageBuilder API server
func (s *Server) Run(ctx context.Context) error {
	s.log.Println("Initializing ImageBuilder API server")

	// Load OpenAPI spec for request validation
	swagger, err := api.GetSwagger()
	if err != nil {
		return fmt.Errorf("failed loading swagger spec: %w", err)
	}
	// Skip server name validation
	swagger.Servers = nil

	oapiOpts := oapimiddleware.Options{
		ErrorHandler: oapiErrorHandler,
	}

	// Initialize auth (same as api_server)
	authN, err := auth.InitMultiAuth(s.cfg, s.log, nil)
	if err != nil {
		return fmt.Errorf("failed initializing auth: %w", err)
	}
	s.authN = authN

	// Start auth provider loader
	go func() {
		if err := authN.Start(ctx); err != nil {
			s.log.Errorf("Failed to start auth provider loader: %v", err)
			return
		}
		s.log.Warn("Auth provider loader stopped unexpectedly")
	}()

	s.authZ, err = auth.InitMultiAuthZ(s.cfg, s.log)
	if err != nil {
		return fmt.Errorf("failed initializing authZ: %w", err)
	}

	// Start multiAuthZ to initialize cache lifecycle management
	if multiAuthZ, ok := s.authZ.(*auth.MultiAuthZ); ok {
		multiAuthZ.Start(ctx)
		s.log.Debug("Started MultiAuthZ with context-based cache lifecycle")
	}

	router := chi.NewRouter()

	// Create identity mapping middleware (same as api_server)
	identityMapper := internalservice.NewIdentityMapper(s.mainStore, s.log)
	go func() {
		identityMapper.Start(ctx)
		s.log.Warn("Identity mapper stopped unexpectedly")
	}()
	identityMappingMiddleware := fcmiddleware.NewIdentityMappingMiddleware(identityMapper, s.log)

	// Create organization extraction and validation middleware
	orgMiddleware := fcmiddleware.ExtractAndValidateOrg(fcmiddleware.QueryOrgIDExtractor, s.log)
	userAgentMiddleware := fcmiddleware.UserAgentLogger(s.log)

	authMiddlewares := []func(http.Handler) http.Handler{
		auth.CreateAuthNMiddleware(s.authN, s.log),
		identityMappingMiddleware.MapIdentityToDB,
		orgMiddleware,
		auth.CreateAuthZMiddleware(s.authZ, s.log),
	}

	// General middleware stack for all route groups
	router.Use(
		fcmiddleware.SecurityHeaders,
		fcmiddleware.RequestID,
		fcmiddleware.AddEventMetadataToCtx,
		middleware.Logger,
		middleware.Recoverer,
		userAgentMiddleware,
	)

	// API routes with OpenAPI validation and auth
	router.Group(func(r chi.Router) {
		r.Use(oapimiddleware.OapiRequestValidatorWithOptions(swagger, &oapiOpts))
		r.Use(authMiddlewares...)

		// Add rate limiting if configured
		if s.cfg.ImageBuilderService.RateLimit != nil && s.cfg.ImageBuilderService.RateLimit.Enabled {
			trustedProxies := s.cfg.ImageBuilderService.RateLimit.TrustedProxies
			requests := 300
			window := time.Minute
			if s.cfg.ImageBuilderService.RateLimit.Requests > 0 {
				requests = s.cfg.ImageBuilderService.RateLimit.Requests
			}
			if s.cfg.ImageBuilderService.RateLimit.Window > 0 {
				window = time.Duration(s.cfg.ImageBuilderService.RateLimit.Window)
			}
			fcmiddleware.InstallRateLimiter(r, fcmiddleware.RateLimitOptions{
				Requests:       requests,
				Window:         window,
				Message:        "Rate limit exceeded, please try again later",
				TrustedProxies: trustedProxies,
			})
		}

		// Create transport handler that implements ServerInterface
		transportHandler := transport.NewTransportHandler(s.imageBuildService, s.log)

		// Register routes from generated OpenAPI spec
		server.HandlerFromMux(transportHandler, r)
	})

	// Health check endpoints (bypass OpenAPI + auth)
	router.Group(func(r chi.Router) {
		if s.cfg.ImageBuilderService.HealthChecks != nil && s.cfg.ImageBuilderService.HealthChecks.Enabled {
			hc := s.cfg.ImageBuilderService.HealthChecks
			r.Get(hc.LivenessPath, s.handleLiveness)
			r.Get(hc.ReadinessPath, s.handleReadiness)
		}
	})

	handler := otelhttp.NewHandler(router, "imagebuilder-api")

	srv := &http.Server{
		Addr:              s.cfg.ImageBuilderService.Address,
		Handler:           handler,
		ReadTimeout:       time.Duration(s.cfg.ImageBuilderService.HttpReadTimeout),
		ReadHeaderTimeout: time.Duration(s.cfg.ImageBuilderService.HttpReadHeaderTimeout),
		WriteTimeout:      time.Duration(s.cfg.ImageBuilderService.HttpWriteTimeout),
		IdleTimeout:       time.Duration(s.cfg.ImageBuilderService.HttpIdleTimeout),
	}

	go func() {
		<-ctx.Done()
		s.log.Println("Shutdown signal received:", ctx.Err())
		ctxTimeout, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
		defer cancel()

		srv.SetKeepAlivesEnabled(false)
		_ = srv.Shutdown(ctxTimeout)
		identityMapper.Stop()
		s.kvStore.Close()
		s.queuesProvider.Stop()
		s.queuesProvider.Wait()
	}()

	// Create TLS listener if certificates are configured
	var listener net.Listener
	if s.cfg.ImageBuilderService.TLSCertFile != "" && s.cfg.ImageBuilderService.TLSKeyFile != "" {
		tlsConfig, err := s.createTLSConfig()
		if err != nil {
			return fmt.Errorf("failed to create TLS config: %w", err)
		}
		listener, err = tls.Listen("tcp", s.cfg.ImageBuilderService.Address, tlsConfig)
		if err != nil {
			return fmt.Errorf("failed to create TLS listener: %w", err)
		}
		s.log.Printf("Listening on %s with TLS...", listener.Addr().String())
	} else {
		var err error
		listener, err = net.Listen("tcp", s.cfg.ImageBuilderService.Address)
		if err != nil {
			return err
		}
		s.log.Printf("Listening on %s (no TLS)...", listener.Addr().String())
	}

	if err := srv.Serve(listener); err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

func (s *Server) createTLSConfig() (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(s.cfg.ImageBuilderService.TLSCertFile, s.cfg.ImageBuilderService.TLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS certificate: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}

	return tlsConfig, nil
}

func (s *Server) handleLiveness(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	if err := s.imageBuilderStore.Ping(); err != nil {
		s.log.Errorf("Readiness check failed - database: %v", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("Database not ready"))
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

// Service returns the ImageBuildService for use in handlers
func (s *Server) Service() service.ImageBuildService {
	return s.imageBuildService
}
