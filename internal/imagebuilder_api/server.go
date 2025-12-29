package imagebuilder_api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/flightctl/flightctl/internal/config"
	"github.com/flightctl/flightctl/internal/kvstore"
	"github.com/flightctl/flightctl/internal/store"
	"github.com/flightctl/flightctl/pkg/queues"
	"github.com/go-chi/chi/v5"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	gracefulShutdownTimeout = 5 * time.Second
)

type Server struct {
	log            logrus.FieldLogger
	cfg            *config.Config
	store          store.Store
	kvStore        kvstore.KVStore
	queuesProvider queues.Provider
}

// New returns a new instance of an ImageBuilder API server.
func New(
	log logrus.FieldLogger,
	cfg *config.Config,
	st store.Store,
	kvStore kvstore.KVStore,
	queuesProvider queues.Provider,
) *Server {
	return &Server{
		log:            log,
		cfg:            cfg,
		store:          st,
		kvStore:        kvStore,
		queuesProvider: queuesProvider,
	}
}

func (s *Server) Run(ctx context.Context) error {
	s.log.Println("Initializing ImageBuilder API server")

	router := chi.NewRouter()

	// Health check endpoints
	if s.cfg.ImageBuilderService.HealthChecks != nil && s.cfg.ImageBuilderService.HealthChecks.Enabled {
		router.Get(s.cfg.ImageBuilderService.HealthChecks.LivenessPath, s.handleLiveness)
		router.Get(s.cfg.ImageBuilderService.HealthChecks.ReadinessPath, s.handleReadiness)
	}

	// TODO: Add ImageBuild API routes here in future stories

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
		s.kvStore.Close()
		s.queuesProvider.Stop()
		s.queuesProvider.Wait()
	}()

	listener, err := net.Listen("tcp", s.cfg.ImageBuilderService.Address)
	if err != nil {
		return err
	}

	s.log.Printf("Listening on %s...", listener.Addr().String())
	if err := srv.Serve(listener); err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

func (s *Server) handleLiveness(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	// Check database connectivity
	if err := s.store.Ping(); err != nil {
		s.log.Errorf("Readiness check failed - database: %v", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("Database not ready"))
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}
