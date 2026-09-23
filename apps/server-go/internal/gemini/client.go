// Package gemini calls the Gemini generateContent API.
package gemini

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultBaseURL     = "https://generativelanguage.googleapis.com/v1beta"
	DefaultTextModel   = "gemini-3.5-flash-lite"
	DefaultImageModel  = "gemini-3.1-flash-image-preview"
	DefaultVisionModel = "gemini-3.5-flash"
	textTimeout        = 90 * time.Second
	imageTimeout       = 120 * time.Second
	reviewTimeout      = 60 * time.Second
	maxResponseBytes   = 32 << 20
)

var (
	ErrMissingAPIKey = errors.New("GEMINI_API_KEY is not set")
	ErrEmptyResponse = errors.New("gemini returned no content")
	// ErrBlocked reports a prompt or response withheld by Gemini's safety filters.
	ErrBlocked = errors.New("gemini blocked the request")
	// ErrNoImage reports that Gemini's response contained no inline image data.
	ErrNoImage = errors.New("gemini returned no image")
)

// Error is a non-2xx Gemini response.
type Error struct {
	Status int
	Body   string
}

func (e *Error) Error() string { return fmt.Sprintf("gemini responded %d: %s", e.Status, e.Body) }

// Transient reports whether retrying later can succeed (rate limits, server errors).
func (e *Error) Transient() bool { return e.Status == http.StatusTooManyRequests || e.Status >= 500 }

type Client struct {
	apiKey      string
	textModel   string
	imageModel  string
	visionModel string
	baseURL     string
	http        *http.Client
}

type Option func(*Client)

func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(baseURL, "/") }
}

func WithHTTPClient(client *http.Client) Option { return func(c *Client) { c.http = client } }

// WithImageModel overrides the model used by GenerateImage.
func WithImageModel(model string) Option {
	return func(c *Client) {
		if model != "" {
			c.imageModel = model
		}
	}
}

// WithVisionModel overrides the model used by ReviewImage.
func WithVisionModel(model string) Option {
	return func(c *Client) {
		if model != "" {
			c.visionModel = model
		}
	}
}

func New(apiKey, textModel string, options ...Option) *Client {
	if textModel == "" {
		textModel = DefaultTextModel
	}
	client := &Client{
		apiKey:      apiKey,
		textModel:   textModel,
		imageModel:  DefaultImageModel,
		visionModel: DefaultVisionModel,
		baseURL:     DefaultBaseURL,
		http:        &http.Client{},
	}
	for _, option := range options {
		option(client)
	}
	return client
}

type part struct {
	Text string `json:"text,omitempty"`
}

// InlineImage is an image part sent with a prompt.
type InlineImage struct {
	MIMEType string
	Data     []byte
}

type inlinePart struct {
	Text       string          `json:"text,omitempty"`
	InlineData *inlineDataJSON `json:"inlineData,omitempty"`
}

type inlineDataJSON struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

type imageResponse struct {
	Candidates []struct {
		FinishReason string `json:"finishReason"`
		Content      struct {
			Parts []struct {
				Text       string `json:"text"`
				InlineData *struct {
					Data string `json:"data"`
				} `json:"inlineData"`
				InlineData2 *struct {
					Data string `json:"data"`
				} `json:"inline_data"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
}

type generateResponse struct {
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	Candidates []struct {
		Content struct {
			Parts []part `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
}

// GenerateJSON sends a text prompt with the Node importer's settings
// (JSON output, 4096 tokens, temperature 0.4) and returns the joined text.
func (c *Client) GenerateJSON(ctx context.Context, prompt string) (string, error) {
	payload := map[string]any{
		"contents": []map[string]any{{"parts": []part{{Text: prompt}}}},
		"generationConfig": map[string]any{
			"maxOutputTokens":  4096,
			"responseMimeType": "application/json",
			"temperature":      0.4,
		},
	}
	ctx, cancel := context.WithTimeout(ctx, textTimeout)
	defer cancel()
	var response generateResponse
	if err := c.generate(ctx, c.textModel, payload, &response); err != nil {
		return "", err
	}
	if reason := response.PromptFeedback.BlockReason; reason != "" {
		return "", fmt.Errorf("%w: prompt %s", ErrBlocked, reason)
	}
	if len(response.Candidates) == 0 {
		return "", ErrEmptyResponse
	}
	var text strings.Builder
	for _, p := range response.Candidates[0].Content.Parts {
		text.WriteString(p.Text)
	}
	if strings.TrimSpace(text.String()) == "" {
		if reason := response.Candidates[0].FinishReason; reason == "SAFETY" {
			return "", fmt.Errorf("%w: response %s", ErrBlocked, reason)
		}
		return "", ErrEmptyResponse
	}
	return strings.TrimSpace(text.String()), nil
}

// GenerateImage ports requestIllustration: 16:9, 2K, text+image modalities,
// optional inline reference. Returns the first inline image's bytes.
func (c *Client) GenerateImage(ctx context.Context, prompt string, reference *InlineImage) ([]byte, error) {
	parts := []inlinePart{{Text: prompt}}
	if reference != nil {
		parts = append(parts, inlinePart{InlineData: &inlineDataJSON{MIMEType: reference.MIMEType, Data: base64.StdEncoding.EncodeToString(reference.Data)}})
	}
	payload := map[string]any{
		"contents": []map[string]any{{"role": "user", "parts": parts}},
		"generationConfig": map[string]any{
			"responseModalities": []string{"TEXT", "IMAGE"},
			"imageConfig":        map[string]any{"aspectRatio": "16:9", "imageSize": "2K"},
		},
	}
	ctx, cancel := context.WithTimeout(ctx, imageTimeout)
	defer cancel()
	var response imageResponse
	if err := c.generate(ctx, c.imageModel, payload, &response); err != nil {
		return nil, err
	}
	finish := ""
	for _, candidate := range response.Candidates {
		if finish == "" {
			finish = candidate.FinishReason
		}
		for _, part := range candidate.Content.Parts {
			encoded := ""
			if part.InlineData != nil {
				encoded = part.InlineData.Data
			} else if part.InlineData2 != nil {
				encoded = part.InlineData2.Data
			}
			if encoded == "" {
				continue
			}
			if data, err := base64.StdEncoding.DecodeString(encoded); err == nil && len(data) > 0 {
				return data, nil
			}
		}
	}
	return nil, fmt.Errorf("%w (blocked: %q, finishReason: %q)", ErrNoImage, response.PromptFeedback.BlockReason, finish)
}

// ReviewImage sends a JPEG with a prompt to the vision model (temperature 0,
// JSON output constrained by schema) and returns the joined text.
func (c *Client) ReviewImage(ctx context.Context, prompt string, jpeg []byte, schema map[string]any) (string, error) {
	payload := map[string]any{
		"contents": []map[string]any{{"role": "user", "parts": []inlinePart{
			{Text: prompt},
			{InlineData: &inlineDataJSON{MIMEType: "image/jpeg", Data: base64.StdEncoding.EncodeToString(jpeg)}},
		}}},
		"generationConfig": map[string]any{"temperature": 0, "responseMimeType": "application/json", "responseSchema": schema},
	}
	ctx, cancel := context.WithTimeout(ctx, reviewTimeout)
	defer cancel()
	var response generateResponse
	if err := c.generate(ctx, c.visionModel, payload, &response); err != nil {
		return "", err
	}
	if len(response.Candidates) == 0 {
		return "", ErrEmptyResponse
	}
	var text strings.Builder
	for _, p := range response.Candidates[0].Content.Parts {
		text.WriteString(p.Text)
	}
	if strings.TrimSpace(text.String()) == "" {
		return "", ErrEmptyResponse
	}
	return strings.TrimSpace(text.String()), nil
}

// generate posts to models/{model}:generateContent and decodes the response.
func (c *Client) generate(ctx context.Context, model string, payload any, into any) error {
	if c.apiKey == "" {
		return ErrMissingAPIKey
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/models/"+model+":generateContent", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("gemini request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read gemini response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		detail := data
		if len(detail) > 300 {
			detail = detail[:300]
		}
		return &Error{Status: resp.StatusCode, Body: strings.ToValidUTF8(string(detail), "")}
	}
	if err := json.Unmarshal(data, into); err != nil {
		return fmt.Errorf("decode gemini response: %w", err)
	}
	return nil
}
