package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

const MaxRequestIDLength = 64

// NewRouter creates the shared HTTP transport and mounts public capability
// routes beneath /api.
func NewRouter(logger *slog.Logger, mountAPI func(chi.Router)) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	router := chi.NewRouter()
	router.Use(requestID)
	router.Use(accessLog(logger))
	router.Use(recoverPanics(logger))
	router.Use(expressCORS(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodPost, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: false,
		MaxAge:           0,
	}))
	router.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, `{"error":"Not found"}`)
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		// Chi's custom 405 hook replaces its default Allow-header writer, so
		// discover the methods that match this path before writing our JSON body.
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
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
	return router
}

func expressCORS(options cors.Options) func(http.Handler) http.Handler {
	configured := cors.Handler(options)
	return func(next http.Handler) http.Handler {
		handler := configured(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Express' default cors middleware emits the wildcard on all responses,
			// including same-origin/no-Origin requests.
			w.Header().Set("Access-Control-Allow-Origin", "*")
			if r.Method == http.MethodOptions && r.Header.Get("Origin") != "" && r.Header.Get("Access-Control-Request-Method") != "" {
				handler.ServeHTTP(&preflightResponseWriter{ResponseWriter: w}, r)
				return
			}
			handler.ServeHTTP(w, r)
		})
	}
}

type preflightResponseWriter struct {
	http.ResponseWriter
}

func (w *preflightResponseWriter) WriteHeader(status int) {
	if status == http.StatusOK {
		status = http.StatusNoContent
	}
	w.ResponseWriter.WriteHeader(status)
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
			logger.InfoContext(r.Context(), "http request",
				"request_id", middleware.GetReqID(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", wrapped.Status(),
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
