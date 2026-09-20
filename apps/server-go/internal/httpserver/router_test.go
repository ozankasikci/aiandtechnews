package httpserver

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

func TestRouterHTTPFoundation(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	router := NewRouter(logger, func(api chi.Router) {
		api.Get("/echo-id", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write([]byte(`{"requestId":"` + chimiddleware.GetReqID(r.Context()) + `"}`))
		})
		api.Get("/panic", func(http.ResponseWriter, *http.Request) { panic("secret panic") })
		api.Get("/only-get", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	})

	t.Run("request ID is accepted, propagated, and logged", func(t *testing.T) {
		logs.Reset()
		request := httptest.NewRequest(http.MethodGet, "/api/echo-id", nil)
		request.Header.Set("X-Request-ID", "client-request_123")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		if got := response.Header().Get("X-Request-ID"); got != "client-request_123" {
			t.Fatalf("X-Request-ID = %q", got)
		}
		if got := response.Body.String(); got != `{"requestId":"client-request_123"}` {
			t.Errorf("body = %q", got)
		}
		var entry map[string]any
		if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
			t.Fatalf("access log is not JSON: %v (%q)", err, logs.String())
		}
		if entry["request_id"] != "client-request_123" || entry["method"] != http.MethodGet || entry["path"] != "/api/echo-id" || entry["status"] != float64(http.StatusOK) {
			t.Errorf("access log fields = %#v", entry)
		}
		if _, exists := entry["authorization"]; exists {
			t.Error("access log must not include authorization")
		}
	})

	t.Run("unsafe and oversized request IDs are replaced", func(t *testing.T) {
		for _, id := range []string{"spaces are unsafe", strings.Repeat("a", MaxRequestIDLength+1)} {
			request := httptest.NewRequest(http.MethodGet, "/api/echo-id", nil)
			request.Header.Set("X-Request-ID", id)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			got := response.Header().Get("X-Request-ID")
			if got == "" || got == id || len(got) > MaxRequestIDLength {
				t.Errorf("replacement request ID = %q for unsafe input", got)
			}
		}
	})

	t.Run("not found is stable JSON", func(t *testing.T) {
		assertJSONError(t, router, http.MethodGet, "/api/missing", http.StatusNotFound, `{"error":"Not found"}`)
	})

	t.Run("method not allowed is stable JSON and sets Allow", func(t *testing.T) {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/only-get", nil))
		if response.Code != http.StatusMethodNotAllowed || response.Body.String() != `{"error":"Method not allowed"}` {
			t.Fatalf("response = %d %q", response.Code, response.Body.String())
		}
		if !strings.Contains(response.Header().Get("Allow"), http.MethodGet) {
			t.Errorf("Allow = %q, want GET", response.Header().Get("Allow"))
		}
	})

	t.Run("panic recovery does not leak panic", func(t *testing.T) {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/panic", nil))
		if response.Code != http.StatusInternalServerError || response.Body.String() != `{"error":"Internal server error"}` {
			t.Fatalf("response = %d %q", response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "secret") {
			t.Error("panic response leaked panic value")
		}
	})

	t.Run("wildcard CORS simple request", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/echo-id", nil)
		request.Header.Set("Origin", "https://example.test")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if got := response.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("Access-Control-Allow-Origin = %q, want *", got)
		}
	})

	t.Run("CORS preflight is Express-compatible", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodOptions, "/api/echo-id", nil)
		request.Header.Set("Origin", "https://example.test")
		request.Header.Set("Access-Control-Request-Method", http.MethodGet)
		request.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Errorf("status = %d, want 204", response.Code)
		}
		if got := response.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("Access-Control-Allow-Origin = %q", got)
		}
		if got := strings.ToLower(response.Header().Get("Access-Control-Allow-Headers")); !strings.Contains(got, "authorization") || !strings.Contains(got, "content-type") {
			t.Errorf("Access-Control-Allow-Headers = %q", got)
		}
	})
}

func assertJSONError(t *testing.T, handler http.Handler, method, path string, status int, body string) {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(method, path, nil))
	if response.Code != status || response.Body.String() != body {
		t.Fatalf("response = %d %q, want %d %q", response.Code, response.Body.String(), status, body)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
}
