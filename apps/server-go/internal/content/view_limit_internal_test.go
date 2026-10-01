package content

import (
	"testing"
	"time"
)

func TestViewLimiterCountsOncePerWindowAndStaysBounded(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	limiter := newViewLimiter(3, 30*time.Minute)
	if !limiter.allow("a", "s", now) || limiter.allow("a", "s", now.Add(29*time.Minute)) {
		t.Fatal("repeat inside the window must not count")
	}
	if !limiter.allow("a", "other", now) || !limiter.allow("b", "s", now) {
		t.Fatal("another slug or client must count")
	}
	if !limiter.allow("a", "s", now.Add(31*time.Minute)) {
		t.Fatal("a view after the window must count")
	}
	for i := range 10 {
		limiter.allow(string(rune('c'+i)), "s", now.Add(32*time.Minute))
	}
	if size := limiter.size(); size > 3 {
		t.Fatalf("limiter size = %d, want <= 3", size)
	}
	for key := range limiter.entries {
		if len(key) != 32 {
			t.Fatalf("entry key %q is not a hash", key)
		}
	}
}
