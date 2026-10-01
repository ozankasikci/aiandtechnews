package content_test

import (
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const browserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"

func postView(t *testing.T, handler http.Handler, slug, remote, userAgent string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/articles/"+slug+"/view", strings.NewReader(""))
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	req.Header.Set("Origin", "https://www.aiandtech.news")
	req.RemoteAddr = remote
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func articleViews(t *testing.T, db *sql.DB, id int) (total, today, hour int) {
	t.Helper()
	if err := db.QueryRow(`SELECT view_count FROM articles WHERE id = ?`, id).Scan(&total); err != nil {
		t.Fatal(err)
	}
	_ = db.QueryRow(`SELECT count FROM article_views WHERE article_id = ? AND day = date('now')`, id).Scan(&today)
	_ = db.QueryRow(`SELECT count FROM article_views_hourly WHERE article_id = ? AND hour = strftime('%Y-%m-%d %H', 'now')`, id).Scan(&hour)
	return total, today, hour
}

func newViewHandler(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	handler, closer := newArticleHandler(t, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return handler, closer.(*sql.DB)
}

func TestPublicArticleReadDoesNotCountAView(t *testing.T) {
	handler, db := newViewHandler(t)
	for range 3 {
		if response := request(t, handler, "/api/articles/synthetic-published-newer"); response.Code != 200 {
			t.Fatalf("read = %d", response.Code)
		}
	}
	if total, today, hour := articleViews(t, db, 301); total != 42 || today != 0 || hour != 0 {
		t.Fatalf("views after reads = %d/%d/%d, want 42/0/0", total, today, hour)
	}
}

func TestPublicArticleViewBeaconCountsOncePerClientWindow(t *testing.T) {
	handler, db := newViewHandler(t)
	response := postView(t, handler, "synthetic-published-newer", "203.0.113.7:5000", browserUA)
	if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("view = %d %q", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("CORS origin = %q", got)
	}
	if total, today, hour := articleViews(t, db, 301); total != 43 || today != 1 || hour != 1 {
		t.Fatalf("views = %d/%d/%d, want 43/1/1", total, today, hour)
	}
	// The same client again is not counted; another client is.
	if response := postView(t, handler, "synthetic-published-newer", "203.0.113.7:5001", browserUA); response.Code != http.StatusNoContent {
		t.Fatalf("repeat view = %d", response.Code)
	}
	if response := postView(t, handler, "synthetic-published-newer", "203.0.113.8:5000", browserUA); response.Code != http.StatusNoContent {
		t.Fatalf("second client view = %d", response.Code)
	}
	if total, today, hour := articleViews(t, db, 301); total != 44 || today != 2 || hour != 2 {
		t.Fatalf("views = %d/%d/%d, want 44/2/2", total, today, hour)
	}
}

func TestPublicArticleViewBeaconUsesForwardedClientBehindTunnel(t *testing.T) {
	handler, db := newViewHandler(t)
	for _, forwarded := range []string{"198.51.100.1", "198.51.100.2, 127.0.0.1", "198.51.100.1"} {
		req := httptest.NewRequest(http.MethodPost, "/api/articles/synthetic-published-newer/view", nil)
		req.RemoteAddr = "127.0.0.1:9000"
		req.Header.Set("X-Forwarded-For", forwarded)
		req.Header.Set("User-Agent", browserUA)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusNoContent {
			t.Fatalf("view from %s = %d", forwarded, response.Code)
		}
	}
	if total, _, _ := articleViews(t, db, 301); total != 44 {
		t.Fatalf("view count = %d, want 44", total)
	}
}

func TestPublicArticleViewBeaconIgnoresBots(t *testing.T) {
	handler, db := newViewHandler(t)
	for _, userAgent := range []string{"", "Googlebot/2.1 (+http://www.google.com/bot.html)", "curl/8.4.0", "Mozilla/5.0 HeadlessChrome/120.0", "facebookexternalhit/1.1 Preview"} {
		if response := postView(t, handler, "synthetic-published-newer", "203.0.113.9:1", userAgent); response.Code != http.StatusNoContent {
			t.Fatalf("bot %q = %d", userAgent, response.Code)
		}
	}
	if total, _, _ := articleViews(t, db, 301); total != 42 {
		t.Fatalf("view count after bots = %d, want 42", total)
	}
}

func TestPublicArticleViewBeaconRejectsUnknownAndUnpublished(t *testing.T) {
	handler, db := newViewHandler(t)
	for _, slug := range []string{"missing", "synthetic-draft"} {
		response := postView(t, handler, slug, "203.0.113.10:1", browserUA)
		if response.Code != http.StatusNotFound || response.Body.String() != `{"error":"Article not found"}` {
			t.Errorf("%s = %d %q", slug, response.Code, response.Body.String())
		}
	}
	if total, _, _ := articleViews(t, db, 303); total != 3 {
		t.Fatalf("draft view count = %d, want 3", total)
	}
}

func TestPublicArticleViewBeaconAnswersCORSPreflight(t *testing.T) {
	handler, _ := newViewHandler(t)
	req := httptest.NewRequest(http.MethodOptions, "/api/articles/synthetic-published-newer/view", nil)
	req.Header.Set("Origin", "https://www.aiandtech.news")
	req.Header.Set("Access-Control-Request-Method", "POST")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusNoContent || !strings.Contains(response.Header().Get("Access-Control-Allow-Methods"), "POST") {
		t.Fatalf("preflight = %d %q", response.Code, response.Header().Get("Access-Control-Allow-Methods"))
	}
}
