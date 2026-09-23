package newsletter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

const testCronSecret = "synthetic-cron-secret"

// fakeService lets HTTP tests choose each use case's outcome.
type fakeService struct {
	mu            sync.Mutex
	subscribeErr  error
	subscribed    []string
	confirmResult ConfirmationResult
	confirmErr    error
	tokens        []string
	unsubscribe   UnsubscribeState
	unsubErr      error
	limits        []float64
	editions      []Edition
	editionsErr   error
	editionKeys   []string
	edition       *Edition
	digest        DigestResult
	digestErr     error
}

func (f *fakeService) Subscribe(_ context.Context, email, placement string, _ time.Time) (SubscriptionState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subscribed = append(f.subscribed, email+"|"+placement)
	if f.subscribeErr != nil {
		return "", f.subscribeErr
	}
	if email == "already@example.com" {
		return StateAlreadyActive, nil
	}
	if email == "" || !strings.Contains(email, "@") {
		return "", ErrInvalidEmail
	}
	return StateSubscribed, nil
}

func (f *fakeService) Confirm(_ context.Context, token string, _ time.Time) (ConfirmationResult, error) {
	f.tokens = append(f.tokens, token)
	return f.confirmResult, f.confirmErr
}

func (f *fakeService) Unsubscribe(_ context.Context, token string, _ time.Time) (UnsubscribeState, error) {
	f.tokens = append(f.tokens, token)
	return f.unsubscribe, f.unsubErr
}

func (f *fakeService) ListEditions(_ context.Context, limit float64) ([]Edition, error) {
	f.limits = append(f.limits, limit)
	return f.editions, f.editionsErr
}

func (f *fakeService) Edition(_ context.Context, key string) (Edition, bool, error) {
	f.editionKeys = append(f.editionKeys, key)
	if f.edition == nil {
		return Edition{}, false, f.editionsErr
	}
	return *f.edition, true, nil
}

func (f *fakeService) SendDailyDigest(_ context.Context, _ time.Time) (DigestResult, error) {
	return f.digest, f.digestErr
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func router(handler *Handler) http.Handler {
	mux := chi.NewRouter()
	mux.Route("/api", handler.Mount)
	return mux
}

func serve(t *testing.T, handler http.Handler, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertResponse(t *testing.T, response *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	if response.Code != status || response.Body.String() != body {
		t.Fatalf("response = %d %s, want %d %s", response.Code, response.Body.String(), status, body)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
}

func newHandler(service newsletterService, now *clock) *Handler {
	return NewHandler(service, "  "+testCronSecret+"\n", now.Now, discardLogger())
}

func TestSubscribeRespondsLikeNode(t *testing.T) {
	now := &clock{now: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)}
	service := &fakeService{}
	handler := router(newHandler(service, now))
	assertResponse(t, serve(t, handler, "POST", "/api/subscribe", `{"email":"reader@example.com","placement":"inline"}`, nil),
		200, `{"success":true,"state":"subscribed","message":"You're subscribed. The next digest will arrive in your inbox."}`)
	assertResponse(t, serve(t, handler, "POST", "/api/subscribe", `{"email":"already@example.com"}`, nil),
		200, `{"success":true,"state":"already_active","message":"You're already subscribed."}`)
	// Non-string values read as "" and "unknown", like typeof checks in Node.
	assertResponse(t, serve(t, handler, "POST", "/api/subscribe", `{"email":5,"placement":["x"]}`, nil), 400, `{"error":"Valid email required"}`)
	assertResponse(t, serve(t, handler, "POST", "/api/subscribe", `[1]`, nil), 400, `{"error":"Valid email required"}`)
	if got := service.subscribed[len(service.subscribed)-2:]; got[0] != "|unknown" || got[1] != "|unknown" {
		t.Errorf("service saw %v", got)
	}
	assertResponse(t, serve(t, handler, "POST", "/api/subscribe", `{"email":`, nil), 400, `{"error":"Invalid request body"}`)

	service.subscribeErr = errors.New("database is locked")
	assertResponse(t, serve(t, handler, "POST", "/api/subscribe", `{"email":"a@b.c"}`, nil), 502, `{"error":"We could not complete your signup. Please try again."}`)
	service.subscribeErr = &ConfigurationError{message: "x"}
	assertResponse(t, serve(t, handler, "POST", "/api/subscribe", `{"email":"a@b.c"}`, nil), 503, `{"error":"Newsletter signup is temporarily unavailable"}`)
}

func TestSignupThrottleIsNodesGlobalRollingMinute(t *testing.T) {
	now := &clock{now: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)}
	service := &fakeService{}
	handler := router(newHandler(service, now))
	// Malformed bodies never reach the route, so they are not counted.
	for i := 0; i < 5; i++ {
		serve(t, handler, "POST", "/api/subscribe", `{`, nil)
	}
	// Invalid addresses are counted.
	for i := 0; i < 30; i++ {
		if response := serve(t, handler, "POST", "/api/subscribe", `{"email":"nope"}`, nil); response.Code != 400 {
			t.Fatalf("attempt %d = %d", i, response.Code)
		}
		now.Advance(time.Second)
	}
	// 30 attempts at t=0..29s: the 31st at t=30s is refused.
	assertResponse(t, serve(t, handler, "POST", "/api/subscribe", `{"email":"a@b.c"}`, nil), 429, `{"error":"Too many signup attempts. Please try again shortly."}`)
	// At t=60s the attempt from t=0 is still inside the window (not older
	// than 60s); at t=60.001s it has expired.
	now.Advance(30 * time.Second)
	if response := serve(t, handler, "POST", "/api/subscribe", `{"email":"a@b.c"}`, nil); response.Code != 429 {
		t.Fatalf("at the window edge = %d, want 429", response.Code)
	}
	now.Advance(time.Millisecond)
	if response := serve(t, handler, "POST", "/api/subscribe", `{"email":"a@b.c"}`, nil); response.Code != 200 {
		t.Fatalf("after the window = %d, want 200", response.Code)
	}
}

func TestConfirmRespondsLikeNode(t *testing.T) {
	now := &clock{now: time.Now()}
	service := &fakeService{confirmResult: ConfirmationResult{State: ConfirmationConfirmed, WelcomeSent: true}}
	handler := router(newHandler(service, now))
	assertResponse(t, serve(t, handler, "GET", "/api/newsletter/confirm?token=abc.def", "", nil), 200, `{"state":"confirmed","welcomeSent":true}`)
	// A missing or repeated token is "" (typeof req.query.token !== "string").
	serve(t, handler, "GET", "/api/newsletter/confirm", "", nil)
	serve(t, handler, "GET", "/api/newsletter/confirm?token=a&token=b", "", nil)
	if got := service.tokens; len(got) != 3 || got[0] != "abc.def" || got[1] != "" || got[2] != "" {
		t.Errorf("tokens = %q", got)
	}
	service.confirmErr = &ConfigurationError{message: "x"}
	assertResponse(t, serve(t, handler, "GET", "/api/newsletter/confirm?token=a", "", nil), 503, `{"state":"unavailable","error":"Newsletter confirmation is temporarily unavailable"}`)
	service.confirmErr = errors.New("disk I/O error")
	assertResponse(t, serve(t, handler, "GET", "/api/newsletter/confirm?token=a", "", nil), 500, `{"state":"invalid","error":"Confirmation failed"}`)
}

func TestUnsubscribeTakesTheQueryTokenThenTheBodyTokenLikeNode(t *testing.T) {
	now := &clock{now: time.Now()}
	service := &fakeService{unsubscribe: UnsubscribeUnsubscribed}
	handler := router(newHandler(service, now))
	assertResponse(t, serve(t, handler, "POST", "/api/newsletter/unsubscribe?token=query", `{"token":"body"}`, nil), 200, `{"state":"unsubscribed"}`)
	serve(t, handler, "POST", "/api/newsletter/unsubscribe", `{"token":"body"}`, nil)
	serve(t, handler, "GET", "/api/newsletter/unsubscribe?token=get", "", nil)
	serve(t, handler, "POST", "/api/newsletter/unsubscribe?token=a&token=b", `{"token":"fallback"}`, nil)
	serve(t, handler, "POST", "/api/newsletter/unsubscribe", `{"token":7}`, nil)
	if got := strings.Join(service.tokens, ","); got != "query,body,get,fallback," {
		t.Errorf("tokens = %q", got)
	}
	assertResponse(t, serve(t, handler, "POST", "/api/newsletter/unsubscribe?token=a", `{"token":`, nil), 400, `{"error":"Invalid request body"}`)
	service.unsubErr = &ConfigurationError{message: "x"}
	assertResponse(t, serve(t, handler, "GET", "/api/newsletter/unsubscribe?token=a", "", nil), 503, `{"state":"unavailable","error":"Unsubscribe is temporarily unavailable"}`)
	service.unsubErr = errors.New("disk I/O error")
	assertResponse(t, serve(t, handler, "POST", "/api/newsletter/unsubscribe?token=a", "", nil), 500, `{"state":"invalid","error":"Unsubscribe failed"}`)
}

func TestEditionsLimitIsParsedLikeNode(t *testing.T) {
	now := &clock{now: time.Now()}
	service := &fakeService{editions: []Edition{}}
	handler := router(newHandler(service, now))
	cases := map[string]float64{
		"":                                   30,
		"?limit=":                            30,
		"?limit=2":                           2,
		"?limit=%207":                        7,
		"?limit=7abc":                        7,
		"?limit=-3":                          -3,
		"?limit=0":                           0,
		"?limit=abc":                         30,
		"?limit=1e3":                         1,
		"?limit=12.9":                        12,
		"?limit=5&limit=9":                   5,
		"?limit=&limit=9":                    30, // String(["", "9"]) is ",9"
		"?limit=" + strings.Repeat("9", 400): 30, // parseInt gives Infinity
	}
	for query, want := range cases {
		service.limits = nil
		assertResponse(t, serve(t, handler, "GET", "/api/newsletter/editions"+query, "", nil), 200, `{"editions":[]}`)
		if len(service.limits) != 1 || service.limits[0] != want {
			t.Errorf("limit for %q = %v, want %v", query, service.limits, want)
		}
	}
	service.editionsErr = errors.New("disk I/O error")
	assertResponse(t, serve(t, handler, "GET", "/api/newsletter/editions", "", nil), 500, `{"error":"Internal server error"}`)
}

func TestEditionRespondsLikeNode(t *testing.T) {
	now := &clock{now: time.Now()}
	service := &fakeService{}
	handler := router(newHandler(service, now))
	assertResponse(t, serve(t, handler, "GET", "/api/newsletter/editions/2026-09-19", "", nil), 404, `{"error":"Edition not found"}`)
	service.edition = &Edition{Edition: "2026-09-19", Subject: "S", Articles: []byte(`[{"title":"T"}]`), CreatedAt: "2026-09-19T05:00:00.000Z"}
	assertResponse(t, serve(t, handler, "GET", "/api/newsletter/editions/2026%2D09%2D19", "", nil), 200,
		`{"edition":{"edition":"2026-09-19","subject":"S","articles":[{"title":"T"}],"createdAt":"2026-09-19T05:00:00.000Z"}}`)
	if got := service.editionKeys; got[len(got)-1] != "2026-09-19" {
		t.Errorf("decoded key = %q", got)
	}
}

func TestDigestAuthorizationMatchesNode(t *testing.T) {
	now := &clock{now: time.Date(2026, 9, 20, 5, 0, 0, 0, time.UTC)}
	service := &fakeService{digest: DigestResult{Edition: "2026-09-20", Articles: 1, Sent: 2}}
	handler := router(newHandler(service, now))
	for header, authorized := range map[string]bool{
		"":                                false,
		"Bearer wrong":                    false,
		"Bearer " + testCronSecret + "x":  false,
		"Bearer":                          false,
		"Bearer ":                         false,
		"Bearer " + testCronSecret:        true,
		"bearer \t " + testCronSecret:     true,
		"BEARER " + testCronSecret:        true,
		testCronSecret:                    true, // the bare secret, like Node
		"Basic " + testCronSecret:         false,
		"Bearer Bearer " + testCronSecret: false,
	} {
		for _, method := range []string{"GET", "POST"} {
			headers := map[string]string{}
			if header != "" {
				headers["Authorization"] = header
			}
			response := serve(t, handler, method, "/api/newsletter/digest", "", headers)
			if authorized {
				assertResponse(t, response, 200, `{"success":true,"edition":"2026-09-20","articles":1,"sent":2,"skipped":0,"failed":0}`)
			} else {
				assertResponse(t, response, 401, `{"error":"Unauthorized"}`)
			}
		}
	}
	// An empty (or blank) configured secret refuses every request.
	for _, secret := range []string{"", "   "} {
		empty := router(NewHandler(service, secret, now.Now, discardLogger()))
		for _, header := range []string{"Bearer ", "Bearer " + secret, ""} {
			assertResponse(t, serve(t, empty, "GET", "/api/newsletter/digest", "", map[string]string{"Authorization": header}), 401, `{"error":"Unauthorized"}`)
		}
	}
}

func TestDigestResponsesMatchNode(t *testing.T) {
	now := &clock{now: time.Date(2026, 9, 20, 5, 0, 0, 0, time.UTC)}
	service := &fakeService{digest: DigestResult{Edition: "2026-09-20", Articles: 2, Sent: 1, Skipped: 3, Failed: 1}}
	handler := router(newHandler(service, now))
	auth := map[string]string{"Authorization": "Bearer " + testCronSecret}
	assertResponse(t, serve(t, handler, "POST", "/api/newsletter/digest", "", auth), 200, `{"success":false,"edition":"2026-09-20","articles":2,"sent":1,"skipped":3,"failed":1}`)
	service.digestErr = &ConfigurationError{message: "NEWSLETTER_TOKEN_SECRET must contain at least 32 characters"}
	assertResponse(t, serve(t, handler, "GET", "/api/newsletter/digest", "", auth), 503, `{"error":"Newsletter delivery is not configured"}`)
	service.digestErr = errors.New("database is locked")
	assertResponse(t, serve(t, handler, "GET", "/api/newsletter/digest", "", auth), 500, `{"error":"Newsletter digest failed"}`)
}

// deadlineRecorder is a ResponseWriter that supports write deadlines, like
// a real connection, so the extension can be observed.
type deadlineRecorder struct {
	*httptest.ResponseRecorder
	writeDeadline time.Time
}

func (d *deadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	d.writeDeadline = deadline
	return nil
}

func TestDigestExtendsItsWriteDeadline(t *testing.T) {
	service := &fakeService{}
	handler := router(newHandler(service, &clock{now: time.Now()}))
	request := httptest.NewRequest("GET", "/api/newsletter/digest", nil)
	request.Header.Set("Authorization", "Bearer "+testCronSecret)
	recorder := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	started := time.Now()
	handler.ServeHTTP(recorder, request)
	if recorder.writeDeadline.Before(started.Add(DigestWriteTimeout)) || recorder.Code != 200 {
		t.Fatalf("write deadline = %v (status %d), want at least %v from now", recorder.writeDeadline.Sub(started), recorder.Code, DigestWriteTimeout)
	}
	// An unauthorized request does not get the long deadline.
	unauthorized := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	handler.ServeHTTP(unauthorized, httptest.NewRequest("GET", "/api/newsletter/digest", nil))
	if !unauthorized.writeDeadline.IsZero() {
		t.Fatal("unauthorized digest request extended its deadline")
	}
}

func TestConfirmExtendsItsWriteDeadlineForTheWelcomeEmail(t *testing.T) {
	service := &fakeService{confirmResult: ConfirmationResult{State: ConfirmationInvalid}}
	handler := router(newHandler(service, &clock{now: time.Now()}))
	recorder := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	started := time.Now()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/newsletter/confirm?token=a", nil))
	if recorder.writeDeadline.Before(started.Add(ConfirmWriteTimeout)) || recorder.Code != 200 {
		t.Fatalf("write deadline = %v (status %d), want at least %v from now", recorder.writeDeadline.Sub(started), recorder.Code, ConfirmWriteTimeout)
	}
}

// Express decodes a route parameter exactly once: /editions/100%25 looks up
// "100%" (and misses), it is not a bad request.
func TestEditionKeyIsDecodedOnceLikeExpress(t *testing.T) {
	service := &fakeService{}
	handler := router(newHandler(service, &clock{now: time.Now()}))
	for target, key := range map[string]string{
		"/api/newsletter/editions/100%25":         "100%",
		"/api/newsletter/editions/2026%2D09%2D19": "2026-09-19",
		"/api/newsletter/editions/a%252Fb":        "a%2Fb",
		"/api/newsletter/editions/2026-09-19":     "2026-09-19",
	} {
		service.editionKeys = nil
		assertResponse(t, serve(t, handler, "GET", target, "", nil), 404, `{"error":"Edition not found"}`)
		if len(service.editionKeys) != 1 || service.editionKeys[0] != key {
			t.Errorf("%s looked up %q, want %q", target, service.editionKeys, key)
		}
	}
}
