package illustration

import (
	"context"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	MaxReferenceBytes = 8 << 20
	referenceTimeout  = 15 * time.Second
	userAgent         = "TechNews-Editorial-Importer/2.0"
)

// FetchReference ports fetchReferenceImage: returns (nil, nil) whenever the
// image cannot be used, so the illustration falls back to text-only.
func FetchReference(ctx context.Context, client *http.Client, imageURL string) ([]byte, error) {
	if imageURL == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, referenceTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, nil
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, nil
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	mediaType = strings.ToLower(mediaType)
	if !strings.HasPrefix(mediaType, "image/") || mediaType == "image/svg+xml" {
		return nil, nil
	}
	if declared, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); err == nil && declared > MaxReferenceBytes {
		return nil, nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxReferenceBytes+1))
	if err != nil || len(data) == 0 || len(data) > MaxReferenceBytes {
		return nil, nil
	}
	return data, nil
}
