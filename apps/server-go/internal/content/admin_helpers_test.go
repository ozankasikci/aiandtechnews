package content_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

// adminNow is the fixed clock of every dashboard test (the Node fixture clock).
var adminNow = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

// A rewritten article that passes the publishing policy: 5 paragraphs, 230 words.
const (
	validTitle    = "Researchers release a new language model"
	validExcerpt  = "Researchers released new benchmark results for a language model."
	validSentence = "The research team shared new benchmark results for the language model and explained how the training data was collected and filtered before release."
)

var validContent = strings.Repeat("<p>"+validSentence+" "+validSentence+"</p>", 5)

// recordingIndexNow records Notify calls and how much of the response body
// had been written when each call happened.
type recordingIndexNow struct {
	calls         [][]string
	bodyAtNotify  []int
	currentWriter *httptest.ResponseRecorder
}

func (r *recordingIndexNow) Notify(slugs []string) {
	r.calls = append(r.calls, append([]string(nil), slugs...))
	r.bodyAtNotify = append(r.bodyAtNotify, r.currentWriter.Body.Len())
}

// seedAdmin extends the public-read seed (301-303) with a deals category and
// a published article (305) that passes the publishing policy.
func seedAdmin(t *testing.T, db *sql.DB) {
	t.Helper()
	seed(t, db)
	if _, err := db.Exec(`INSERT INTO categories(id,name,slug,description,color) VALUES (104,'Deals','deals','Promotions','#999999')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO articles(id,title,slug,excerpt,content,featured_image,category_id,author_id,status,published_at,meta_title,meta_description,source,source_url,view_count,created_at,updated_at)
		VALUES (305,?,'approved-published',?,?,NULL,101,202,'published','2026-09-01T00:00:00.000Z',NULL,NULL,'TechCrunch','https://techcrunch.com/2026/09/01/model',5,'2026-09-01 00:00:00','2026-09-01 00:00:00')`,
		validTitle, validExcerpt, validContent); err != nil {
		t.Fatal(err)
	}
}

func adminServer(t *testing.T) (http.Handler, *sql.DB, *recordingIndexNow) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	seedAdmin(t, db)
	notifier := &recordingIndexNow{}
	service := content.NewAdminService(content.NewSQLiteStore(db), func() time.Time { return adminNow })
	router := chi.NewRouter()
	content.NewAdminHandler(service, notifier, slog.New(slog.NewTextHandler(io.Discard, nil))).Mount(router)
	return router, db, notifier
}

func adminRequest(t *testing.T, handler http.Handler, notifier *recordingIndexNow, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	notifier.currentWriter = response
	handler.ServeHTTP(response, request)
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("%s %s Content-Type = %q", method, target, got)
	}
	return response
}

func expectBody(t *testing.T, response *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	if response.Code != status || response.Body.String() != body {
		t.Fatalf("response = %d %s, want %d %s", response.Code, response.Body.String(), status, body)
	}
}

// articleFields decodes {"article": {...}} and returns the article object.
func articleFields(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope struct {
		Article map[string]any `json:"article"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Article == nil {
		t.Fatalf("decode article from %d %s: %v", response.Code, response.Body.String(), err)
	}
	return envelope.Article
}

func expectFields(t *testing.T, got map[string]any, want map[string]any) {
	t.Helper()
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %#v, want %#v", key, got[key], value)
		}
	}
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

const internalError = `{"error":"Internal server error"}`
