package app

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/editorial"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/health"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/httpserver"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
)

type App struct {
	address string
	handler http.Handler
	server  *httpserver.Server
}

// New composes the health-only application without opening or inspecting a database.
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

// NewWithDatabase composes database-backed routes around a caller-owned pool.
// It does not ping, migrate, seed, or close the database.
func NewWithDatabase(cfg config.Config, logger *slog.Logger, db *sql.DB) (*App, error) {
	return NewWithDatabaseAt(cfg, logger, db, time.Now)
}

// NewWithDatabaseAt is the deterministic database-backed composition seam.
// Production uses NewWithDatabase, which supplies time.Now.
func NewWithDatabaseAt(cfg config.Config, logger *slog.Logger, db *sql.DB, now func() time.Time) (*App, error) {
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	if db == nil {
		return nil, errors.New("database is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	tokens, err := editorial.NewJWT(cfg.JWTSecret, now)
	if err != nil {
		return nil, err
	}
	contentStore := content.NewSQLiteStore(db)
	articles := content.NewPublicHandler(content.NewService(contentStore), logger)
	categories := content.NewCategoryPublicHandler(content.NewCategoryService(contentStore), logger)
	editorialStore := editorial.NewSQLiteStore(db)
	authors := editorial.NewPublicHandler(editorial.NewService(editorialStore), logger)
	auth := editorial.NewAuthHandler(editorial.NewLoginService(editorialStore, editorial.NewBcryptVerifier(), tokens), tokens, logger)
	handler := httpserver.NewRouter(logger, func(router chi.Router) {
		health.MountPublic(router)
		articles.MountPublic(router)
		categories.MountPublic(router)
		authors.MountPublic(router)
		auth.Mount(router)
	})
	server := httpserver.NewServer(cfg.Address, handler, logger)
	return &App{address: cfg.Address, handler: handler, server: server}, nil
}

// Migrations explicitly collects capability-owned descriptors in global order.
func Migrations() []migrate.Descriptor {
	descriptors := editorial.Migrations()
	descriptors = append(descriptors, content.Migrations()...)
	return append(descriptors, newsroom.Migrations()...)
}

func (a *App) Address() string               { return a.address }
func (a *App) Handler() http.Handler         { return a.handler }
func (a *App) Run(ctx context.Context) error { return a.server.Run(ctx) }
