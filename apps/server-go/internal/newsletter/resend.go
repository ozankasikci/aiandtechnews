package newsletter

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/jsonbody"
)

const (
	// DefaultResendEndpoint is the URL Node posts every email to.
	DefaultResendEndpoint = "https://api.resend.com/emails"
	// ResendAttemptTimeout bounds one HTTP attempt (connect, request, and
	// response). Node's fetch has no deadline of its own.
	ResendAttemptTimeout = 30 * time.Second
	// resendAttempts is Node's retry budget: three attempts in total.
	resendAttempts = 3
	// maxResendResponseBytes caps how much of a response body is read.
	maxResendResponseBytes = 1 << 20
	// maxTimerDelay is Node's TIMEOUT_MAX (2^31-1 ms); setTimeout fires
	// after 1ms instead of any longer delay.
	maxTimerDelay = 2147483647
)

// ConfigurationError is Node's NewsletterConfigurationError: the newsletter
// is missing a setting it needs for this operation.
type ConfigurationError struct{ message string }

func (e *ConfigurationError) Error() string { return e.message }

var errDeliveryNotConfigured = &ConfigurationError{message: "Newsletter delivery is not configured"}

// fetchError is a transport failure. Node's fetch rejects with
// TypeError("fetch failed") and the sender does not retry it; the message
// stored for the delivery is the same, the cause is kept for logs.
type fetchError struct{ cause error }

func (e *fetchError) Error() string { return "fetch failed" }
func (e *fetchError) Unwrap() error { return e.cause }

// providerError is a Resend error response, with Node's message.
type providerError struct {
	status  int
	message string
}

func (e *providerError) Error() string { return e.message }

// ResendConfig holds the raw settings; like Node they are trimmed on use.
type ResendConfig struct {
	APIKey  string // RESEND_API_KEY
	From    string // NEWSLETTER_FROM
	ReplyTo string // NEWSLETTER_REPLY_TO
	// Endpoint defaults to DefaultResendEndpoint. Tests point it at an
	// httptest server; nothing else should change it.
	Endpoint string
	// Client defaults to a client with ResendAttemptTimeout.
	Client *http.Client
	// Sleep waits between attempts; it defaults to a context-aware timer.
	Sleep func(context.Context, time.Duration) error
}

// ResendSender is Node's createResendSender.
type ResendSender struct {
	apiKey, from, replyTo string
	endpoint              string
	client                *http.Client
	sleep                 func(context.Context, time.Duration) error
}

func NewResendSender(cfg ResendConfig) *ResendSender {
	sender := &ResendSender{
		apiKey: jsTrim(cfg.APIKey), from: jsTrim(cfg.From), replyTo: jsTrim(cfg.ReplyTo),
		endpoint: cfg.Endpoint, client: cfg.Client, sleep: cfg.Sleep,
	}
	if sender.endpoint == "" {
		sender.endpoint = DefaultResendEndpoint
	}
	if sender.client == nil {
		sender.client = &http.Client{Timeout: ResendAttemptTimeout}
	}
	if sender.sleep == nil {
		sender.sleep = sleepContext
	}
	return sender
}

// Send posts email with Node's request body, headers, and Idempotency-Key.
// Like Node it makes at most three attempts, retrying only 429 and 5xx
// responses after Retry-After seconds (or 650ms, then 1300ms); transport
// failures, other statuses, and a 2xx without an id fail at once.
func (s *ResendSender) Send(ctx context.Context, email Email, idempotencyKey string) (string, error) {
	if s.apiKey == "" || s.from == "" {
		return "", errDeliveryNotConfigured
	}
	body := s.requestBody(email)
	for attempt := 0; attempt < resendAttempts; attempt++ {
		status, header, response, err := s.post(ctx, body, idempotencyKey)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", &fetchError{cause: err}
		}
		ok := status >= 200 && status <= 299
		if ok {
			if response.null {
				return "", &providerError{status: status, message: "Cannot read properties of null (reading 'id')"}
			}
			if id := response.field("id"); id.Truthy() {
				return id.JSString(), nil
			}
		}
		if retryable := status == http.StatusTooManyRequests || status >= 500; !retryable || attempt == resendAttempts-1 {
			return "", &providerError{status: status, message: response.errorMessage(status)}
		}
		if err := s.sleep(ctx, retryDelay(header, attempt)); err != nil {
			return "", err
		}
	}
	return "", &providerError{message: "Email provider retry limit reached"}
}

// requestBody is JSON.stringify of Node's request object, key order included.
func (s *ResendSender) requestBody(email Email) string {
	var body strings.Builder
	field := func(name string) {
		if body.Len() > 1 {
			body.WriteByte(',')
		}
		writeJSONString(&body, name)
		body.WriteByte(':')
	}
	body.WriteByte('{')
	field("from")
	writeJSONString(&body, s.from)
	field("to")
	body.WriteByte('[')
	writeJSONString(&body, email.To)
	body.WriteByte(']')
	field("subject")
	writeJSONString(&body, email.Subject)
	field("html")
	writeJSONString(&body, email.HTML)
	field("text")
	writeJSONString(&body, email.Text)
	if s.replyTo != "" {
		field("reply_to")
		writeJSONString(&body, s.replyTo)
	}
	if email.Headers != nil {
		field("headers")
		body.WriteByte('{')
		for i, header := range email.Headers {
			if i > 0 {
				body.WriteByte(',')
			}
			writeJSONString(&body, header.Name)
			body.WriteByte(':')
			writeJSONString(&body, header.Value)
		}
		body.WriteByte('}')
	}
	if email.Tags != nil {
		field("tags")
		body.WriteByte('[')
		for i, tag := range email.Tags {
			if i > 0 {
				body.WriteByte(',')
			}
			body.WriteString(`{"name":`)
			writeJSONString(&body, tag.Name)
			body.WriteString(`,"value":`)
			writeJSONString(&body, tag.Value)
			body.WriteByte('}')
		}
		body.WriteByte(']')
	}
	body.WriteByte('}')
	return body.String()
}

func (s *ResendSender) post(ctx context.Context, body, idempotencyKey string) (int, http.Header, resendResponse, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, ResendAttemptTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, s.endpoint, strings.NewReader(body))
	if err != nil {
		return 0, nil, resendResponse{}, err
	}
	request.Header.Set("Authorization", "Bearer "+s.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", idempotencyKey)
	response, err := s.client.Do(request)
	if err != nil {
		return 0, nil, resendResponse{}, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResendResponseBytes))
	if err != nil {
		return 0, nil, resendResponse{}, err
	}
	return response.StatusCode, response.Header, parseResendResponse(data), nil
}

// resendResponse is `await response.json().catch(() => ({}))`: a body that
// is not JSON reads as {}; JSON null stays null (property reads on it throw
// in Node); any other non-object value has no properties.
type resendResponse struct {
	null   bool
	fields map[string]json.RawMessage
}

func parseResendResponse(data []byte) resendResponse {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return resendResponse{}
	}
	if value == nil {
		return resendResponse{null: true}
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return resendResponse{}
	}
	return resendResponse{fields: fields}
}

func (r resendResponse) field(name string) jsonbody.Value {
	raw, ok := r.fields[name]
	if !ok {
		return jsonbody.Value{}
	}
	return jsonbody.FromRaw(raw)
}

// errorMessage is new Error(body.message || body.error || `Email provider
// returned ${status}`).message.
func (r resendResponse) errorMessage(status int) string {
	if r.null {
		return "Cannot read properties of null (reading 'message')"
	}
	for _, name := range []string{"message", "error"} {
		if value := r.field(name); value.Truthy() {
			return value.JSString()
		}
	}
	return "Email provider returned " + strconv.Itoa(status)
}

// retryDelay is Node's wait before the next attempt: Number(Retry-After)
// seconds when finite and positive, else 650ms times the attempt number,
// with setTimeout's clamping (below 1ms or above 2^31-1 ms fires after 1ms).
func retryDelay(header http.Header, attempt int) time.Duration {
	milliseconds := 650 * float64(attempt+1)
	if values := header.Values("Retry-After"); len(values) > 0 {
		if seconds, ok := jsNumber(strings.Join(values, ", ")); ok && !math.IsInf(seconds, 0) && seconds > 0 {
			milliseconds = seconds * 1000
		}
	}
	if milliseconds < 1 || milliseconds > maxTimerDelay {
		milliseconds = 1
	}
	return time.Duration(milliseconds * float64(time.Millisecond))
}

var jsDecimalLiteral = regexp.MustCompile(`^[+-]?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

// jsNumber is Number(value) for a string; ok is false for NaN.
func jsNumber(value string) (float64, bool) {
	value = jsTrim(value)
	if value == "" {
		return 0, true
	}
	switch value {
	case "Infinity", "+Infinity":
		return math.Inf(1), true
	case "-Infinity":
		return math.Inf(-1), true
	}
	if len(value) > 2 && value[0] == '0' {
		base := 0
		switch value[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			integer, ok := new(big.Int).SetString(value[2:], base)
			if !ok || strings.ContainsAny(value[2:], "_+-") {
				return 0, false
			}
			parsed, _ := new(big.Float).SetInt(integer).Float64()
			return parsed, true
		}
	}
	if !jsDecimalLiteral.MatchString(value) {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil && !math.IsInf(parsed, 0) {
		return 0, false
	}
	return parsed, true
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
