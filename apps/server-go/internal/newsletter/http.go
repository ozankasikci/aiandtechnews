package newsletter

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/jsonbody"
)

const (
	// signupWindow and signupLimit are Node's global signup throttle: at most
	// 30 attempts in any rolling minute, across all clients.
	signupWindow = 60 * time.Second
	signupLimit  = 30
	// DigestWriteTimeout replaces the server's 30s write timeout for the
	// digest route, which sends one email every ~550ms. Node has no timeout;
	// the digest itself keeps running after this (or after the client
	// leaves), like Node's.
	DigestWriteTimeout = 15 * time.Minute
	// ConfirmWriteTimeout covers the welcome email a confirmation sends
	// (up to three Resend attempts of ResendAttemptTimeout each), which
	// would not fit in the server's 30s write timeout.
	ConfirmWriteTimeout = 2 * time.Minute
)

type newsletterService interface {
	Subscribe(ctx context.Context, rawEmail, placement string, now time.Time) (SubscriptionState, error)
	Confirm(ctx context.Context, token string, now time.Time) (ConfirmationResult, error)
	Unsubscribe(ctx context.Context, token string, now time.Time) (UnsubscribeState, error)
	ListEditions(ctx context.Context, limit float64) ([]Edition, error)
	Edition(ctx context.Context, key string) (Edition, bool, error)
	SendDailyDigest(ctx context.Context, now time.Time) (DigestResult, error)
}

// Handler serves the public newsletter routes of apps/server/src/routes/public.ts.
type Handler struct {
	service    newsletterService
	cronSecret string
	now        func() time.Time
	logger     *slog.Logger

	signupMu       sync.Mutex
	signupAttempts []int64
}

// NewHandler takes the raw cron secret (NEWSLETTER_CRON_SECRET, else
// CRON_SECRET); like Node it is trimmed per request and an empty one
// rejects every digest request.
func NewHandler(service newsletterService, cronSecret string, now func() time.Time, logger *slog.Logger) *Handler {
	return &Handler{service: service, cronSecret: cronSecret, now: now, logger: logger}
}

// Mount registers the routes relative to /api. None of them requires a
// dashboard login; the digest checks the cron secret itself.
func (h *Handler) Mount(router chi.Router) {
	router.Post("/subscribe", h.subscribe)
	router.Get("/newsletter/confirm", h.confirm)
	// Vercel's /api/newsletter/unsubscribe forwards with POST; email clients
	// use the link (GET) or List-Unsubscribe-Post (POST).
	router.Get("/newsletter/unsubscribe", h.unsubscribe)
	router.Post("/newsletter/unsubscribe", h.unsubscribe)
	router.Get("/newsletter/editions", h.editions)
	router.Get("/newsletter/editions/{edition}", h.edition)
	// Vercel Cron invokes GET; POST is the secured manual retry (and what
	// the website's cron route forwards).
	router.Get("/newsletter/digest", h.digest)
	router.Post("/newsletter/digest", h.digest)
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

func errorBody(message string) map[string]string { return map[string]string{"error": message} }

type stateError struct {
	State string `json:"state"`
	Error string `json:"error"`
}

// allowSignup is Node's signupAttempts window: attempts older than 60s are
// dropped, and a 31st attempt inside the window is refused (and not counted).
func (h *Handler) allowSignup(now time.Time) bool {
	timestamp := now.UnixMilli()
	h.signupMu.Lock()
	defer h.signupMu.Unlock()
	for len(h.signupAttempts) > 0 && h.signupAttempts[0] < timestamp-signupWindow.Milliseconds() {
		h.signupAttempts = h.signupAttempts[1:]
	}
	if len(h.signupAttempts) >= signupLimit {
		return false
	}
	h.signupAttempts = append(h.signupAttempts, timestamp)
	return true
}

func (h *Handler) subscribe(w http.ResponseWriter, r *http.Request) {
	// express.json() runs before the route, so a malformed body is rejected
	// before it counts as a signup attempt.
	body, err := jsonbody.Decode(w, r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("Invalid request body"))
		return
	}
	now := h.now()
	if !h.allowSignup(now) {
		writeJSON(w, http.StatusTooManyRequests, errorBody("Too many signup attempts. Please try again shortly."))
		return
	}
	email := body.Get("email").StringOrEmpty()
	placement, ok := body.Get("placement").Str()
	if !ok {
		placement = "unknown"
	}
	// Like Node, a signup the client abandons still completes.
	state, err := h.service.Subscribe(context.WithoutCancel(r.Context()), email, placement, now)
	var configuration *ConfigurationError
	switch {
	case errors.Is(err, ErrInvalidEmail):
		writeJSON(w, http.StatusBadRequest, errorBody(err.Error()))
		return
	case errors.As(err, &configuration):
		writeJSON(w, http.StatusServiceUnavailable, errorBody("Newsletter signup is temporarily unavailable"))
		return
	case err != nil:
		h.logger.ErrorContext(r.Context(), "Newsletter signup failed", "error", err)
		writeJSON(w, http.StatusBadGateway, errorBody("We could not complete your signup. Please try again."))
		return
	}
	message := "You're subscribed. The next digest will arrive in your inbox."
	if state == StateAlreadyActive {
		message = "You're already subscribed."
	}
	writeJSON(w, http.StatusOK, struct {
		Success bool              `json:"success"`
		State   SubscriptionState `json:"state"`
		Message string            `json:"message"`
	}{true, state, message})
}

// queryString is `typeof req.query[name] === "string" ? value : undefined`
// under Express's qs parser: a repeated parameter becomes an array.
func queryString(r *http.Request, name string) (string, bool) {
	values := r.URL.Query()[name]
	if len(values) != 1 {
		return "", false
	}
	return values[0], true
}

func (h *Handler) confirm(w http.ResponseWriter, r *http.Request) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(ConfirmWriteTimeout))
	token, _ := queryString(r, "token")
	result, err := h.service.Confirm(context.WithoutCancel(r.Context()), token, h.now())
	var configuration *ConfigurationError
	switch {
	case errors.As(err, &configuration):
		writeJSON(w, http.StatusServiceUnavailable, stateError{"unavailable", "Newsletter confirmation is temporarily unavailable"})
	case err != nil:
		h.logger.ErrorContext(r.Context(), "Newsletter confirmation failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, stateError{"invalid", "Confirmation failed"})
	default:
		writeJSON(w, http.StatusOK, result)
	}
}

func (h *Handler) unsubscribe(w http.ResponseWriter, r *http.Request) {
	body, err := jsonbody.Decode(w, r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("Invalid request body"))
		return
	}
	token, ok := queryString(r, "token")
	if !ok {
		token = body.Get("token").StringOrEmpty()
	}
	state, err := h.service.Unsubscribe(context.WithoutCancel(r.Context()), token, h.now())
	var configuration *ConfigurationError
	switch {
	case errors.As(err, &configuration):
		writeJSON(w, http.StatusServiceUnavailable, stateError{"unavailable", "Unsubscribe is temporarily unavailable"})
	case err != nil:
		h.logger.ErrorContext(r.Context(), "Newsletter unsubscribe failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, stateError{"invalid", "Unsubscribe failed"})
	default:
		writeJSON(w, http.StatusOK, struct {
			State UnsubscribeState `json:"state"`
		}{state})
	}
}

// editionsLimit is parseInt(String(req.query.limit || "30"), 10), falling
// back to 30 when that is not finite. A repeated parameter is an array,
// whose String() joins the values with commas.
func editionsLimit(r *http.Request) float64 {
	raw := "30"
	switch values := r.URL.Query()["limit"]; {
	case len(values) == 1 && values[0] != "":
		raw = values[0]
	case len(values) > 1:
		raw = strings.Join(values, ",")
	}
	limit, ok := parseInt10(raw)
	if !ok || math.IsInf(limit, 0) {
		return 30
	}
	return limit
}

func (h *Handler) editions(w http.ResponseWriter, r *http.Request) {
	editions, err := h.service.ListEditions(r.Context(), editionsLimit(r))
	if err != nil {
		h.logger.ErrorContext(r.Context(), "list newsletter editions", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody("Internal server error"))
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Editions []Edition `json:"editions"`
	}{editions})
}

func (h *Handler) edition(w http.ResponseWriter, r *http.Request) {
	// Express decodes a route parameter exactly once. chi matches on the
	// decoded r.URL.Path unless the escaping is non-canonical, in which case
	// it matches on r.URL.RawPath and hands over the escaped segment.
	key := chi.URLParam(r, "edition")
	if r.URL.RawPath != "" {
		unescaped, err := url.PathUnescape(key)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody("Invalid request"))
			return
		}
		key = unescaped
	}
	edition, found, err := h.service.Edition(r.Context(), key)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "get newsletter edition", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody("Internal server error"))
		return
	}
	if !found {
		writeJSON(w, http.StatusNotFound, errorBody("Edition not found"))
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Edition Edition `json:"edition"`
	}{edition})
}

var bearerPrefix = regexp.MustCompile(`^(?i:bearer)[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]+`)

// cronAuthorized is Node's check: the Authorization header, with a leading
// "Bearer " (any case, any whitespace) removed if present, must equal the
// trimmed cron secret, compared in constant time; an empty secret refuses
// everything. Like Node, the bare secret without "Bearer " is accepted too.
func (h *Handler) cronAuthorized(r *http.Request) bool {
	expected := jsTrim(h.cronSecret)
	values := r.Header.Values("Authorization")
	if expected == "" || len(values) == 0 {
		return false
	}
	supplied := bearerPrefix.ReplaceAllLiteralString(values[0], "")
	return supplied != "" && subtle.ConstantTimeCompare([]byte(supplied), []byte(expected)) == 1
}

func (h *Handler) digest(w http.ResponseWriter, r *http.Request) {
	if !h.cronAuthorized(r) {
		writeJSON(w, http.StatusUnauthorized, errorBody("Unauthorized"))
		return
	}
	// Writers without deadline support (test recorders) are skipped.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(DigestWriteTimeout))
	result, err := h.service.SendDailyDigest(r.Context(), h.now())
	var configuration *ConfigurationError
	switch {
	case errors.As(err, &configuration):
		writeJSON(w, http.StatusServiceUnavailable, errorBody("Newsletter delivery is not configured"))
	case err != nil:
		h.logger.ErrorContext(r.Context(), "Newsletter digest failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody("Newsletter digest failed"))
	default:
		writeJSON(w, http.StatusOK, struct {
			Success  bool   `json:"success"`
			Edition  string `json:"edition"`
			Articles int    `json:"articles"`
			Sent     int    `json:"sent"`
			Skipped  int    `json:"skipped"`
			Failed   int    `json:"failed"`
		}{result.Failed == 0, result.Edition, result.Articles, result.Sent, result.Skipped, result.Failed})
	}
}
