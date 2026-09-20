package content

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type publicCategoryService interface {
	List(context.Context) ([]Category, error)
}

type CategoryPublicHandler struct {
	service publicCategoryService
	logger  *slog.Logger
}

func NewCategoryPublicHandler(service publicCategoryService, logger *slog.Logger) *CategoryPublicHandler {
	return &CategoryPublicHandler{service: service, logger: logger}
}

func (h *CategoryPublicHandler) MountPublic(router chi.Router) {
	router.Get("/categories", h.list)
}

func (h *CategoryPublicHandler) list(w http.ResponseWriter, r *http.Request) {
	categories, err := h.service.List(r.Context())
	if err != nil {
		h.logger.ErrorContext(r.Context(), "list categories", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Internal server error"})
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Categories []Category `json:"categories"`
	}{categories})
}
