package newsletter

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	// signupWindow and signupLimit keep Node's rolling-minute throttle of 30
	// attempts, applied per client IP instead of globally (approved
	// divergence); signupGlobalLimit is a ceiling across all clients.
	signupWindow      = 60 * time.Second
	signupLimit       = 30
	signupGlobalLimit = 300
	// maxSignupClients bounds the per-IP table.
	maxSignupClients = 10000
)

// signupLimiter counts signup attempts in a rolling window, per client and
// in total. Like Node, a refused attempt is not counted.
type signupLimiter struct {
	mu         sync.Mutex
	maxClients int
	global     []int64
	clients    map[string][]int64
}

func newSignupLimiter(maxClients int) *signupLimiter {
	return &signupLimiter{maxClients: maxClients, clients: map[string][]int64{}}
}

// prune drops attempts older than the window, like Node's
// `while (attempts[0] < timestamp - 60_000) attempts.shift()`.
func prune(attempts []int64, cutoff int64) []int64 {
	for len(attempts) > 0 && attempts[0] < cutoff {
		attempts = attempts[1:]
	}
	return attempts
}

func (l *signupLimiter) allow(client string, now time.Time) bool {
	timestamp := now.UnixMilli()
	cutoff := timestamp - signupWindow.Milliseconds()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.global = prune(l.global, cutoff)
	attempts := prune(l.clients[client], cutoff)
	if len(l.global) >= signupGlobalLimit || len(attempts) >= signupLimit {
		if len(attempts) == 0 {
			delete(l.clients, client)
		} else {
			l.clients[client] = attempts
		}
		return false
	}
	if _, tracked := l.clients[client]; !tracked && len(l.clients) >= l.maxClients {
		l.evict(cutoff)
	}
	l.global = append(l.global, timestamp)
	l.clients[client] = append(attempts, timestamp)
	return true
}

// evict removes every client without an attempt in the window and, if the
// table is still full, the least recently seen one.
func (l *signupLimiter) evict(cutoff int64) {
	oldest, oldestAt := "", int64(0)
	for client, attempts := range l.clients {
		attempts = prune(attempts, cutoff)
		if len(attempts) == 0 {
			delete(l.clients, client)
			continue
		}
		l.clients[client] = attempts
		if last := attempts[len(attempts)-1]; oldest == "" || last < oldestAt {
			oldest, oldestAt = client, last
		}
	}
	if len(l.clients) >= l.maxClients && oldest != "" {
		delete(l.clients, oldest)
	}
}

func (l *signupLimiter) tracked() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.clients)
}

// clientIP is the address a signup is counted against. The API runs behind
// a tunnel or reverse proxy on the same host, so for a loopback peer the
// first X-Forwarded-For address is used; any other peer is used directly
// (a forwarded header from it is not trusted). Addresses are normalized
// (IPv4-mapped IPv6 to IPv4, canonical IPv6 text).
func clientIP(r *http.Request) string {
	remote := parseIP(r.RemoteAddr)
	if remote.IsValid() && remote.IsLoopback() {
		first, _, _ := strings.Cut(r.Header.Get("X-Forwarded-For"), ",")
		if forwarded := parseIP(strings.TrimSpace(first)); forwarded.IsValid() {
			return forwarded.String()
		}
	}
	if remote.IsValid() {
		return remote.String()
	}
	return r.RemoteAddr
}

func parseIP(value string) netip.Addr {
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	address, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}
	}
	return address.Unmap().WithZone("")
}
