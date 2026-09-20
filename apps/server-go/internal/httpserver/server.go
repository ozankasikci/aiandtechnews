package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

const defaultShutdownTimeout = 10 * time.Second

type Option func(*Server)

func WithShutdownTimeout(timeout time.Duration) Option {
	return func(server *Server) {
		if timeout > 0 {
			server.shutdownTimeout = timeout
		}
	}
}

type Server struct {
	httpServer      *http.Server
	logger          *slog.Logger
	shutdownTimeout time.Duration
}

func NewServer(address string, handler http.Handler, logger *slog.Logger, options ...Option) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	server := &Server{
		httpServer: &http.Server{
			Addr:              address,
			Handler:           handler,
			ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
			MaxHeaderBytes:    1 << 20,
		},
		logger:          logger,
		shutdownTimeout: defaultShutdownTimeout,
	}
	for _, option := range options {
		if option != nil {
			option(server)
		}
	}
	return server
}

// Run listens on the configured address and serves until cancellation or an
// HTTP serving error. The listener is created only when Run is called.
func (s *Server) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("run context is required")
	}
	listener, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.httpServer.Addr, err)
	}
	return s.Serve(ctx, listener)
}

// Serve supports caller-owned listeners, making ephemeral-port execution safe
// in tests. Shutdown always receives a fresh background-derived deadline so a
// canceled request context cannot skip draining active requests.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	if ctx == nil {
		return errors.New("serve context is required")
	}
	if listener == nil {
		return errors.New("listener is required")
	}

	serveResult := make(chan error, 1)
	go func() {
		serveResult <- s.httpServer.Serve(listener)
	}()

	select {
	case err := <-serveResult:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
	defer cancel()
	shutdownErr := s.httpServer.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		s.logger.Error("graceful HTTP shutdown failed; forcing close", "error", shutdownErr)
		if closeErr := s.httpServer.Close(); closeErr != nil {
			shutdownErr = errors.Join(shutdownErr, fmt.Errorf("force close HTTP server: %w", closeErr))
		}
	}

	serveErr := <-serveResult
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = fmt.Errorf("serve HTTP during shutdown: %w", serveErr)
	} else {
		serveErr = nil
	}
	return errors.Join(shutdownErr, serveErr)
}
