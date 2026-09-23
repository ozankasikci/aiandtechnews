package app_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/editorial"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

var newsroomNow = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

func newsroomApplication(t *testing.T) (http.Handler, *sql.DB, string) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4402", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), JWTSecret: authTestSecret}
	clock := func() time.Time { return newsroomNow }
	application, err := app.NewWithDatabaseAt(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db, clock)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := editorial.NewJWT(authTestSecret, clock)
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.Sign(editorial.Identity{ID: 1, Email: "editor@example.invalid", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	return application.Handler(), db, "Bearer " + token
}

func seedCandidates(t *testing.T, db *sql.DB, urls ...string) {
	t.Helper()
	store := newsroom.NewSQLiteStore(db)
	for _, url := range urls {
		if _, inserted, err := store.Insert(context.Background(), newsroom.NewCandidate{
			SourceURL: url, SourceName: "The Verge", FeedURL: "https://www.theverge.com/rss/index.xml", Title: "Title " + url,
		}, newsroomNow); err != nil || !inserted {
			t.Fatalf("seed %s: inserted=%v err=%v", url, inserted, err)
		}
	}
}

func decodeBody[T any](t *testing.T, body string) T {
	t.Helper()
	var value T
	if err := json.Unmarshal([]byte(body), &value); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return value
}

func expectStatus(t *testing.T, method, path string, code int, got int, body string) {
	t.Helper()
	if got != code {
		t.Fatalf("%s %s = %d %s, want %d", method, path, got, body, code)
	}
}

// Every operation in the newsroom contract must exist and require auth. A
// second pass with a valid token proves the routes actually exist: auth runs
// before route matching in the /api/newsroom subtree, so an unauthenticated
// request to any path under it returns 401 regardless of whether the route
// is registered. Only a request WITH a valid token can distinguish a real
// route (200/404 "Candidate not found"/etc.) from the router's own
// unmatched-route bodies ({"error":"Not found"} / {"error":"Method not
// allowed"}).
func TestNewsroomContractOperationsRequireAuth(t *testing.T) {
	handler, _, auth := newsroomApplication(t)
	raw, err := os.ReadFile(filepath.Join("..", "..", "contracts", "newsroom.openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	operations := 0
	for path, methods := range document.Paths {
		for method := range methods {
			concrete := strings.ReplaceAll(path, "{id}", "1")
			response := request(t, handler, strings.ToUpper(method), concrete, "", "")
			if response.Code != http.StatusUnauthorized || response.Body.String() != `{"error":"Authentication required"}` {
				t.Errorf("%s %s = %d %s, want 401", method, concrete, response.Code, response.Body.String())
			}
			operations++

			authed := request(t, handler, strings.ToUpper(method), concrete, contractRequestBody(concrete, strings.ToUpper(method)), auth)
			if authed.Body.String() == `{"error":"Not found"}` || authed.Body.String() == `{"error":"Method not allowed"}` {
				t.Errorf("%s %s (authed) = %d %s, route does not exist", method, concrete, authed.Code, authed.Body.String())
			}
		}
	}
	if operations != 9 {
		t.Fatalf("contract operations = %d, want 9", operations)
	}
}

// contractRequestBody returns a body that is valid JSON for the given
// contract route, or empty for routes that take no body.
func contractRequestBody(path, method string) string {
	switch {
	case method == http.MethodPost && (path == "/api/newsroom/candidates/publish" || path == "/api/newsroom/candidates/reject"):
		return `{"ids":[1]}`
	case method == http.MethodPut && path == "/api/newsroom/settings":
		return `{"publish_delay_min_minutes":30,"publish_delay_max_minutes":40}`
	default:
		return ""
	}
}

func TestNewsroomReviewAndQueueFlow(t *testing.T) {
	handler, db, auth := newsroomApplication(t)
	seedCandidates(t, db, "https://example.com/1", "https://example.com/2")

	list := request(t, handler, http.MethodGet, "/api/newsroom/candidates", "", auth)
	expectStatus(t, "GET", "/candidates", http.StatusOK, list.Code, list.Body.String())
	page := decodeBody[newsroom.Page](t, list.Body.String())
	if page.Total != 2 || page.TotalPages != 1 || len(page.Candidates) != 2 {
		t.Fatalf("page = %+v", page)
	}

	publish := request(t, handler, http.MethodPost, "/api/newsroom/candidates/publish", `{"ids":[1,2,99]}`, auth)
	expectStatus(t, "POST", "/publish", http.StatusOK, publish.Code, publish.Body.String())
	published := decodeBody[struct {
		Queued  []newsroom.Candidate `json:"queued"`
		Skipped []newsroom.Skipped   `json:"skipped"`
	}](t, publish.Body.String())
	if len(published.Queued) != 2 || len(published.Skipped) != 1 || published.Skipped[0] != (newsroom.Skipped{ID: 99, Reason: "not found"}) {
		t.Fatalf("publish = %+v", published)
	}
	first, err := time.Parse(time.RFC3339, *published.Queued[0].ScheduledFor)
	if err != nil || first.Before(newsroomNow.Add(30*time.Minute)) || first.After(newsroomNow.Add(40*time.Minute)) {
		t.Fatalf("first scheduled_for = %v err=%v", published.Queued[0].ScheduledFor, err)
	}

	overview := decodeBody[newsroom.Overview](t, request(t, handler, http.MethodGet, "/api/newsroom/overview", "", auth).Body.String())
	if overview.Pending != 0 || overview.Queued != 2 || overview.NextPublishAt == nil || *overview.NextPublishAt != *published.Queued[0].ScheduledFor {
		t.Fatalf("overview = %+v", overview)
	}

	queue := decodeBody[newsroom.Page](t, request(t, handler, http.MethodGet, "/api/newsroom/candidates?status=queued,processing,failed", "", auth).Body.String())
	if queue.Total != 2 || queue.Candidates[0].ID != 1 {
		t.Fatalf("queue = %+v", queue)
	}

	unqueue := request(t, handler, http.MethodPost, "/api/newsroom/candidates/1/unqueue", "", auth)
	expectStatus(t, "POST", "/1/unqueue", http.StatusOK, unqueue.Code, unqueue.Body.String())
	if got := decodeBody[struct{ Candidate newsroom.Candidate }](t, unqueue.Body.String()).Candidate; got.Status != newsroom.StatusPending {
		t.Fatalf("unqueued = %+v", got)
	}
	again := request(t, handler, http.MethodPost, "/api/newsroom/candidates/1/unqueue", "", auth)
	if again.Code != http.StatusConflict || again.Body.String() != `{"error":"Candidate is not queued"}` {
		t.Fatalf("second unqueue = %d %s", again.Code, again.Body.String())
	}

	reject := request(t, handler, http.MethodPost, "/api/newsroom/candidates/reject", `{"ids":[1,2]}`, auth)
	expectStatus(t, "POST", "/reject", http.StatusOK, reject.Code, reject.Body.String())
	rejected := decodeBody[struct {
		Rejected []int64            `json:"rejected"`
		Skipped  []newsroom.Skipped `json:"skipped"`
	}](t, reject.Body.String())
	if len(rejected.Rejected) != 1 || rejected.Rejected[0] != 1 || len(rejected.Skipped) != 1 || rejected.Skipped[0].Reason != "not pending or failed" {
		t.Fatalf("reject = %+v", rejected)
	}

	retry := request(t, handler, http.MethodPost, "/api/newsroom/candidates/2/retry", "", auth)
	if retry.Code != http.StatusConflict || retry.Body.String() != `{"error":"Candidate is not failed"}` {
		t.Fatalf("retry queued = %d %s", retry.Code, retry.Body.String())
	}
}

func TestNewsroomInputValidation(t *testing.T) {
	handler, _, auth := newsroomApplication(t)
	cases := []struct {
		method, path, body string
		code               int
		response           string
	}{
		{http.MethodGet, "/api/newsroom/candidates?status=bogus", "", http.StatusBadRequest, `{"error":"unknown status \"bogus\""}`},
		{http.MethodPost, "/api/newsroom/candidates/publish", `{"ids":[]}`, http.StatusBadRequest, `{"error":"ids must contain 1 to 100 candidate ids"}`},
		{http.MethodPost, "/api/newsroom/candidates/publish", `{"ids":[1]} {}`, http.StatusBadRequest, `{"error":"Invalid request body"}`},
		{http.MethodPost, "/api/newsroom/candidates/reject", `{"id":1}`, http.StatusBadRequest, `{"error":"Invalid request body"}`},
		{http.MethodPost, "/api/newsroom/candidates/abc/retry", "", http.StatusNotFound, `{"error":"Candidate not found"}`},
		{http.MethodPost, "/api/newsroom/candidates/7/unqueue", "", http.StatusNotFound, `{"error":"Candidate not found"}`},
		{http.MethodPut, "/api/newsroom/settings", `{"publish_delay_min_minutes":50,"publish_delay_max_minutes":40}`, http.StatusBadRequest, `{"error":"publish delay minutes must be between 1 and 1440, with min not above max"}`},
		{http.MethodPut, "/api/newsroom/settings", `{"publish_delay_min_minutes":10}`, http.StatusBadRequest, `{"error":"Invalid request body"}`},
		{http.MethodPost, "/api/newsroom/collect", "", http.StatusServiceUnavailable, `{"error":"Collector is not enabled"}`},
	}
	for _, tc := range cases {
		response := request(t, handler, tc.method, tc.path, tc.body, auth)
		if response.Code != tc.code || response.Body.String() != tc.response {
			t.Errorf("%s %s %s = %d %s, want %d %s", tc.method, tc.path, tc.body, response.Code, response.Body.String(), tc.code, tc.response)
		}
	}
}

func TestNewsroomSettingsRoundTrip(t *testing.T) {
	handler, _, auth := newsroomApplication(t)
	get := request(t, handler, http.MethodGet, "/api/newsroom/settings", "", auth)
	if get.Code != http.StatusOK || get.Body.String() != `{"publish_delay_min_minutes":30,"publish_delay_max_minutes":40}` {
		t.Fatalf("default settings = %d %s", get.Code, get.Body.String())
	}
	put := request(t, handler, http.MethodPut, "/api/newsroom/settings", `{"publish_delay_min_minutes":10,"publish_delay_max_minutes":20}`, auth)
	if put.Code != http.StatusOK || put.Body.String() != `{"publish_delay_min_minutes":10,"publish_delay_max_minutes":20}` {
		t.Fatalf("put settings = %d %s", put.Code, put.Body.String())
	}
	if again := request(t, handler, http.MethodGet, "/api/newsroom/settings", "", auth); again.Body.String() != put.Body.String() {
		t.Fatalf("settings after put = %s", again.Body.String())
	}
}
