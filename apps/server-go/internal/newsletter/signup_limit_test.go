package newsletter

import (
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func signupFrom(t *testing.T, handler *Handler, remote, forwarded string) int {
	t.Helper()
	request := httptest.NewRequest("POST", "/api/subscribe", strings.NewReader(`{"email":"a@b.c"}`))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = remote
	if forwarded != "" {
		request.Header.Set("X-Forwarded-For", forwarded)
	}
	response := httptest.NewRecorder()
	router(handler).ServeHTTP(response, request)
	if response.Code == 429 && response.Body.String() != `{"error":"Too many signup attempts. Please try again shortly."}` {
		t.Fatalf("429 body = %s", response.Body.String())
	}
	return response.Code
}

func TestSignupLimitIsPerClientIP(t *testing.T) {
	now := &clock{now: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)}
	handler := newHandler(&fakeService{}, now)
	for i := 0; i < signupLimit; i++ {
		if code := signupFrom(t, handler, "203.0.113.1:5000", ""); code != 200 {
			t.Fatalf("attempt %d = %d", i, code)
		}
	}
	if code := signupFrom(t, handler, "203.0.113.1:5001", ""); code != 429 {
		t.Fatalf("31st attempt from one IP = %d, want 429", code)
	}
	if code := signupFrom(t, handler, "203.0.113.2:5000", ""); code != 200 {
		t.Fatalf("another IP = %d, want 200", code)
	}
	// The per-IP window rolls like Node's: an attempt expires after 60s.
	now.Advance(signupWindow + time.Millisecond)
	if code := signupFrom(t, handler, "203.0.113.1:5000", ""); code != 200 {
		t.Fatalf("after the window = %d, want 200", code)
	}
}

func TestForwardedForIsTrustedOnlyFromLoopback(t *testing.T) {
	for _, tt := range []struct {
		remote, forwarded, want string
	}{
		{"127.0.0.1:40000", "198.51.100.7, 10.0.0.1", "198.51.100.7"},
		{"[::1]:40000", " 198.51.100.8 ", "198.51.100.8"},
		{"127.0.0.1:40000", "[2001:DB8::0001]:443", "2001:db8::1"},
		{"127.0.0.1:40000", "::ffff:203.0.113.9", "203.0.113.9"},
		{"127.0.0.1:40000", "not-an-ip", "127.0.0.1"},
		{"127.0.0.1:40000", "", "127.0.0.1"},
		{"203.0.113.1:5000", "198.51.100.7", "203.0.113.1"},
		{"[2001:db8::0:1]:5000", "198.51.100.7", "2001:db8::1"},
		{"[::ffff:203.0.113.3]:5000", "", "203.0.113.3"},
	} {
		request := httptest.NewRequest("POST", "/api/subscribe", nil)
		request.RemoteAddr = tt.remote
		if tt.forwarded != "" {
			request.Header.Set("X-Forwarded-For", tt.forwarded)
		}
		if got := clientIP(request); got != tt.want {
			t.Errorf("clientIP(%s, XFF %q) = %q, want %q", tt.remote, tt.forwarded, got, tt.want)
		}
	}
}

func TestSignupHasAGlobalCeiling(t *testing.T) {
	now := &clock{now: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)}
	handler := newHandler(&fakeService{}, now)
	for i := 0; i < signupGlobalLimit; i++ {
		ip := "198.51.100." + strconv.Itoa(i/signupLimit+1) + ":1"
		if code := signupFrom(t, handler, ip, ""); code != 200 {
			t.Fatalf("attempt %d from %s = %d", i, ip, code)
		}
	}
	if code := signupFrom(t, handler, "192.0.2.200:1", ""); code != 429 {
		t.Fatalf("attempt over the global ceiling = %d, want 429", code)
	}
}

func TestSignupLimiterBoundsTrackedClients(t *testing.T) {
	limiter := newSignupLimiter(3)
	start := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		limiter.allow("192.0.2."+strconv.Itoa(i), start)
	}
	// Idle entries (no attempt inside the window) are evicted first.
	later := start.Add(signupWindow + time.Second)
	if !limiter.allow("192.0.2.100", later) || limiter.tracked() != 1 {
		t.Fatalf("tracked after idle eviction = %d, want 1", limiter.tracked())
	}
	// When every tracked client is active, the least recently seen goes.
	limiter.allow("192.0.2.101", later.Add(time.Millisecond))
	limiter.allow("192.0.2.102", later.Add(2*time.Millisecond))
	if !limiter.allow("192.0.2.103", later.Add(3*time.Millisecond)) || limiter.tracked() != 3 {
		t.Fatalf("tracked = %d, want 3", limiter.tracked())
	}
	if _, ok := limiter.clients["192.0.2.100"]; ok {
		t.Fatal("the least recently seen client was not evicted")
	}
}
