package app_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/editorial"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

var dashboardRoutes = []struct{ method, path string }{
	{http.MethodGet, "/api/dashboard/articles"},
	{http.MethodGet, "/api/dashboard/articles/303"},
	{http.MethodPost, "/api/dashboard/articles"},
	{http.MethodPut, "/api/dashboard/articles/303"},
	{http.MethodDelete, "/api/dashboard/articles/303"},
	{http.MethodGet, "/api/dashboard/categories"},
	{http.MethodPost, "/api/dashboard/categories"},
	{http.MethodPut, "/api/dashboard/categories/101"},
	{http.MethodDelete, "/api/dashboard/categories/104"},
	{http.MethodGet, "/api/dashboard/settings"},
	{http.MethodPut, "/api/dashboard/settings"},
	{http.MethodGet, "/api/dashboard/not-a-route"},
}

// Node mounts requireAuth for the whole dashboard router (dashboard.ts:94),
// so every route, known or not, answers 401 before any other check.
func TestDashboardRoutesRequireAuthentication(t *testing.T) {
	handler, db := dashboardApplication(t)
	expired, err := editorial.NewJWT(authTestSecret, func() time.Time { return time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	stale, err := expired.Sign(editorial.Identity{ID: 201, Email: "editorial@example.invalid", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range dashboardRoutes {
		for authorization, want := range map[string]string{
			"":                 `{"error":"Authentication required"}`,
			"Basic abc":        `{"error":"Authentication required"}`,
			"Bearer not-a-jwt": `{"error":"Invalid or expired token"}`,
			"Bearer " + stale:  `{"error":"Invalid or expired token"}`,
		} {
			// A malformed body must not be parsed before authentication.
			assertAuthResponse(t, request(t, handler, route.method, route.path, `{`, authorization), http.StatusUnauthorized, want)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM articles`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("unauthenticated requests changed data: %d articles, %v", count, err)
	}
}

func TestDashboardAcceptsAnyValidTokenRegardlessOfRole(t *testing.T) {
	handler, _ := dashboardApplication(t)
	tokens, err := editorial.NewJWT(authTestSecret, func() time.Time { return time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	// Node checks only the signature and expiry: an editor, even one whose
	// author row no longer exists, may use every dashboard route.
	token, err := tokens.Sign(editorial.Identity{ID: 999, Email: "gone@example.invalid", Role: "editor"})
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, handler, http.MethodDelete, "/api/dashboard/categories/104", "", "Bearer "+token)
	assertAuthResponse(t, response, http.StatusOK, `{"success":true}`)
	unknown := request(t, handler, http.MethodGet, "/api/dashboard/not-a-route", "", "Bearer "+token)
	assertAuthResponse(t, unknown, http.StatusNotFound, `{"error":"Not found"}`)
}

// Deviation from the plan (reviewed contract change, user decision 2):
// GET /api/dashboard/settings hides newsroom's own "newsroom.*" keys from the
// shared settings table, and PUT can never write them because they are not
// in the dashboard's allowlist. This replaces the plan's
// TestDashboardSettingsExposeNewsroomKeysFromTheSharedTable, which asserted
// the opposite (that they were returned).
func TestDashboardSettingsHideNewsroomKeysFromTheSharedTable(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	cfg := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4402", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), JWTSecret: authTestSecret, UploadsDir: t.TempDir()}
	application, err := app.NewWithDatabaseAt(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db, func() time.Time { return fixed })
	if err != nil {
		t.Fatal(err)
	}
	tokens, _ := editorial.NewJWT(authTestSecret, func() time.Time { return fixed })
	token, _ := tokens.Sign(editorial.Identity{ID: 1, Email: "editor@example.invalid", Role: "admin"})
	// Node's SELECT key, value FROM settings has no filter and would return
	// the keys newsroom migration 3 seeds; Go hides them from the dashboard.
	response := request(t, application.Handler(), http.MethodGet, "/api/dashboard/settings", "", "Bearer "+token)
	assertAuthResponse(t, response, http.StatusOK, `{"settings":{}}`)

	update := request(t, application.Handler(), http.MethodPut, "/api/dashboard/settings",
		`{"newsroom.publish_delay_min_minutes":"999","site_name":"New"}`, "Bearer "+token)
	assertAuthResponse(t, update, http.StatusOK, `{"settings":{"site_name":"New"}}`)

	var value string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'newsroom.publish_delay_min_minutes'`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "30" {
		t.Errorf("dashboard PUT wrote a newsroom key: value = %q, want unchanged 30", value)
	}
}
