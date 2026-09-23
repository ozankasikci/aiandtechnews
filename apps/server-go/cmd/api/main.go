package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
)

func main() {
	ctx, stop := notifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "api: %v\n", err)
		os.Exit(1)
	}
}

func notifyContext(parent context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
	notifications := make(chan os.Signal, 1)
	signal.Notify(notifications, signals...)
	return contextCanceledOnSignal(parent, notifications, func() {
		signal.Stop(notifications)
	})
}

// contextCanceledOnSignal restores the process's default signal handling
// before cancellation starts graceful shutdown. A second signal can therefore
// terminate a server that is taking too long to drain.
func contextCanceledOnSignal(parent context.Context, notifications <-chan os.Signal, restore func()) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	finished := make(chan struct{})
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			restore()
			cancel()
		})
	}

	go func() {
		defer close(finished)
		select {
		case <-notifications:
			cleanup()
		case <-ctx.Done():
			cleanup()
		}
	}()

	return ctx, func() {
		cleanup()
		<-finished
	}
}

func run(ctx context.Context) (err error) {
	// Production has no path defaults, so a prebuilt binary runs without a
	// repository checkout; development defaults live beneath the worktree.
	var root string
	if os.Getenv("APP_ENV") != string(config.ModeProduction) {
		if root, err = runtimeWorktreeRoot(); err != nil {
			return err
		}
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	return runConfigured(ctx, root, os.Getenv, database.Open, func(cfg config.Config, logger *slog.Logger, db *sql.DB) (apiApplication, error) {
		return app.NewWithDatabase(cfg, logger, db)
	}, logger)
}

type apiApplication interface {
	Address() string
	Run(context.Context) error
}

func runConfigured(
	ctx context.Context,
	root string,
	getenv func(string) string,
	openDatabase func(context.Context, string) (*sql.DB, error),
	compose func(config.Config, *slog.Logger, *sql.DB) (apiApplication, error),
	logger *slog.Logger,
) (err error) {
	cfg, err := config.Load(getenv, root)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	// Like Node's index.ts, create the uploads directory at startup; the media
	// library opens it per request and never creates it itself.
	if cfg.UploadsDir == "" {
		return errors.New("UPLOADS_DIR is required when APP_ENV=production")
	}
	if err := os.MkdirAll(cfg.UploadsDir, 0o755); err != nil {
		return fmt.Errorf("create uploads directory: %w", err)
	}
	// Production never creates a database and never serves one that is not
	// fully migrated: a wrong DATABASE_PATH, a Node database that was not
	// adopted, or a missing cmd/migrate run all stop the process here.
	open := openDatabase
	if cfg.Mode == config.ModeProduction {
		open = database.OpenForServing
	}
	db, err := open(ctx, cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	if cfg.Mode == config.ModeProduction {
		if err := app.CheckSchema(ctx, db); err != nil {
			return fmt.Errorf("refusing to serve %s: %w", cfg.DatabasePath, err)
		}
	}
	application, err := compose(cfg, logger, db)
	if err != nil {
		return fmt.Errorf("compose application: %w", err)
	}
	logger.Info("starting HTTP server", "address", application.Address(), "mode", cfg.Mode)
	if err := application.Run(ctx); err != nil {
		return fmt.Errorf("run application: %w", err)
	}
	logger.Info("HTTP server stopped")
	return nil
}

func runtimeWorktreeRoot() (string, error) {
	cwd, cwdErr := os.Getwd()
	if cwdErr == nil {
		if root, err := findWorktreeRoot(cwd); err == nil {
			return root, nil
		}
	}

	executable, executableErr := os.Executable()
	if executableErr == nil {
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
		if root, err := findWorktreeRoot(filepath.Dir(executable)); err == nil {
			return root, nil
		}
	}
	return "", errors.New("locate worktree root: expected pnpm-workspace.yaml and apps/server-go/go.mod in an ancestor")
}

func findWorktreeRoot(start string) (string, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("absolute start path: %w", err)
	}
	if info, statErr := os.Stat(current); statErr == nil && !info.IsDir() {
		current = filepath.Dir(current)
	}

	for {
		workspace := filepath.Join(current, "pnpm-workspace.yaml")
		module := filepath.Join(current, "apps", "server-go", "go.mod")
		if regularFile(workspace) && regularFile(module) {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return "", fmt.Errorf("worktree root not found from %q", start)
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
