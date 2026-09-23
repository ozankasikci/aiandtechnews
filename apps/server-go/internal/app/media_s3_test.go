package app_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/contracttest"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

// memoryBucket is an in-memory S3 stand-in; bucketServer serves its objects
// the way the public base URL would. No AWS, no network beyond loopback.
type memoryBucket struct {
	mu      sync.Mutex
	objects map[string][]byte
	types   map[string]string
	deleted []string
}

func (b *memoryBucket) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	body, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.objects[*input.Key] = body
	b.types[*input.Key] = *input.ContentType
	return &s3.PutObjectOutput{}, nil
}

func (b *memoryBucket) DeleteObject(_ context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.objects, *input.Key)
	b.deleted = append(b.deleted, *input.Key)
	return &s3.DeleteObjectOutput{}, nil
}

func s3MediaApplication(t *testing.T) (http.Handler, *sql.DB, string, *memoryBucket, string) {
	t.Helper()
	bucket := &memoryBucket{objects: map[string][]byte{}, types: map[string]string{}}
	public := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bucket.mu.Lock()
		body, ok := bucket.objects[strings.TrimPrefix(r.URL.Path, "/")]
		contentType := bucket.types[strings.TrimPrefix(r.URL.Path, "/")]
		bucket.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(body)
	}))
	t.Cleanup(public.Close)
	app.StubObjectStorageForTest(t, func(context.Context, config.Config) (media.ObjectAPI, error) { return bucket, nil }, public.Client())

	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	seedContractArticles(t, db)
	uploads := t.TempDir()
	seedContractMedia(t, db, uploads)
	cfg := config.Config{
		Mode: config.ModeDevelopment, Address: "127.0.0.1:4402", DatabasePath: filepath.Join(t.TempDir(), "unused.db"),
		JWTSecret: authTestSecret, UploadsDir: uploads,
		MediaStorage: config.MediaStorageS3, MediaS3Prefix: "uploads",
		AWSRegion: "eu-west-1", S3Bucket: "bucket", S3Prefix: "features", S3PublicURL: public.URL,
	}
	fixed := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	application, err := app.NewWithDatabaseAt(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db, func() time.Time { return fixed })
	if err != nil {
		t.Fatal(err)
	}
	return application.Handler(), db, uploads, bucket, public.URL
}

func authorized(t *testing.T, handler http.Handler) string {
	t.Helper()
	contract, err := contracttest.Load(filepath.Join("..", "..", "contracts", "fixtures", "node-contracts.json"))
	if err != nil {
		t.Fatal(err)
	}
	return contractBindings(t, handler, contract)["$AUTHORIZATION"]
}

func serve(handler http.Handler, method, target, authorization, contentType, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestS3MediaStorageUploadsNewFilesAndKeepsLegacyUploadsWorking(t *testing.T) {
	handler, db, uploads, bucket, publicURL := s3MediaApplication(t)
	authorization := authorized(t, handler)

	body := "--B\r\nContent-Disposition: form-data; name=\"file\"; filename=\"photo.webp\"\r\nContent-Type: image/webp\r\n\r\nwebp bytes\r\n--B--\r\n"
	response := serve(handler, http.MethodPost, "/api/dashboard/media/upload", authorization, "multipart/form-data; boundary=B", body)
	if response.Code != http.StatusCreated {
		t.Fatalf("upload = %d %s", response.Code, response.Body.String())
	}
	var uploaded struct {
		Media media.Item `json:"media"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	key := "uploads/" + uploaded.Media.URL[len(publicURL+"/uploads/"):]
	if !strings.HasPrefix(uploaded.Media.URL, publicURL+"/uploads/") || !storedS3Name.MatchString(key) ||
		string(bucket.objects[key]) != "webp bytes" || bucket.types[key] != "image/webp" ||
		uploaded.Media.Filename != "photo.webp" || uploaded.Media.Size != 10 || uploaded.Media.UploadedAt != "2026-09-20 12:00:00" {
		t.Fatalf("uploaded = %+v, bucket keys = %v", uploaded.Media, bucket.objects)
	}
	if entries, _ := os.ReadDir(uploads); len(entries) != 1 {
		t.Fatalf("local uploads = %v, want only the legacy file", entries)
	}

	// The legacy Node file keeps serving from UPLOADS_DIR.
	legacy := serve(handler, http.MethodGet, "/uploads/synthetic-contract-image.png", "", "", "")
	if legacy.Code != http.StatusOK || legacy.Body.String() != "synthetic contract media\n" {
		t.Fatalf("legacy GET = %d %q", legacy.Code, legacy.Body.String())
	}

	// Deletes are routed by URL shape: the S3 row deletes its object ...
	if response := serve(handler, http.MethodDelete, fmt.Sprintf("/api/dashboard/media/%d", uploaded.Media.ID), authorization, "", ""); response.Code != http.StatusOK {
		t.Fatalf("delete S3 row = %d %s", response.Code, response.Body.String())
	}
	if len(bucket.deleted) != 1 || bucket.deleted[0] != key {
		t.Fatalf("deleted keys = %v", bucket.deleted)
	}
	// ... and the legacy row deletes its local file.
	if response := serve(handler, http.MethodDelete, "/api/dashboard/media/401", authorization, "", ""); response.Code != http.StatusOK {
		t.Fatalf("delete legacy row = %d %s", response.Code, response.Body.String())
	}
	if entries, _ := os.ReadDir(uploads); len(entries) != 0 || len(bucket.deleted) != 1 {
		t.Fatalf("after legacy delete: local %v, s3 deletes %v", entries, bucket.deleted)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM media`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("media rows = %d, %v", rows, err)
	}
}

var storedS3Name = regexp.MustCompile(`^uploads/[0-9a-f]{32}\.webp$`)
