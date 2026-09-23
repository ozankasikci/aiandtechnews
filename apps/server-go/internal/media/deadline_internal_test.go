package media

import (
	"errors"
	"net/http"
	"net/http/httptest"
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
