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
const RunTimeout = 15 * time.Minute

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
	Judged       int // items the AI relevance judge decided this run
	Rejections   map[string]int
}

// Collector turns approved feed items into pending candidates. It never publishes.
type Collector struct {
	fetcher FeedFetcher
	store   CandidateStore
	feeds   []content.ApprovedFeed
	now     func() time.Time
	logger  *slog.Logger
	judge   Judge       // nil: the AI keyword rule decides relevance
	memory  JudgeMemory // set with judge
	running atomic.Bool

	mu        sync.Mutex
	lifecycle context.Context
	startWG   sync.WaitGroup
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

// Bind sets the lifecycle context that bounds background runs started by
// Start: such a run is cancelled once ctx is done, in addition to its own
// RunTimeout. App.Run binds the collector to its tasks context so shutdown
// (via Wait) does not wait forever for a background run.
func (c *Collector) Bind(ctx context.Context) {
	c.mu.Lock()
	c.lifecycle = ctx
	c.mu.Unlock()
}

func (c *Collector) boundLifecycle() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lifecycle != nil {
		return c.lifecycle
	}
	return context.Background()
}

// Start implements newsroom.Collector: it begins a run on a context detached
// from the caller's (the HTTP request ends once 202 is written) and returns
// immediately. The run still stops when the collector's bound lifecycle ends
// (see Bind), so process shutdown does not wait forever for it.
func (c *Collector) Start(ctx context.Context) error {
	if !c.running.CompareAndSwap(false, true) {
		return newsroom.ErrCollectInProgress
	}
	c.startWG.Add(1)
	go func() {
		defer c.startWG.Done()
		defer c.running.Store(false)
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), RunTimeout)
		defer cancel()
		stop := context.AfterFunc(c.boundLifecycle(), cancel)
		defer stop()
		if _, err := c.collect(runCtx); err != nil {
			c.logger.ErrorContext(runCtx, "collection failed", "error", err)
		}
	}()
	return nil
}

// Wait blocks until every run started by Start has finished. It is used by
// App.Run during shutdown so background runs are not abandoned mid-write.
func (c *Collector) Wait() {
	c.startWG.Wait()
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
				if ctx.Err() == nil {
					c.logger.WarnContext(ctx, "feed fetch failed", "source", feed.Source, "error", err)
				}
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
			rule := content.AutomaticItemRejectionReason
			if c.judge != nil {
				rule = content.StructuralItemRejectionReason
			}
			if reason := rule(item.Title, item.URL, item.Source, now); reason != "" {
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

	if c.judge != nil {
		var err error
		if accepted, err = c.applyJudge(ctx, accepted, &report); err != nil {
			return report, err
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
	if report.Feeds > 0 && report.FeedFailures == report.Feeds {
		c.logger.ErrorContext(ctx, "all feeds failed", "feeds", report.Feeds)
	} else if err := c.store.SetLastCollected(ctx, now); err != nil {
		return report, err
	}
	c.logger.InfoContext(ctx, "collection finished",
		"feeds", report.Feeds, "feed_failures", report.FeedFailures, "items", report.Items,
		"rejected", report.Rejected, "duplicates", report.Duplicates, "inserted", report.Inserted, "known", report.Known, "judged", report.Judged,
		"rejections", report.Rejections)
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

// applyJudge keeps items already stored, drops items the judge rejected
// before, and asks the judge about the rest. Items it cannot decide fall
// back to the AI keyword rule and are judged again next run.
func (c *Collector) applyJudge(ctx context.Context, items []FeedItem, report *Report) ([]FeedItem, error) {
	urls := make([]string, len(items))
	for i, item := range items {
		urls[i] = item.URL
	}
	known, err := c.memory.KnownURLs(ctx, urls)
	if err != nil {
		return nil, err
	}
	rejected, err := c.memory.RejectedURLs(ctx, urls)
	if err != nil {
		return nil, err
	}
	var kept, fresh []FeedItem
	for _, item := range items {
		switch {
		case known[item.URL]:
			kept = append(kept, item)
		case rejected[item.URL]:
			report.Rejected++
			report.Rejections[JudgedRejection]++
		default:
			fresh = append(fresh, item)
		}
	}
	if len(fresh) == 0 {
		return kept, nil
	}
	verdicts, undecided := c.judgeItems(ctx, fresh)
	reasons := map[string]string{}
	for _, item := range fresh {
		verdict, ok := verdicts[item.URL]
		switch {
		case ok && verdict.Candidate:
			kept = append(kept, item)
		case ok:
			report.Rejected++
			report.Rejections[JudgedRejection]++
			reasons[item.URL] = verdict.Reason
		}
	}
	for _, item := range undecided {
		if content.MentionsAI(item.Title, item.URL) {
			kept = append(kept, item)
		} else {
			report.Rejected++
			report.Rejections[content.NotAIRelatedReason]++
		}
	}
	report.Judged = len(verdicts)
	if len(reasons) > 0 {
		if err := c.memory.RememberRejected(ctx, reasons); err != nil {
			return nil, err
		}
	}
	return kept, nil
}
