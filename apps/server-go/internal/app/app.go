package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/health"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/httpserver"
)

type App struct {
	address string
	handler http.Handler
	server  *httpserver.Server
}

// New is the application composition root. Task 2 intentionally composes no
// database adapter; constructing the health-only application performs no I/O.
func New(cfg config.Config, logger *slog.Logger) (*App, error) {
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	handler := httpserver.NewRouter(logger, health.MountPublic)
	server := httpserver.NewServer(cfg.Address, handler, logger)
	return &App{address: cfg.Address, handler: handler, server: server}, nil
}

func (a *App) Address() string {
	return a.address
}

func (a *App) Handler() http.Handler {
	return a.handler
}

func (a *App) Server() *httpserver.Server {
	return a.server
}

func (a *App) Run(ctx context.Context) error {
	return a.server.Run(ctx)
}
