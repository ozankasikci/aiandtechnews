package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

func TestNewComposesHealthAPIWithoutOpeningDatabase(t *testing.T) {
	cfg := config.Config{
		Mode:         config.ModeDevelopment,
		Address:      "127.0.0.1:4402",
		DatabasePath: filepath.Join(t.TempDir(), "missing", "must-not-be-opened.db"),
	}
	application, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if application.Address() != cfg.Address {
		t.Errorf("Address() = %q, want %q", application.Address(), cfg.Address)
	}

	response := httptest.NewRecorder()
	application.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if response.Code != http.StatusOK || response.Body.String() != `{"status":"ok"}` {
		t.Fatalf("health response = %d %q", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q", got)
	}
}

func TestNewRejectsInvalidCompositionInputs(t *testing.T) {
	valid := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4401", DatabasePath: filepath.Join(t.TempDir(), "dev.db"), JWTSecret: "synthetic-test-secret", UploadsDir: t.TempDir()}
	if _, err := New(valid, nil); err == nil {
		t.Fatal("New() with nil logger error = nil")
	}

	invalid := valid
	invalid.Address = "bad"
	if _, err := New(invalid, slog.Default()); err == nil {
		t.Fatal("New() with invalid config error = nil")
	}
}

func TestNewWithDatabaseValidatesDependenciesWithoutDatabaseIOOrOwnership(t *testing.T) {
	valid := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4401", DatabasePath: filepath.Join(t.TempDir(), "dev.db"), JWTSecret: "synthetic-test-secret", UploadsDir: t.TempDir()}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := NewWithDatabase(valid, logger, nil); err == nil {
		t.Fatal("NewWithDatabase() with nil database error = nil")
	}

	closed, _ := testutil.OpenDatabase(t)
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWithDatabase(valid, logger, closed); err != nil {
		t.Fatalf("NewWithDatabase() touched closed database: %v", err)
	}

	db, _ := testutil.OpenDatabase(t)
	if _, err := NewWithDatabase(valid, logger, db); err != nil {
		t.Fatal(err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("caller-owned database was closed: %v", err)
	}

	missingSecret := valid
	missingSecret.JWTSecret = ""
	if _, err := NewWithDatabase(missingSecret, logger, db); err == nil {
		t.Fatal("NewWithDatabase() with empty JWT secret error = nil")
	}

	missingUploads := valid
	missingUploads.UploadsDir = ""
	if _, err := NewWithDatabase(missingUploads, logger, db); err == nil || err.Error() != "uploads directory is required" {
		t.Fatalf("NewWithDatabase() with no uploads directory error = %v", err)
	}

	if _, err := NewWithDatabaseAt(valid, logger, db, nil); err == nil {
		t.Fatal("NewWithDatabaseAt() with nil clock error = nil")
	}
}

func TestDatabaseBackedHealthChecksTheDatabase(t *testing.T) {
	cfg := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4401", DatabasePath: filepath.Join(t.TempDir(), "dev.db"), JWTSecret: "synthetic-test-secret", UploadsDir: t.TempDir()}
	db, _ := testutil.OpenDatabase(t)
	application, err := NewWithDatabase(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db)
	if err != nil {
		t.Fatal(err)
	}
	get := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		application.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/health", nil))
		return response
	}
	if response := get(); response.Code != http.StatusOK || response.Body.String() != `{"status":"ok"}` {
		t.Fatalf("healthy: status = %d, body = %q", response.Code, response.Body.String())
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if response := get(); response.Code != http.StatusServiceUnavailable || response.Body.String() != `{"status":"error"}` {
		t.Fatalf("closed database: status = %d, body = %q", response.Code, response.Body.String())
	}
}
