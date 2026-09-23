package content_test

import (
	"net/http"
	"reflect"
	"testing"
)

// Expected statuses and bodies were recorded from the Node server on the same seed.
func TestDashboardUpdateArticleRejectsLikeNode(t *testing.T) {
	for _, tt := range []struct {
		name, target, body string
		status             int
		want               string
	}{
		{"missing article", "/articles/999", `{"title":"x"}`, 404, `{"error":"Article not found"}`},
		{"invalid source url", "/articles/303", `{"source_url":"nope"}`, 400, `{"error":"Source URL is invalid"}`},
		{"empty draft update", "/articles/303", `{}`, 400, `{"error":"No fields to update"}`},
		{"non-json draft update", "/articles/303", ``, 400, `{"error":"No fields to update"}`},
		{"deals category", "/articles/303", `{"category_id":104}`, 400, `{"error":"Deals articles are not allowed"}`},
		{"unknown category", "/articles/303", `{"category_id":999}`, 400, `{"error":"Category not found"}`},
		{"duplicate slug", "/articles/303", `{"slug":"synthetic-published-newer"}`, 409, `{"error":"Article slug or source_url already exists"}`},
		{"published legacy article fails policy", "/articles/301", `{}`, 400,
			`{"error":"Article failed publishing policy","details":["excerpt must be exactly one sentence","article must contain 5 to 12 paragraphs","article must contain 150 to 800 words, found 5","source URL is not from an approved publication"]}`},
		{"published without source", "/articles/302", `{"title":"x"}`, 400, `{"error":"Published articles require source and source_url"}`},
		{"publishing a draft runs the policy", "/articles/303", `{"status":"published"}`, 400,
			`{"error":"Article failed publishing policy","details":["excerpt must be exactly one sentence","article must contain 5 to 12 paragraphs","article must contain 150 to 800 words, found 2","source URL is not from an approved publication"]}`},
		{"null title hits NOT NULL", "/articles/303", `{"title":null}`, 500, internalError},
		{"null category hits NOT NULL", "/articles/303", `{"category_id":null}`, 500, internalError},
		{"malformed json", "/articles/303", `{"title":`, 400, `{"error":"Invalid request body"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler, db, notifier := adminServer(t)
			expectBody(t, adminRequest(t, handler, notifier, http.MethodPut, tt.target, tt.body), tt.status, tt.want)
			if changed := countRows(t, db, `SELECT COUNT(*) FROM articles WHERE updated_at = '2026-09-20 12:00:00'`); changed != 0 {
				t.Errorf("%d rows were updated", changed)
			}
			if len(notifier.calls) != 0 {
				t.Errorf("IndexNow calls = %v", notifier.calls)
			}
		})
	}
}

func TestDashboardUpdateArticleWritesOnlyDefinedFields(t *testing.T) {
	handler, _, notifier := adminServer(t)
	response := adminRequest(t, handler, notifier, http.MethodPut, "/articles/303", `{"title":"Draft Updated","meta_description":null}`)
	expectFields(t, articleFields(t, response), map[string]any{
		"title": "Draft Updated", "slug": "synthetic-draft", "excerpt": "Draft excerpt", "meta_description": nil,
		"source": "Synthetic Wire", "source_url": "https://news.example.invalid/draft", "author_id": float64(201),
		"status": "draft", "published_at": nil, "created_at": "2026-09-17T00:00:00.000Z", "updated_at": "2026-09-20 12:00:00",
	})
	cleared := adminRequest(t, handler, notifier, http.MethodPut, "/articles/303", `{"source_url":""}`)
	expectFields(t, articleFields(t, cleared), map[string]any{"source_url": nil, "source": "Synthetic Wire"})
	padded := adminRequest(t, handler, notifier, http.MethodPut, "/articles/%20303", `{"title":"Padded"}`)
	expectFields(t, articleFields(t, padded), map[string]any{"id": float64(303), "title": "Padded"})
	if len(notifier.calls) != 0 {
		t.Errorf("draft updates notified IndexNow: %v", notifier.calls)
	}
}

func TestDashboardUpdatePublishTransitionsMatchNode(t *testing.T) {
	for _, tt := range []struct {
		name, target, body string
		fields             map[string]any
		indexNow           [][]string
	}{
		{"empty update of a published article reassigns the editorial author", "/articles/305", `{}`,
			map[string]any{"author_id": float64(201), "published_at": "2026-09-01T00:00:00.000Z", "updated_at": "2026-09-20 12:00:00"},
			[][]string{{"approved-published"}}},
		{"publishing a valid draft stamps published_at", "/articles/303",
			jsonObject(map[string]any{"title": validTitle, "excerpt": validExcerpt, "content": validContent, "status": "published",
				"source": "TechCrunch", "source_url": "https://techcrunch.com/2026/09/20/draft"}),
			map[string]any{"status": "published", "published_at": "2026-09-20T12:00:00.000Z", "author_id": float64(201), "source_url": "https://techcrunch.com/2026/09/20/draft"},
			[][]string{{"synthetic-draft"}}},
		{"unpublishing notifies the old URL and keeps the author", "/articles/305", `{"status":"draft"}`,
			map[string]any{"status": "draft", "author_id": float64(202), "published_at": "2026-09-01T00:00:00.000Z"},
			[][]string{{"approved-published"}}},
		{"renaming a published article notifies both URLs", "/articles/305", `{"slug":"renamed"}`,
			map[string]any{"slug": "renamed", "published_at": "2026-09-01T00:00:00.000Z"},
			[][]string{{"approved-published", "renamed"}}},
		{"re-sending status published re-stamps published_at", "/articles/305", `{"status":"published"}`,
			map[string]any{"published_at": "2026-09-20T12:00:00.000Z"},
			[][]string{{"approved-published"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler, _, notifier := adminServer(t)
			response := adminRequest(t, handler, notifier, http.MethodPut, tt.target, tt.body)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d %s", response.Code, response.Body.String())
			}
			expectFields(t, articleFields(t, response), tt.fields)
			if !reflect.DeepEqual(notifier.calls, tt.indexNow) {
				t.Errorf("IndexNow calls = %v, want %v", notifier.calls, tt.indexNow)
			}
			if notifier.bodyAtNotify[0] != response.Body.Len() {
				t.Error("IndexNow was notified before the response body was written")
			}
		})
	}
}
