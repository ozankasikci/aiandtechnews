package newsletter

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type recordedRequest struct {
	method  string
	path    string
	headers http.Header
	body    string
}

// scriptedResend is an httptest stand-in for the Resend API. It never
// forwards anything anywhere.
type scriptedResend struct {
	mu        sync.Mutex
	responses []scriptedResponse
	requests  []recordedRequest
}

type scriptedResponse struct {
	status     int
	body       string
	retryAfter *string
	disconnect bool
}

func (s *scriptedResend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.requests = append(s.requests, recordedRequest{method: r.Method, path: r.URL.Path, headers: r.Header.Clone(), body: string(body)})
	next := s.responses[min(len(s.requests)-1, len(s.responses)-1)]
	s.mu.Unlock()
	if next.disconnect {
		connection, _, err := http.NewResponseController(w).Hijack()
		if err == nil {
			_ = connection.Close()
		}
		return
	}
	if next.retryAfter != nil {
		w.Header().Set("Retry-After", *next.retryAfter)
	}
	w.WriteHeader(next.status)
	_, _ = io.WriteString(w, next.body)
}

// snapshot returns the recorded requests under the lock (the handler runs
// on the server's goroutines).
func (s *scriptedResend) snapshot() []recordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recordedRequest(nil), s.requests...)
}

func startResend(t *testing.T, responses ...scriptedResponse) (*scriptedResend, *httptest.Server) {
	t.Helper()
	fake := &scriptedResend{responses: responses}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return fake, server
}

// orderedHeaders restores Node's header order (object key order), which a
// Go map cannot carry.
func orderedHeaders(headers map[string]string) []Header {
	if headers == nil {
		return nil
	}
	ordered := []Header{}
	for _, name := range []string{"List-Unsubscribe", "List-Unsubscribe-Post"} {
		if value, ok := headers[name]; ok {
			ordered = append(ordered, Header{Name: name, Value: value})
		}
	}
	return ordered
}

func emailFromNode(email nodeEmail) Email {
	var tags []Tag
	if email.Tags != nil {
		tags = []Tag{}
		for _, tag := range email.Tags {
			tags = append(tags, Tag{Name: tag.Name, Value: tag.Value})
		}
	}
	return Email{To: email.To, Subject: email.Subject, HTML: email.HTML, Text: email.Text, Headers: orderedHeaders(email.Headers), Tags: tags}
}

func TestResendSenderMatchesNodeRequestsRetriesAndErrors(t *testing.T) {
	cases := golden(t).Sender
	if len(cases) < 30 {
		t.Fatalf("recorded sender cases = %d", len(cases))
	}
	for _, vector := range cases {
		t.Run(vector.Name, func(t *testing.T) {
			var responses []scriptedResponse
			for _, scripted := range vector.Scripted {
				response := scriptedResponse{status: scripted.Status, retryAfter: scripted.RetryAfter, disconnect: scripted.Throws != nil}
				if scripted.Body != nil {
					response.body = *scripted.Body
				}
				responses = append(responses, response)
			}
			fake, server := startResend(t, responses...)
			var delays []float64
			sender := NewResendSender(ResendConfig{
				APIKey: vector.Env["RESEND_API_KEY"], From: vector.Env["NEWSLETTER_FROM"], ReplyTo: vector.Env["NEWSLETTER_REPLY_TO"],
				Endpoint: server.URL + "/emails", Client: server.Client(),
				Sleep: func(_ context.Context, delay time.Duration) error {
					delays = append(delays, float64(delay)/float64(time.Millisecond))
					return nil
				},
			})
			id, err := sender.Send(context.Background(), emailFromNode(vector.Email), vector.IdempotencyKey)

			requests := fake.snapshot()
			if len(requests) != len(vector.Calls) {
				t.Fatalf("requests = %d, want %d", len(requests), len(vector.Calls))
			}
			for i, call := range vector.Calls {
				got := requests[i]
				if call.URL != DefaultResendEndpoint || got.method != call.Method || got.path != "/emails" {
					t.Errorf("request %d = %s %s (Node: %s %s)", i, got.method, got.path, call.Method, call.URL)
				}
				for name, value := range call.Headers {
					if got.headers.Get(name) != value {
						t.Errorf("request %d header %s = %q, want %q", i, name, got.headers.Get(name), value)
					}
				}
				if got.body != call.Body {
					t.Errorf("request %d body differs at byte %d\ngot:  %s\nwant: %s", i, firstDifference(got.body, call.Body), got.body, call.Body)
				}
			}
			if len(delays) != len(vector.Delays) {
				t.Fatalf("delays = %v, want %v", delays, vector.Delays)
			}
			for i := range delays {
				if delays[i] != vector.Delays[i] {
					t.Errorf("delay %d = %vms, want %vms", i, delays[i], vector.Delays[i])
				}
			}
			switch {
			case vector.Result != nil:
				if err != nil || id != vector.Result.ID {
					t.Fatalf("Send = %q, %v, want %q", id, err, vector.Result.ID)
				}
			case vector.Error != nil:
				if err == nil {
					t.Fatalf("Send = %q, want error %q", id, vector.Error.Message)
				}
				if err.Error() != vector.Error.Message {
					t.Errorf("error = %q, want %q", err.Error(), vector.Error.Message)
				}
				var configuration *ConfigurationError
				if errors.As(err, &configuration) != vector.Error.Configuration {
					t.Errorf("configuration error = %t, want %t", !vector.Error.Configuration, vector.Error.Configuration)
				}
			}
		})
	}
}

func TestResendSenderStopsWaitingWhenTheContextEnds(t *testing.T) {
	retryAfter := "3600"
	_, server := startResend(t, scriptedResponse{status: 503, retryAfter: &retryAfter})
	sender := NewResendSender(ResendConfig{APIKey: "re_synthetic", From: "News <news@example.invalid>", Endpoint: server.URL, Client: server.Client()})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := sender.Send(ctx, Email{To: "a@example.invalid"}, "key")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 5*time.Second {
		t.Fatalf("Send = %v after %v, want the context error promptly", err, time.Since(started))
	}
}

// The expected delays were checked in Node 22: Number() of each value,
// Headers.get joining repeated headers with ", ", and setTimeout clamping
// (TimeoutOverflowWarning above 2^31-1 ms, at least 1ms).
func TestResendRetryDelayFollowsNodeTimers(t *testing.T) {
	for _, tt := range []struct {
		retryAfter []string
		attempt    int
		want       time.Duration
	}{
		{nil, 0, 650 * time.Millisecond},
		{nil, 1, 1300 * time.Millisecond},
		{[]string{"1", "2"}, 0, 650 * time.Millisecond}, // Headers.get joins: "1, 2" is NaN
		{[]string{"0.0001"}, 0, time.Millisecond},       // setTimeout clamps below 1ms to 1ms
		{[]string{"3000000"}, 0, time.Millisecond},      // beyond 2^31-1 ms Node fires after 1ms
		{[]string{"2147483.647"}, 0, 2147483647 * time.Millisecond},
		{[]string{"+2"}, 0, 2 * time.Second},
		{[]string{"-0x2"}, 0, 650 * time.Millisecond},
		{[]string{"0b11"}, 0, 3 * time.Second},
		{[]string{"0o7"}, 0, 7 * time.Second},
		{[]string{"\u00a02\u2028"}, 0, 2 * time.Second},
	} {
		header := http.Header{}
		for _, value := range tt.retryAfter {
			header.Add("Retry-After", value)
		}
		if got := retryDelay(header, tt.attempt); got != tt.want {
			t.Errorf("retryDelay(%q, %d) = %v, want %v", tt.retryAfter, tt.attempt, got, tt.want)
		}
	}
}

func TestNewResendSenderHasBoundedTimeouts(t *testing.T) {
	sender := NewResendSender(ResendConfig{})
	if sender.endpoint != DefaultResendEndpoint || sender.client.Timeout != ResendAttemptTimeout || ResendAttemptTimeout > time.Minute {
		t.Fatalf("defaults = %q, %v", sender.endpoint, sender.client.Timeout)
	}
}
