package indexnow_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/indexnow"
)

func TestSubmitSlugsSendsNodePayload(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json; charset=utf-8" {
			t.Errorf("content type = %q", r.Header.Get("Content-Type"))
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := indexnow.New(indexnow.WithEndpoint(server.URL))
	if err := client.SubmitSlugs(context.Background(), []string{"a-story", "a-story", "b story"}); err != nil {
		t.Fatal(err)
	}
	urls := payload["urlList"].([]any)
	if payload["host"] != "www.aiandtech.news" || payload["key"] != indexnow.Key ||
		payload["keyLocation"] != "https://www.aiandtech.news/"+indexnow.Key+".txt" ||
		len(urls) != 2 || urls[0] != "https://www.aiandtech.news/article/a-story" || urls[1] != "https://www.aiandtech.news/article/b%20story" {
		t.Fatalf("payload = %v", payload)
	}
}

func TestSubmitSlugsReportsRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte("bad key"))
	}))
	defer server.Close()
	err := indexnow.New(indexnow.WithEndpoint(server.URL)).SubmitSlugs(context.Background(), []string{"a"})
	if err == nil || !strings.Contains(err.Error(), "422") || !strings.Contains(err.Error(), "bad key") {
		t.Fatalf("err = %v", err)
	}
	if err := indexnow.New(indexnow.WithEndpoint(server.URL)).SubmitSlugs(context.Background(), nil); err != nil {
		t.Fatalf("empty submit should be a no-op: %v", err)
	}
}
