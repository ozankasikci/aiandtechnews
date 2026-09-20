package httpserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"slices"
	"strings"
	"testing"
	"time"

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

	t.Run("generated request ID matches context, response, and access log", func(t *testing.T) {
		logs.Reset()
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/echo-id", nil))

		id := response.Header().Get("X-Request-ID")
		if id == "" {
			t.Fatal("generated X-Request-ID is empty")
		}
		if got := response.Body.String(); got != `{"requestId":"`+id+`"}` {
			t.Errorf("body = %q, want generated response ID %q", got, id)
		}
		entries := decodeJSONLogs(t, logs.Bytes())
		if len(entries) != 1 || entries[0]["request_id"] != id {
			t.Errorf("access logs = %#v, want generated request ID %q", entries, id)
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

	t.Run("not found is stable JSON inside and outside API", func(t *testing.T) {
		assertJSONError(t, router, http.MethodGet, "/api/missing", http.StatusNotFound, `{"error":"Not found"}`)
		assertJSONError(t, router, http.MethodGet, "/health", http.StatusNotFound, `{"error":"Not found"}`)
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

	t.Run("panic recovery logs request ID and access status without leaking panic", func(t *testing.T) {
		logs.Reset()
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/panic", nil))
		if response.Code != http.StatusInternalServerError || response.Body.String() != `{"error":"Internal server error"}` {
			t.Fatalf("response = %d %q", response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "secret") {
			t.Error("panic response leaked panic value")
		}
		id := response.Header().Get("X-Request-ID")
		entries := decodeJSONLogs(t, logs.Bytes())
		if len(entries) != 2 {
			t.Fatalf("log entries = %#v, want panic and access entries", entries)
		}
		if entries[0]["msg"] != "http request panic" || entries[0]["request_id"] != id {
			t.Errorf("panic log = %#v, want request ID %q", entries[0], id)
		}
		if entries[1]["msg"] != "http request" || entries[1]["request_id"] != id || entries[1]["status"] != float64(http.StatusInternalServerError) {
			t.Errorf("access log = %#v, want request ID %q and status 500", entries[1], id)
		}
	})

	t.Run("wildcard CORS simple responses with and without Origin", func(t *testing.T) {
		for _, origin := range []string{"", "https://example.test"} {
			request := httptest.NewRequest(http.MethodGet, "/api/echo-id", nil)
			if origin != "" {
				request.Header.Set("Origin", origin)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if got := response.Header().Get("Access-Control-Allow-Origin"); got != "*" {
				t.Errorf("origin %q: Access-Control-Allow-Origin = %q, want *", origin, got)
			}
			if got := response.Header().Values("Vary"); len(got) != 0 {
				t.Errorf("origin %q: Vary = %q, want absent on simple response", origin, got)
			}
		}
	})

	t.Run("CORS preflight is exactly Express-compatible when headers are requested", func(t *testing.T) {
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
		if got := response.Header().Get("Access-Control-Allow-Methods"); got != "GET,HEAD,PUT,PATCH,POST,DELETE" {
			t.Errorf("Access-Control-Allow-Methods = %q", got)
		}
		if got := response.Header().Get("Access-Control-Allow-Headers"); got != "Authorization, Content-Type" {
			t.Errorf("Access-Control-Allow-Headers = %q, want exact request value", got)
		}
		if got := response.Header().Values("Vary"); len(got) != 1 || got[0] != "Access-Control-Request-Headers" {
			t.Errorf("Vary = %q, want only Access-Control-Request-Headers", got)
		}
		if got := response.Body.String(); got != "" {
			t.Errorf("preflight body = %q, want empty", got)
		}
		if got := response.Result().ContentLength; got != 0 {
			t.Errorf("Content-Length = %d, want 0", got)
		}
	})

	t.Run("CORS preflight without requested headers still varies on request headers", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodOptions, "/api/echo-id", nil)
		request.Header.Set("Origin", "https://example.test")
		request.Header.Set("Access-Control-Request-Method", http.MethodGet)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Errorf("status = %d, want 204", response.Code)
		}
		if got := response.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("Access-Control-Allow-Origin = %q", got)
		}
		if got := response.Header().Get("Access-Control-Allow-Methods"); got != "GET,HEAD,PUT,PATCH,POST,DELETE" {
			t.Errorf("Access-Control-Allow-Methods = %q", got)
		}
		if got := response.Header().Values("Access-Control-Allow-Headers"); len(got) != 0 {
			t.Errorf("Access-Control-Allow-Headers = %q, want absent", got)
		}
		if got := response.Header().Values("Vary"); len(got) != 1 || got[0] != "Access-Control-Request-Headers" {
			t.Errorf("Vary = %q, want only Access-Control-Request-Headers", got)
		}
		if got := response.Body.String(); got != "" {
			t.Errorf("preflight body = %q, want empty", got)
		}
		if got := response.Result().ContentLength; got != 0 {
			t.Errorf("Content-Length = %d, want 0", got)
		}
	})

	t.Run("plain OPTIONS merges the request-header Vary field", func(t *testing.T) {
		response := httptest.NewRecorder()
		response.Header().Set("Vary", "Accept-Encoding")
		expressCORS(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Error("OPTIONS request reached next handler")
		})).ServeHTTP(response, httptest.NewRequest(http.MethodOptions, "/", nil))

		if got := response.Header().Values("Vary"); len(got) != 1 || got[0] != "Accept-Encoding, Access-Control-Request-Headers" {
			t.Errorf("Vary = %q, want merged Express-compatible value", got)
		}
	})

	t.Run("plain OPTIONS short-circuits exactly like Express CORS", func(t *testing.T) {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodOptions, "/api/echo-id", nil))

		if response.Code != http.StatusNoContent {
			t.Errorf("status = %d, want 204", response.Code)
		}
		if got := response.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("Access-Control-Allow-Origin = %q", got)
		}
		if got := response.Header().Get("Access-Control-Allow-Methods"); got != "GET,HEAD,PUT,PATCH,POST,DELETE" {
			t.Errorf("Access-Control-Allow-Methods = %q", got)
		}
		if got := response.Header().Values("Access-Control-Allow-Headers"); len(got) != 0 {
			t.Errorf("Access-Control-Allow-Headers = %q, want absent", got)
		}
		if got := response.Header().Values("Vary"); len(got) != 1 || got[0] != "Access-Control-Request-Headers" {
			t.Errorf("Vary = %q, want only Access-Control-Request-Headers", got)
		}
		if got := response.Body.String(); got != "" {
			t.Errorf("OPTIONS body = %q, want empty", got)
		}
		if got := response.Result().ContentLength; got != 0 {
			t.Errorf("Content-Length = %d, want 0", got)
		}
	})
}

func TestOptionsWireResponseOmitsContentLength(t *testing.T) {
	router := NewRouter(discardLogger(), func(api chi.Router) {
		api.Get("/echo-id", func(http.ResponseWriter, *http.Request) {})
	})
	listener := localListener(t)
	server := &http.Server{Handler: router}
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(listener) }()
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
		select {
		case err := <-serveResult:
			if err != nil && err != http.ErrServerClosed {
				t.Errorf("Serve() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Error("Serve() did not return after Close")
		}
	})

	connection, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetDeadline() error = %v", err)
	}
	_, err = io.WriteString(connection, "OPTIONS /api/echo-id HTTP/1.1\r\n"+
		"Host: example.test\r\n"+
		"Origin: https://example.test\r\n"+
		"Access-Control-Request-Method: GET\r\n"+
		"Access-Control-Request-Headers: Authorization, Content-Type\r\n"+
		"Connection: close\r\n\r\n")
	if err != nil {
		t.Fatalf("write OPTIONS request error = %v", err)
	}

	buffered := bufio.NewReader(connection)
	wireResponse := textproto.NewReader(buffered)
	statusLine, err := wireResponse.ReadLine()
	if err != nil {
		t.Fatalf("read status line error = %v", err)
	}
	if statusLine != "HTTP/1.1 204 No Content" {
		t.Errorf("status line = %q, want HTTP/1.1 204 No Content", statusLine)
	}
	headers, err := wireResponse.ReadMIMEHeader()
	if err != nil {
		t.Fatalf("read response headers error = %v", err)
	}
	wantHeaders := map[string][]string{
		"Access-Control-Allow-Origin":  {"*"},
		"Access-Control-Allow-Methods": {"GET,HEAD,PUT,PATCH,POST,DELETE"},
		"Access-Control-Allow-Headers": {"Authorization, Content-Type"},
		"Vary":                         {"Access-Control-Request-Headers"},
	}
	for name, want := range wantHeaders {
		if got := headers.Values(name); !slices.Equal(got, want) {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	for name := range headers {
		if strings.HasPrefix(name, "Access-Control-") {
			if _, expected := wantHeaders[name]; !expected {
				t.Errorf("unexpected CORS response header %s = %q", name, headers.Values(name))
			}
		}
	}
	if got := headers.Values("Content-Length"); len(got) != 0 {
		t.Errorf("wire Content-Length = %q, want absent for 204", got)
	}
	body, err := io.ReadAll(buffered)
	if err != nil {
		t.Fatalf("read wire body error = %v", err)
	}
	if len(body) != 0 {
		t.Errorf("wire body = %q, want empty", body)
	}
}

func decodeJSONLogs(t *testing.T, output []byte) []map[string]any {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(output), []byte("\n"))
	entries := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("log is not JSON: %v (%q)", err, line)
		}
		entries = append(entries, entry)
	}
	return entries
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
