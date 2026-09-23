package media_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
)

// recordingStorage saves nothing to disk and records the context each Remove
// ran with. onSave runs inside Save (used to cancel the request mid-upload).
type recordingStorage struct {
	onSave  func()
	removes []removeCall
}

type removeCall struct {
	url         string
	ctxErr      error
	hasDeadline bool
}

func (s *recordingStorage) Save(_ context.Context, content io.Reader, _ string, _ string) (media.StoredFile, error) {
	size, err := io.Copy(io.Discard, content)
	if s.onSave != nil {
		s.onSave()
	}
	return media.StoredFile{Name: "saved.png", Size: size, URL: "/uploads/saved.png"}, err
}

func (s *recordingStorage) Remove(ctx context.Context, url string) error {
	_, hasDeadline := ctx.Deadline()
	s.removes = append(s.removes, removeCall{url: url, ctxErr: ctx.Err(), hasDeadline: hasDeadline})
	return nil
}

func assertDetachedCleanup(t *testing.T, removes []removeCall) {
	t.Helper()
	if len(removes) != 1 || removes[0].url != "/uploads/saved.png" {
		t.Fatalf("removes = %+v", removes)
	}
	if removes[0].ctxErr != nil || !removes[0].hasDeadline {
		t.Fatalf("cleanup ran with ctx error %v, deadline %v; want a live, bounded context", removes[0].ctxErr, removes[0].hasDeadline)
	}
}

type failingInsert struct{ media.SQLiteLibrary }

func (failingInsert) List(context.Context) ([]media.Item, error) { return nil, nil }
func (failingInsert) Get(context.Context, string) (media.Item, error) {
	return media.Item{}, media.ErrMediaNotFound
}
func (failingInsert) Delete(context.Context, string) error { return nil }
func (failingInsert) Insert(context.Context, media.NewItem) (media.Item, error) {
	return media.Item{}, errors.New("database is locked")
}

func TestRecordCleanupSurvivesACanceledRequest(t *testing.T) {
	storage := &recordingStorage{}
	library := media.NewLibrary(failingInsert{}, storage, time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := library.Record(ctx, media.StoredFile{Name: "saved.png", URL: "/uploads/saved.png"}, "a.png", "image/png"); err == nil {
		t.Fatal("Record() error = nil")
	}
	assertDetachedCleanup(t, storage.removes)
}

func TestUploadCleanupSurvivesACanceledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	storage := &recordingStorage{onSave: cancel}
	library := media.NewLibrary(failingInsert{}, storage, time.Now)
	router := chi.NewRouter()
	media.NewHandler(library, storage, slog.New(slog.NewTextHandler(io.Discard, nil))).Mount(router)
	body := multipartBody(
		part{name: "file", filename: "a.png", contentType: "image/png", content: "x"},
		part{name: "other", filename: "b.png", contentType: "image/png", content: "y"},
	)
	request := httptest.NewRequest(http.MethodPost, "/media/upload", bytes.NewReader(body)).WithContext(ctx)
	request.Header.Set("Content-Type", "multipart/form-data; boundary=B")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
	assertDetachedCleanup(t, storage.removes)
}
