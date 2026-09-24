package illustration_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

func TestSourceImageIllustratorCopiesTheOriginalToStorage(t *testing.T) {
	png := tinyPNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	}))
	defer server.Close()
	store := &fakeImageStore{}
	illustrator := illustration.NewSourceImageIllustrator(store, illustration.NewReferenceClientAllowingAnyAddress(), discardLogger())

	result, err := illustrator.Illustrate(context.Background(), publisher.IllustrationRequest{Slug: "my-slug", ReferenceImageURL: server.URL + "/og.png"})
	if err != nil {
		t.Fatal(err)
	}
	if store.storedSlug != "my-slug" || result.URL != store.storedResult.URL {
		t.Fatalf("stored %q url %q", store.storedSlug, result.URL)
	}
	result.Discard(context.Background())
	if len(store.deletedKeys) != 1 {
		t.Fatalf("deleted = %v", store.deletedKeys)
	}
}

func TestSourceImageIllustratorFailsPermanentlyWithoutAnImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer server.Close()
	store := &fakeImageStore{}
	illustrator := illustration.NewSourceImageIllustrator(store, illustration.NewReferenceClientAllowingAnyAddress(), discardLogger())

	for _, url := range []string{"", server.URL + "/page"} {
		_, err := illustrator.Illustrate(context.Background(), publisher.IllustrationRequest{Slug: "s", ReferenceImageURL: url})
		if !errors.Is(err, illustration.ErrNoSourceImage) || !publisher.IsPermanent(err) {
			t.Fatalf("url %q: err = %v", url, err)
		}
	}
	if store.storedSlug != "" {
		t.Fatal("nothing should be stored")
	}
}
