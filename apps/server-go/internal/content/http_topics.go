package content

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type publicTopicStore interface {
	ListTopics(context.Context) ([]TopicSummary, error)
	TopicBySlug(context.Context, string) (Topic, []Article, error)
}

// TopicPublicHandler serves the public topic hubs.
type TopicPublicHandler struct {
	store  publicTopicStore
	logger *slog.Logger
}

func NewTopicPublicHandler(store publicTopicStore, logger *slog.Logger) *TopicPublicHandler {
	return &TopicPublicHandler{store: store, logger: logger}
}

func (h *TopicPublicHandler) MountPublic(router chi.Router) {
	router.Get("/topics", h.list)
	router.Get("/topics/{slug}", h.bySlug)
}

func (h *TopicPublicHandler) list(w http.ResponseWriter, r *http.Request) {
	topics, err := h.store.ListTopics(r.Context())
	if err != nil {
		h.logger.ErrorContext(r.Context(), "list topics", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Internal server error"})
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Topics []TopicSummary `json:"topics"`
	}{topics})
}

func (h *TopicPublicHandler) bySlug(w http.ResponseWriter, r *http.Request) {
	topic, articles, err := h.store.TopicBySlug(r.Context(), chi.URLParam(r, "slug"))
	if errors.Is(err, ErrTopicNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Topic not found"})
		return
	}
	if err != nil {
		h.logger.ErrorContext(r.Context(), "get topic by slug", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Internal server error"})
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Topic    Topic           `json:"topic"`
		Articles []publicArticle `json:"articles"`
	}{topic, publicArticles(articles)})
}
