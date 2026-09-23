package newsroom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

const (
	maxBodyBytes     = 100 * 1024
	maxIDsPerRequest = 100
)

// ErrCollectInProgress is returned by a Collector when a run is already active.
var ErrCollectInProgress = errors.New("collection already running")

// Collector starts a collection run in the background. It is nil until the
// Go collector exists, and /collect answers 503 in the meantime.
//
// Start must return promptly (it only kicks off the run) and must not retain
// the context it is given beyond the call: that context belongs to the HTTP
// request and is cancelled once the 202 response is written, so the run
// itself must execute on a context the implementation owns.
type Collector interface {
	Start(context.Context) error
}

type Handler struct {
	service   *Service
	collector Collector
	logger    *slog.Logger
}

func NewHandler(service *Service, collector Collector, logger *slog.Logger) *Handler {
	return &Handler{service: service, collector: collector, logger: logger}
}

// Mount registers the /newsroom routes behind requireAuth.
func (h *Handler) Mount(router chi.Router, requireAuth func(http.Handler) http.Handler) {
	router.Route("/newsroom", func(r chi.Router) {
		r.Use(requireAuth)
		r.Get("/overview", h.overview)
		r.Get("/candidates", h.list)
		r.Post("/candidates/publish", h.publish)
		r.Post("/candidates/reject", h.reject)
		r.Post("/candidates/{id}/unqueue", h.unqueue)
		r.Post("/candidates/{id}/retry", h.retry)
		r.Post("/collect", h.collect)
		r.Get("/settings", h.settings)
		r.Put("/settings", h.updateSettings)
	})
}

func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	overview, err := h.service.Overview(r.Context())
	if err != nil {
		h.internalError(w, r, "newsroom overview", err)
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	statuses, err := parseStatuses(query.Get("status"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Cap page so (page-1)*limit cannot overflow the store's OFFSET.
	page := parsePositive(query.Get("page"), 1, 100000)
	limit := parsePositive(query.Get("limit"), DefaultPageLimit, MaxPageLimit)
	result, err := h.service.List(r.Context(), statuses, page, limit)
	if err != nil {
		h.internalError(w, r, "list candidates", err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) publish(w http.ResponseWriter, r *http.Request) {
	ids, ok := decodeIDs(w, r)
	if !ok {
		return
	}
	queued, skipped, err := h.service.Publish(r.Context(), ids)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "publish candidates", "error", err, "queued_ids", candidateIDs(queued), "skipped", len(skipped))
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Queued  []Candidate `json:"queued"`
		Skipped []Skipped   `json:"skipped"`
	}{queued, skipped})
}

func (h *Handler) reject(w http.ResponseWriter, r *http.Request) {
	ids, ok := decodeIDs(w, r)
	if !ok {
		return
	}
	rejected, skipped, err := h.service.Reject(r.Context(), ids)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "reject candidates", "error", err, "rejected_ids", rejected, "skipped", len(skipped))
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Rejected []int64   `json:"rejected"`
		Skipped  []Skipped `json:"skipped"`
	}{rejected, skipped})
}

func (h *Handler) unqueue(w http.ResponseWriter, r *http.Request) {
	h.single(w, r, h.service.Unqueue, "Candidate is not queued")
}

func (h *Handler) retry(w http.ResponseWriter, r *http.Request) {
	h.single(w, r, h.service.Retry, "Candidate is not failed")
}

func (h *Handler) single(w http.ResponseWriter, r *http.Request, action func(context.Context, int64) (Candidate, error), staleMessage string) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "Candidate not found")
		return
	}
	candidate, err := action(r.Context(), id)
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "Candidate not found")
	case errors.Is(err, ErrStaleTransition):
		writeError(w, http.StatusConflict, staleMessage)
	case err != nil:
		h.internalError(w, r, "update candidate", err)
	default:
		writeJSON(w, http.StatusOK, struct {
			Candidate Candidate `json:"candidate"`
		}{candidate})
	}
}

func (h *Handler) collect(w http.ResponseWriter, r *http.Request) {
	if h.collector == nil {
		writeError(w, http.StatusServiceUnavailable, "Collector is not enabled")
		return
	}
	err := h.collector.Start(r.Context())
	if errors.Is(err, ErrCollectInProgress) {
		writeError(w, http.StatusConflict, "Collection already running")
		return
	}
	if err != nil {
		h.internalError(w, r, "start collection", err)
		return
	}
	writeJSON(w, http.StatusAccepted, struct {
		Started bool `json:"started"`
	}{true})
}

func (h *Handler) settings(w http.ResponseWriter, r *http.Request) {
	delay, err := h.service.PublishDelay(r.Context())
	if err != nil {
		h.internalError(w, r, "read newsroom settings", err)
		return
	}
	writeJSON(w, http.StatusOK, delay)
}

func (h *Handler) updateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MinMinutes *int `json:"publish_delay_min_minutes"`
		MaxMinutes *int `json:"publish_delay_max_minutes"`
	}
	if err := decodeJSON(w, r, &body); err != nil || body.MinMinutes == nil || body.MaxMinutes == nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	delay, err := h.service.SetPublishDelay(r.Context(), PublishDelay{MinMinutes: *body.MinMinutes, MaxMinutes: *body.MaxMinutes})
	if errors.Is(err, ErrInvalidDelay) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		h.internalError(w, r, "update newsroom settings", err)
		return
	}
	writeJSON(w, http.StatusOK, delay)
}

// candidateIDs extracts ids for logging partial batch results.
func candidateIDs(candidates []Candidate) []int64 {
	ids := make([]int64, len(candidates))
	for i, candidate := range candidates {
		ids[i] = candidate.ID
	}
	return ids
}

func decodeIDs(w http.ResponseWriter, r *http.Request) ([]int64, bool) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return nil, false
	}
	if len(body.IDs) == 0 || len(body.IDs) > maxIDsPerRequest {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("ids must contain 1 to %d candidate ids", maxIDsPerRequest))
		return nil, false
	}
	return body.IDs, true
}

// decodeJSON reads exactly one JSON value with no unknown fields.
func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("trailing data after JSON body")
	}
	return nil
}

func parseStatuses(value string) ([]Status, error) {
	if strings.TrimSpace(value) == "" {
		return []Status{StatusPending}, nil
	}
	statuses := make([]Status, 0)
	seen := make(map[Status]bool)
	for _, part := range strings.Split(value, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		status, ok := ParseStatus(name)
		if !ok {
			return nil, fmt.Errorf("unknown status %q", name)
		}
		if !seen[status] {
			seen[status] = true
			statuses = append(statuses, status)
		}
	}
	if len(statuses) == 0 {
		return []Status{StatusPending}, nil
	}
	return statuses, nil
}

// parsePositive returns fallback for missing or invalid values and clamps to max when max > 0.
func parsePositive(value string, fallback, max int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 1 {
		return fallback
	}
	if max > 0 && parsed > max {
		return max
	}
	return parsed
}

func (h *Handler) internalError(w http.ResponseWriter, r *http.Request, message string, err error) {
	h.logger.ErrorContext(r.Context(), message, "error", err)
	writeError(w, http.StatusInternalServerError, "Internal server error")
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
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
