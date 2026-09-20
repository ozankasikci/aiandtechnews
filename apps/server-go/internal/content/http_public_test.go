package content_test

import (
	"bytes"
	"context"
	"encoding/json"
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

func newArticleHandler(t *testing.T, logger *slog.Logger) (http.Handler, interface{ Close() error }) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	seed(t, db)
	cfg := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4402", DatabasePath: filepath.Join(t.TempDir(), "unused.db")}
	application, err := app.NewWithDatabase(cfg, logger, db)
	if err != nil {
		t.Fatal(err)
	}
	return application.Handler(), db
}

func request(t *testing.T, handler http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
	return response
}

func TestPublicArticleNegativeAndVisibilityScenarios(t *testing.T) {
	handler, _ := newArticleHandler(t, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, target := range []string{"/api/articles/missing", "/api/articles/id/999", "/api/articles/id/not-a-number", "/api/articles/synthetic-draft"} {
		response := request(t, handler, target)
		if response.Code != 404 || response.Body.String() != `{"error":"Article not found"}` {
			t.Errorf("%s = %d %q", target, response.Code, response.Body.String())
		}
		if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
			t.Errorf("%s content-type = %q", target, got)
		}
		if got := response.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("%s CORS origin = %q", target, got)
		}
	}
	response := request(t, handler, "/api/articles/id/303")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"status":"draft"`) {
		t.Fatalf("draft ID = %d %q", response.Code, response.Body.String())
	}
	for _, target := range []string{"/api/articles", "/api/articles/trending"} {
		if strings.Contains(request(t, handler, target).Body.String(), "synthetic-draft") {
			t.Errorf("%s exposed draft", target)
		}
	}
}

func TestPublicArticleQueryDefaultsFiltersPaginationAndClamps(t *testing.T) {
	handler, _ := newArticleHandler(t, slog.New(slog.NewTextHandler(io.Discard, nil)))
	tests := []struct {
		target             string
		total, page, pages int
		ids                []float64
	}{
		{"/api/articles", 2, 1, 1, []float64{301, 302}},
		{"/api/articles?category=synthetic-code", 1, 1, 1, []float64{302}},
		{"/api/articles?search=Entirely", 1, 1, 1, []float64{301}},
		{"/api/articles?page=2&limit=1", 2, 2, 2, []float64{302}},
		{"/api/articles?page=-9&limit=0", 2, 1, 1, []float64{301, 302}},
		{"/api/articles?page=nope&limit=999", 2, 1, 1, []float64{301, 302}},
		{"/api/articles?limit=-999999999999999999999", 2, 1, 2, []float64{301}},
		{"/api/articles?limit=+999999999999999999999", 2, 1, 1, []float64{301, 302}},
		{"/api/articles?limit=2abc", 2, 1, 1, []float64{301, 302}},
		{"/api/articles?limit=1.9", 2, 1, 2, []float64{301}},
		{"/api/articles?limit=0x10", 2, 1, 1, []float64{301, 302}},
		{"/api/articles?limit=Infinity", 2, 1, 1, []float64{301, 302}},
		{"/api/articles?page=999999999999999999999", 2, int(^uint(0) >> 1), 1, []float64{}},
		{"/api/articles?category=missing", 0, 1, 0, []float64{}},
		{"/api/articles?search=%20", 2, 1, 1, []float64{301, 302}},
		{"/api/articles?search=%25", 2, 1, 1, []float64{301, 302}},
	}
	for _, tt := range tests {
		response := request(t, handler, tt.target)
		if response.Code != 200 || strings.HasSuffix(response.Body.String(), "\n") {
			t.Fatalf("%s = %d %q", tt.target, response.Code, response.Body.String())
		}
		var body struct {
			Articles   []map[string]any `json:"articles"`
			Total      int              `json:"total"`
			Page       int              `json:"page"`
			TotalPages int              `json:"totalPages"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Total != tt.total || body.Page != tt.page || body.TotalPages != tt.pages || len(body.Articles) != len(tt.ids) {
			t.Errorf("%s body = %#v", tt.target, body)
			continue
		}
		if len(tt.ids) == 0 && !strings.Contains(response.Body.String(), `"articles":[]`) {
			t.Errorf("%s did not encode an empty result as []: %q", tt.target, response.Body.String())
		}
		for i, id := range tt.ids {
			if body.Articles[i]["id"] != id {
				t.Errorf("%s article %d ID = %v", tt.target, i, body.Articles[i]["id"])
			}
		}
	}
	response := request(t, handler, "/api/articles/trending?limit=999")
	if response.Code != 200 || strings.Index(response.Body.String(), `"id":301`) > strings.Index(response.Body.String(), `"id":302`) {
		t.Errorf("trending = %q", response.Body.String())
	}
}

func TestPublicArticleDatabaseFailureIsLoggedAndNotLeaked(t *testing.T) {
	var logs bytes.Buffer
	handler, closer := newArticleHandler(t, slog.New(slog.NewJSONHandler(&logs, nil)))
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	response := request(t, handler, "/api/articles")
	if response.Code != 500 || response.Body.String() != `{"error":"Internal server error"}` {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
	if !strings.Contains(logs.String(), "list articles") || strings.Contains(response.Body.String(), "database is closed") {
		t.Errorf("logs/body = %q / %q", logs.String(), response.Body.String())
	}
}
