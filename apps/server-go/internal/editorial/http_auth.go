package editorial

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

const maxLoginBodyBytes = 100 * 1024

type loginService interface {
	Login(context.Context, string, string) (LoginResult, error)
}

type tokenVerifier interface {
	Verify(string) (Claims, error)
}

type AuthHandler struct {
	login  loginService
	tokens tokenVerifier
	logger *slog.Logger
}

func NewAuthHandler(login loginService, tokens tokenVerifier, logger *slog.Logger) *AuthHandler {
	return &AuthHandler{login: login, tokens: tokens, logger: logger}
}

func (h *AuthHandler) Mount(router chi.Router) {
	router.Post("/auth/login", h.handleLogin)
	router.With(h.requireAuth).Get("/auth/me", h.me)
	router.With(h.requireAuth).Post("/auth/logout", h.logout)
}

func (h *AuthHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxLoginBodyBytes)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}
	if request.Email == "" || request.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Email and password are required"})
		return
	}
	result, err := h.login.Login(r.Context(), request.Email, request.Password)
	if errors.Is(err, ErrInvalidCredentials) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid email or password"})
		return
	}
	if err != nil {
		h.logger.ErrorContext(r.Context(), "login", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Internal server error"})
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Token string   `json:"token"`
		User  AuthUser `json:"user"`
	}{result.Token, result.User})
}

type authContextKey struct{}

func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	claims, ok := ctx.Value(authContextKey{}).(Claims)
	return claims, ok
}

func (h *AuthHandler) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Authentication required"})
			return
		}
		claims, err := h.tokens.Verify(header[7:])
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid or expired token"})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authContextKey{}, claims)))
	})
}

// RequireAuth lets other capabilities protect their routes with the same
// bearer-token check used by /auth/me.
func (h *AuthHandler) RequireAuth(next http.Handler) http.Handler { return h.requireAuth(next) }

func (h *AuthHandler) me(w http.ResponseWriter, r *http.Request) {
	claims, ok := ClaimsFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Authentication required"})
		return
	}
	writeJSON(w, http.StatusOK, struct {
		User Claims `json:"user"`
	}{claims})
}

func (h *AuthHandler) logout(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, struct {
		Success bool `json:"success"`
	}{true})
}
