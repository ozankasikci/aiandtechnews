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
	valid := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4401", DatabasePath: filepath.Join(t.TempDir(), "dev.db")}
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
	valid := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4401", DatabasePath: filepath.Join(t.TempDir(), "dev.db")}
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
}
