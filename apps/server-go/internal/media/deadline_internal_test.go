package media

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	read, write time.Time
	err         error
}

func (d *deadlineRecorder) SetReadDeadline(deadline time.Time) error {
	d.read = deadline
	return d.err
}

func (d *deadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	d.write = deadline
	return d.err
}

func TestExtendUploadDeadlinesSetsTwoMinutesToRead(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	recorder := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	if err := extendUploadDeadlines(recorder, now); err != nil {
		t.Fatal(err)
	}
	if want := now.Add(2 * time.Minute); !recorder.read.Equal(want) {
		t.Fatalf("read deadline = %v, want %v", recorder.read, want)
	}
	// The response is written after the body is read, so the write deadline
	// must outlast the read deadline or the 201 could never be sent.
	if want := now.Add(2*time.Minute + 30*time.Second); !recorder.write.Equal(want) {
		t.Fatalf("write deadline = %v, want %v", recorder.write, want)
	}
}

func TestExtendUploadDeadlinesToleratesWritersWithoutDeadlines(t *testing.T) {
	if err := extendUploadDeadlines(httptest.NewRecorder(), time.Now()); err != nil {
		t.Fatalf("unsupported writer error = %v", err)
	}
	failing := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder(), err: errors.New("connection closed")}
	if err := extendUploadDeadlines(failing, time.Now()); err == nil {
		t.Fatal("deadline error was swallowed")
	}
}

var _ http.ResponseWriter = (*deadlineRecorder)(nil)

func TestServeExtendsTheWriteDeadlineForDownloadsOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	uploads := NewUploads(dir)

	before := time.Now()
	recorder := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	uploads.serve(recorder, httptest.NewRequest(http.MethodGet, "/uploads/a.png", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if !recorder.read.IsZero() {
		t.Fatalf("read deadline changed to %v", recorder.read)
	}
	if recorder.write.Before(before.Add(DownloadWriteTimeout)) || recorder.write.After(time.Now().Add(DownloadWriteTimeout)) {
		t.Fatalf("write deadline = %v, want now + %v", recorder.write, DownloadWriteTimeout)
	}
	if DownloadWriteTimeout != 2*time.Minute {
		t.Fatalf("DownloadWriteTimeout = %v", DownloadWriteTimeout)
	}

	missing := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	uploads.serve(missing, httptest.NewRequest(http.MethodGet, "/uploads/missing.png", nil))
	if missing.Code != http.StatusNotFound || !missing.write.IsZero() {
		t.Fatalf("404 = %d, write deadline %v", missing.Code, missing.write)
	}
}
