package media_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
)

// webPBytes is a minimal buffer that sniffs as image/webp (RIFF....WEBP header).
func webPBytes(payload string) []byte {
	body := []byte(payload)
	riff := make([]byte, 0, 12+len(body))
	riff = append(riff, []byte("RIFF")...)
	riff = append(riff, 0, 0, 0, 0)
	riff = append(riff, []byte("WEBP")...)
	riff = append(riff, body...)
	return riff
}

type putCall struct {
	bucket, key, contentType, cacheControl, checksum string
	body                                             []byte
}

type deleteCall struct {
	bucket, key string
	deadline    bool
}

type fakeObjectAPI struct {
	mu      sync.Mutex
	objects map[string][]byte
	puts    []putCall
	deletes []deleteCall
}

func newFakeObjectAPI() *fakeObjectAPI {
	return &fakeObjectAPI{objects: map[string][]byte{}}
}

func (f *fakeObjectAPI) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	body, _ := io.ReadAll(input.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[*input.Key] = body
	f.puts = append(f.puts, putCall{
		bucket: *input.Bucket, key: *input.Key,
		contentType: strOrEmpty(input.ContentType), cacheControl: strOrEmpty(input.CacheControl),
		checksum: strOrEmpty(input.ChecksumSHA256), body: body,
	})
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeObjectAPI) DeleteObject(ctx context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, *input.Key)
	_, deadline := ctx.Deadline()
	f.deletes = append(f.deletes, deleteCall{bucket: *input.Bucket, key: *input.Key, deadline: deadline})
	return &s3.DeleteObjectOutput{}, nil
}

func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// newServingServer serves the fake API's stored objects over TLS, mimicking
// the public base URL. It returns the server; the test's config PublicBaseURL
// must be server.URL.
func newServingServer(t *testing.T, api *fakeObjectAPI, contentType string, corrupt bool, notFoundPaths map[string]bool) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path[1:]
		if notFoundPaths[key] {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		api.mu.Lock()
		body, ok := api.objects[key]
		api.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		} else {
			w.Header().Set("Content-Type", "image/webp")
		}
		if corrupt {
			_, _ = w.Write([]byte("not the right bytes at all"))
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server
}

func fixedNow(t time.Time) func() time.Time { return func() time.Time { return t } }

func TestStoreWebPKeyLayoutAndUpload(t *testing.T) {
	api := newFakeObjectAPI()
	server := newServingServer(t, api, "", false, nil)
	config := media.Config{Region: "eu-west-1", Bucket: "bucket", Prefix: "dev", PublicBaseURL: server.URL}
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	store := media.NewStore(config, api, server.Client(), fixedNow(now))

	data := webPBytes("hello world")
	stored, err := store.StoreWebP(context.Background(), "Hello, World!", data)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := "dev/2026/09/hello-world-"
	if len(stored.Key) < len(wantPrefix) || stored.Key[:len(wantPrefix)] != wantPrefix || stored.Key[len(stored.Key)-5:] != ".webp" {
		t.Fatalf("key = %q", stored.Key)
	}
	if stored.URL != server.URL+"/"+stored.Key {
		t.Fatalf("url = %q", stored.URL)
	}
	if len(api.puts) != 1 {
		t.Fatalf("puts = %d", len(api.puts))
	}
	put := api.puts[0]
	if put.contentType != "image/webp" || put.cacheControl != "public, max-age=31536000, immutable" || put.checksum == "" {
		t.Fatalf("put = %+v", put)
	}
	if !bytes.Equal(put.body, data) {
		t.Fatal("uploaded body mismatch")
	}
}

func TestStoreWebPVerificationFailureDeletesObject(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		corrupt     bool
		notFound    bool
	}{
		{name: "wrong content type", contentType: "image/png"},
		{name: "wrong bytes", corrupt: true},
		{name: "404", notFound: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := newFakeObjectAPI()
			notFoundPaths := map[string]bool{}
			var server *httptest.Server
			if tc.notFound {
				// Build the server first is tricky since key depends on now; use a
				// server that 404s everything.
				server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
				t.Cleanup(server.Close)
			} else {
				server = newServingServer(t, api, tc.contentType, tc.corrupt, notFoundPaths)
			}
			config := media.Config{Region: "eu-west-1", Bucket: "bucket", Prefix: "dev", PublicBaseURL: server.URL}
			store := media.NewStore(config, api, server.Client(), fixedNow(time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)))

			_, err := store.StoreWebP(context.Background(), "slug", webPBytes("payload"))
			if err == nil {
				t.Fatal("expected verification failure")
			}
			if len(api.puts) != 1 {
				t.Fatalf("puts = %d", len(api.puts))
			}
			if len(api.deletes) != 1 || api.deletes[0].key != api.puts[0].key {
				t.Fatalf("deletes = %+v puts = %+v", api.deletes, api.puts)
			}
			if !api.deletes[0].deadline {
				t.Fatal("cleanup delete ran without a timeout")
			}
			var statusErr *media.PublicStatusError
			if gotStatus := errors.As(err, &statusErr); gotStatus != tc.notFound || (tc.notFound && statusErr.Status != http.StatusNotFound) {
				t.Fatalf("err = %v, want PublicStatusError only for the 404 case", err)
			}
		})
	}
}

func TestStoreWebPRejectsNonWebPBeforeUpload(t *testing.T) {
	api := newFakeObjectAPI()
	config := media.Config{Region: "eu-west-1", Bucket: "bucket", Prefix: "dev", PublicBaseURL: "https://example.test"}
	store := media.NewStore(config, api, http.DefaultClient, fixedNow(time.Now()))
	_, err := store.StoreWebP(context.Background(), "slug", []byte("not webp"))
	if err == nil {
		t.Fatal("expected rejection")
	}
	if len(api.puts) != 0 {
		t.Fatalf("puts = %d, want 0", len(api.puts))
	}
}

func TestDeleteRefusesKeysOutsidePrefix(t *testing.T) {
	api := newFakeObjectAPI()
	config := media.Config{Region: "eu-west-1", Bucket: "bucket", Prefix: "dev", PublicBaseURL: "https://example.test"}
	store := media.NewStore(config, api, http.DefaultClient, fixedNow(time.Now()))
	if err := store.Delete(context.Background(), "other/key"); err == nil {
		t.Fatal("expected refusal for other/key")
	}
	if err := store.Delete(context.Background(), "dev/../x"); err == nil {
		t.Fatal("expected refusal for dev/../x")
	}
	if len(api.deletes) != 0 {
		t.Fatalf("deletes = %d, want 0", len(api.deletes))
	}
}

func TestConfigValidate(t *testing.T) {
	empty := media.Config{}
	err := empty.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"AWS_REGION", "S3_FEATURE_IMAGE_BUCKET", "S3_FEATURE_IMAGE_PUBLIC_URL"} {
		if !bytes.Contains([]byte(err.Error()), []byte(want)) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
	httpOnly := media.Config{Region: "eu-west-1", Bucket: "b", Prefix: "features", PublicBaseURL: "http://example.test"}
	if err := httpOnly.Validate(); err == nil {
		t.Fatal("expected https-only error")
	}
	ok := media.Config{Region: "eu-west-1", Bucket: "b", Prefix: "features", PublicBaseURL: "https://example.test"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSanitizeSlug(t *testing.T) {
	got, err := media.SanitizeSlug("Hello, World!")
	if err != nil || got != "hello-world" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if _, err := media.SanitizeSlug("!!!"); err == nil {
		t.Fatal("expected error for empty slug")
	}
}

func TestConfigValidateRejectsUnsafePrefixAndPublicURL(t *testing.T) {
	base := media.Config{Region: "eu-west-1", Bucket: "b", Prefix: "features", PublicBaseURL: "https://example.test"}
	for _, prefix := range []string{"/", "//", "../x", "a/../b", "a/.."} {
		c := base
		c.Prefix = prefix
		if err := c.Validate(); err == nil {
			t.Errorf("prefix %q accepted", prefix)
		}
	}
	for _, publicURL := range []string{"https://", "https:///path", "ftp://example.test", "https://exa mple.test", "HTTP://example.test"} {
		c := base
		c.PublicBaseURL = publicURL
		if err := c.Validate(); err == nil {
			t.Errorf("public URL %q accepted", publicURL)
		}
	}
	for _, prefix := range []string{"/features/", "a/b"} {
		c := base
		c.Prefix = prefix
		if err := c.Validate(); err != nil {
			t.Errorf("prefix %q rejected: %v", prefix, err)
		}
	}
}
