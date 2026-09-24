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
	"reflect"
	"sort"
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
	cfg := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4402", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), JWTSecret: authTestSecret, UploadsDir: t.TempDir()}
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
	if operations != 11 {
		t.Fatalf("contract operations = %d, want 11", operations)
	}
}

// contractRequestBody returns a body that is valid JSON for the given
// contract route, or empty for routes that take no body.
func contractRequestBody(path, method string) string {
	switch {
	case method == http.MethodPost && (path == "/api/newsroom/candidates/publish" || path == "/api/newsroom/candidates/reject"):
		return `{"ids":[1]}`
	case method == http.MethodPost && path == "/api/newsroom/queue/shift":
		return `{"minutes":30}`
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
		{http.MethodPost, "/api/newsroom/queue/shift", `{"minutes":0}`, http.StatusBadRequest, `{"error":"minutes must be a non-zero integer between -1440 and 1440"}`},
		{http.MethodPost, "/api/newsroom/queue/shift", `{"minutes":1441}`, http.StatusBadRequest, `{"error":"minutes must be a non-zero integer between -1440 and 1440"}`},
		{http.MethodPost, "/api/newsroom/queue/shift", `{"minutes":-1441}`, http.StatusBadRequest, `{"error":"minutes must be a non-zero integer between -1440 and 1440"}`},
		{http.MethodPost, "/api/newsroom/queue/shift", `{}`, http.StatusBadRequest, `{"error":"minutes must be a non-zero integer between -1440 and 1440"}`},
		{http.MethodPost, "/api/newsroom/queue/shift", `{"minutes":5,"x":1}`, http.StatusBadRequest, `{"error":"Invalid request body"}`},
		{http.MethodPost, "/api/newsroom/collect", "", http.StatusServiceUnavailable, `{"error":"Collector is not enabled"}`},
	}
	for _, tc := range cases {
		response := request(t, handler, tc.method, tc.path, tc.body, auth)
		if response.Code != tc.code || response.Body.String() != tc.response {
			t.Errorf("%s %s %s = %d %s, want %d %s", tc.method, tc.path, tc.body, response.Code, response.Body.String(), tc.code, tc.response)
		}
	}
}

// TestNewsroomResponsesMatchContractSchemas proves every newsroom response
// this handler actually sends carries exactly the JSON keys its OpenAPI
// schema declares required — no more, no less.
func TestNewsroomResponsesMatchContractSchemas(t *testing.T) {
	handler, db, auth := newsroomApplication(t)
	seedCandidates(t, db, "https://example.com/1", "https://example.com/2")

	raw, err := os.ReadFile(filepath.Join("..", "..", "contracts", "newsroom.openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}

	overview := decodeBody[map[string]any](t, request(t, handler, http.MethodGet, "/api/newsroom/overview", "", auth).Body.String())
	assertKeySet(t, "overview", overview, schemaRequired(t, document, "Overview"))

	candidatePage := decodeBody[map[string]any](t, request(t, handler, http.MethodGet, "/api/newsroom/candidates", "", auth).Body.String())
	assertKeySet(t, "candidate page", candidatePage, schemaRequired(t, document, "CandidatePage"))
	candidates := asObjectList(t, "candidates", candidatePage["candidates"])
	if len(candidates) == 0 {
		t.Fatal("expected at least one candidate")
	}
	for _, candidate := range candidates {
		assertKeySet(t, "candidate", candidate, schemaRequired(t, document, "Candidate"))
	}

	publish := decodeBody[map[string]any](t, request(t, handler, http.MethodPost, "/api/newsroom/candidates/publish", `{"ids":[1]}`, auth).Body.String())
	assertKeySet(t, "publish response", publish, []string{"queued", "skipped"})
	queued := asObjectList(t, "queued", publish["queued"])
	if len(queued) == 0 {
		t.Fatal("expected at least one queued candidate")
	}
	assertKeySet(t, "queued[0]", queued[0], schemaRequired(t, document, "Candidate"))

	unqueue := decodeBody[map[string]any](t, request(t, handler, http.MethodPost, "/api/newsroom/candidates/1/unqueue", "", auth).Body.String())
	assertKeySet(t, "unqueue response", unqueue, []string{"candidate"})
	unqueuedCandidate, ok := unqueue["candidate"].(map[string]any)
	if !ok {
		t.Fatalf("unqueue response candidate is not an object: %#v", unqueue["candidate"])
	}
	assertKeySet(t, "unqueue candidate", unqueuedCandidate, schemaRequired(t, document, "Candidate"))

	publishNow := decodeBody[map[string]any](t, request(t, handler, http.MethodPost, "/api/newsroom/candidates/2/publish-now", "", auth).Body.String())
	assertKeySet(t, "publish-now response", publishNow, []string{"candidate"})
	publishNowCandidate, ok := publishNow["candidate"].(map[string]any)
	if !ok {
		t.Fatalf("publish-now response candidate is not an object: %#v", publishNow["candidate"])
	}
	assertKeySet(t, "publish-now candidate", publishNowCandidate, schemaRequired(t, document, "Candidate"))

	shift := decodeBody[map[string]any](t, request(t, handler, http.MethodPost, "/api/newsroom/queue/shift", `{"minutes":15}`, auth).Body.String())
	assertKeySet(t, "queue shift response", shift, schemaRequired(t, document, "QueueShift"))
	shiftedCandidates := asObjectList(t, "shift candidates", shift["candidates"])
	if len(shiftedCandidates) == 0 {
		t.Fatal("expected at least one queued candidate after the shift")
	}
	for _, candidate := range shiftedCandidates {
		assertKeySet(t, "shift candidate", candidate, schemaRequired(t, document, "Candidate"))
	}
	shiftExample := responseExample(t, document, "/api/newsroom/queue/shift", "post", "200")
	assertKeySet(t, "queue shift example", shiftExample, schemaRequired(t, document, "QueueShift"))

	reject := decodeBody[map[string]any](t, request(t, handler, http.MethodPost, "/api/newsroom/candidates/reject", `{"ids":[1]}`, auth).Body.String())
	assertKeySet(t, "reject response", reject, []string{"rejected", "skipped"})

	settings := decodeBody[map[string]any](t, request(t, handler, http.MethodGet, "/api/newsroom/settings", "", auth).Body.String())
	assertKeySet(t, "settings", settings, schemaRequired(t, document, "PublishDelay"))

	overviewExample := responseExample(t, document, "/api/newsroom/overview", "get", "200")
	assertKeySet(t, "overview example", overviewExample, schemaRequired(t, document, "Overview"))

	candidatesExample := responseExample(t, document, "/api/newsroom/candidates", "get", "200")
	exampleCandidates := asObjectList(t, "example candidates", candidatesExample["candidates"])
	if len(exampleCandidates) == 0 {
		t.Fatal("expected at least one example candidate")
	}
	assertKeySet(t, "example candidates[0]", exampleCandidates[0], schemaRequired(t, document, "Candidate"))
}

// schemaRequired resolves components.schemas.<name> (following a $ref chain
// if the schema itself is one) and returns its `required` key list.
func schemaRequired(t *testing.T, document map[string]any, name string) []string {
	t.Helper()
	schema := resolveSchema(t, document, name)
	rawRequired, _ := schema["required"].([]any)
	required := make([]string, len(rawRequired))
	for i, value := range rawRequired {
		required[i], _ = value.(string)
	}
	return required
}

func resolveSchema(t *testing.T, document map[string]any, name string) map[string]any {
	t.Helper()
	components, _ := document["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	schema, ok := schemas[name].(map[string]any)
	if !ok {
		t.Fatalf("schema %s not found in contract", name)
	}
	if ref, ok := schema["$ref"].(string); ok {
		return resolveSchema(t, document, strings.TrimPrefix(ref, "#/components/schemas/"))
	}
	return schema
}

// responseExample returns the `example` object under
// paths.<path>.<method>.responses.<code>.content.application/json.
func responseExample(t *testing.T, document map[string]any, path, method, code string) map[string]any {
	t.Helper()
	paths, _ := document["paths"].(map[string]any)
	pathItem, _ := paths[path].(map[string]any)
	operation, _ := pathItem[method].(map[string]any)
	responses, _ := operation["responses"].(map[string]any)
	response, _ := responses[code].(map[string]any)
	content, _ := response["content"].(map[string]any)
	appJSON, _ := content["application/json"].(map[string]any)
	example, ok := appJSON["example"].(map[string]any)
	if !ok {
		t.Fatalf("no example for %s %s -> %s", method, path, code)
	}
	return example
}

// asObjectList type-asserts a decoded JSON array field into a slice of
// generic objects, failing the test with the field name on mismatch.
func asObjectList(t *testing.T, field string, value any) []map[string]any {
	t.Helper()
	raw, ok := value.([]any)
	if !ok {
		t.Fatalf("%s is not an array: %#v", field, value)
	}
	list := make([]map[string]any, len(raw))
	for i, item := range raw {
		object, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("%s[%d] is not an object: %#v", field, i, item)
		}
		list[i] = object
	}
	return list
}

// assertKeySet fails the test unless body's keys are exactly want, as sets.
func assertKeySet(t *testing.T, label string, body map[string]any, want []string) {
	t.Helper()
	got := make([]string, 0, len(body))
	for key := range body {
		got = append(got, key)
	}
	sort.Strings(got)
	wantSorted := append([]string(nil), want...)
	sort.Strings(wantSorted)
	if !reflect.DeepEqual(got, wantSorted) {
		t.Errorf("%s keys = %v, want %v", label, got, wantSorted)
	}
}

func TestNewsroomPublishNow(t *testing.T) {
	handler, db, auth := newsroomApplication(t)
	seedCandidates(t, db, "https://example.com/1", "https://example.com/2")

	path := "/api/newsroom/candidates/1/publish-now"
	if response := request(t, handler, http.MethodPost, path, "", ""); response.Code != http.StatusUnauthorized ||
		response.Body.String() != `{"error":"Authentication required"}` {
		t.Fatalf("unauthenticated publish-now = %d %s", response.Code, response.Body.String())
	}

	response := request(t, handler, http.MethodPost, path, "", auth)
	expectStatus(t, "POST", path, http.StatusOK, response.Code, response.Body.String())
	candidate := decodeBody[struct{ Candidate newsroom.Candidate }](t, response.Body.String()).Candidate
	if candidate.ID != 1 || candidate.Status != newsroom.StatusQueued || !candidate.PublishNow ||
		candidate.ScheduledFor == nil || *candidate.ScheduledFor != newsroomNow.Format(time.RFC3339) {
		t.Fatalf("publish-now candidate = %+v", candidate)
	}
	if !strings.Contains(response.Body.String(), `"publish_now":true`) {
		t.Fatalf("publish-now body = %s", response.Body.String())
	}

	if _, err := db.Exec(`UPDATE candidates SET status = 'rejected', scheduled_for = NULL WHERE id = 2`); err != nil {
		t.Fatal(err)
	}
	conflict := request(t, handler, http.MethodPost, "/api/newsroom/candidates/2/publish-now", "", auth)
	if conflict.Code != http.StatusConflict || conflict.Body.String() != `{"error":"Candidate cannot be published now"}` {
		t.Fatalf("publish-now rejected = %d %s", conflict.Code, conflict.Body.String())
	}
	for _, missing := range []string{"99", "abc", "0"} {
		notFound := request(t, handler, http.MethodPost, "/api/newsroom/candidates/"+missing+"/publish-now", "", auth)
		if notFound.Code != http.StatusNotFound || notFound.Body.String() != `{"error":"Candidate not found"}` {
			t.Fatalf("publish-now %s = %d %s", missing, notFound.Code, notFound.Body.String())
		}
	}
}

func TestNewsroomQueueShift(t *testing.T) {
	handler, db, auth := newsroomApplication(t)
	path := "/api/newsroom/queue/shift"
	if response := request(t, handler, http.MethodPost, path, `{"minutes":30}`, ""); response.Code != http.StatusUnauthorized ||
		response.Body.String() != `{"error":"Authentication required"}` {
		t.Fatalf("unauthenticated shift = %d %s", response.Code, response.Body.String())
	}

	empty := request(t, handler, http.MethodPost, path, `{"minutes":30}`, auth)
	if empty.Code != http.StatusOK || empty.Body.String() != `{"shifted":0,"minutes":0,"candidates":[]}` {
		t.Fatalf("empty queue shift = %d %s", empty.Code, empty.Body.String())
	}

	seedCandidates(t, db, "https://example.com/1", "https://example.com/2", "https://example.com/3")
	for _, statement := range []string{
		`UPDATE candidates SET status = 'queued', scheduled_for = '2026-09-20T12:20:00Z' WHERE id = 1`,
		`UPDATE candidates SET status = 'queued', scheduled_for = '2026-09-20T12:55:00Z' WHERE id = 2`,
		`UPDATE candidates SET status = 'processing', scheduled_for = '2026-09-20T12:10:00Z' WHERE id = 3`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if response := request(t, handler, http.MethodPost, "/api/newsroom/candidates/2/publish-now", "", auth); response.Code != http.StatusOK {
		t.Fatalf("publish-now = %d %s", response.Code, response.Body.String())
	}

	response := request(t, handler, http.MethodPost, path, `{"minutes":-45}`, auth)
	expectStatus(t, "POST", path, http.StatusOK, response.Code, response.Body.String())
	result := decodeBody[newsroom.QueueShift](t, response.Body.String())
	if result.Shifted != 1 || result.Minutes != -20 || len(result.Candidates) != 2 {
		t.Fatalf("shift = %+v", result)
	}
	// Candidate 1 is clamped to now; the publish-now candidate 2 stays due now
	// and ties with it, so id breaks the tie.
	if result.Candidates[0].ID != 1 || result.Candidates[0].PublishNow || *result.Candidates[0].ScheduledFor != "2026-09-20T12:00:00Z" ||
		result.Candidates[1].ID != 2 || !result.Candidates[1].PublishNow || *result.Candidates[1].ScheduledFor != "2026-09-20T12:00:00Z" {
		t.Fatalf("shift candidates = %+v", result.Candidates)
	}
	var processing string
	if err := db.QueryRow(`SELECT scheduled_for FROM candidates WHERE id = 3`).Scan(&processing); err != nil || processing != "2026-09-20T12:10:00Z" {
		t.Fatalf("processing candidate scheduled_for = %q err=%v", processing, err)
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
