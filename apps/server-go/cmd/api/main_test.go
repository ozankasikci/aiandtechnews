package main

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
)

type stubAPIApplication struct{ err error }

func (a stubAPIApplication) Address() string           { return "127.0.0.1:4402" }
func (a stubAPIApplication) Run(context.Context) error { return a.err }

func TestContextCanceledOnSignalRestoresDefaultHandlingFirst(t *testing.T) {
	notifications := make(chan os.Signal, 1)
	restored := make(chan struct{})
	ctx, stop := contextCanceledOnSignal(context.Background(), notifications, func() {
		close(restored)
	})
	defer stop()

	notifications <- syscall.SIGTERM
	select {
	case <-ctx.Done():
		select {
		case <-restored:
		default:
			t.Fatal("context canceled before default signal handling was restored")
		}
	case <-time.After(time.Second):
		t.Fatal("context was not canceled after signal")
	}
}

func TestContextCanceledOnSignalStopCleansUpWithoutSignal(t *testing.T) {
	notifications := make(chan os.Signal)
	restored := false
	ctx, stop := contextCanceledOnSignal(context.Background(), notifications, func() {
		restored = true
	})
	stop()
	if !restored {
		t.Fatal("stop did not restore signal handling")
	}
	if ctx.Err() != context.Canceled {
		t.Fatalf("context error = %v, want context canceled", ctx.Err())
	}
}

func TestFindWorktreeRootFromRepositoryAndModuleDirectories(t *testing.T) {
	root := t.TempDir()
	module := filepath.Join(root, "apps", "server-go")
	if err := os.MkdirAll(filepath.Join(module, "cmd", "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pnpm-workspace.yaml"), []byte("packages: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, "go.mod"), []byte("module github.com/ozankasikci/aiandtechnews/apps/server-go\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, start := range []string{root, module, filepath.Join(module, "cmd", "api")} {
		got, err := findWorktreeRoot(start)
		if err != nil {
			t.Fatalf("findWorktreeRoot(%q) error = %v", start, err)
		}
		if got != root {
			t.Errorf("findWorktreeRoot(%q) = %q, want %q", start, got, root)
		}
	}
}

func TestFindWorktreeRootRejectsUnrelatedDirectory(t *testing.T) {
	if _, err := findWorktreeRoot(t.TempDir()); err == nil {
		t.Fatal("findWorktreeRoot() error = nil")
	}
}

func TestRunConfiguredOwnsDatabaseWithoutMigratingOrListening(t *testing.T) {
	for _, tt := range []struct {
		name       string
		composeErr error
		runErr     error
	}{
		{name: "run failure", runErr: errors.New("stop without listener")},
		{name: "composition failure", composeErr: errors.New("composition failed")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			databasePath := filepath.Join(t.TempDir(), "api.db")
			environment := map[string]string{"DATABASE_PATH": databasePath, "SERVER_ADDR": "127.0.0.1:4402"}
			getenv := func(key string) string { return environment[key] }
			var opened *sql.DB
			open := func(ctx context.Context, path string) (*sql.DB, error) {
				var err error
				opened, err = database.Open(ctx, path)
				return opened, err
			}
			compose := func(_ config.Config, _ *slog.Logger, db *sql.DB) (apiApplication, error) {
				if err := db.PingContext(context.Background()); err != nil {
					t.Fatalf("injected database was not pinged and usable: %v", err)
				}
				var migrationTables int
				if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name='schema_migrations'`).Scan(&migrationTables); err != nil {
					t.Fatal(err)
				}
				if migrationTables != 0 {
					t.Fatal("API startup applied migrations")
				}
				if tt.composeErr != nil {
					return nil, tt.composeErr
				}
				return stubAPIApplication{err: tt.runErr}, nil
			}

			err := runConfigured(context.Background(), t.TempDir(), getenv, open, compose, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err == nil {
				t.Fatal("runConfigured error = nil")
			}
			if opened == nil {
				t.Fatal("configured database was not opened")
			}
			if err := opened.PingContext(context.Background()); err == nil {
				t.Fatal("database remained open after return")
			}
		})
	}
}

func TestRunConfiguredCreatesUploadsDirectoryBeforeComposing(t *testing.T) {
	uploads := filepath.Join(t.TempDir(), "nested", "uploads")
	environment := map[string]string{
		"DATABASE_PATH": filepath.Join(t.TempDir(), "api.db"),
		"SERVER_ADDR":   "127.0.0.1:4402",
		"UPLOADS_DIR":   uploads,
	}
	compose := func(cfg config.Config, _ *slog.Logger, _ *sql.DB) (apiApplication, error) {
		if cfg.UploadsDir != uploads {
			t.Fatalf("UploadsDir = %q, want %q", cfg.UploadsDir, uploads)
		}
		info, err := os.Stat(uploads)
		if err != nil || !info.IsDir() {
			t.Fatalf("uploads directory missing at composition: %v", err)
		}
		return stubAPIApplication{err: errors.New("stop without listener")}, nil
	}
	err := runConfigured(context.Background(), t.TempDir(), func(key string) string { return environment[key] }, database.Open, compose, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("runConfigured error = nil")
	}
}

func TestRunConfiguredRequiresExplicitUploadsDirInProduction(t *testing.T) {
	environment := map[string]string{"APP_ENV": "production", "DATABASE_PATH": filepath.Join(t.TempDir(), "api.db"), "SERVER_ADDR": "127.0.0.1:4402"}
	open := func(context.Context, string) (*sql.DB, error) {
		t.Fatal("database opened without an uploads directory")
		return nil, nil
	}
	compose := func(config.Config, *slog.Logger, *sql.DB) (apiApplication, error) {
		t.Fatal("composed without an uploads directory")
		return nil, nil
	}
	err := runConfigured(context.Background(), t.TempDir(), func(key string) string { return environment[key] }, open, compose, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil || !strings.Contains(err.Error(), "UPLOADS_DIR is required when APP_ENV=production") {
		t.Fatalf("runConfigured error = %v", err)
	}
}

// productionRoot returns a marked production root and the production
// environment for it.
func productionRoot(t *testing.T) (string, map[string]string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, config.ProductionMarker), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return root, map[string]string{
		"APP_ENV":       "production",
		"DATABASE_PATH": filepath.Join(root, "data", "technews.db"),
		"UPLOADS_DIR":   filepath.Join(root, "uploads"),
		"SERVER_ADDR":   "127.0.0.1:4402",
		"JWT_SECRET":    "0123456789abcdef0123456789abcdef",
		"TZ":            "Europe/Istanbul",
	}
}

func migratedDatabase(t *testing.T, path string, descriptors []migrate.Descriptor) {
	t.Helper()
	db, err := database.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrate.Run(context.Background(), db, descriptors); err != nil {
		t.Fatal(err)
	}
}

func refusingCompose(t *testing.T) func(config.Config, *slog.Logger, *sql.DB) (apiApplication, error) {
	return func(config.Config, *slog.Logger, *sql.DB) (apiApplication, error) {
		t.Fatal("composed an application for a database that must be refused")
		return nil, nil
	}
}

func TestRunConfiguredStartsInProductionWithoutAWorktree(t *testing.T) {
	_, environment := productionRoot(t)
	migratedDatabase(t, environment["DATABASE_PATH"], app.Migrations())
	composed := false
	compose := func(cfg config.Config, _ *slog.Logger, _ *sql.DB) (apiApplication, error) {
		composed = true
		if cfg.Mode != config.ModeProduction {
			t.Fatalf("Mode = %q", cfg.Mode)
		}
		return stubAPIApplication{err: errors.New("stop without listener")}, nil
	}
	err := runConfigured(context.Background(), "", func(key string) string { return environment[key] }, database.Open, compose, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil || !composed {
		t.Fatalf("runConfigured error = %v, composed = %t", err, composed)
	}
}

func TestProductionNeverCreatesAMissingDatabase(t *testing.T) {
	_, environment := productionRoot(t)
	err := runConfigured(context.Background(), "", func(key string) string { return environment[key] }, database.Open, refusingCompose(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("runConfigured error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Dir(environment["DATABASE_PATH"])); !os.IsNotExist(statErr) {
		t.Fatal("production created the database directory")
	}
}

func TestProductionRefusesAnUnmanagedDatabase(t *testing.T) {
	_, environment := productionRoot(t)
	path := environment["DATABASE_PATH"]
	db, err := database.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE articles (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	err = runConfigured(context.Background(), "", func(key string) string { return environment[key] }, database.Open, refusingCompose(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil || !strings.Contains(err.Error(), "cmd/adopt") {
		t.Fatalf("runConfigured error = %v", err)
	}
}

func TestProductionRefusesPendingMigrations(t *testing.T) {
	_, environment := productionRoot(t)
	all := app.Migrations()
	migratedDatabase(t, environment["DATABASE_PATH"], all[:len(all)-1])
	err := runConfigured(context.Background(), "", func(key string) string { return environment[key] }, database.Open, refusingCompose(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil || !strings.Contains(err.Error(), "cmd/migrate") || !strings.Contains(err.Error(), all[len(all)-1].Name) {
		t.Fatalf("runConfigured error = %v", err)
	}
}
