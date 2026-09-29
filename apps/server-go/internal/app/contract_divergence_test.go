package app_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/contracttest"
)

// publicArticleOperations are recorded Node article reads whose responses
// this server deliberately changes: it never publishes an article's source
// or source_url, which stay stored for duplicate detection and the dashboard.
var publicArticleOperations = map[string]bool{
	"articles.list": true, "articles.trending": true, "articles.getBySlug": true, "articles.getById": true,
}

// withoutArticleSource returns op with source and source_url removed from the
// recorded article(s) when op is a public article read, and op unchanged
// otherwise.
func withoutArticleSource(t *testing.T, op contracttest.Operation) contracttest.Operation {
	t.Helper()
	if !publicArticleOperations[op.OperationID] {
		return op
	}
	var body map[string]any
	if err := json.Unmarshal(op.Response.Body, &body); err != nil {
		t.Fatalf("%s body: %v", op.OperationID, err)
	}
	strip := func(article any) {
		if fields, ok := article.(map[string]any); ok {
			delete(fields, "source")
			delete(fields, "source_url")
		}
	}
	if articles, ok := body["articles"].([]any); ok {
		for _, article := range articles {
			strip(article)
		}
	}
	strip(body["article"])
	stripped, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	op.Response.Body = stripped
	return op
}

// countBeaconViewAfter applies the second approved divergence from the
// recorded Node reads: Node counted a view when articles.getBySlug was read,
// while this server counts views only through the website's view beacon
// (POST /api/articles/{slug}/view), because the site caches article pages.
// After replaying articles.getBySlug it sends that beacon for the same slug,
// so the recorded 42 -> 43 view_count transition seen by later operations
// still holds without editing the fixture.
func countBeaconViewAfter(t *testing.T, handler http.Handler, op contracttest.Operation) {
	t.Helper()
	if op.OperationID != "articles.getBySlug" {
		return
	}
	var slug string
	if err := json.Unmarshal(op.Request.PathParameters["slug"], &slug); err != nil || slug == "" {
		t.Fatalf("articles.getBySlug slug: %q, %v", slug, err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/articles/"+slug+"/view", nil)
	request.Header.Set("User-Agent", "Mozilla/5.0 (contract replay)")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("view beacon for %s = %d %q", slug, response.Code, response.Body.String())
	}
}
