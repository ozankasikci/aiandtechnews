package quiz_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/quiz"
)

func quizRouter(t *testing.T, service *quiz.Service) http.Handler {
	t.Helper()
	handler := quiz.NewHandler(service, discard)
	router := chi.NewRouter()
	router.Route("/api", func(api chi.Router) {
		handler.MountPublic(api)
		api.Route("/dashboard", handler.Mount)
	})
	return router
}

func serve(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	return recorder
}

func assertResponse(t *testing.T, response *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	if response.Code != status || response.Body.String() != body {
		t.Fatalf("response = %d %s, want %d %s", response.Code, response.Body.String(), status, body)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestTodayServesTheLatestPublishedQuizWithoutEvidence(t *testing.T) {
	service, store, _ := newService(t, nil, time.Date(2026, 9, 27, 6, 0, 0, 0, time.UTC))
	router := quizRouter(t, service)
	assertResponse(t, serve(router, http.MethodGet, "/api/quiz/today"), http.StatusNotFound, `{"error":"No quiz available"}`)

	saved, err := store.Save(context.Background(), "2026-09-27", sampleQuestions[:1], time.Now())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"quiz":{"number":` + itoa(saved.Number) + `,"day":"2026-09-27","questions":[` +
		`{"question":"Q1?","options":["a","b","c","d"],"answer":2,"article":{"slug":"one","title":"One"}}]}}`
	assertResponse(t, serve(router, http.MethodGet, "/api/quiz/today"), http.StatusOK, want)
}

func itoa(n int64) string {
	body, _ := json.Marshal(n)
	return string(body)
}

func TestDashboardPullAndRegenerate(t *testing.T) {
	text := &lockedText{responses: []string{encode(t, validQuestions())}}
	service, _, _ := newService(t, text, time.Date(2026, 9, 27, 6, 0, 0, 0, time.UTC))
	router := quizRouter(t, service)

	assertResponse(t, serve(router, http.MethodPost, "/api/dashboard/quiz/pull"), http.StatusNotFound, `{"error":"No quiz for today"}`)

	regenerated := serve(router, http.MethodPost, "/api/dashboard/quiz/regenerate")
	if regenerated.Code != http.StatusOK {
		t.Fatalf("regenerate = %d %s", regenerated.Code, regenerated.Body.String())
	}
	var body struct {
		Quiz struct {
			Number    int64  `json:"number"`
			Day       string `json:"day"`
			Questions []struct {
				Question string            `json:"question"`
				Options  []string          `json:"options"`
				Answer   int               `json:"answer"`
				Article  map[string]string `json:"article"`
				Evidence *string           `json:"evidence"`
			} `json:"questions"`
		} `json:"quiz"`
	}
	if err := json.Unmarshal(regenerated.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Quiz.Day != "2026-09-27" || len(body.Quiz.Questions) != 3 || body.Quiz.Questions[0].Article["title"] != testArticles[0].Title || body.Quiz.Questions[0].Evidence != nil {
		t.Fatalf("regenerated = %s", regenerated.Body.String())
	}

	assertResponse(t, serve(router, http.MethodPost, "/api/dashboard/quiz/pull"), http.StatusOK, `{"pulled":"2026-09-27"}`)
	assertResponse(t, serve(router, http.MethodGet, "/api/quiz/today"), http.StatusNotFound, `{"error":"No quiz available"}`)
}

func TestDashboardRegenerateReportsGenerationFailures(t *testing.T) {
	bad := validQuestions()
	bad[1].Evidence = "Nvidia unveiled the Rubin chip at CES in Las Vegas."
	text := &lockedText{responses: []string{encode(t, bad)}}
	service, _, _ := newService(t, text, time.Date(2026, 9, 27, 6, 0, 0, 0, time.UTC))
	assertResponse(t, serve(quizRouter(t, service), http.MethodPost, "/api/dashboard/quiz/regenerate"), http.StatusBadGateway,
		`{"error":"Quiz generation failed: quiz failed validation: question 2: evidence is not copied from the article"}`)

	disabled, _, _ := newService(t, nil, time.Date(2026, 9, 27, 6, 0, 0, 0, time.UTC))
	assertResponse(t, serve(quizRouter(t, disabled), http.MethodPost, "/api/dashboard/quiz/regenerate"), http.StatusServiceUnavailable,
		`{"error":"Quiz generation is not configured"}`)
}
