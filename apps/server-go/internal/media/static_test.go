package media_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
)

// staticRouter mounts the uploads directory the way internal/app does.
func staticRouter(t *testing.T) (http.Handler, string, string) {
	t.Helper()
	parent, dir := uploadsDir(t)
	router := chi.NewRouter()
	media.NewUploads(dir).MountStatic(router)
	return router, parent, dir
}

func writeUpload(t *testing.T, path, content string, modified time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
}

func serveStatic(handler http.Handler, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

var modified = time.Date(2026, 9, 23, 19, 43, 31, 425_000_000, time.UTC)

func TestStaticServesUploadsWithSendHeaders(t *testing.T) {
	handler, _, dir := staticRouter(t)
	writeUpload(t, filepath.Join(dir, "84752918b034c4faf917817b751fc0e8.png"), "synthetic image bytes", modified)

	response := serveStatic(handler, http.MethodGet, "/uploads/84752918b034c4faf917817b751fc0e8.png?v=1", nil)
	want := map[string]string{
		"Content-Type":   "image/png",
		"Content-Length": "21",
		"Cache-Control":  "public, max-age=0",
		"Accept-Ranges":  "bytes",
		"Last-Modified":  "Wed, 23 Sep 2026 19:43:31 GMT",
		"ETag":           fmt.Sprintf(`W/"15-%x"`, modified.UnixMilli()),
	}
	if response.Code != http.StatusOK || response.Body.String() != "synthetic image bytes" {
		t.Fatalf("GET = %d %q", response.Code, response.Body.String())
	}
	for name, value := range want {
		if got := response.Header().Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}

	head := serveStatic(handler, http.MethodHead, "/uploads/84752918b034c4faf917817b751fc0e8.png", nil)
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") != "21" || head.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("HEAD = %d %q %v", head.Code, head.Body.String(), head.Header())
	}
}

func TestStaticSupportsRangesAndConditionalRequests(t *testing.T) {
	handler, _, dir := staticRouter(t)
	writeUpload(t, filepath.Join(dir, "a.png"), "synthetic image bytes", modified)
	etag := fmt.Sprintf(`W/"15-%x"`, modified.UnixMilli())

	partial := serveStatic(handler, http.MethodGet, "/uploads/a.png", map[string]string{"Range": "bytes=0-3"})
	if partial.Code != http.StatusPartialContent || partial.Body.String() != "synt" || partial.Header().Get("Content-Range") != "bytes 0-3/21" {
		t.Fatalf("range = %d %q %q", partial.Code, partial.Body.String(), partial.Header().Get("Content-Range"))
	}
	for name, headers := range map[string]map[string]string{
		"If-None-Match":     {"If-None-Match": etag},
		"If-Modified-Since": {"If-Modified-Since": "Wed, 23 Sep 2026 19:43:31 GMT"},
	} {
		if response := serveStatic(handler, http.MethodGet, "/uploads/a.png", headers); response.Code != http.StatusNotModified || response.Body.Len() != 0 {
			t.Errorf("%s = %d %q", name, response.Code, response.Body.String())
		}
	}
}

func TestStaticContentTypeFollowsTheStoredExtensionLikeNode(t *testing.T) {
	handler, _, dir := staticRouter(t)
	for name, want := range map[string]string{
		"098e437b380ffed8f4eaf9316dfc6e90.JPEG": "image/jpeg",
		"cccff1c03913c356183d1ff7cdf1fd41":      "application/octet-stream",
		"73b54898d1a80c7dc91d53b727ef197d.html": "text/html; charset=UTF-8",
		"6316bd9688ed22436fa3dfc2d6209ceb.":     "application/octet-stream",
		"a.webp":                                "image/webp",
		"a.gif":                                 "image/gif",
	} {
		writeUpload(t, filepath.Join(dir, name), "x", modified)
		if got := serveStatic(handler, http.MethodGet, "/uploads/"+name, nil).Header().Get("Content-Type"); got != want {
			t.Errorf("%s Content-Type = %q, want %q", name, got, want)
		}
	}
}

func TestStaticServesNestedFilesAndEncodedSlashesLikeServeStatic(t *testing.T) {
	handler, _, dir := staticRouter(t)
	writeUpload(t, filepath.Join(dir, "sub", "n.png"), "nested", modified)
	for _, target := range []string{"/uploads/sub/n.png", "/uploads/sub%2fn.png"} {
		if response := serveStatic(handler, http.MethodGet, target, nil); response.Code != http.StatusOK || response.Body.String() != "nested" {
			t.Errorf("%s = %d %q", target, response.Code, response.Body.String())
		}
	}
}

func TestStaticNeverServesOutsideTheUploadsDirectory(t *testing.T) {
	handler, parent, dir := staticRouter(t)
	writeUpload(t, filepath.Join(parent, "secret.txt"), "secret", modified)
	writeUpload(t, filepath.Join(dir, ".hidden"), "hidden", modified)
	writeUpload(t, filepath.Join(dir, "sub", ".env"), "hidden", modified)
	if err := os.Mkdir(filepath.Join(dir, "emptydir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(parent, "secret.txt"), filepath.Join(dir, "link.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := os.Symlink(parent, filepath.Join(dir, "parentlink")); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{
		"/uploads/missing.png",
		"/uploads/",
		"/uploads/sub",
		"/uploads/sub/",
		"/uploads/emptydir",
		"/uploads/.hidden",
		"/uploads/sub/.env",
		"/uploads/../secret.txt",
		"/uploads/%2e%2e/secret.txt",
		"/uploads/..%2fsecret.txt",
		"/uploads/sub/../../secret.txt",
		"/uploads/./a.png",
		"/uploads//a.png",
		"/uploads/a%00.png",
		"/uploads/link.txt",
		"/uploads/parentlink/secret.txt",
		"/uploads/" + filepath.Join(parent, "secret.txt"),
	} {
		response := serveStatic(handler, http.MethodGet, target, nil)
		if response.Code != http.StatusNotFound || response.Body.String() != `{"error":"Not found"}` ||
			response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
			t.Errorf("GET %s = %d %q", target, response.Code, response.Body.String())
		}
	}
}

func TestStaticAnswersNotFoundWhenTheUploadsDirectoryIsMissing(t *testing.T) {
	router := chi.NewRouter()
	media.NewUploads(filepath.Join(t.TempDir(), "missing")).MountStatic(router)
	if response := serveStatic(router, http.MethodGet, "/uploads/a.png", nil); response.Code != http.StatusNotFound {
		t.Fatalf("GET = %d", response.Code)
	}
}
