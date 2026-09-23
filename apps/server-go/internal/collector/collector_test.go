package collector_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/collector"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

var now = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

type fakeFetcher struct {
	mu     sync.Mutex
	bodies map[string]string
	calls  atomic.Int32
	block  chan struct{}
}

func (f *fakeFetcher) FetchText(ctx context.Context, url, _ string) (string, string, error) {
	f.calls.Add(1)
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return "", "", ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	body, ok := f.bodies[url]
	if !ok {
		return "", "", errors.New("feed unavailable")
	}
	return body, url, nil
}

func rss(items ...string) string {
	return "<rss><channel>" + concat(items) + "</channel></rss>"
}

func item(title, link, date string) string {
	return "<item><title>" + title + "</title><link>" + link + "</link><pubDate>" + date + "</pubDate><description>Summary of " + title + "</description></item>"
}

func concat(parts []string) (out string) {
	for _, part := range parts {
		out += part
	}
	return out
}

var feeds = []content.ApprovedFeed{
	{Source: "TechCrunch", URL: "https://techcrunch.com/feed/"},
	{Source: "The Verge", URL: "https://www.theverge.com/rss/index.xml"},
	{Source: "WIRED", URL: "https://www.wired.com/feed/rss"},
}

func setup(t *testing.T, fetcher collector.FeedFetcher) (*collector.Collector, *newsroom.SQLiteStore) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	store := newsroom.NewSQLiteStore(db)
	return collector.New(fetcher, store, feeds, func() time.Time { return now }, slog.New(slog.NewTextHandler(io.Discard, nil))), store
}

func pendingTitles(t *testing.T, store *newsroom.SQLiteStore) []string {
	t.Helper()
	page, err := store.List(context.Background(), []newsroom.Status{newsroom.StatusPending}, 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	titles := make([]string, len(page.Candidates))
	for i, candidate := range page.Candidates {
		titles[i] = candidate.Title
	}
	return titles
}

func TestRunStoresPolicyPassingItemsNewestFirst(t *testing.T) {
	fetcher := &fakeFetcher{bodies: map[string]string{
		"https://techcrunch.com/feed/": rss(
			item("OpenAI raises prices for its API", "https://techcrunch.com/2026/09/22/openai-prices/", "Tue, 22 Sep 2026 08:00:00 +0000"),
			item("Best laptops for students", "https://techcrunch.com/2026/09/22/best-laptops/", "Tue, 22 Sep 2026 09:00:00 +0000"),
			item("Anthropic launches a new Claude model", "https://techcrunch.com/2026/09/23/anthropic-claude/", "Wed, 23 Sep 2026 10:00:00 +0000"),
		),
		"https://www.theverge.com/rss/index.xml": rss(
			item("Anthropic launches a new Claude model", "https://www.theverge.com/2026/9/23/claude", "Wed, 23 Sep 2026 10:05:00 +0000"),
			item("Nvidia unveils an AI inference chip", "https://www.theverge.com/2026/9/23/nvidia-chip", "Wed, 23 Sep 2026 11:00:00 +0000"),
			item("Nvidia unveils an AI inference chip", "https://techcrunch.com/2026/09/23/wrong-source/", "Wed, 23 Sep 2026 11:00:00 +0000"),
		),
		// WIRED feed is unavailable: counted as a failure, the run continues.
	}}
	c, store := setup(t, fetcher)

	report, err := c.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Feeds != 3 || report.FeedFailures != 1 || report.Items != 6 || report.Inserted != 3 || report.Duplicates != 1 || report.Rejected != 2 {
		t.Fatalf("report = %+v", report)
	}
	got := pendingTitles(t, store)
	want := []string{"Nvidia unveils an AI inference chip", "Anthropic launches a new Claude model", "OpenAI raises prices for its API"}
	if len(got) != len(want) {
		t.Fatalf("titles = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("titles = %v, want %v", got, want)
		}
	}

	page, _ := store.List(context.Background(), []newsroom.Status{newsroom.StatusPending}, 1, 1)
	first := page.Candidates[0]
	if first.SourceName != "The Verge" || first.FeedSummary != "Summary of Nvidia unveils an AI inference chip" ||
		first.FeedPublishedAt == nil || *first.FeedPublishedAt != "2026-09-23T11:00:00Z" {
		t.Fatalf("first = %+v", first)
	}
	overview, _ := store.Overview(context.Background(), now.Add(-time.Hour))
	if overview.LastCollectedAt == nil || *overview.LastCollectedAt != "2026-09-23T12:00:00Z" {
		t.Fatalf("last collected = %v", overview.LastCollectedAt)
	}
}

func TestRunIsIdempotentAcrossRuns(t *testing.T) {
	fetcher := &fakeFetcher{bodies: map[string]string{
		"https://techcrunch.com/feed/": rss(item("OpenAI raises prices for its API", "https://techcrunch.com/2026/09/22/openai-prices/", "")),
	}}
	c, _ := setup(t, fetcher)
	if _, err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	report, err := c.Run(context.Background())
	if err != nil || report.Inserted != 0 || report.Known != 1 {
		t.Fatalf("second run = %+v err=%v", report, err)
	}
}

func TestStartRunsInBackgroundAndRejectsOverlap(t *testing.T) {
	fetcher := &fakeFetcher{bodies: map[string]string{}, block: make(chan struct{})}
	c, _ := setup(t, fetcher)

	requestCtx, cancelRequest := context.WithCancel(context.Background())
	if err := c.Start(requestCtx); err != nil {
		t.Fatal(err)
	}
	cancelRequest() // the HTTP request ends; the run must keep going
	if err := c.Start(context.Background()); !errors.Is(err, newsroom.ErrCollectInProgress) {
		t.Fatalf("overlapping start err = %v", err)
	}
	if _, err := c.Run(context.Background()); !errors.Is(err, newsroom.ErrCollectInProgress) {
		t.Fatalf("overlapping run err = %v", err)
	}
	close(fetcher.block)
	deadline := time.Now().Add(2 * time.Second)
	for c.Running() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if c.Running() {
		t.Fatal("background run did not finish")
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("start after finish err = %v", err)
	}
}

func TestBindStopsBackgroundRunWhenLifecycleEndsAndWaitReturns(t *testing.T) {
	fetcher := &fakeFetcher{bodies: map[string]string{}, block: make(chan struct{})}
	c, _ := setup(t, fetcher)
	lifecycle, cancelLifecycle := context.WithCancel(context.Background())
	c.Bind(lifecycle)

	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for fetcher.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if fetcher.calls.Load() == 0 {
		t.Fatal("background run never called the fetcher")
	}

	cancelLifecycle()
	// fetcher.block is never closed, so Wait() only returns if the
	// background run observed the bound lifecycle's cancellation.
	done := make(chan struct{})
	go func() {
		c.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait() did not return after the bound lifecycle was cancelled")
	}
}

func TestAllFeedsFailingSkipsSetLastCollected(t *testing.T) {
	fetcher := &fakeFetcher{bodies: map[string]string{}}
	c, store := setup(t, fetcher)

	report, err := c.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Feeds == 0 || report.FeedFailures != report.Feeds {
		t.Fatalf("report = %+v, want all feeds failing", report)
	}
	overview, err := store.Overview(context.Background(), now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if overview.LastCollectedAt != nil {
		t.Fatalf("LastCollectedAt = %v, want nil when every feed failed", *overview.LastCollectedAt)
	}
}

func TestLoopRunsImmediatelyThenOnInterval(t *testing.T) {
	fetcher := &fakeFetcher{bodies: map[string]string{}}
	c, _ := setup(t, fetcher)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Loop(ctx, 20*time.Millisecond); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for fetcher.calls.Load() < int32(2*len(feeds)) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if fetcher.calls.Load() < int32(2*len(feeds)) {
		t.Fatalf("feed fetches = %d, want at least two runs", fetcher.calls.Load())
	}
}
