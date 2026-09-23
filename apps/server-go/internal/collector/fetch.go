package collector

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
)

const (
	userAgent      = "TechNews-Editorial-Importer/2.0"
	acceptHeader   = "text/html,application/xhtml+xml,application/rss+xml,application/atom+xml,application/xml;q=0.9,*/*;q=0.8"
	defaultTimeout = 20 * time.Second
	maxRedirects   = 5
	maxBodyBytes   = 5 << 20
)

var (
	// ErrRedirectOutsideSource reports a redirect that leaves the expected approved publication.
	ErrRedirectOutsideSource = errors.New("redirect leaves the approved source")
	// ErrBodyTooLarge reports a response body over the 5 MB cap.
	ErrBodyTooLarge = errors.New("body exceeds size limit")
)

// StatusError is a non-2xx response to a fetch.
type StatusError struct {
	URL    string
	Status int
}

func (e *StatusError) Error() string { return fmt.Sprintf("fetch %s: status %d", e.URL, e.Status) }

// Fetcher performs the importer's HTTP GETs (feeds now, source pages in the
// publisher). It ports fetchText + resolveApprovedArticleRedirect.
type Fetcher struct {
	transport http.RoundTripper
	timeout   time.Duration
	sourceFor func(string) (string, bool)
}

func NewFetcher() *Fetcher {
	return &Fetcher{transport: http.DefaultTransport, timeout: defaultTimeout, sourceFor: content.SourceForURL}
}

// FetchText returns the body (capped at 5 MB) and final URL. When
// expectedSource is non-empty, every redirect hop and the final URL must
// belong to that approved source.
func (f *Fetcher) FetchText(ctx context.Context, rawURL, expectedSource string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	client := &http.Client{
		Transport: f.transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return f.checkSource(req.URL.String(), expectedSource)
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", acceptHeader)

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", "", &StatusError{URL: rawURL, Status: resp.StatusCode}
	}
	finalURL := resp.Request.URL.String()
	if err := f.checkSource(finalURL, expectedSource); err != nil {
		return "", "", err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return "", "", fmt.Errorf("read %s: %w", rawURL, err)
	}
	if len(body) > maxBodyBytes {
		return "", "", &bodyTooLargeError{url: rawURL}
	}
	return string(body), finalURL, nil
}

func (f *Fetcher) checkSource(rawURL, expectedSource string) error {
	if expectedSource == "" {
		return nil
	}
	if source, ok := f.sourceFor(rawURL); !ok || source != expectedSource {
		return fmt.Errorf("%w %s: %s", ErrRedirectOutsideSource, expectedSource, rawURL)
	}
	return nil
}

// bodyTooLargeError keeps the existing message while matching ErrBodyTooLarge.
type bodyTooLargeError struct{ url string }

func (e *bodyTooLargeError) Error() string {
	return fmt.Sprintf("fetch %s: body exceeds %d bytes", e.url, maxBodyBytes)
}

func (e *bodyTooLargeError) Unwrap() error { return ErrBodyTooLarge }
