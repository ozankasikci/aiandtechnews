package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
)

const (
	// DefaultBaseURL is the Bot API's address.
	DefaultBaseURL = "https://api.telegram.org"

	requestTimeout = 60 * time.Second
	// maxImageBytes caps the featured image download (Telegram takes photos
	// up to 10 MB).
	maxImageBytes = 10 << 20
	// photoMaxEdge and photoQuality shape the JPEG a WebP image becomes.
	photoMaxEdge = 2560
	photoQuality = 88
	// maxResponseBytes caps a Bot API response body.
	maxResponseBytes = 1 << 20
)

// Error is a failed Bot API call or a failed request to it. Its text never
// contains the bot token.
type Error struct {
	Method string
	// StatusCode is the HTTP status; 0 when no response arrived.
	StatusCode int
	// Description is Telegram's description of the failure, if any.
	Description string
	// RetryAfter is Telegram's parameters.retry_after on a 429.
	RetryAfter time.Duration
	message    string
}

func (e *Error) Error() string { return e.message }

// Transient reports whether the call may succeed if simply tried again
// later: no response, a 429 (flood control) or a 5xx.
func (e *Error) Transient() bool {
	return e.StatusCode == 0 || e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500
}

// IsTransient reports whether err is a transient *Error.
func IsTransient(err error) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.Transient()
}

// RetryAfter is the wait Telegram asked for, or 0.
func RetryAfter(err error) time.Duration {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.RetryAfter
	}
	return 0
}

// Client calls the Telegram Bot API. The token is part of every request
// path, so it is scrubbed from every error the client returns.
type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

// NewClient builds a client; baseURL "" means DefaultBaseURL and a nil
// httpClient one with a 60s timeout. Tests point baseURL at a fake server.
func NewClient(token, baseURL string, httpClient *http.Client) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	return &Client{token: token, baseURL: strings.TrimRight(baseURL, "/"), http: httpClient}
}

// Post sends text to chat as a photo caption when imageURL is a usable
// image, and as a plain message otherwise: when there is no image, when the
// download or JPEG conversion fails, or when Telegram rejects the photo
// itself (a 400). A transient sendPhoto failure is returned, not downgraded.
// It returns the new message's id.
func (c *Client) Post(ctx context.Context, chat, text, imageURL string) (int64, error) {
	if imageURL != "" {
		photo, name, err := c.photo(ctx, imageURL)
		if err == nil {
			id, err := c.SendPhoto(ctx, chat, photo, name, text)
			if err == nil || IsTransient(err) || !isBadRequest(err) {
				return id, err
			}
		}
	}
	return c.SendMessage(ctx, chat, text)
}

func isBadRequest(err error) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest
}

// photo downloads an image and returns bytes Telegram accepts as a photo:
// JPEG and PNG as they are, WebP re-encoded as JPEG.
func (c *Client) photo(ctx context.Context, imageURL string) ([]byte, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("image request: %w", err)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("download image: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download image: status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxImageBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("download image: %w", err)
	}
	if len(data) > maxImageBytes {
		return nil, "", errors.New("download image: larger than 10 MB")
	}
	switch imaging.SniffMIME(data) {
	case "image/jpeg":
		return data, "photo.jpg", nil
	case "image/png":
		return data, "photo.png", nil
	case "image/webp":
		jpeg, err := imaging.FitJPEG(data, photoMaxEdge, photoQuality)
		if err != nil {
			return nil, "", fmt.Errorf("convert WebP: %w", err)
		}
		return jpeg, "photo.jpg", nil
	}
	return nil, "", errors.New("image is not JPEG, PNG or WebP")
}

// SendPhoto uploads photo with an HTML caption (multipart sendPhoto).
func (c *Client) SendPhoto(ctx context.Context, chat string, photo []byte, filename, caption string) (int64, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for _, field := range [][2]string{{"chat_id", chat}, {"caption", caption}, {"parse_mode", "HTML"}} {
		if err := form.WriteField(field[0], field[1]); err != nil {
			return 0, err
		}
	}
	part, err := form.CreateFormFile("photo", filename)
	if err != nil {
		return 0, err
	}
	if _, err := part.Write(photo); err != nil {
		return 0, err
	}
	if err := form.Close(); err != nil {
		return 0, err
	}
	return c.call(ctx, "sendPhoto", form.FormDataContentType(), &body)
}

// SendMessage sends an HTML text message; the link preview stays on.
func (c *Client) SendMessage(ctx context.Context, chat, text string) (int64, error) {
	values := url.Values{
		"chat_id":                  {chat},
		"text":                     {text},
		"parse_mode":               {"HTML"},
		"disable_web_page_preview": {"false"},
	}
	return c.call(ctx, "sendMessage", "application/x-www-form-urlencoded", strings.NewReader(values.Encode()))
}

type apiResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	Result      struct {
		MessageID int64 `json:"message_id"`
	} `json:"result"`
	Parameters struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

func (c *Client) call(ctx context.Context, method, contentType string, body io.Reader) (int64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/bot"+c.token+"/"+method, body)
	if err != nil {
		return 0, c.fail(method, 0, "", 0, err.Error())
	}
	request.Header.Set("Content-Type", contentType)
	response, err := c.http.Do(request)
	if err != nil {
		return 0, c.fail(method, 0, "", 0, err.Error())
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return 0, c.fail(method, 0, "", 0, "read response: "+err.Error())
	}
	var decoded apiResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		status := response.StatusCode
		if status == http.StatusOK {
			status = http.StatusBadGateway // an unreadable 200 is the gateway's fault
		}
		return 0, c.fail(method, status, "", 0, fmt.Sprintf("status %d, unreadable response", response.StatusCode))
	}
	if !decoded.OK || response.StatusCode != http.StatusOK {
		retryAfter := time.Duration(decoded.Parameters.RetryAfter) * time.Second
		message := "status " + strconv.Itoa(response.StatusCode)
		if decoded.Description != "" {
			message += ": " + decoded.Description
		}
		if retryAfter > 0 {
			message += fmt.Sprintf(" (retry after %s)", retryAfter)
		}
		status := response.StatusCode
		if status == http.StatusOK {
			status = http.StatusBadRequest // ok:false with a 200 is a refusal
		}
		return 0, c.fail(method, status, decoded.Description, retryAfter, message)
	}
	if decoded.Result.MessageID == 0 {
		return 0, c.fail(method, http.StatusBadGateway, "", 0, "response has no message_id")
	}
	return decoded.Result.MessageID, nil
}

func (c *Client) fail(method string, status int, description string, retryAfter time.Duration, message string) *Error {
	return &Error{
		Method: method, StatusCode: status, Description: c.scrub(description), RetryAfter: retryAfter,
		message: "telegram " + method + ": " + c.scrub(message),
	}
}

// scrub removes the bot token (it is in every request URL, so transport
// errors quote it).
func (c *Client) scrub(text string) string {
	if c.token == "" {
		return text
	}
	return strings.ReplaceAll(text, c.token, "[REDACTED]")
}
