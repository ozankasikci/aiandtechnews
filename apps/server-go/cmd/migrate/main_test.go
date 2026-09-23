package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
)

func productionEnv(t *testing.T) map[string]string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, config.ProductionMarker), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"APP_ENV":       "production",
		"DATABASE_PATH": filepath.Join(root, "data", "technews.db"),
		"UPLOADS_DIR":   filepath.Join(root, "uploads"),
		"JWT_SECRET":    "0123456789abcdef0123456789abcdef",
		"TZ":            "Europe/Istanbul",
	}
}

func runMigrate(t *testing.T, env map[string]string) error {
	t.Helper()
	return run(context.Background(), func(key string) string { return env[key] }, func() (string, error) {
		if env["APP_ENV"] == "production" {
			t.Fatal("worktree lookup in production")
		}
		return t.TempDir(), nil
	})
}

func TestProductionRefusesAMissingDatabaseAndCreatesNothing(t *testing.T) {
	env := productionEnv(t)
	err := runMigrate(t, env)
	if err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("run() error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Dir(env["DATABASE_PATH"])); !os.IsNotExist(statErr) {
		t.Fatal("production migrate created the database directory")
	}
}

func TestProductionRefusesAnUnadoptedNodeDatabaseAndLeavesItUnchanged(t *testing.T) {
	env := productionEnv(t)
	path := env["DATABASE_PATH"]
	db, err := database.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode = DELETE; CREATE TABLE articles (id INTEGER PRIMARY KEY, title TEXT); INSERT INTO articles VALUES (1, 'kept')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	err = runMigrate(t, env)
	if !errors.Is(err, migrate.ErrUnmanagedDatabase) || !strings.Contains(err.Error(), "bin/adopt") {
		t.Fatalf("run() error = %v, want %v pointing to bin/adopt", err, migrate.ErrUnmanagedDatabase)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("the unadopted database changed")
	}
}

func TestProductionRefusesAnEmptyDatabaseFile(t *testing.T) {
	env := productionEnv(t)
	if err := os.MkdirAll(filepath.Dir(env["DATABASE_PATH"]), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env["DATABASE_PATH"], nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runMigrate(t, env); !errors.Is(err, migrate.ErrUnmanagedDatabase) {
		t.Fatalf("run() error = %v, want %v", err, migrate.ErrUnmanagedDatabase)
	}
}

func TestProductionMigratesAnAdoptedDatabaseWithPendingMigrations(t *testing.T) {
	env := productionEnv(t)
	all := app.Migrations()
	db, err := database.Open(context.Background(), env["DATABASE_PATH"])
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate.Run(context.Background(), db, all[:len(all)-1]); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if err := runMigrate(t, env); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	db, err = database.OpenExisting(context.Background(), env["DATABASE_PATH"], true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pending, err := migrate.Status(context.Background(), db, all)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending after migrate = %v, %v", pending, err)
	}
}

func TestDevelopmentStillCreatesAndMigratesANewDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dev", "technews.db")
	if err := runMigrate(t, map[string]string{"DATABASE_PATH": path, "UPLOADS_DIR": filepath.Join(t.TempDir(), "uploads")}); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("development database not created: %v", err)
	}
}
