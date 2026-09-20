package editorial

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type publicAuthorService interface {
	List(context.Context) ([]Author, error)
}

type PublicHandler struct {
	service publicAuthorService
	logger  *slog.Logger
}

func NewPublicHandler(service publicAuthorService, logger *slog.Logger) *PublicHandler {
	return &PublicHandler{service: service, logger: logger}
}

func (h *PublicHandler) MountPublic(router chi.Router) {
	router.Get("/authors", h.list)
}

func (h *PublicHandler) list(w http.ResponseWriter, r *http.Request) {
	authors, err := h.service.List(r.Context())
	if err != nil {
		h.logger.ErrorContext(r.Context(), "list authors", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Internal server error"})
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Authors []Author `json:"authors"`
	}{authors})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		body = []byte(`{"error":"Internal server error"}`)
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
