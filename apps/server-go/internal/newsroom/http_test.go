package newsroom_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func passThroughAuth(next http.Handler) http.Handler { return next }

// fakeCollector returns nil, then ErrCollectInProgress, then a generic error,
// on successive calls to Start.
type fakeCollector struct {
	calls int
}

func (f *fakeCollector) Start(context.Context) error {
	f.calls++
	switch f.calls {
	case 1:
		return nil
	case 2:
		return newsroom.ErrCollectInProgress
	default:
		return errors.New("boom")
	}
}

func request2(t *testing.T, handler http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(""))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func TestCollectReportsStartedInProgressAndError(t *testing.T) {
	store, _ := openStore(t)
	service := newService(t, store, t0, sequence(30))
	fake := &fakeCollector{}
	handlerUnderTest := newsroom.NewHandler(service, fake, discardLogger())
	router := chi.NewRouter()
	handlerUnderTest.Mount(router, passThroughAuth)

	started := request2(t, router, http.MethodPost, "/newsroom/collect")
	if started.Code != http.StatusAccepted || started.Body.String() != `{"started":true}` {
		t.Fatalf("first collect = %d %s", started.Code, started.Body.String())
	}
	if contentType := started.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}

	inProgress := request2(t, router, http.MethodPost, "/newsroom/collect")
	if inProgress.Code != http.StatusConflict || inProgress.Body.String() != `{"error":"Collection already running"}` {
		t.Fatalf("second collect = %d %s", inProgress.Code, inProgress.Body.String())
	}

	failed := request2(t, router, http.MethodPost, "/newsroom/collect")
	if failed.Code != http.StatusInternalServerError || failed.Body.String() != `{"error":"Internal server error"}` {
		t.Fatalf("third collect = %d %s", failed.Code, failed.Body.String())
	}
}

func TestListStatusQueryTrailingComma(t *testing.T) {
	store, _ := openStore(t)
	service := newService(t, store, t0, sequence(30))
	handlerUnderTest := newsroom.NewHandler(service, nil, discardLogger())
	router := chi.NewRouter()
	handlerUnderTest.Mount(router, passThroughAuth)

	trailing := request2(t, router, http.MethodGet, "/newsroom/candidates?status=pending,")
	if trailing.Code != http.StatusOK {
		t.Fatalf("status=pending, = %d %s", trailing.Code, trailing.Body.String())
	}

	onlyComma := request2(t, router, http.MethodGet, "/newsroom/candidates?status=,")
	if onlyComma.Code != http.StatusOK {
		t.Fatalf("status=, = %d %s", onlyComma.Code, onlyComma.Body.String())
	}
}
