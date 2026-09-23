package gemini_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
)

func TestGenerateJSONSendsNodeCompatibleRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models/gemini-test:generateContent" || r.Header.Get("x-goog-api-key") != "k" {
			t.Errorf("path=%s key=%q", r.URL.Path, r.Header.Get("x-goog-api-key"))
		}
		var body struct {
			Contents []struct {
				Parts []struct{ Text string } `json:"parts"`
			} `json:"contents"`
			GenerationConfig struct {
				MaxOutputTokens  int     `json:"maxOutputTokens"`
				ResponseMimeType string  `json:"responseMimeType"`
				Temperature      float64 `json:"temperature"`
			} `json:"generationConfig"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Contents[0].Parts[0].Text != "prompt" || body.GenerationConfig.MaxOutputTokens != 4096 ||
			body.GenerationConfig.ResponseMimeType != "application/json" || body.GenerationConfig.Temperature != 0.4 {
			t.Errorf("body = %+v", body)
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":" {\"a\":"},{"text":"1} "}]}}]}`))
	}))
	defer server.Close()

	client := gemini.New("k", "gemini-test", gemini.WithBaseURL(server.URL))
	text, err := client.GenerateJSON(context.Background(), "prompt")
	if err != nil || text != `{"a":1}` {
		t.Fatalf("text=%q err=%v", text, err)
	}
}

func TestGenerateJSONClassifiesErrors(t *testing.T) {
	for _, tc := range []struct {
		status    int
		transient bool
	}{{429, true}, {503, true}, {400, false}, {403, false}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"error":{"message":"nope"}}`))
		}))
		_, err := gemini.New("k", "m", gemini.WithBaseURL(server.URL)).GenerateJSON(context.Background(), "p")
		server.Close()
		var apiErr *gemini.Error
		if !errors.As(err, &apiErr) || apiErr.Status != tc.status || apiErr.Transient() != tc.transient {
			t.Errorf("status %d: err=%v", tc.status, err)
		}
	}
}

func TestGenerateJSONRequiresKeyAndContent(t *testing.T) {
	if _, err := gemini.New("", "m").GenerateJSON(context.Background(), "p"); !errors.Is(err, gemini.ErrMissingAPIKey) {
		t.Fatalf("missing key err = %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[]}`))
	}))
	defer server.Close()
	if _, err := gemini.New("k", "m", gemini.WithBaseURL(server.URL)).GenerateJSON(context.Background(), "p"); !errors.Is(err, gemini.ErrEmptyResponse) {
		t.Fatalf("empty err = %v", err)
	}
}

func TestGenerateJSONReportsSafetyBlocks(t *testing.T) {
	for name, body := range map[string]string{
		"prompt blocked":    `{"promptFeedback":{"blockReason":"SAFETY"}}`,
		"candidate blocked": `{"candidates":[{"content":{"parts":[]},"finishReason":"SAFETY"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			_, err := gemini.New("k", "m", gemini.WithBaseURL(server.URL)).GenerateJSON(context.Background(), "p")
			if !errors.Is(err, gemini.ErrBlocked) || !strings.Contains(err.Error(), "SAFETY") {
				t.Fatalf("err = %v, want ErrBlocked with the reason", err)
			}
		})
	}
}

func TestErrorBodySnippetIsValidUTF8(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(strings.Repeat("a", 299) + "\u00e9" + strings.Repeat("b", 50)))
	}))
	defer server.Close()
	_, err := gemini.New("k", "m", gemini.WithBaseURL(server.URL)).GenerateJSON(context.Background(), "p")
	var apiErr *gemini.Error
	if !errors.As(err, &apiErr) || !utf8.ValidString(apiErr.Body) || apiErr.Body != strings.Repeat("a", 299) {
		t.Fatalf("body = %q (len %d)", apiErr.Body, len(apiErr.Body))
	}
}
