package content_test

import (
	"net/http"
	"reflect"
	"testing"
)

func TestDashboardDeleteArticleMatchesNode(t *testing.T) {
	for _, tt := range []struct {
		name, target string
		status       int
		body         string
		remaining    int
		indexNow     [][]string
	}{
		{"published article notifies its URL", "/articles/305", 200, `{"success":true}`, 3, [][]string{{"approved-published"}}},
		{"draft article does not notify", "/articles/303", 200, `{"success":true}`, 3, nil},
		{"missing article", "/articles/999", 404, `{"error":"Article not found"}`, 4, nil},
		{"non-numeric id", "/articles/abc", 404, `{"error":"Article not found"}`, 4, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler, db, notifier := adminServer(t)
			expectBody(t, adminRequest(t, handler, notifier, http.MethodDelete, tt.target, ""), tt.status, tt.body)
			if remaining := countRows(t, db, `SELECT COUNT(*) FROM articles`); remaining != tt.remaining {
				t.Errorf("articles = %d, want %d", remaining, tt.remaining)
			}
			if !reflect.DeepEqual(notifier.calls, tt.indexNow) {
				t.Errorf("IndexNow calls = %v, want %v", notifier.calls, tt.indexNow)
			}
		})
	}
}

func TestDashboardDeleteArticleUnlinksNewsroomCandidates(t *testing.T) {
	handler, db, notifier := adminServer(t)
	if _, err := db.Exec(`INSERT INTO candidates (source_url, source_name, feed_url, title, discovered_at, status, article_id, published_at, updated_at)
		VALUES ('https://techcrunch.com/2026/09/01/model', 'TechCrunch', 'https://techcrunch.com/feed/', 'T', '2026-09-01T00:00:00Z', 'published', 305, '2026-09-01T00:00:00Z', '2026-09-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	expectBody(t, adminRequest(t, handler, notifier, http.MethodDelete, "/articles/305", ""), http.StatusOK, `{"success":true}`)
	if linked := countRows(t, db, `SELECT COUNT(*) FROM candidates WHERE article_id IS NOT NULL`); linked != 0 {
		t.Errorf("candidates still linked to the deleted article: %d", linked)
	}
}
