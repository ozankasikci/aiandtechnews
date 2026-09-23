package newsroom_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
)

func TestInsertDedupesBySourceURL(t *testing.T) {
	store, _ := openStore(t)
	insert(t, store, "https://example.com/a", t0)
	_, inserted, err := store.Insert(context.Background(), newsroom.NewCandidate{
		SourceURL: "https://example.com/a", SourceName: "X", FeedURL: "https://x/rss", Title: "Again",
	}, t0)
	if err != nil || inserted {
		t.Fatalf("duplicate insert: inserted=%v err=%v", inserted, err)
	}
}

func TestGetReturnsPendingCandidate(t *testing.T) {
	store, _ := openStore(t)
	id := insert(t, store, "https://example.com/a", t0)
	got := mustGet(t, store, id)
	if got.Status != newsroom.StatusPending || got.DiscoveredAt != "2026-09-20T12:00:00Z" ||
		got.ScheduledFor != nil || got.ArticleSlug != nil || got.Attempts != 0 || got.Title != "Title https://example.com/a" {
		t.Fatalf("candidate = %#v", got)
	}
	if _, err := store.Get(context.Background(), 999); !errors.Is(err, newsroom.ErrNotFound) {
		t.Fatalf("missing err = %v", err)
	}
}

func TestTransitionsAreGuarded(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	id := insert(t, store, "https://example.com/a", t0)
	at := t0.Add(35 * time.Minute)

	if err := store.MarkQueued(ctx, id, newsroom.StatusPending, at, t0); err != nil {
		t.Fatal(err)
	}
	if got := mustGet(t, store, id); got.Status != newsroom.StatusQueued || got.ScheduledFor == nil || *got.ScheduledFor != "2026-09-20T12:35:00Z" {
		t.Fatalf("queued = %#v", got)
	}
	if err := store.MarkQueued(ctx, id, newsroom.StatusPending, at, t0); !errors.Is(err, newsroom.ErrStaleTransition) {
		t.Fatalf("second queue err = %v", err)
	}
	if err := store.MarkPending(ctx, id, t0); err != nil {
		t.Fatal(err)
	}
	if got := mustGet(t, store, id); got.Status != newsroom.StatusPending || got.ScheduledFor != nil {
		t.Fatalf("unqueued = %#v", got)
	}
	if err := store.MarkPending(ctx, id, t0); !errors.Is(err, newsroom.ErrStaleTransition) {
		t.Fatalf("second unqueue err = %v", err)
	}
	if err := store.MarkRejected(ctx, id, t0); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkRejected(ctx, id, t0); !errors.Is(err, newsroom.ErrStaleTransition) {
		t.Fatalf("second reject err = %v", err)
	}
}

func TestListFiltersOrdersAndPaginates(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	oldest := insert(t, store, "https://example.com/1", t0)
	middle := insert(t, store, "https://example.com/2", t0.Add(time.Minute))
	newest := insert(t, store, "https://example.com/3", t0.Add(2*time.Minute))
	later := insert(t, store, "https://example.com/4", t0)
	sooner := insert(t, store, "https://example.com/5", t0)
	if err := store.MarkQueued(ctx, later, newsroom.StatusPending, t0.Add(80*time.Minute), t0); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkQueued(ctx, sooner, newsroom.StatusPending, t0.Add(40*time.Minute), t0); err != nil {
		t.Fatal(err)
	}

	pending, err := store.List(ctx, []newsroom.Status{newsroom.StatusPending}, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, pending.Candidates, newest, middle, oldest)
	if pending.Total != 3 || pending.TotalPages != 1 || pending.Page != 1 {
		t.Fatalf("pending page = %+v", pending)
	}

	mixed, err := store.List(ctx, []newsroom.Status{newsroom.StatusPending, newsroom.StatusQueued}, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, mixed.Candidates, sooner, later, newest, middle, oldest)

	second, err := store.List(ctx, []newsroom.Status{newsroom.StatusPending}, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, second.Candidates, oldest)
	if second.Total != 3 || second.TotalPages != 2 || second.Page != 2 {
		t.Fatalf("second page = %+v", second)
	}
}

func TestOverviewCountsAndDelaySettings(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	insert(t, store, "https://example.com/pending", t0)
	queued := insert(t, store, "https://example.com/queued", t0)
	failed := insert(t, store, "https://example.com/failed", t0)
	today := insert(t, store, "https://example.com/today", t0)
	yesterday := insert(t, store, "https://example.com/yesterday", t0)
	if err := store.MarkQueued(ctx, queued, newsroom.StatusPending, t0.Add(33*time.Minute), t0); err != nil {
		t.Fatal(err)
	}
	setStatus(t, db, failed, "failed", t0)
	setStatus(t, db, today, "published", t0.Add(-time.Hour))
	setStatus(t, db, yesterday, "published", t0.Add(-48*time.Hour))

	overview, err := store.Overview(ctx, t0.Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if overview.Pending != 1 || overview.Queued != 1 || overview.Processing != 0 || overview.Failed != 1 ||
		overview.PublishedToday != 1 || overview.NextPublishAt == nil || *overview.NextPublishAt != "2026-09-20T12:33:00Z" ||
		overview.LastCollectedAt != nil {
		t.Fatalf("overview = %+v", overview)
	}

	delay, err := store.PublishDelay(ctx)
	if err != nil || delay != (newsroom.PublishDelay{MinMinutes: 30, MaxMinutes: 40}) {
		t.Fatalf("default delay = %+v err=%v", delay, err)
	}
	if err := store.SetPublishDelay(ctx, newsroom.PublishDelay{MinMinutes: 5, MaxMinutes: 9}); err != nil {
		t.Fatal(err)
	}
	if delay, err := store.PublishDelay(ctx); err != nil || delay != (newsroom.PublishDelay{MinMinutes: 5, MaxMinutes: 9}) {
		t.Fatalf("updated delay = %+v err=%v", delay, err)
	}
}

func TestLatestScheduledIncludesProcessing(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	if latest, err := store.LatestScheduled(ctx); err != nil || latest != nil {
		t.Fatalf("empty latest = %v err=%v", latest, err)
	}
	a := insert(t, store, "https://example.com/a", t0)
	b := insert(t, store, "https://example.com/b", t0)
	if err := store.MarkQueued(ctx, a, newsroom.StatusPending, t0.Add(30*time.Minute), t0); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkQueued(ctx, b, newsroom.StatusPending, t0.Add(70*time.Minute), t0); err != nil {
		t.Fatal(err)
	}
	setStatus(t, db, b, "processing", t0)
	latest, err := store.LatestScheduled(ctx)
	if err != nil || latest == nil || !latest.Equal(t0.Add(70*time.Minute)) {
		t.Fatalf("latest = %v err=%v", latest, err)
	}
}

func TestPublishDelayRejectsInvalidStoredValues(t *testing.T) {
	store, db := openStore(t)
	if _, err := db.Exec(`UPDATE settings SET value = '50' WHERE key = 'newsroom.publish_delay_min_minutes'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE settings SET value = '40' WHERE key = 'newsroom.publish_delay_max_minutes'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishDelay(context.Background()); !errors.Is(err, newsroom.ErrInvalidDelay) {
		t.Fatalf("delay err = %v, want ErrInvalidDelay", err)
	}
}

func TestMarkQueuedGuardsSourceStatus(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	id := insert(t, store, "https://example.com/a", t0)
	if err := store.MarkQueued(ctx, id, newsroom.StatusPending, t0.Add(30*time.Minute), t0); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkQueued(ctx, id, newsroom.StatusQueued, t0.Add(90*time.Minute), t0); !errors.Is(err, newsroom.ErrStaleTransition) {
		t.Fatalf("queued->queued err = %v, want ErrStaleTransition", err)
	}
	var scheduledFor string
	if err := db.QueryRow(`SELECT scheduled_for FROM candidates WHERE id = ?`, id).Scan(&scheduledFor); err != nil {
		t.Fatal(err)
	}
	if scheduledFor != "2026-09-20T12:30:00Z" {
		t.Fatalf("scheduled_for changed: %s", scheduledFor)
	}
}

func TestListRejectsInvalidInputs(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	if _, err := store.List(ctx, []newsroom.Status{newsroom.StatusPending}, 1, 0); err == nil {
		t.Fatal("limit 0: want error")
	}
	if _, err := store.List(ctx, []newsroom.Status{newsroom.StatusPending}, 0, 20); err == nil {
		t.Fatal("page 0: want error")
	}
	if _, err := store.List(ctx, nil, 1, 20); err == nil {
		t.Fatal("no statuses: want error")
	}
}

func TestInsertStoresOptionalFieldsAndNormalizesTimezone(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	imageURL := "https://cdn.example.com/a.jpg"
	feedPublished := time.Date(2026, 9, 20, 9, 30, 0, 0, time.FixedZone("x", 3*3600))
	id, inserted, err := store.Insert(ctx, newsroom.NewCandidate{
		SourceURL: "https://example.com/opt", SourceName: "The Verge", FeedURL: "https://www.theverge.com/rss/index.xml",
		Title: "T", FeedSummary: "S", SourceImageURL: &imageURL, FeedPublishedAt: &feedPublished,
	}, t0)
	if err != nil || !inserted {
		t.Fatalf("insert: inserted=%v err=%v", inserted, err)
	}
	got := mustGet(t, store, id)
	if got.SourceImageURL == nil || *got.SourceImageURL != imageURL {
		t.Fatalf("source image = %v", got.SourceImageURL)
	}
	if got.FeedPublishedAt == nil || *got.FeedPublishedAt != "2026-09-20T06:30:00Z" {
		t.Fatalf("feed published at = %v", got.FeedPublishedAt)
	}
}

func TestRejectedURLStaysDeduped(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	id := insert(t, store, "https://example.com/rej", t0)
	if err := store.MarkRejected(ctx, id, t0); err != nil {
		t.Fatal(err)
	}
	_, inserted, err := store.Insert(ctx, newsroom.NewCandidate{
		SourceURL: "https://example.com/rej", SourceName: "X", FeedURL: "https://x/rss", Title: "Different Title",
	}, t0)
	if err != nil || inserted {
		t.Fatalf("re-insert after reject: inserted=%v err=%v", inserted, err)
	}
	got := mustGet(t, store, id)
	if got.Status != newsroom.StatusRejected || got.Title != "Title https://example.com/rej" {
		t.Fatalf("rejected candidate = %#v", got)
	}
}

func TestArticleSlugJoinAndDeleteSetsNull(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	id := insert(t, store, "https://example.com/withArticle", t0)
	if _, err := db.Exec(`INSERT INTO authors (name, email, password_hash, role) VALUES ('A','a@example.invalid','x','admin')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO categories (name, slug) VALUES ('AI','ai')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO articles (title, slug, category_id, author_id, status) VALUES ('T','the-slug',1,1,'published')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE candidates SET article_id = 1 WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	got := mustGet(t, store, id)
	if got.ArticleSlug == nil || *got.ArticleSlug != "the-slug" {
		t.Fatalf("article slug = %v", got.ArticleSlug)
	}
	if _, err := db.Exec(`DELETE FROM articles WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	got = mustGet(t, store, id)
	if got.ArticleSlug != nil {
		t.Fatalf("article slug after delete = %v", got.ArticleSlug)
	}
	_ = ctx
}

func TestListOrdersByStatusGroup(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	processing := insert(t, store, "https://example.com/processing", t0)
	queued := insert(t, store, "https://example.com/queued", t0)
	failed := insert(t, store, "https://example.com/failed", t0)
	pending := insert(t, store, "https://example.com/pending", t0)
	published := insert(t, store, "https://example.com/published", t0)

	if err := store.MarkQueued(ctx, queued, newsroom.StatusPending, t0.Add(30*time.Minute), t0); err != nil {
		t.Fatal(err)
	}
	setStatus(t, db, processing, "processing", t0)
	setStatus(t, db, failed, "failed", t0)
	setStatus(t, db, published, "published", t0)
	_ = pending

	page, err := store.List(ctx, []newsroom.Status{
		newsroom.StatusProcessing, newsroom.StatusQueued, newsroom.StatusFailed,
		newsroom.StatusPending, newsroom.StatusPublished,
	}, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, page.Candidates, processing, queued, failed, pending, published)
}

func TestOverviewUsesPublishedAtNotUpdatedAt(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	id := insert(t, store, "https://example.com/pubat", t0)
	// published_at is old (outside window) even though updated_at is recent.
	if _, err := db.Exec(`UPDATE candidates SET status = 'published', published_at = ?, updated_at = ? WHERE id = ?`,
		t0.Add(-48*time.Hour).UTC().Format(time.RFC3339), t0.UTC().Format(time.RFC3339), id); err != nil {
		t.Fatal(err)
	}
	overview, err := store.Overview(ctx, t0.Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if overview.PublishedToday != 0 {
		t.Fatalf("published_today = %d, want 0 (published_at outside window despite recent updated_at)", overview.PublishedToday)
	}
}

func TestListOrdersPublishedByPublishedAtDesc(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	earlier := insert(t, store, "https://example.com/earlier", t0)
	later := insert(t, store, "https://example.com/later", t0)
	// updated_at is set opposite of published_at to prove published_at drives the order.
	if _, err := db.Exec(`UPDATE candidates SET status = 'published', published_at = ?, updated_at = ? WHERE id = ?`,
		t0.Add(time.Hour).UTC().Format(time.RFC3339), t0.UTC().Format(time.RFC3339), earlier); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE candidates SET status = 'published', published_at = ?, updated_at = ? WHERE id = ?`,
		t0.Add(2*time.Hour).UTC().Format(time.RFC3339), t0.Add(-time.Hour).UTC().Format(time.RFC3339), later); err != nil {
		t.Fatal(err)
	}
	page, err := store.List(ctx, []newsroom.Status{newsroom.StatusPublished}, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, page.Candidates, later, earlier)
}

func TestOverviewOnEmptyTable(t *testing.T) {
	store, _ := openStore(t)
	overview, err := store.Overview(context.Background(), t0)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Pending != 0 || overview.Queued != 0 || overview.Processing != 0 || overview.Failed != 0 ||
		overview.PublishedToday != 0 || overview.NextPublishAt != nil || overview.LastCollectedAt != nil {
		t.Fatalf("overview = %+v", overview)
	}
}

func TestQueuedRequiresScheduledFor(t *testing.T) {
	store, db := openStore(t)
	id := insert(t, store, "https://example.com/a", t0)
	_, err := db.Exec(`UPDATE candidates SET status = 'queued', scheduled_for = NULL WHERE id = ?`, id)
	if err == nil {
		t.Fatal("update to queued with NULL scheduled_for: want error")
	}
}

func assertIDs(t *testing.T, candidates []newsroom.Candidate, want ...int64) {
	t.Helper()
	got := make([]int64, len(candidates))
	for i, candidate := range candidates {
		got[i] = candidate.ID
	}
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ids = %v, want %v", got, want)
		}
	}
}

func TestInsertSkipsStoriesAlreadyPublishedAsArticles(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	mustExec(t, db, `INSERT INTO authors (name, email, password_hash, role) VALUES ('A', 'a@example.invalid', 'x', 'admin')`)
	mustExec(t, db, `INSERT INTO categories (name, slug) VALUES ('AI', 'ai')`)
	mustExec(t, db, `INSERT INTO articles (title, slug, category_id, author_id, status, source_url)
		VALUES ('Old', 'openai-ships-a-model', 1, 1, 'published', 'https://techcrunch.com/2026/09/01/old/')`)

	_, inserted, err := store.Insert(ctx, newsroom.NewCandidate{
		SourceURL: "https://techcrunch.com/2026/09/01/old/", SourceName: "TechCrunch", FeedURL: "https://techcrunch.com/feed/", Title: "Different title",
	}, t0)
	if err != nil || inserted {
		t.Fatalf("same source URL: inserted=%v err=%v", inserted, err)
	}
	_, inserted, err = store.Insert(ctx, newsroom.NewCandidate{
		SourceURL: "https://techcrunch.com/2026/09/02/new/", SourceName: "TechCrunch", FeedURL: "https://techcrunch.com/feed/", Title: "OpenAI ships a model",
	}, t0)
	if err != nil || inserted {
		t.Fatalf("same slug: inserted=%v err=%v", inserted, err)
	}
	insert(t, store, "https://techcrunch.com/2026/09/03/fresh/", t0)
}

func TestSetLastCollectedShowsInOverview(t *testing.T) {
	store, _ := openStore(t)
	if err := store.SetLastCollected(context.Background(), t0); err != nil {
		t.Fatal(err)
	}
	overview, err := store.Overview(context.Background(), t0.Add(-time.Hour))
	if err != nil || overview.LastCollectedAt == nil || *overview.LastCollectedAt != "2026-09-20T12:00:00Z" {
		t.Fatalf("overview = %+v err=%v", overview, err)
	}
}

// seedArticle inserts an author, category and article so article_id foreign keys resolve.
func seedArticle(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	mustExec(t, db, `INSERT OR IGNORE INTO authors (id, name, email, password_hash, role) VALUES (1, 'A', 'a@example.invalid', 'x', 'admin')`)
	mustExec(t, db, `INSERT OR IGNORE INTO categories (id, name, slug) VALUES (1, 'AI', 'ai')`)
	result, err := db.Exec(`INSERT INTO articles (title, slug, category_id, author_id, status) VALUES ('T', ?, 1, 1, 'published')`,
		fmt.Sprintf("slug-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	return id
}

func queueAt(t *testing.T, store *newsroom.SQLiteStore, url string, at time.Time) int64 {
	t.Helper()
	id := insert(t, store, url, t0)
	if err := store.MarkQueued(context.Background(), id, newsroom.StatusPending, at, t0); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestClaimDueTakesEarliestDueOneAtATimeWithMinimumGap(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	a := queueAt(t, store, "https://example.com/a", t0)
	b := queueAt(t, store, "https://example.com/b", t0.Add(10*time.Minute))
	gap := 30 * time.Minute

	if _, ok, err := store.ClaimDue(ctx, t0.Add(-time.Minute), gap); err != nil || ok {
		t.Fatalf("nothing due yet: ok=%v err=%v", ok, err)
	}
	claimed, ok, err := store.ClaimDue(ctx, t0.Add(20*time.Minute), gap)
	if err != nil || !ok || claimed.ID != a || claimed.Status != newsroom.StatusProcessing || claimed.Attempts != 1 {
		t.Fatalf("first claim = %+v ok=%v err=%v", claimed, ok, err)
	}
	if _, ok, _ := store.ClaimDue(ctx, t0.Add(20*time.Minute), gap); ok {
		t.Fatal("must not claim while another candidate is processing")
	}

	if err := store.MarkPublished(ctx, a, seedArticle(t, db), t0.Add(20*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.ClaimDue(ctx, t0.Add(45*time.Minute), gap); ok {
		t.Fatal("must wait the minimum gap after the last publish")
	}
	claimed, ok, err = store.ClaimDue(ctx, t0.Add(51*time.Minute), gap)
	if err != nil || !ok || claimed.ID != b {
		t.Fatalf("second claim = %+v ok=%v err=%v", claimed, ok, err)
	}

	published := mustGet(t, store, a)
	if published.Status != newsroom.StatusPublished || published.ScheduledFor != nil || published.ArticleSlug == nil {
		t.Fatalf("published = %+v", published)
	}
}

func TestRequeueFailAndResetProcessing(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	a := queueAt(t, store, "https://example.com/a", t0)

	if _, _, err := store.ClaimDue(ctx, t0, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.Requeue(ctx, a, "Gemini returned 503", t0.Add(5*time.Minute), t0); err != nil {
		t.Fatal(err)
	}
	requeued := mustGet(t, store, a)
	if requeued.Status != newsroom.StatusQueued || *requeued.ScheduledFor != "2026-09-20T12:05:00Z" ||
		requeued.LastError == nil || *requeued.LastError != "Gemini returned 503" || requeued.Attempts != 1 {
		t.Fatalf("requeued = %+v", requeued)
	}

	claimed, ok, _ := store.ClaimDue(ctx, t0.Add(6*time.Minute), 0)
	if !ok || claimed.Attempts != 2 {
		t.Fatalf("second claim = %+v", claimed)
	}
	if err := store.MarkFailed(ctx, a, "source text too short", t0.Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}
	failed := mustGet(t, store, a)
	if failed.Status != newsroom.StatusFailed || failed.ScheduledFor != nil || *failed.LastError != "source text too short" {
		t.Fatalf("failed = %+v", failed)
	}
	if err := store.MarkFailed(ctx, a, "again", t0); !errors.Is(err, newsroom.ErrStaleTransition) {
		t.Fatalf("stale fail err = %v", err)
	}

	b := queueAt(t, store, "https://example.com/b", t0)
	if _, ok, _ := store.ClaimDue(ctx, t0, 0); !ok {
		t.Fatal("claim b")
	}
	count, err := store.ResetProcessing(ctx, t0.Add(time.Hour))
	if err != nil || count != 1 {
		t.Fatalf("reset count=%d err=%v", count, err)
	}
	reset := mustGet(t, store, b)
	if reset.Status != newsroom.StatusQueued || *reset.ScheduledFor != "2026-09-20T13:00:00Z" {
		t.Fatalf("reset = %+v", reset)
	}
}
