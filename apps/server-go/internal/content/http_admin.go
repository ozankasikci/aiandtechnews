package content

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/jsonbody"
)

type adminService interface {
	ListArticles(context.Context, DashboardQuery) (Page, error)
	GetArticle(context.Context, string) (Article, error)
	CreateArticle(context.Context, jsonbody.Object) (ArticleChange, error)
	ListCategories(context.Context) ([]CategoryWithCount, error)
}

// IndexNowNotifier queues public article slugs for IndexNow. Notify must not
// block: Node sends the HTTP response first and never lets IndexNow delay or
// fail a dashboard request.
type IndexNowNotifier interface {
	Notify(slugs []string)
}

// AdminHandler serves the authenticated dashboard article and category routes.
type AdminHandler struct {
	service  adminService
	indexNow IndexNowNotifier
	logger   *slog.Logger
}

func NewAdminHandler(service adminService, indexNow IndexNowNotifier, logger *slog.Logger) *AdminHandler {
	return &AdminHandler{service: service, indexNow: indexNow, logger: logger}
}

// Mount registers the routes relative to /api/dashboard. The caller owns
// authentication: every route here must sit behind RequireAuth.
func (h *AdminHandler) Mount(router chi.Router) {
	router.Get("/articles", h.listArticles)
	router.Get("/articles/{id}", h.getArticle)
	router.Post("/articles", h.createArticle)
	router.Get("/categories", h.listCategories)
}

type articleEnvelope struct {
	Article Article `json:"article"`
}

type categoryEnvelope struct {
	Category Category `json:"category"`
}

type successEnvelope struct {
	Success bool `json:"success"`
}

func (h *AdminHandler) listArticles(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	page, err := h.service.ListArticles(r.Context(), DashboardQuery{
		Page:     parseNodeNumber(values.Get("page"), 1),
		Limit:    parseNodeLimit(values.Get("limit"), 20, 50),
		Status:   values.Get("status"),
		Category: values.Get("category"),
		Search:   values.Get("search"),
	})
	if err != nil {
		h.fail(w, r, "list dashboard articles", err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (h *AdminHandler) getArticle(w http.ResponseWriter, r *http.Request) {
	article, err := h.service.GetArticle(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, "get dashboard article", err)
		return
	}
	writeJSON(w, http.StatusOK, articleEnvelope{article})
}

func (h *AdminHandler) createArticle(w http.ResponseWriter, r *http.Request) {
	body, ok := h.decode(w, r)
	if !ok {
		return
	}
	change, err := h.service.CreateArticle(r.Context(), body)
	if err != nil {
		h.fail(w, r, "create dashboard article", err)
		return
	}
	writeJSON(w, http.StatusCreated, articleEnvelope{change.Article})
	h.notify(change.IndexNowSlugs)
}

func (h *AdminHandler) listCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := h.service.ListCategories(r.Context())
	if err != nil {
		h.fail(w, r, "list dashboard categories", err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Categories []CategoryWithCount `json:"categories"`
	}{categories})
}

func (h *AdminHandler) decode(w http.ResponseWriter, r *http.Request) (jsonbody.Object, bool) {
	body, err := jsonbody.Decode(w, r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return nil, false
	}
	return body, true
}

// notify runs after the response is written, like Node's queueIndexNowNotification.
func (h *AdminHandler) notify(slugs []string) {
	if len(slugs) > 0 && h.indexNow != nil {
		h.indexNow.Notify(slugs)
	}
}

// fail maps AdminError to Node's status and body; anything else is an
// unexpected failure (Express's default 500) logged without leaking detail.
func (h *AdminHandler) fail(w http.ResponseWriter, r *http.Request, message string, err error) {
	var adminErr *AdminError
	if errors.As(err, &adminErr) {
		if adminErr.Status >= http.StatusInternalServerError {
			h.logger.ErrorContext(r.Context(), message, "error", err)
		}
		if len(adminErr.Details) > 0 {
			writeJSON(w, adminErr.Status, struct {
				Error   string   `json:"error"`
				Details []string `json:"details"`
			}{adminErr.Message, adminErr.Details})
			return
		}
		writeJSON(w, adminErr.Status, map[string]string{"error": adminErr.Message})
		return
	}
	h.logger.ErrorContext(r.Context(), message, "error", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Internal server error"})
}
