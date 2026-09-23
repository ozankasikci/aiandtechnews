package illustration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	MaxReferenceBytes     = 8 << 20
	referenceTimeout      = 15 * time.Second
	maxReferenceRedirects = 3
	userAgent             = "TechNews-Editorial-Importer/2.0"
)

// ErrUnusableReference wraps every reason a reference image cannot be used.
// The illustration then falls back to text-only, so callers only log it.
var ErrUnusableReference = errors.New("reference image unusable")

// FetchReference ports fetchReferenceImage. An empty URL returns (nil, nil);
// any image that cannot be used returns nil and an error wrapping
// ErrUnusableReference that says why, so the illustration falls back to
// text-only.
func FetchReference(ctx context.Context, client *http.Client, imageURL string) ([]byte, error) {
	if imageURL == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, referenceTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnusableReference, err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnusableReference, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%w: HTTP %d", ErrUnusableReference, resp.StatusCode)
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	mediaType = strings.ToLower(mediaType)
	if !strings.HasPrefix(mediaType, "image/") || mediaType == "image/svg+xml" {
		return nil, fmt.Errorf("%w: content type %q", ErrUnusableReference, mediaType)
	}
	if declared, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); err == nil && declared > MaxReferenceBytes {
		return nil, fmt.Errorf("%w: declares %d bytes (max %d)", ErrUnusableReference, declared, MaxReferenceBytes)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxReferenceBytes+1))
	switch {
	case err != nil:
		return nil, fmt.Errorf("%w: read body: %w", ErrUnusableReference, err)
	case len(data) == 0:
		return nil, fmt.Errorf("%w: empty body", ErrUnusableReference)
	case len(data) > MaxReferenceBytes:
		return nil, fmt.Errorf("%w: body exceeds %d bytes", ErrUnusableReference, MaxReferenceBytes)
	}
	return data, nil
}

// NewReferenceClient returns the HTTP client for fetching source reference
// images. Those URLs come from third-party feeds, so the dialer refuses
// loopback, private, link-local, multicast and unspecified addresses after
// DNS resolution (which also covers DNS rebinding and redirects), no proxy
// is used, and at most 3 http(s) redirects are followed. The per-request
// timeout is applied by FetchReference.
func NewReferenceClient() *http.Client {
	return newReferenceClient(checkReferenceDialAddress)
}

func newReferenceClient(control func(network, address string, conn syscall.RawConn) error) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second, Control: control}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialer.DialContext
	return &http.Client{Transport: transport, CheckRedirect: checkReferenceRedirect}
}

// checkReferenceDialAddress runs as net.Dialer.Control, i.e. on the resolved
// IP about to be connected to.
func checkReferenceDialAddress(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("reference dial address %q: %w", address, err)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("reference dial address %q is not an IP: %w", address, err)
	}
	ip = ip.Unmap()
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return fmt.Errorf("reference dial to non-public address %s is not allowed", ip)
	}
	return nil
}

func checkReferenceRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > maxReferenceRedirects {
		return fmt.Errorf("stopped after %d redirects", maxReferenceRedirects)
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return fmt.Errorf("redirect to unsupported scheme %q", req.URL.Scheme)
	}
	return nil
}
