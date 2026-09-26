package app_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/editorial"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

// The quiz routes are mounted in the application: the public read with the
// shared JSON and CORS conventions, the dashboard actions behind auth. With
// the publisher disabled there is no text generator, so regenerate is 503.
func TestQuizRoutesAreMounted(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 9, 27, 6, 0, 0, 0, time.UTC)
	cfg := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4402", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), JWTSecret: authTestSecret, UploadsDir: t.TempDir()}
	application, err := app.NewWithDatabaseAt(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db, func() time.Time { return fixed })
	if err != nil {
		t.Fatal(err)
	}
	handler := application.Handler()

	today := request(t, handler, http.MethodGet, "/api/quiz/today", "", "")
	assertAuthResponse(t, today, http.StatusNotFound, `{"error":"No quiz available"}`)
	if got := today.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q", got)
	}

	if _, err := db.Exec(`INSERT INTO quizzes (day, questions, status, created_at) VALUES ('2026-09-27',
		'[{"question":"Q?","options":["a","b","c","d"],"answer":1,"slug":"s","title":"T","evidence":"hidden evidence sentence"}]', 'published', '2026-09-27T04:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	assertAuthResponse(t, request(t, handler, http.MethodGet, "/api/quiz/today", "", ""), http.StatusOK,
		`{"quiz":{"number":1,"day":"2026-09-27","questions":[{"question":"Q?","options":["a","b","c","d"],"answer":1,"article":{"slug":"s","title":"T"}}]}}`)

	tokens, _ := editorial.NewJWT(authTestSecret, func() time.Time { return fixed })
	token, _ := tokens.Sign(editorial.Identity{ID: 1, Email: "editor@example.invalid", Role: "admin"})
	assertAuthResponse(t, request(t, handler, http.MethodPost, "/api/dashboard/quiz/regenerate", "", "Bearer "+token),
		http.StatusServiceUnavailable, `{"error":"Quiz generation is not configured"}`)
	assertAuthResponse(t, request(t, handler, http.MethodPost, "/api/dashboard/quiz/pull", "", "Bearer "+token),
		http.StatusOK, `{"pulled":"2026-09-27"}`)
	assertAuthResponse(t, request(t, handler, http.MethodGet, "/api/quiz/today", "", ""), http.StatusNotFound, `{"error":"No quiz available"}`)
}
