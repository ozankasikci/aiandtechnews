package app_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

func taxonomyApplication(t *testing.T, logger *slog.Logger, seed bool) (http.Handler, interface{ Close() error }) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	if seed {
		seedContractArticles(t, db)
	}
	cfg := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4402", DatabasePath: filepath.Join(t.TempDir(), "unused.db")}
	application, err := app.NewWithDatabase(cfg, logger, db)
	if err != nil {
		t.Fatal(err)
	}
	return application.Handler(), db
}

func taxonomyGet(handler http.Handler, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Origin", "https://client.example.invalid")
	handler.ServeHTTP(response, request)
	return response
}

func TestPublicTaxonomyReadsExactHTTPShape(t *testing.T) {
	handler, _ := taxonomyApplication(t, slog.New(slog.NewTextHandler(io.Discard, nil)), true)
	tests := []struct {
		path string
		body string
	}{
		{"/api/categories", `{"categories":[{"id":101,"name":"Synthetic AI","slug":"synthetic-ai","description":"Synthetic artificial intelligence fixtures","color":"#111111"},{"id":102,"name":"Synthetic Code","slug":"synthetic-code","description":"Synthetic programming fixtures","color":"#222222"},{"id":103,"name":"Synthetic Startups","slug":"synthetic-startups","description":"Synthetic startup fixtures","color":"#333333"},{"id":104,"name":"Synthetic Unused","slug":"synthetic-unused","description":"Intentionally unused contract category","color":"#444444"}]}`},
		{"/api/authors", `{"authors":[{"id":202,"name":"Synthetic Reporter","email":"reporter@example.invalid","avatar":null,"bio":null,"role":"editor"},{"id":201,"name":"TechNews Editorial","email":"editorial@example.invalid","avatar":"/uploads/synthetic-contract-image.png","bio":"Synthetic editorial contract fixture.","role":"admin"}]}`},
	}
	for _, tt := range tests {
		response := taxonomyGet(handler, tt.path)
		if response.Code != http.StatusOK || response.Body.String() != tt.body {
			t.Errorf("GET %s = %d %q", tt.path, response.Code, response.Body.String())
		}
		if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
			t.Errorf("GET %s content type = %q", tt.path, got)
		}
		if got := response.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("GET %s CORS = %q", tt.path, got)
		}
		if strings.HasSuffix(response.Body.String(), "\n") || strings.Contains(response.Body.String(), "password_hash") || strings.Contains(response.Body.String(), "do-not-expose") {
			t.Errorf("GET %s unsafe or newline body = %q", tt.path, response.Body.String())
		}
	}
}

func TestPublicTaxonomyReadsEncodeEmptyArrays(t *testing.T) {
	handler, _ := taxonomyApplication(t, slog.New(slog.NewTextHandler(io.Discard, nil)), false)
	for path, want := range map[string]string{"/api/categories": `{"categories":[]}`, "/api/authors": `{"authors":[]}`} {
		response := taxonomyGet(handler, path)
		if response.Code != http.StatusOK || response.Body.String() != want {
			t.Errorf("GET %s = %d %q", path, response.Code, response.Body.String())
		}
	}
}

func TestPublicTaxonomyDatabaseFailuresAreStableLoggedAndNonleaking(t *testing.T) {
	var logs bytes.Buffer
	handler, closer := taxonomyApplication(t, slog.New(slog.NewJSONHandler(&logs, nil)), false)
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/categories", "/api/authors"} {
		response := taxonomyGet(handler, path)
		if response.Code != http.StatusInternalServerError || response.Body.String() != `{"error":"Internal server error"}` {
			t.Errorf("GET %s = %d %q", path, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "database is closed") {
			t.Errorf("GET %s leaked database error", path)
		}
	}
	if !strings.Contains(logs.String(), "list categories") || !strings.Contains(logs.String(), "list authors") {
		t.Errorf("missing taxonomy errors in logs: %q", logs.String())
	}
}
