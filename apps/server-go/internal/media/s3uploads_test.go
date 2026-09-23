package media_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
)

// The S3 backend is tested with the fake object API from storage_test.go and
// an in-process TLS server standing in for the public base URL. No AWS.

func newS3Uploads(t *testing.T, contentType string, corrupt bool) (*media.S3Uploads, *fakeObjectAPI, *httptest.Server) {
	t.Helper()
	api := newFakeObjectAPI()
	server := newServingServer(t, api, contentType, corrupt, nil)
	return media.NewS3Uploads(media.S3UploadsConfig{Bucket: "bucket", Prefix: "/uploads/", PublicBaseURL: server.URL + "/"}, api, server.Client()), api, server
}

var s3Key = regexp.MustCompile(`^uploads/[0-9a-f]{32}\.png$`)

func TestS3UploadsSavePutsAPublicImmutableObject(t *testing.T) {
	store, api, server := newS3Uploads(t, "image/png", false)
	file, err := store.Save(context.Background(), strings.NewReader("synthetic image bytes"), "capture.png", "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if len(api.puts) != 1 {
		t.Fatalf("puts = %d", len(api.puts))
	}
	put := api.puts[0]
	sum := sha256.Sum256([]byte("synthetic image bytes"))
	if put.bucket != "bucket" || !s3Key.MatchString(put.key) || put.contentType != "image/png" ||
		put.cacheControl != "public, max-age=31536000, immutable" || put.checksum != base64.StdEncoding.EncodeToString(sum[:]) ||
		string(put.body) != "synthetic image bytes" {
		t.Fatalf("put = %+v", put)
	}
	if file.URL != server.URL+"/"+put.key || file.Name != strings.TrimPrefix(put.key, "uploads/") || file.Size != 21 {
		t.Fatalf("stored = %+v, key %s", file, put.key)
	}
	if !store.Owns(file.URL) || store.Owns("/uploads/"+file.Name) || store.Owns(server.URL+"/features/x.webp") {
		t.Fatal("Owns() does not follow the media prefix")
	}
}

func TestS3UploadsKeepTheExtensionCase(t *testing.T) {
	store, api, _ := newS3Uploads(t, "image/jpeg", false)
	if _, err := store.Save(context.Background(), strings.NewReader("x"), "A.Photo.JPEG", "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(api.puts[0].key, ".JPEG") {
		t.Fatalf("key = %q", api.puts[0].key)
	}
}

func TestS3UploadsRejectFilesThatReachFiveMiBBeforeUploading(t *testing.T) {
	store, api, _ := newS3Uploads(t, "image/png", false)
	if _, err := store.Save(context.Background(), bytes.NewReader(make([]byte, media.MaxUploadBytes)), "big.png", "image/png"); !errors.Is(err, media.ErrFileTooLarge) {
		t.Fatalf("Save() error = %v", err)
	}
	if len(api.puts) != 0 {
		t.Fatalf("puts = %d", len(api.puts))
	}
	if _, err := store.Save(context.Background(), bytes.NewReader(make([]byte, media.MaxUploadBytes-1)), "big.png", "image/png"); err != nil {
		t.Fatalf("5 MiB - 1 byte: %v", err)
	}
}

func TestS3UploadsDeleteTheObjectWhenPublicVerificationFails(t *testing.T) {
	for name, tc := range map[string]struct {
		contentType string
		corrupt     bool
	}{
		"wrong content type": {contentType: "image/webp"},
		"wrong bytes":        {contentType: "image/png", corrupt: true},
	} {
		t.Run(name, func(t *testing.T) {
			store, api, _ := newS3Uploads(t, tc.contentType, tc.corrupt)
			if _, err := store.Save(context.Background(), strings.NewReader("x"), "a.png", "image/png"); err == nil {
				t.Fatal("Save() error = nil")
			}
			if len(api.deletes) != 1 || api.deletes[0].key != api.puts[0].key || !api.deletes[0].deadline {
				t.Fatalf("deletes = %+v", api.deletes)
			}
		})
	}
}

func TestS3UploadsRemoveOnlyKeysInsideTheMediaPrefix(t *testing.T) {
	store, api, server := newS3Uploads(t, "image/png", false)
	if err := store.Remove(context.Background(), server.URL+"/uploads/abc.png"); err != nil {
		t.Fatal(err)
	}
	if len(api.deletes) != 1 || api.deletes[0].key != "uploads/abc.png" || api.deletes[0].bucket != "bucket" {
		t.Fatalf("deletes = %+v", api.deletes)
	}
	for _, url := range []string{
		"/uploads/abc.png",
		server.URL + "/features/2026/09/a.webp",
		server.URL + "/uploads/",
		server.URL + "/uploads/..",
		server.URL + "/uploads/../features/a.webp",
		server.URL + "/uploads/nested/a.png",
		"https://other.example/uploads/abc.png",
	} {
		if err := store.Remove(context.Background(), url); err == nil {
			t.Errorf("Remove(%q) error = nil", url)
		}
	}
	if len(api.deletes) != 1 {
		t.Fatalf("unexpected deletes = %+v", api.deletes)
	}
}

func TestNewStorageRoutesByBackendAndURLShape(t *testing.T) {
	_, dir := uploadsDir(t)
	local := media.NewUploads(dir)
	if media.NewStorage(local, nil) != media.Storage(local) {
		t.Fatal("local-only storage is not the local backend")
	}

	s3, api, server := newS3Uploads(t, "image/png", false)
	storage := media.NewStorage(local, s3)
	file, err := storage.Save(context.Background(), strings.NewReader("x"), "a.png", "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(file.URL, server.URL+"/uploads/") || len(dirNames(t, dir)) != 0 {
		t.Fatalf("new upload went to %q (local files %v)", file.URL, dirNames(t, dir))
	}
	// Rows created before the switch keep deleting from UPLOADS_DIR.
	if err := os.WriteFile(filepath.Join(dir, "legacy.png"), []byte("legacy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := storage.Remove(context.Background(), "/uploads/legacy.png"); err != nil {
		t.Fatal(err)
	}
	if len(dirNames(t, dir)) != 0 || len(api.deletes) != 0 {
		t.Fatalf("legacy delete: local %v, s3 deletes %+v", dirNames(t, dir), api.deletes)
	}
	if err := storage.Remove(context.Background(), file.URL); err != nil {
		t.Fatal(err)
	}
	if len(api.deletes) != 1 || api.deletes[0].key != "uploads/"+file.Name {
		t.Fatalf("s3 deletes = %+v", api.deletes)
	}
}

var _ media.Storage = (*media.S3Uploads)(nil)

func TestS3UploadsBoundPutObjectWithItsOwnTimeout(t *testing.T) {
	store, api, _ := newS3Uploads(t, "image/png", false)
	if _, err := store.Save(context.Background(), strings.NewReader("x"), "a.png", "image/png"); err != nil {
		t.Fatal(err)
	}
	assertPutTimeout(t, api.puts[0])
}
