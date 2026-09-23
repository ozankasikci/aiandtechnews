package newsroom_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
)

// sequence returns a deterministic random source cycling through minutes.
func sequence(minutes ...int) func(int, int) int {
	i := 0
	return func(int, int) int {
		value := minutes[i%len(minutes)]
		i++
		return value
	}
}

func newService(t *testing.T, store *newsroom.SQLiteStore, now time.Time, random func(int, int) int) *newsroom.Service {
	t.Helper()
	service, err := newsroom.NewService(store, func() time.Time { return now }, random)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func scheduledFor(t *testing.T, candidate newsroom.Candidate) string {
	t.Helper()
	if candidate.ScheduledFor == nil {
		t.Fatalf("candidate %d has no scheduled_for", candidate.ID)
	}
	return *candidate.ScheduledFor
}

func TestPublishSpacesItemsAfterEachOther(t *testing.T) {
	store, _ := openStore(t)
	a := insert(t, store, "https://example.com/a", t0)
	b := insert(t, store, "https://example.com/b", t0)
	c := insert(t, store, "https://example.com/c", t0)
	service := newService(t, store, t0, sequence(35, 31, 40))

	queued, skipped, err := service.Publish(context.Background(), []int64{a, b, c})
	if err != nil || len(skipped) != 0 || len(queued) != 3 {
		t.Fatalf("queued=%v skipped=%v err=%v", queued, skipped, err)
	}
	want := []string{"2026-09-20T12:35:00Z", "2026-09-20T13:06:00Z", "2026-09-20T13:46:00Z"}
	for i, candidate := range queued {
		if candidate.Status != newsroom.StatusQueued || scheduledFor(t, candidate) != want[i] {
			t.Fatalf("queued[%d] = %#v, want scheduled %s", i, candidate, want[i])
		}
	}
}

func TestPublishAppendsAfterExistingQueueTail(t *testing.T) {
	store, _ := openStore(t)
	a := insert(t, store, "https://example.com/a", t0)
	b := insert(t, store, "https://example.com/b", t0)
	service := newService(t, store, t0, sequence(35, 31))

	if _, _, err := service.Publish(context.Background(), []int64{a}); err != nil {
		t.Fatal(err)
	}
	queued, _, err := service.Publish(context.Background(), []int64{b})
	if err != nil || len(queued) != 1 || scheduledFor(t, queued[0]) != "2026-09-20T13:06:00Z" {
		t.Fatalf("queued=%v err=%v", queued, err)
	}
}

func TestPublishPassesConfiguredDelayToRandomSource(t *testing.T) {
	store, _ := openStore(t)
	a := insert(t, store, "https://example.com/a", t0)
	var gotMin, gotMax int
	service := newService(t, store, t0, func(min, max int) int { gotMin, gotMax = min, max; return min })

	if _, err := service.SetPublishDelay(context.Background(), newsroom.PublishDelay{MinMinutes: 10, MaxMinutes: 20}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Publish(context.Background(), []int64{a}); err != nil {
		t.Fatal(err)
	}
	if gotMin != 10 || gotMax != 20 {
		t.Fatalf("random called with %d..%d", gotMin, gotMax)
	}
}

func TestPublishSkipsDuplicateMissingAndNonPending(t *testing.T) {
	store, db := openStore(t)
	a := insert(t, store, "https://example.com/a", t0)
	rejected := insert(t, store, "https://example.com/r", t0)
	setStatus(t, db, rejected, "rejected", t0)
	service := newService(t, store, t0, sequence(30))

	queued, skipped, err := service.Publish(context.Background(), []int64{a, a, 999, rejected})
	if err != nil {
		t.Fatal(err)
	}
	if len(queued) != 1 || queued[0].ID != a {
		t.Fatalf("queued = %v", queued)
	}
	want := []newsroom.Skipped{{ID: a, Reason: "duplicate id"}, {ID: 999, Reason: "not found"}, {ID: rejected, Reason: "not pending"}}
	if len(skipped) != len(want) {
		t.Fatalf("skipped = %v", skipped)
	}
	for i := range want {
		if skipped[i] != want[i] {
			t.Fatalf("skipped = %v, want %v", skipped, want)
		}
	}
}

func TestRetryAppendsFailedCandidateToQueueTail(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	queuedID := insert(t, store, "https://example.com/q", t0)
	failed := insert(t, store, "https://example.com/f", t0)
	service := newService(t, store, t0, sequence(35, 32))
	if _, _, err := service.Publish(ctx, []int64{queuedID}); err != nil {
		t.Fatal(err)
	}
	setStatus(t, db, failed, "failed", t0)
	if _, err := db.Exec(`UPDATE candidates SET attempts = 3, last_error = 'boom' WHERE id = ?`, failed); err != nil {
		t.Fatal(err)
	}

	retried, err := service.Retry(ctx, failed)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Status != newsroom.StatusQueued || scheduledFor(t, retried) != "2026-09-20T13:07:00Z" ||
		retried.Attempts != 0 || retried.LastError != nil {
		t.Fatalf("retried = %#v", retried)
	}
	if _, err := service.Retry(ctx, queuedID); !errors.Is(err, newsroom.ErrStaleTransition) {
		t.Fatalf("retry queued err = %v", err)
	}
	if _, err := service.Retry(ctx, 999); !errors.Is(err, newsroom.ErrNotFound) {
		t.Fatalf("retry missing err = %v", err)
	}
}

func TestUnqueueReturnsCandidateToReview(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	a := insert(t, store, "https://example.com/a", t0)
	service := newService(t, store, t0, sequence(30))
	if _, _, err := service.Publish(ctx, []int64{a}); err != nil {
		t.Fatal(err)
	}

	unqueued, err := service.Unqueue(ctx, a)
	if err != nil || unqueued.Status != newsroom.StatusPending || unqueued.ScheduledFor != nil {
		t.Fatalf("unqueued = %#v err=%v", unqueued, err)
	}
	if _, err := service.Unqueue(ctx, a); !errors.Is(err, newsroom.ErrStaleTransition) {
		t.Fatalf("second unqueue err = %v", err)
	}
	if _, err := service.Unqueue(ctx, 999); !errors.Is(err, newsroom.ErrNotFound) {
		t.Fatalf("missing unqueue err = %v", err)
	}
}

func TestRejectAcceptsPendingAndFailedOnly(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	pending := insert(t, store, "https://example.com/p", t0)
	failed := insert(t, store, "https://example.com/f", t0)
	queued := insert(t, store, "https://example.com/q", t0)
	setStatus(t, db, failed, "failed", t0)
	service := newService(t, store, t0, sequence(30))
	if _, _, err := service.Publish(ctx, []int64{queued}); err != nil {
		t.Fatal(err)
	}

	rejected, skipped, err := service.Reject(ctx, []int64{pending, failed, queued, 999, pending})
	if err != nil {
		t.Fatal(err)
	}
	if len(rejected) != 2 || rejected[0] != pending || rejected[1] != failed {
		t.Fatalf("rejected = %v", rejected)
	}
	want := []newsroom.Skipped{
		{ID: queued, Reason: "not pending or failed"},
		{ID: 999, Reason: "not found"},
		{ID: pending, Reason: "duplicate id"},
	}
	if len(skipped) != len(want) {
		t.Fatalf("skipped = %v", skipped)
	}
	for i := range want {
		if skipped[i] != want[i] {
			t.Fatalf("skipped = %v, want %v", skipped, want)
		}
	}
}

func TestOverviewCountsPublishedSinceIstanbulMidnight(t *testing.T) {
	store, db := openStore(t)
	// 12:00 UTC on 2026-09-20 is 15:00 in Istanbul; local midnight is 2026-09-19T21:00:00Z.
	after := insert(t, store, "https://example.com/after", t0)
	before := insert(t, store, "https://example.com/before", t0)
	setStatus(t, db, after, "published", time.Date(2026, 9, 19, 21, 30, 0, 0, time.UTC))
	setStatus(t, db, before, "published", time.Date(2026, 9, 19, 20, 30, 0, 0, time.UTC))
	service := newService(t, store, t0, sequence(30))

	overview, err := service.Overview(context.Background())
	if err != nil || overview.PublishedToday != 1 {
		t.Fatalf("overview = %+v err=%v", overview, err)
	}
}

func TestSetPublishDelayValidatesRange(t *testing.T) {
	store, _ := openStore(t)
	service := newService(t, store, t0, sequence(30))
	for _, invalid := range []newsroom.PublishDelay{{MinMinutes: 0, MaxMinutes: 10}, {MinMinutes: 41, MaxMinutes: 40}, {MinMinutes: 30, MaxMinutes: 1441}} {
		if _, err := service.SetPublishDelay(context.Background(), invalid); !errors.Is(err, newsroom.ErrInvalidDelay) {
			t.Fatalf("SetPublishDelay(%+v) err = %v", invalid, err)
		}
	}
	saved, err := service.SetPublishDelay(context.Background(), newsroom.PublishDelay{MinMinutes: 1, MaxMinutes: 1440})
	if err != nil || saved != (newsroom.PublishDelay{MinMinutes: 1, MaxMinutes: 1440}) {
		t.Fatalf("saved = %+v err=%v", saved, err)
	}
}
