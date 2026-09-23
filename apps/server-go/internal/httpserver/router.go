package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const MaxRequestIDLength = 64

// NewRouter creates the shared HTTP transport and mounts public capability
// routes beneath /api. mountRoot registers routes outside /api (such as the
// static /uploads/* files) on the same router, so they share its middleware:
// request IDs, access logs, panic recovery, CORS, and the JSON 404/405 pages.
func NewRouter(logger *slog.Logger, mountAPI func(chi.Router), mountRoot ...func(chi.Router)) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	router := chi.NewRouter()
	router.Use(requestID)
	router.Use(accessLog(logger))
	router.Use(recoverPanics(logger))
	router.Use(expressCORS)
	router.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, `{"error":"Not found"}`)
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		// Chi's custom 405 hook replaces its default Allow-header writer, so
		// discover the methods that match this path before writing our JSON body.
		// Chi does not expose the methods registered for a path. Check the
		// standard methods supported by this service; custom extension methods
		// therefore cannot be advertised automatically.
		for _, method := range supportedMethods(true) {
			matchContext := chi.NewRouteContext()
			if router.Match(matchContext, method, r.URL.Path) {
				w.Header().Add("Allow", method)
			}
		}
		writeError(w, http.StatusMethodNotAllowed, `{"error":"Method not allowed"}`)
	})
	router.Route("/api", func(api chi.Router) {
		if mountAPI != nil {
			mountAPI(api)
		}
	})
	for _, mount := range mountRoot {
		if mount != nil {
			mount(router)
		}
	}
	return router
}

// supportedMethods keeps method discovery and the Express-compatible CORS
// list in one place. OPTIONS is deliberately excluded from the latter.
func supportedMethods(includeOptions bool) []string {
	methods := []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPut,
		http.MethodPatch,
		http.MethodPost,
		http.MethodDelete,
		http.MethodOptions,
	}
	if !includeOptions {
		return methods[:len(methods)-1]
	}
	return methods
}

func expressCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Match the default Express cors() contract rather than a browser-equivalent
		// approximation. In particular, wildcard origins do not require Vary: Origin.
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", strings.Join(supportedMethods(false), ","))
			mergeVary(w.Header(), "Access-Control-Request-Headers")
			if requestedHeaders := r.Header.Get("Access-Control-Request-Headers"); requestedHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", requestedHeaders)
			}
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func mergeVary(header http.Header, field string) {
	values := header.Values("Vary")
	for _, value := range values {
		for _, token := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), field) {
				return
			}
		}
	}
	header.Set("Vary", strings.Join(append(values, field), ", "))
}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if !validRequestID(id) {
			id = newRequestID()
		}
		ctx := context.WithValue(r.Context(), middleware.RequestIDKey, id)
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func validRequestID(id string) bool {
	if len(id) == 0 || len(id) > MaxRequestIDLength {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' {
			continue
		}
		return false
	}
	return true
}

func newRequestID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}
	// crypto/rand failures are exceptionally rare. The fallback remains bounded
	// and safe for logs while avoiding rejection of the request.
	return hex.EncodeToString([]byte(time.Now().UTC().Format("150405.000000000")))[:32]
}

func recoverPanics(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					if recovered == http.ErrAbortHandler {
						panic(recovered)
					}
					logger.ErrorContext(r.Context(), "http request panic",
						"request_id", middleware.GetReqID(r.Context()),
						"method", r.Method,
						"path", r.URL.Path,
						"panic", recovered,
						"stack", string(debug.Stack()),
					)
					writeError(w, http.StatusInternalServerError, `{"error":"Internal server error"}`)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func accessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			started := time.Now()
			next.ServeHTTP(wrapped, r)
			status := wrapped.Status()
			if status == 0 {
				status = http.StatusOK
			}
			logger.InfoContext(r.Context(), "http request",
				"request_id", middleware.GetReqID(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"bytes", wrapped.BytesWritten(),
				"duration_ms", time.Since(started).Milliseconds(),
			)
		})
	}
}

func writeError(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}
