package newsroom_test

import (
	"context"
	"errors"
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
