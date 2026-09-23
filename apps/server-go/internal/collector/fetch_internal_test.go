package collector

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// testFetcher maps each httptest server host to a source name.
func testFetcher(sources map[string]string) *Fetcher {
	fetcher := NewFetcher()
	fetcher.sourceFor = func(raw string) (string, bool) {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "", false
		}
		source, ok := sources[parsed.Host]
		return source, ok
	}
	return fetcher
}

func TestFetchTextSendsHeadersAndReturnsBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != userAgent || !strings.Contains(r.Header.Get("Accept"), "application/rss+xml") {
			t.Errorf("headers = %v", r.Header)
		}
		_, _ = w.Write([]byte("<rss/>"))
	}))
	defer server.Close()

	body, final, err := NewFetcher().FetchText(context.Background(), server.URL+"/feed", "")
	if err != nil || body != "<rss/>" || final != server.URL+"/feed" {
		t.Fatalf("body=%q final=%q err=%v", body, final, err)
	}
}

func TestFetchTextRejectsNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer server.Close()
	if _, _, err := NewFetcher().FetchText(context.Background(), server.URL, ""); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchTextEnforcesApprovedSourceAcrossRedirects(t *testing.T) {
	outside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("elsewhere")) }))
	defer outside.Close()
	approved := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/same":
			http.Redirect(w, r, "/final", http.StatusFound)
		case "/away":
			http.Redirect(w, r, outside.URL+"/x", http.StatusFound)
		default:
			_, _ = w.Write([]byte("article"))
		}
	}))
	defer approved.Close()
	fetcher := testFetcher(map[string]string{
		strings.TrimPrefix(approved.URL, "http://"): "TechCrunch",
		strings.TrimPrefix(outside.URL, "http://"):  "Elsewhere",
	})

	body, final, err := fetcher.FetchText(context.Background(), approved.URL+"/same", "TechCrunch")
	if err != nil || body != "article" || final != approved.URL+"/final" {
		t.Fatalf("same-source redirect: body=%q final=%q err=%v", body, final, err)
	}
	if _, _, err := fetcher.FetchText(context.Background(), approved.URL+"/away", "TechCrunch"); !errors.Is(err, ErrRedirectOutsideSource) {
		t.Fatalf("outside redirect err = %v", err)
	}
	if _, _, err := fetcher.FetchText(context.Background(), approved.URL+"/away", ""); err != nil {
		t.Fatalf("feeds (no expected source) may redirect anywhere: %v", err)
	}
}

func TestFetchTextStopsAfterFiveRedirectsAndTimesOut(t *testing.T) {
	loop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path+"x", http.StatusFound)
	}))
	defer loop.Close()
	if _, _, err := NewFetcher().FetchText(context.Background(), loop.URL+"/", ""); err == nil {
		t.Fatal("redirect loop should fail")
	}

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(time.Second):
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	fetcher := NewFetcher()
	fetcher.timeout = 50 * time.Millisecond
	if _, _, err := fetcher.FetchText(context.Background(), slow.URL, ""); err == nil {
		t.Fatal("slow response should time out")
	}
}

func TestFetchTextCapsBodySize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", maxBodyBytes+10)))
	}))
	defer server.Close()
	body, _, err := NewFetcher().FetchText(context.Background(), server.URL, "")
	if body != "" || err == nil || !strings.Contains(err.Error(), "body exceeds") {
		t.Fatalf("body=%q err=%v, want a body-exceeds error", body, err)
	}
}
