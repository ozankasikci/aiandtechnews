package quiz

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type quizService interface {
	Latest(context.Context) (Quiz, error)
	PullToday(context.Context) (string, bool, error)
	Regenerate(context.Context) (Quiz, error)
}

type Handler struct {
	service quizService
	logger  *slog.Logger
}

func NewHandler(service quizService, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

// MountPublic registers GET /quiz/today relative to /api.
func (h *Handler) MountPublic(router chi.Router) {
	router.Get("/quiz/today", h.today)
}

// Mount registers the routes relative to /api/dashboard; the caller applies RequireAuth.
func (h *Handler) Mount(router chi.Router) {
	router.Post("/quiz/pull", h.pull)
	router.Post("/quiz/regenerate", h.regenerate)
}

type articleRef struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// publicQuestion is a question as the website sees it: the evidence sentence
// stays in the database.
type publicQuestion struct {
	Question string     `json:"question"`
	Options  []string   `json:"options"`
	Answer   int        `json:"answer"`
	Article  articleRef `json:"article"`
}

type publicQuiz struct {
	Number    int64            `json:"number"`
	Day       string           `json:"day"`
	Questions []publicQuestion `json:"questions"`
}

type envelope struct {
	Quiz publicQuiz `json:"quiz"`
}

func public(quiz Quiz) envelope {
	questions := make([]publicQuestion, len(quiz.Questions))
	for i, question := range quiz.Questions {
		questions[i] = publicQuestion{
			Question: question.Question, Options: question.Options, Answer: question.Answer,
			Article: articleRef{Slug: question.Slug, Title: question.Title},
		}
	}
	return envelope{publicQuiz{Number: quiz.Number, Day: quiz.Day, Questions: questions}}
}

func (h *Handler) today(w http.ResponseWriter, r *http.Request) {
	quiz, err := h.service.Latest(r.Context())
	if errors.Is(err, ErrNoQuiz) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "No quiz available"})
		return
	}
	if err != nil {
		h.internalError(w, r, "read latest quiz", err)
		return
	}
	writeJSON(w, http.StatusOK, public(quiz))
}

func (h *Handler) pull(w http.ResponseWriter, r *http.Request) {
	day, pulled, err := h.service.PullToday(r.Context())
	if err != nil {
		h.internalError(w, r, "pull quiz", err)
		return
	}
	if !pulled {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "No quiz for today"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"pulled": day})
}

func (h *Handler) regenerate(w http.ResponseWriter, r *http.Request) {
	quiz, err := h.service.Regenerate(r.Context())
	var failed generationError
	switch {
	case errors.Is(err, ErrGenerationDisabled):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Quiz generation is not configured"})
	case errors.As(err, &failed):
		h.logger.WarnContext(r.Context(), "quiz regeneration failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Quiz generation failed: " + err.Error()})
	case err != nil:
		h.internalError(w, r, "regenerate quiz", err)
	default:
		writeJSON(w, http.StatusOK, public(quiz))
	}
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
