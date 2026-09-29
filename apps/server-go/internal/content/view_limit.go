package content

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	// viewWindow is how long one client's view of one article counts once.
	viewWindow = 30 * time.Minute
	// maxViewClients bounds the table of recent client+article views.
	maxViewClients = 50000
)

// viewLimiter lets a client count one view per article per viewWindow. It
// never keeps an IP address: entries are keyed by a salted hash of the
// client address and slug, and the salt is random per process.
type viewLimiter struct {
	mu      sync.Mutex
	salt    [16]byte
	max     int
	window  time.Duration
	entries map[string]time.Time
}

func newViewLimiter(max int, window time.Duration) *viewLimiter {
	l := &viewLimiter{max: max, window: window, entries: map[string]time.Time{}}
	_, _ = rand.Read(l.salt[:])
	return l
}

func (l *viewLimiter) key(client, slug string) string {
	sum := sha256.New()
	sum.Write(l.salt[:])
	sum.Write([]byte(client))
	sum.Write([]byte{0})
	sum.Write([]byte(slug))
	return hex.EncodeToString(sum.Sum(nil)[:16])
}

// allow reports whether this client's view of slug counts now, and if so
// remembers it.
func (l *viewLimiter) allow(client, slug string, now time.Time) bool {
	key := l.key(client, slug)
	l.mu.Lock()
	defer l.mu.Unlock()
	if seen, ok := l.entries[key]; ok && now.Sub(seen) < l.window {
		return false
	}
	if len(l.entries) >= l.max {
		l.evict(now)
	}
	l.entries[key] = now
	return true
}

// evict drops expired entries and, if the table is still full, the oldest.
func (l *viewLimiter) evict(now time.Time) {
	oldest, oldestAt := "", time.Time{}
	for key, seen := range l.entries {
		if now.Sub(seen) >= l.window {
			delete(l.entries, key)
			continue
		}
		if oldest == "" || seen.Before(oldestAt) {
			oldest, oldestAt = key, seen
		}
	}
	if len(l.entries) >= l.max && oldest != "" {
		delete(l.entries, oldest)
	}
}

func (l *viewLimiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

// botMarkers are User-Agent substrings of crawlers, previewers, and
// scripted clients whose requests should not count as reads.
var botMarkers = []string{
	"bot", "crawl", "spider", "slurp", "preview", "fetch", "headless", "lighthouse",
	"curl", "wget", "python", "go-http-client", "okhttp", "java/", "httpclient", "axios", "node-fetch", "phantomjs", "puppeteer", "playwright",
}

func isBotUserAgent(userAgent string) bool {
	userAgent = strings.ToLower(strings.TrimSpace(userAgent))
	if userAgent == "" {
		return true
	}
	for _, marker := range botMarkers {
		if strings.Contains(userAgent, marker) {
			return true
		}
	}
	return false
}

// viewClient is the address a view is limited by. The API runs behind a
// tunnel or reverse proxy on the same host, so for a loopback peer the first
// X-Forwarded-For address is used; any other peer is used directly.
func viewClient(r *http.Request) string {
	remote := parseAddr(r.RemoteAddr)
	if remote.IsValid() && remote.IsLoopback() {
		first, _, _ := strings.Cut(r.Header.Get("X-Forwarded-For"), ",")
		if forwarded := parseAddr(strings.TrimSpace(first)); forwarded.IsValid() {
			return forwarded.String()
		}
	}
	if remote.IsValid() {
		return remote.String()
	}
	return r.RemoteAddr
}

func parseAddr(value string) netip.Addr {
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	address, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}
	}
	return address.Unmap().WithZone("")
}
