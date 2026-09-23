package settings_test

import (
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/settings"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

// settingsServer creates Node's settings table (db.ts:67-70) with the rows the
// recorded Node vectors started from. The table's migration is owned by
// newsroom; the app-level test covers the migrated schema.
func settingsServer(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if _, err := db.Exec(`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES ('site_name','Synthetic TechNews'),('newsletter_enabled','true'),('social_twitter','')`); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	settings.NewHandler(settings.NewService(settings.NewSQLiteStore(db)), slog.New(slog.NewTextHandler(io.Discard, nil))).Mount(router)
	return router, db
}

func serve(t *testing.T, handler http.Handler, method, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/settings", strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	return response
}

const unchanged = `{"settings":{"site_name":"Synthetic TechNews","newsletter_enabled":true,"social_twitter":""}}`

// Expected bodies were recorded from the Node server on the same rows, except
// where noted: keys keep SQLite row order, so newly inserted keys come last.
func TestSettingsMatchNode(t *testing.T) {
	for _, tt := range []struct {
		name, method, contentType, body string
		status                          int
		want                            string
	}{
		{"get", "GET", "", "", 200, unchanged},
		{"allowlisted update with boolean", "PUT", "application/json", `{"site_name":"New","newsletter_enabled":false,"ignored":"x"}`, 200,
			`{"settings":{"site_name":"New","newsletter_enabled":false,"social_twitter":""}}`},
		{"number is stored as REAL text", "PUT", "application/json", `{"site_description":5}`, 200,
			`{"settings":{"site_name":"Synthetic TechNews","newsletter_enabled":true,"social_twitter":"","site_description":"5.0"}}`},
		{"one-element array spreads", "PUT", "application/json", `{"newsletter_provider":["resend"]}`, 200,
			`{"settings":{"site_name":"Synthetic TechNews","newsletter_enabled":true,"social_twitter":"","newsletter_provider":"resend"}}`},
		{"non-json body is ignored", "PUT", "text/plain", `{"site_name":"T"}`, 200, unchanged},
		{"empty body", "PUT", "application/json", ``, 200, unchanged},
		// Deviation from Node (reviewed contract change, docs/superpowers/plans/
		// 2026-09-24-go-admin-content.md user decision 1): Node upserts null as
		// SQL NULL, which violates settings.value NOT NULL and rolls the whole
		// update back with a 500 -- the dashboard sends null for every blank
		// social/webhook field, so Node cannot save settings while any of them
		// is blank. Go stores an empty string for a null value instead.
		{"null value is stored as an empty string", "PUT", "application/json", `{"site_name":"Z","social_twitter":null}`, 200,
			`{"settings":{"site_name":"Z","newsletter_enabled":true,"social_twitter":""}}`},
		{"object value", "PUT", "application/json", `{"site_description":{"a":1}}`, 500, `{"error":"Internal server error"}`},
		{"malformed json", "PUT", "application/json", `{"site_name":`, 400, `{"error":"Invalid request body"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler, _ := settingsServer(t)
			response := serve(t, handler, tt.method, tt.contentType, tt.body)
			if response.Code != tt.status || response.Body.String() != tt.want {
				t.Fatalf("%s = %d %s, want %d %s", tt.name, response.Code, response.Body.String(), tt.status, tt.want)
			}
			if tt.status != http.StatusOK {
				if after := serve(t, handler, "GET", "", ""); after.Body.String() != unchanged {
					t.Errorf("failed update changed settings: %s", after.Body.String())
				}
			}
		})
	}
}

func TestSettingsEmptyTableAndDatabaseFailure(t *testing.T) {
	handler, db := settingsServer(t)
	if _, err := db.Exec(`DELETE FROM settings`); err != nil {
		t.Fatal(err)
	}
	if response := serve(t, handler, "GET", "", ""); response.Body.String() != `{"settings":{}}` {
		t.Fatalf("empty settings = %s", response.Body.String())
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "PUT"} {
		if response := serve(t, handler, method, "application/json", `{}`); response.Code != 500 || response.Body.String() != `{"error":"Internal server error"}` {
			t.Errorf("%s after close = %d %s", method, response.Code, response.Body.String())
		}
	}
}

// Deviation from Node (reviewed contract change, user decision 2): Node's
// GET /api/dashboard/settings returns every row in the shared settings
// table, including newsroom's own "newsroom.*" keys. Go hides them from the
// dashboard settings surface, and PUT can never write them because they are
// not in the allowlist.
func TestSettingsHideNewsroomKeys(t *testing.T) {
	handler, db := settingsServer(t)
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES ('newsroom.publish_delay_min_minutes','30'),('newsroom.publish_delay_max_minutes','40')`); err != nil {
		t.Fatal(err)
	}
	if response := serve(t, handler, "GET", "", ""); response.Body.String() != unchanged {
		t.Fatalf("GET exposed newsroom keys: %s", response.Body.String())
	}
	response := serve(t, handler, "PUT", "application/json", `{"site_name":"New","newsroom.publish_delay_min_minutes":"999"}`)
	if response.Body.String() != `{"settings":{"site_name":"New","newsletter_enabled":true,"social_twitter":""}}` {
		t.Fatalf("PUT result exposed newsroom keys: %s", response.Body.String())
	}
	var value string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'newsroom.publish_delay_min_minutes'`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "30" {
		t.Errorf("PUT wrote a newsroom key: value = %q, want unchanged 30", value)
	}
}
