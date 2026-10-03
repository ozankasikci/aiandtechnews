// Package siterevalidate tells the website to refresh its cached pages for
// articles that changed. The site caches article pages for an hour and list
// pages for five minutes; POST /api/revalidate on the site (authorized with
// its CRON_SECRET) regenerates them on demand. Notifications are best
// effort: they run in the background with a short timeout, failures are
// only logged, and they never fail the change that caused them.
package siterevalidate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// MaxSlugs is how many slugs the site accepts per request; longer
	// lists are sent in several requests.
	MaxSlugs = 50
	// DefaultTimeout bounds one revalidation request.
	DefaultTimeout = 10 * time.Second
	// drainDeadline bounds how long Close waits for in-flight requests.
	drainDeadline = 10 * time.Second
)

// Notifier queues article slugs whose site pages must be refreshed. Notify
// returns at once and never fails.
type Notifier interface {
	Notify(slugs []string)
}

// TopicNotifier is a Notifier that can also name topic hub pages to refresh.
type TopicNotifier interface {
	Notifier
	NotifyWithTopics(slugs, topics []string)
}

// NotifyWithTopics queues article slugs and topic slugs on n. A notifier that
// cannot name topics (not a TopicNotifier) still gets the article slugs.
func NotifyWithTopics(n Notifier, slugs, topics []string) {
	if tn, ok := n.(TopicNotifier); ok {
		tn.NotifyWithTopics(slugs, topics)
		return
	}
	n.Notify(slugs)
}

// Disabled drops every notification (SITE_REVALIDATE_URL is unset).
type Disabled struct{}

func (Disabled) Notify([]string)                {}
func (Disabled) NotifyWithTopics(_, _ []string) {}

// Client posts {"slugs": [...], "topics": [...]} to the site's revalidation
// endpoint; topics is left out when empty.
type Client struct {
	url    string
	secret string
	http   *http.Client
	// lists asks the site to refresh its list pages too (homepage, category
	// pages, archive, feeds, sitemaps). Only changes that alter lists (a
	// publish, a dashboard edit) set it: every list refresh rewrites many
	// cached pages, which counts against Vercel's ISR write limit.
	lists bool
}

func NewClient(url, secret string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultTimeout}
	}
	return &Client{url: url, secret: strings.TrimSpace(secret), http: httpClient}
}

// Revalidate sends one request for up to MaxSlugs slugs; any 2xx is success.
func (c *Client) Revalidate(ctx context.Context, slugs []string) error {
	return c.RevalidateWithTopics(ctx, slugs, nil)
}

// RevalidateWithTopics sends one request for up to MaxSlugs article slugs and
// MaxSlugs topic slugs. slugs is always sent (an empty list when only topics
// changed); topics only when there are some.
func (c *Client) RevalidateWithTopics(ctx context.Context, slugs, topics []string) error {
	if slugs == nil {
		slugs = []string{}
	}
	body, err := json.Marshal(struct {
		Slugs  []string `json:"slugs"`
		Topics []string `json:"topics,omitempty"`
		Lists  bool     `json:"lists,omitempty"`
	}{slugs, topics, c.lists})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build revalidation request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.secret)
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("post revalidation: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("site revalidation answered %d", response.StatusCode)
	}
	return nil
}

type revalidator interface {
	Revalidate(ctx context.Context, slugs []string) error
}

type topicRevalidator interface {
	RevalidateWithTopics(ctx context.Context, slugs, topics []string) error
}

// Queue runs revalidations in the background. Close stops accepting new
// ones and waits, up to a deadline, for those in flight.
type Queue struct {
	client  revalidator
	logger  *slog.Logger
	timeout time.Duration
	drain   time.Duration

	mu      sync.Mutex
	closed  bool
	pending sync.WaitGroup
}

func NewQueue(client revalidator, logger *slog.Logger) *Queue {
	if logger == nil {
		logger = slog.Default()
	}
	return &Queue{client: client, logger: logger, timeout: DefaultTimeout, drain: drainDeadline}
}

// Notify revalidates the site pages for slugs (deduplicated, blanks
// dropped) in the background.
func (q *Queue) Notify(slugs []string) { q.NotifyWithTopics(slugs, nil) }

// NotifyWithTopics also refreshes the given topic hub pages.
func (q *Queue) NotifyWithTopics(slugs, topics []string) {
	slugs, topics = unique(slugs), unique(topics)
	if len(slugs) == 0 && len(topics) == 0 {
		return
	}
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		q.logger.Warn("site revalidation dropped after shutdown", "slugs", slugs, "topics", topics)
		return
	}
	q.pending.Add(1)
	q.mu.Unlock()
	go func() {
		defer q.pending.Done()
		for start := 0; start < max(len(slugs), len(topics)); start += MaxSlugs {
			batch := slugs[min(start, len(slugs)):min(start+MaxSlugs, len(slugs))]
			topicBatch := topics[min(start, len(topics)):min(start+MaxSlugs, len(topics))]
			ctx, cancel := context.WithTimeout(context.Background(), q.timeout)
			var err error
			if tr, ok := q.client.(topicRevalidator); ok {
				err = tr.RevalidateWithTopics(ctx, batch, topicBatch)
			} else {
				err = q.client.Revalidate(ctx, batch)
			}
			cancel()
			if err != nil {
				q.logger.Warn("site revalidation failed", "slugs", batch, "topics", topicBatch, "error", err)
				continue
			}
			q.logger.Info("site revalidated", "slugs", batch, "topics", topicBatch)
		}
	}()
}

func (q *Queue) Close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	done := make(chan struct{})
	go func() {
		q.pending.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(q.drain):
		q.logger.Warn("site revalidation drain deadline exceeded; abandoning in-flight requests")
	}
}

func unique(slugs []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(slugs))
	for _, slug := range slugs {
		slug = strings.TrimSpace(slug)
		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		out = append(out, slug)
	}
	return out
}

// New returns the notifier for the configured site: Disabled when url is
// empty, otherwise a Queue, plus a drain function for shutdown.
func New(url, secret string, logger *slog.Logger) (Notifier, func()) {
	if strings.TrimSpace(url) == "" {
		return Disabled{}, func() {}
	}
	queue := NewQueue(NewClient(strings.TrimSpace(url), secret, nil), logger)
	return queue, queue.Close
}

// NewForLists is New for changes that alter the site's lists (a publish, a
// dashboard edit or removal): its requests also refresh every list page.
func NewForLists(url, secret string, logger *slog.Logger) (Notifier, func()) {
	if strings.TrimSpace(url) == "" {
		return Disabled{}, func() {}
	}
	client := NewClient(strings.TrimSpace(url), secret, nil)
	client.lists = true
	queue := NewQueue(client, logger)
	return queue, queue.Close
}

// Multi fans one notification out to several notifiers.
type Multi []Notifier

func (m Multi) Notify(slugs []string) { m.NotifyWithTopics(slugs, nil) }

// NotifyWithTopics passes topics only to members that take them.
func (m Multi) NotifyWithTopics(slugs, topics []string) {
	for _, notifier := range m {
		if notifier != nil {
			NotifyWithTopics(notifier, slugs, topics)
		}
	}
}
