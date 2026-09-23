// Package gemini calls the Gemini generateContent API.
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultBaseURL   = "https://generativelanguage.googleapis.com/v1beta"
	DefaultTextModel = "gemini-3.5-flash-lite"
	textTimeout      = 90 * time.Second
	maxResponseBytes = 32 << 20
)

var (
	ErrMissingAPIKey = errors.New("GEMINI_API_KEY is not set")
	ErrEmptyResponse = errors.New("gemini returned no content")
	// ErrBlocked reports a prompt or response withheld by Gemini's safety filters.
	ErrBlocked = errors.New("gemini blocked the request")
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
	apiKey    string
	textModel string
	baseURL   string
	http      *http.Client
}

type Option func(*Client)

func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(baseURL, "/") }
}

func WithHTTPClient(client *http.Client) Option { return func(c *Client) { c.http = client } }

func New(apiKey, textModel string, options ...Option) *Client {
	if textModel == "" {
		textModel = DefaultTextModel
	}
	client := &Client{apiKey: apiKey, textModel: textModel, baseURL: DefaultBaseURL, http: &http.Client{}}
	for _, option := range options {
		option(client)
	}
	return client
}

type part struct {
	Text string `json:"text,omitempty"`
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
