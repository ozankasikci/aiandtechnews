// Package indexnow notifies search engines about new article URLs (port of apps/server/src/indexnow.ts).
package indexnow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultEndpoint   = "https://api.indexnow.org/indexnow"
	DefaultSiteOrigin = "https://www.aiandtech.news"
	// Key is public by design: it is served at <origin>/<key>.txt.
	Key = "841819f6d7012eca7cd9104b4dd45d8e"
)

type Client struct {
	endpoint   string
	siteOrigin string
	http       *http.Client
}

type Option func(*Client)

func WithEndpoint(endpoint string) Option { return func(c *Client) { c.endpoint = endpoint } }

func New(options ...Option) *Client {
	client := &Client{endpoint: DefaultEndpoint, siteOrigin: DefaultSiteOrigin, http: &http.Client{Timeout: 15 * time.Second}}
	for _, option := range options {
		option(client)
	}
	return client
}

// ArticleURL mirrors articleUrl: <origin>/article/<encoded slug>.
func ArticleURL(siteOrigin, slug string) string {
	return strings.TrimRight(siteOrigin, "/") + "/article/" + url.PathEscape(slug)
}

// SubmitSlugs submits the article URLs for slugs; 200 and 202 are success.
func (c *Client) SubmitSlugs(ctx context.Context, slugs []string) error {
	seen := map[string]bool{}
	urls := make([]string, 0, len(slugs))
	for _, slug := range slugs {
		articleURL := ArticleURL(c.siteOrigin, slug)
		if !seen[articleURL] {
			seen[articleURL] = true
			urls = append(urls, articleURL)
		}
	}
	if len(urls) == 0 {
		return nil
	}
	origin, err := url.Parse(c.siteOrigin)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{
		"host":        origin.Host,
		"key":         Key,
		"keyLocation": c.siteOrigin + "/" + Key + ".txt",
		"urlList":     urls,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("indexnow request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("IndexNow rejected %d URL(s) with %d: %s", len(urls), resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	// Drain so the connection can be reused.
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
