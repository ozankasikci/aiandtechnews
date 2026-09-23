package gemini_test

import (
	"context"
	"encoding/base64"
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

func TestGenerateImageSendsReferenceAndParsesInlineData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models/image-model:generateContent" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		contents := body["contents"].([]any)[0].(map[string]any)
		parts := contents["parts"].([]any)
		config := body["generationConfig"].(map[string]any)
		imageConfig := config["imageConfig"].(map[string]any)
		if contents["role"] != "user" || len(parts) != 2 || imageConfig["aspectRatio"] != "16:9" || imageConfig["imageSize"] != "2K" {
			t.Errorf("body = %v", body)
		}
		inline := parts[1].(map[string]any)["inlineData"].(map[string]any)
		if inline["mimeType"] != "image/jpeg" || inline["data"] != base64.StdEncoding.EncodeToString([]byte("ref")) {
			t.Errorf("inline = %v", inline)
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"here"},{"inline_data":{"mime_type":"image/png","data":"` +
			base64.StdEncoding.EncodeToString([]byte("PNGDATA")) + `"}}]}}]}`))
	}))
	defer server.Close()
	client := gemini.New("k", "", gemini.WithBaseURL(server.URL), gemini.WithImageModel("image-model"))
	image, err := client.GenerateImage(context.Background(), "draw", &gemini.InlineImage{MIMEType: "image/jpeg", Data: []byte("ref")})
	if err != nil || string(image) != "PNGDATA" {
		t.Fatalf("image=%q err=%v", image, err)
	}
}

func TestGenerateImageReportsBlockedPrompt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[{"finishReason":"SAFETY","content":{"parts":[]}}],"promptFeedback":{"blockReason":"OTHER"}}`))
	}))
	defer server.Close()
	_, err := gemini.New("k", "", gemini.WithBaseURL(server.URL)).GenerateImage(context.Background(), "draw", nil)
	if !errors.Is(err, gemini.ErrNoImage) || !strings.Contains(err.Error(), "OTHER") || !strings.Contains(err.Error(), "SAFETY") {
		t.Fatalf("err = %v", err)
	}
}

func TestReviewImageSendsSchemaAndReturnsText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		config := body["generationConfig"].(map[string]any)
		if r.URL.Path != "/models/vision-model:generateContent" || config["temperature"] != 0.0 || config["responseSchema"] == nil {
			t.Errorf("path=%s config=%v", r.URL.Path, config)
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"has_text\":false}"}]}}]}`))
	}))
	defer server.Close()
	client := gemini.New("k", "", gemini.WithBaseURL(server.URL), gemini.WithVisionModel("vision-model"))
	text, err := client.ReviewImage(context.Background(), "review", []byte("jpeg"), map[string]any{"type": "OBJECT"})
	if err != nil || text != `{"has_text":false}` {
		t.Fatalf("text=%q err=%v", text, err)
	}
}

func TestGenerateImageBlockedIsBothNoImageAndBlocked(t *testing.T) {
	for name, body := range map[string]string{
		"prompt feedback":    `{"candidates":[],"promptFeedback":{"blockReason":"OTHER"}}`,
		"safety":             `{"candidates":[{"finishReason":"SAFETY","content":{"parts":[]}}]}`,
		"image safety":       `{"candidates":[{"finishReason":"IMAGE_SAFETY","content":{"parts":[]}}]}`,
		"prohibited content": `{"candidates":[{"finishReason":"PROHIBITED_CONTENT","content":{"parts":[]}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			_, err := gemini.New("k", "", gemini.WithBaseURL(server.URL)).GenerateImage(context.Background(), "draw", nil)
			if !errors.Is(err, gemini.ErrNoImage) || !errors.Is(err, gemini.ErrBlocked) {
				t.Fatalf("err = %v, want ErrNoImage and ErrBlocked", err)
			}
		})
	}
}

func TestGenerateImageWithoutImageIsNotBlocked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"no image today"}]}}]}`))
	}))
	defer server.Close()
	_, err := gemini.New("k", "", gemini.WithBaseURL(server.URL)).GenerateImage(context.Background(), "draw", nil)
	if !errors.Is(err, gemini.ErrNoImage) || errors.Is(err, gemini.ErrBlocked) {
		t.Fatalf("err = %v, want ErrNoImage only", err)
	}
}
