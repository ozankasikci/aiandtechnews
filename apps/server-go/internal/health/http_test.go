package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestMountPublicHealthContract(t *testing.T) {
	router := chi.NewRouter()
	MountPublic(router)

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json; charset=utf-8")
	}
	if got := response.Body.String(); got != `{"status":"ok"}` {
		t.Errorf("body = %q, want exact no-newline JSON", got)
	}
}

func TestMountPublicIsRelativeToAPIRouter(t *testing.T) {
	router := chi.NewRouter()
	router.Route("/api", MountPublic)

	for _, path := range []string{"/health", "/api/api/health"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, response.Code)
		}
	}
}

func TestMountCheckedAnswersOKWhileTheCheckPasses(t *testing.T) {
	router := chi.NewRouter()
	MountChecked(router, func(context.Context) error { return nil })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	if response.Code != http.StatusOK || response.Body.String() != `{"status":"ok"}` ||
		response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("status = %d, body = %q, content type = %q", response.Code, response.Body.String(), response.Header().Get("Content-Type"))
	}
}

func TestMountCheckedAnswers503WhenTheCheckFails(t *testing.T) {
	router := chi.NewRouter()
	var deadline bool
	MountChecked(router, func(ctx context.Context) error {
		_, deadline = ctx.Deadline()
		return errors.New("database is closed")
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	if response.Code != http.StatusServiceUnavailable || response.Body.String() != `{"status":"error"}` ||
		response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
	if !deadline {
		t.Error("the check ran without a deadline")
	}
}
