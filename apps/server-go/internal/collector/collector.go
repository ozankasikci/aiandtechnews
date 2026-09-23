package collector

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
)

// RunTimeout bounds one collection run, including background runs started by
// POST /api/newsroom/collect.
const RunTimeout = 5 * time.Minute

type FeedFetcher interface {
	FetchText(ctx context.Context, url, expectedSource string) (body, finalURL string, err error)
}

type CandidateStore interface {
	Insert(ctx context.Context, candidate newsroom.NewCandidate, now time.Time) (int64, bool, error)
	SetLastCollected(ctx context.Context, at time.Time) error
}

// Report summarizes one run for logs.
type Report struct {
	Feeds        int
	FeedFailures int
	Items        int
	Rejected     int
	Duplicates   int
	Inserted     int
	Known        int
	Rejections   map[string]int
}

// Collector turns approved feed items into pending candidates. It never publishes.
type Collector struct {
	fetcher FeedFetcher
	store   CandidateStore
	feeds   []content.ApprovedFeed
	now     func() time.Time
	logger  *slog.Logger
	running atomic.Bool
}

func New(fetcher FeedFetcher, store CandidateStore, feeds []content.ApprovedFeed, now func() time.Time, logger *slog.Logger) *Collector {
	return &Collector{fetcher: fetcher, store: store, feeds: feeds, now: now, logger: logger}
}

// Running reports whether a run is in progress.
func (c *Collector) Running() bool { return c.running.Load() }

// Run collects synchronously. It returns newsroom.ErrCollectInProgress when a
// run is already active.
func (c *Collector) Run(ctx context.Context) (Report, error) {
	if !c.running.CompareAndSwap(false, true) {
		return Report{}, newsroom.ErrCollectInProgress
	}
	defer c.running.Store(false)
	ctx, cancel := context.WithTimeout(ctx, RunTimeout)
	defer cancel()
	return c.collect(ctx)
}

// Start implements newsroom.Collector: it begins a run on a context detached
// from the caller's (the HTTP request ends once 202 is written) and returns
// immediately.
func (c *Collector) Start(ctx context.Context) error {
	if !c.running.CompareAndSwap(false, true) {
		return newsroom.ErrCollectInProgress
	}
	go func() {
		defer c.running.Store(false)
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), RunTimeout)
		defer cancel()
		if _, err := c.collect(runCtx); err != nil {
			c.logger.ErrorContext(runCtx, "collection failed", "error", err)
		}
	}()
	return nil
}

// Loop collects immediately and then every interval until ctx is done.
func (c *Collector) Loop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := c.Run(ctx); err != nil && !errors.Is(err, newsroom.ErrCollectInProgress) && ctx.Err() == nil {
			c.logger.ErrorContext(ctx, "scheduled collection failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *Collector) collect(ctx context.Context) (Report, error) {
	results := make([][]FeedItem, len(c.feeds))
	var failures atomic.Int32
	var wg sync.WaitGroup
	for i, feed := range c.feeds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, _, err := c.fetcher.FetchText(ctx, feed.URL, "")
			if err != nil {
				failures.Add(1)
				c.logger.WarnContext(ctx, "feed fetch failed", "source", feed.Source, "error", err)
				return
			}
			items := ParseFeed(body, feed.Source)
			for j := range items {
				items[j].FeedURL = feed.URL
			}
			results[i] = items
		}()
	}
	wg.Wait()

	now := c.now()
	report := Report{Feeds: len(c.feeds), FeedFailures: int(failures.Load()), Rejections: map[string]int{}}
	seenURLs := map[string]bool{}
	seenSlugs := map[string]bool{}
	var accepted []FeedItem
	for _, items := range results {
		for _, item := range items {
			report.Items++
			if reason := content.AutomaticItemRejectionReason(item.Title, item.URL, item.Source, now); reason != "" {
				report.Rejected++
				report.Rejections[reason]++
				continue
			}
			slug := content.Slugify(item.Title)
			if seenURLs[item.URL] || seenSlugs[slug] {
				report.Duplicates++
				continue
			}
			seenURLs[item.URL] = true
			seenSlugs[slug] = true
			accepted = append(accepted, item)
		}
	}

	// Insert oldest first so the newest item gets the highest id and lists first.
	sort.SliceStable(accepted, func(i, j int) bool { return publishedUnix(accepted[i]) < publishedUnix(accepted[j]) })
	for _, item := range accepted {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		_, inserted, err := c.store.Insert(ctx, item.candidate(), now)
		if err != nil {
			return report, err
		}
		if inserted {
			report.Inserted++
		} else {
			report.Known++
		}
	}
	if err := c.store.SetLastCollected(ctx, now); err != nil {
		return report, err
	}
	c.logger.InfoContext(ctx, "collection finished",
		"feeds", report.Feeds, "feed_failures", report.FeedFailures, "items", report.Items,
		"rejected", report.Rejected, "duplicates", report.Duplicates, "inserted", report.Inserted, "known", report.Known)
	return report, nil
}

func publishedUnix(item FeedItem) int64 {
	if item.PublishedAt == nil {
		return 0
	}
	return item.PublishedAt.Unix()
}

func (item FeedItem) candidate() newsroom.NewCandidate {
	candidate := newsroom.NewCandidate{
		SourceURL:       item.URL,
		SourceName:      item.Source,
		FeedURL:         item.FeedURL,
		Title:           item.Title,
		FeedSummary:     item.Summary,
		FeedPublishedAt: item.PublishedAt,
	}
	if item.ImageURL != "" {
		image := item.ImageURL
		candidate.SourceImageURL = &image
	}
	return candidate
}
