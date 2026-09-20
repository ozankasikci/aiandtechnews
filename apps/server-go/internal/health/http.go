package health

import (
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// MountPublic registers unauthenticated health routes relative to an API router.
func MountPublic(router chi.Router) {
	router.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
}
