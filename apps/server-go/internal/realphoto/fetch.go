package realphoto

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// UserAgent identifies us to Wikimedia (its API policy requires a
	// descriptive agent with a way to reach the operator) and to makers' sites.
	UserAgent = "AIAndTechNewsBot/1.0 (+https://aiandtech.news; in-article photo finder)"

	fetchTimeout  = 20 * time.Second
	maxPageBytes  = 5 << 20
	maxTitleBytes = 512 << 10
	maxImageBytes = 15 << 20
	maxAPIBytes   = 4 << 20
)

// errNotHTTP refuses anything but an absolute http(s) URL.
var errNotHTTP = errors.New("only absolute http(s) URLs are fetched")

// fetched is one response body, capped, and the URL it finally came from.
type fetched struct {
	Body      []byte
	FinalURL  string
	MediaType string
	Truncated bool
}

// get fetches rawURL with f.http (a client that refuses private and
// loopback addresses), a timeout and a size cap. accept checks the media
// type; a body over maxBytes is an error unless allowTruncate, in which case
// the first maxBytes are returned (enough to read a page's <title>).
func (f *Finder) get(ctx context.Context, rawURL, accept string, maxBytes int64, allowTruncate bool, acceptType func(string) bool) (fetched, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fetched{}, fmt.Errorf("%w: %q", errNotHTTP, rawURL)
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return fetched{}, err
	}
	req.Header.Set("User-Agent", UserAgent)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := f.http.Do(req)
	if err != nil {
		return fetched{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fetched{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	mediaType = strings.ToLower(mediaType)
	if acceptType != nil && !acceptType(mediaType) {
		return fetched{}, fmt.Errorf("content type %q", mediaType)
	}
	if declared, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); err == nil && declared > maxBytes && !allowTruncate {
		return fetched{}, fmt.Errorf("declares %d bytes (max %d)", declared, maxBytes)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil && len(body) == 0 {
		return fetched{}, fmt.Errorf("read body: %w", err)
	}
	out := fetched{Body: body, FinalURL: resp.Request.URL.String(), MediaType: mediaType}
	if int64(len(body)) > maxBytes {
		if !allowTruncate {
			return fetched{}, fmt.Errorf("body exceeds %d bytes", maxBytes)
		}
		out.Body, out.Truncated = body[:maxBytes], true
	}
	if len(out.Body) == 0 {
		return fetched{}, errors.New("empty body")
	}
	return out, nil
}

func isHTML(mediaType string) bool {
	return mediaType == "text/html" || mediaType == "application/xhtml+xml"
}

func isRasterImage(mediaType string) bool {
	return strings.HasPrefix(mediaType, "image/") && mediaType != "image/svg+xml"
}

func (f *Finder) getPage(ctx context.Context, rawURL string) (fetched, error) {
	return f.get(ctx, rawURL, "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5", maxPageBytes, false, isHTML)
}

func (f *Finder) getImage(ctx context.Context, rawURL string) ([]byte, error) {
	page, err := f.get(ctx, rawURL, "image/avif;q=0,image/webp,image/jpeg,image/png,image/*;q=0.8", maxImageBytes, false, isRasterImage)
	return page.Body, err
}
