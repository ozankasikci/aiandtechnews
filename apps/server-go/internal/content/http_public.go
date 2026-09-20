package content

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

type publicArticleService interface {
	List(context.Context, ListQuery) (Page, error)
	Trending(context.Context, int) ([]Article, error)
	BySlug(context.Context, string) (Article, error)
	ByID(context.Context, string) (Article, error)
}

type PublicHandler struct {
	service publicArticleService
	logger  *slog.Logger
}

func NewPublicHandler(service publicArticleService, logger *slog.Logger) *PublicHandler {
	return &PublicHandler{service: service, logger: logger}
}

// MountPublic registers the auditable public article route manifest. The ID
// route is intentionally registered before the slug route.
func (h *PublicHandler) MountPublic(router chi.Router) {
	router.Get("/articles", h.list)
	router.Get("/articles/trending", h.trending)
	router.Get("/articles/id/{id}", h.byID)
	router.Get("/articles/{slug}", h.bySlug)
}

func (h *PublicHandler) list(w http.ResponseWriter, r *http.Request) {
	query := ListQuery{
		Page: parseNodeNumber(r.URL.Query().Get("page"), 1), Limit: parseNodeLimit(r.URL.Query().Get("limit"), 12, 50),
		Category: r.URL.Query().Get("category"), Search: r.URL.Query().Get("search"),
	}
	page, err := h.service.List(r.Context(), query)
	if err != nil {
		h.internalError(w, r, "list articles", err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (h *PublicHandler) trending(w http.ResponseWriter, r *http.Request) {
	articles, err := h.service.Trending(r.Context(), parseNodeLimit(r.URL.Query().Get("limit"), 5, 20))
	if err != nil {
		h.internalError(w, r, "list trending articles", err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Articles []Article `json:"articles"`
	}{articles})
}

func (h *PublicHandler) bySlug(w http.ResponseWriter, r *http.Request) {
	article, err := h.service.BySlug(r.Context(), chi.URLParam(r, "slug"))
	if errors.Is(err, ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Article not found"})
		return
	}
	if err != nil {
		h.internalError(w, r, "get article by slug", err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Article Article `json:"article"`
	}{article})
}

func (h *PublicHandler) byID(w http.ResponseWriter, r *http.Request) {
	article, err := h.service.ByID(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Article not found"})
		return
	}
	if err != nil {
		h.internalError(w, r, "get article by ID", err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Article Article `json:"article"`
	}{article})
}

func (h *PublicHandler) internalError(w http.ResponseWriter, r *http.Request, message string, err error) {
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

// parseNodeNumber implements parseInt(value) || fallback while retaining the
// rounded IEEE-754 Number that JavaScript passes to SQLite.
func parseNodeNumber(value string, fallback float64) float64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	sign := 1.0
	if value[0] == '+' || value[0] == '-' {
		if value[0] == '-' {
			sign = -1
		}
		value = value[1:]
	}
	base := 10
	if len(value) >= 2 && value[0] == '0' && (value[1] == 'x' || value[1] == 'X') {
		base = 16
		value = value[2:]
	}
	end := 0
	for end < len(value) {
		c := value[end]
		valid := c >= '0' && c <= '9' || base == 16 && (c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F')
		if !valid {
			break
		}
		end++
	}
	if end == 0 {
		return fallback
	}
	digits := value[:end]
	var parsed float64
	if base == 10 {
		parsed, _ = strconv.ParseFloat(digits, 64)
	} else {
		integer := new(big.Int)
		if _, ok := integer.SetString(digits, base); !ok {
			return fallback
		}
		parsed, _ = integer.Float64()
	}
	result := parsed * sign
	if result == 0 {
		return fallback
	}
	return result
}

func parseNodeLimit(value string, fallback, maximum int) int {
	parsed := parseNodeNumber(value, float64(fallback))
	if parsed < 1 {
		parsed = 1
	}
	if parsed > float64(maximum) {
		parsed = float64(maximum)
	}
	return int(parsed)
}
