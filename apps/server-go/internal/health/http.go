package health

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// CheckTimeout bounds a MountChecked check, so a busy or stuck database
// makes health fail instead of hang.
const CheckTimeout = 2 * time.Second

// MountPublic registers unauthenticated health routes relative to an API router.
func MountPublic(router chi.Router) {
	router.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		write(w, http.StatusOK, `{"status":"ok"}`)
	})
}

// MountChecked registers the health route with a readiness check (the
// database-backed composition passes a database query). While the check
// passes the response is exactly MountPublic's; when it fails the route
// answers 503 {"status":"error"} without details.
func MountChecked(router chi.Router, check func(context.Context) error) {
	router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), CheckTimeout)
		defer cancel()
		if err := check(ctx); err != nil {
			write(w, http.StatusServiceUnavailable, `{"status":"error"}`)
			return
		}
		write(w, http.StatusOK, `{"status":"ok"}`)
	})
}

func write(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}
