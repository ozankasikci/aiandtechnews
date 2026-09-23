package settings

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/jsonbody"
)

type settingsService interface {
	Get(context.Context) (Values, error)
	Update(context.Context, jsonbody.Object) (Values, error)
}

type Handler struct {
	service settingsService
	logger  *slog.Logger
}

func NewHandler(service settingsService, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

// Mount registers the routes relative to /api/dashboard; the caller applies RequireAuth.
func (h *Handler) Mount(router chi.Router) {
	router.Get("/settings", h.get)
	router.Put("/settings", h.update)
}

type envelope struct {
	Settings Values `json:"settings"`
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	values, err := h.service.Get(r.Context())
	if err != nil {
		h.internalError(w, r, "get settings", err)
		return
	}
	writeJSON(w, http.StatusOK, envelope{values})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	body, err := jsonbody.Decode(w, r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}
	values, err := h.service.Update(r.Context(), body)
	if err != nil {
		h.internalError(w, r, "update settings", err)
		return
	}
	writeJSON(w, http.StatusOK, envelope{values})
}

func (h *Handler) internalError(w http.ResponseWriter, r *http.Request, message string, err error) {
	h.logger.ErrorContext(r.Context(), message, "error", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Internal server error"})
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
