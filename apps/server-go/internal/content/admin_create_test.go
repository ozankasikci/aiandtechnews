package content_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

func publishedBody(extra map[string]any) string {
	body := map[string]any{
		"title": validTitle, "excerpt": validExcerpt, "content": validContent,
		"slug": "new-published", "category_id": 101, "status": "published", "source": "TechCrunch",
	}
	for key, value := range extra {
		body[key] = value
	}
	return jsonObject(body)
}

func jsonObject(body map[string]any) string {
	data, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// Every expected status and body below was recorded from the Node server
// (apps/server/src/routes/dashboard.ts) against the same seed.
func TestDashboardCreateArticleRejectsLikeNode(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		status     int
		want       string
	}{
		{"missing fields", `{}`, 400, `{"error":"Title, slug, and category_id are required"}`},
		{"falsy category", `{"title":"T","slug":"s","category_id":0}`, 400, `{"error":"Title, slug, and category_id are required"}`},
		{"empty slug", `{"title":"T","slug":"","category_id":101}`, 400, `{"error":"Title, slug, and category_id are required"}`},
		{"invalid source url", `{"title":"T","slug":"s","category_id":101,"source_url":"nope"}`, 400, `{"error":"Source URL is invalid"}`},
		{"published without source", `{"title":"T","slug":"s","category_id":101,"status":"published","source_url":"https://techcrunch.com/a"}`, 400, `{"error":"Published articles require source and source_url"}`},
		{"published without source url", `{"title":"T","slug":"s","category_id":101,"status":"published","source":"TechCrunch"}`, 400, `{"error":"Published articles require source and source_url"}`},
		{"policy failure", `{"title":"Big news!","slug":"s","category_id":101,"status":"published","source":"The Verge","source_url":"https://techcrunch.com/a"}`, 400,
			`{"error":"Article failed publishing policy","details":["headline appears clickbait-like","excerpt is missing","article must contain 5 to 12 paragraphs","article must contain 150 to 800 words, found 0","source does not match source URL"]}`},
		{"unapproved source", publishedBody(map[string]any{"slug": "s", "source": "Synthetic Wire", "source_url": "https://news.example.invalid/x"}), 400,
			`{"error":"Article failed publishing policy","details":["source URL is not from an approved publication"]}`},
		{"unknown category", `{"title":"T","slug":"s","category_id":999}`, 400, `{"error":"Category not found"}`},
		{"deals category", `{"title":"T","slug":"s","category_id":104}`, 400, `{"error":"Deals articles are not allowed"}`},
		{"duplicate slug", `{"title":"T","slug":"synthetic-draft","category_id":101}`, 409, `{"error":"Article slug or source_url already exists"}`},
		{"duplicate normalized source url", `{"title":"T","slug":"s","category_id":101,"source_url":"https://techcrunch.com/2026/09/01/model/?utm_source=x#top"}`, 409, `{"error":"Article slug or source_url already exists"}`},
		{"unbindable title", `{"title":true,"slug":"s","category_id":101}`, 500, internalError},
		{"status outside CHECK", `{"title":"T","slug":"s","category_id":101,"status":"archived"}`, 500, internalError},
		{"malformed json", `{`, 400, `{"error":"Invalid request body"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler, db, notifier := adminServer(t)
			expectBody(t, adminRequest(t, handler, notifier, http.MethodPost, "/articles", tt.body), tt.status, tt.want)
			if count := countRows(t, db, `SELECT COUNT(*) FROM articles`); count != 4 {
				t.Errorf("articles = %d, want 4 (nothing written)", count)
			}
			if len(notifier.calls) != 0 {
				t.Errorf("IndexNow calls = %v", notifier.calls)
			}
		})
	}
}

func TestDashboardCreateArticleRequiresEditorialAuthor(t *testing.T) {
	handler, db, notifier := adminServer(t)
	if _, err := db.Exec(`UPDATE authors SET name = 'Former Editorial' WHERE id = 201`); err != nil {
		t.Fatal(err)
	}
	expectBody(t, adminRequest(t, handler, notifier, http.MethodPost, "/articles", `{"title":"T","slug":"s","category_id":101}`),
		http.StatusInternalServerError, `{"error":"TechNews Editorial author is missing"}`)
}

func TestDashboardCreateDraftAppliesNodeDefaults(t *testing.T) {
	handler, _, notifier := adminServer(t)
	response := adminRequest(t, handler, notifier, http.MethodPost, "/articles",
		`{"title":"T","slug":"new-draft","category_id":"101","featured_image":"","meta_title":"","source":""}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
	expectFields(t, articleFields(t, response), map[string]any{
		"id": float64(306), "title": "T", "slug": "new-draft", "excerpt": "", "content": "", "featured_image": nil,
		"category_id": float64(101), "author_id": float64(201), "status": "draft", "published_at": nil,
		"meta_title": nil, "meta_description": nil, "source": nil, "source_url": nil, "view_count": float64(0),
		"created_at": "2026-09-20 12:00:00", "updated_at": "2026-09-20 12:00:00",
	})
	if len(notifier.calls) != 0 {
		t.Errorf("draft create notified IndexNow: %v", notifier.calls)
	}
}

func TestDashboardCreatePublishedStampsNormalizesAndNotifiesAfterResponse(t *testing.T) {
	handler, _, notifier := adminServer(t)
	response := adminRequest(t, handler, notifier, http.MethodPost, "/articles",
		publishedBody(map[string]any{"source_url": "https://www.techcrunch.com/2026/09/20/new/?utm_campaign=x"}))
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
	expectFields(t, articleFields(t, response), map[string]any{
		"status": "published", "published_at": "2026-09-20T12:00:00.000Z", "author_id": float64(201),
		"source": "TechCrunch", "source_url": "https://www.techcrunch.com/2026/09/20/new",
		"created_at": "2026-09-20 12:00:00", "updated_at": "2026-09-20 12:00:00",
	})
	if !reflect.DeepEqual(notifier.calls, [][]string{{"new-published"}}) {
		t.Fatalf("IndexNow calls = %v", notifier.calls)
	}
	if notifier.bodyAtNotify[0] != response.Body.Len() {
		t.Errorf("IndexNow was notified before the response body was written")
	}
}

func TestDashboardCreatePublishedKeepsExplicitPublishedAt(t *testing.T) {
	handler, _, notifier := adminServer(t)
	response := adminRequest(t, handler, notifier, http.MethodPost, "/articles",
		publishedBody(map[string]any{"source_url": "https://techcrunch.com/2026/09/20/new", "published_at": "2026-09-19T08:00:00.000Z"}))
	expectFields(t, articleFields(t, response), map[string]any{"published_at": "2026-09-19T08:00:00.000Z"})
}

func TestDashboardCreateBindsNumbersLikeBetterSQLite3(t *testing.T) {
	handler, _, notifier := adminServer(t)
	response := adminRequest(t, handler, notifier, http.MethodPost, "/articles", `{"title":5,"slug":"numeric","category_id":101.0}`)
	expectFields(t, articleFields(t, response), map[string]any{"title": "5.0", "category_id": float64(101)})
}
