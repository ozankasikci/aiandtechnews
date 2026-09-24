package newsroom_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
)

func woken(service *newsroom.Service) bool {
	select {
	case <-service.Wakeups():
		return true
	default:
		return false
	}
}

func TestPublishNowQueuesPendingDueNowWithPriority(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	other := queueAt(t, store, "https://example.com/other", t0.Add(35*time.Minute))
	id := insert(t, store, "https://example.com/a", t0)
	service := newService(t, store, t0, sequence(30))

	candidate, err := service.PublishNow(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Status != newsroom.StatusQueued || !candidate.PublishNow || scheduledFor(t, candidate) != "2026-09-20T12:00:00Z" ||
		candidate.Attempts != 0 || candidate.LastError != nil {
		t.Fatalf("candidate = %+v", candidate)
	}
	if !woken(service) {
		t.Fatal("publish now must wake the publisher")
	}
	if got := mustGet(t, store, other); got.PublishNow || scheduledFor(t, got) != "2026-09-20T12:35:00Z" {
		t.Fatalf("other queued candidate changed: %+v", got)
	}
}

func TestPublishNowMovesAQueuedCandidateUpKeepingItsAttempts(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	id := queueAt(t, store, "https://example.com/a", t0.Add(2*time.Hour))
	mustExec(t, db, `UPDATE candidates SET attempts = 1, last_error = 'Gemini returned 503' WHERE id = ?`, id)
	service := newService(t, store, t0, sequence(30))

	candidate, err := service.PublishNow(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Status != newsroom.StatusQueued || !candidate.PublishNow || scheduledFor(t, candidate) != "2026-09-20T12:00:00Z" ||
		candidate.Attempts != 1 || candidate.LastError == nil || *candidate.LastError != "Gemini returned 503" {
		t.Fatalf("candidate = %+v", candidate)
	}
	// A second request is idempotent.
	if again, err := service.PublishNow(ctx, id); err != nil || !again.PublishNow || again.Status != newsroom.StatusQueued {
		t.Fatalf("second publish now = %+v, %v", again, err)
	}
}

func TestPublishNowResetsAFailedCandidateLikeRetry(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	id := insert(t, store, "https://example.com/a", t0)
	setStatus(t, db, id, "failed", t0)
	mustExec(t, db, `UPDATE candidates SET attempts = 3, last_error = 'boom' WHERE id = ?`, id)
	service := newService(t, store, t0, sequence(30))

	candidate, err := service.PublishNow(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Status != newsroom.StatusQueued || !candidate.PublishNow || candidate.Attempts != 0 || candidate.LastError != nil ||
		scheduledFor(t, candidate) != "2026-09-20T12:00:00Z" {
		t.Fatalf("candidate = %+v", candidate)
	}
}

func TestPublishNowRejectsOtherStatusesAndMissingCandidates(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	service := newService(t, store, t0, sequence(30))
	for _, status := range []string{"processing", "published", "rejected"} {
		id := insert(t, store, "https://example.com/"+status, t0)
		setStatus(t, db, id, status, t0)
		if _, err := service.PublishNow(ctx, id); !errors.Is(err, newsroom.ErrStaleTransition) {
			t.Fatalf("%s: err = %v, want ErrStaleTransition", status, err)
		}
		if got := mustGet(t, store, id); string(got.Status) != status || got.PublishNow {
			t.Fatalf("%s candidate changed: %+v", status, got)
		}
	}
	if _, err := service.PublishNow(ctx, 999); !errors.Is(err, newsroom.ErrNotFound) {
		t.Fatalf("missing: err = %v, want ErrNotFound", err)
	}
	if woken(service) {
		t.Fatal("a refused publish now must not wake the publisher")
	}
}

func TestMarkPublishNowGuardsSourceStatus(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	id := insert(t, store, "https://example.com/a", t0)
	if err := store.MarkPublishNow(ctx, id, newsroom.StatusFailed, t0); !errors.Is(err, newsroom.ErrStaleTransition) {
		t.Fatalf("wrong from status: err = %v", err)
	}
	if err := store.MarkPublishNow(ctx, id, newsroom.StatusProcessing, t0); !errors.Is(err, newsroom.ErrStaleTransition) {
		t.Fatalf("processing from status: err = %v", err)
	}
	if got := mustGet(t, store, id); got.Status != newsroom.StatusPending || got.PublishNow {
		t.Fatalf("candidate changed: %+v", got)
	}
}

func TestClaimDueTakesPublishNowFirstIgnoringTheMinimumGap(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	gap := 30 * time.Minute
	early := queueAt(t, store, "https://example.com/early", t0.Add(-time.Hour))
	urgent := insert(t, store, "https://example.com/urgent", t0)
	if err := store.MarkPublishNow(ctx, urgent, newsroom.StatusPending, t0); err != nil {
		t.Fatal(err)
	}
	// Something was published five minutes ago: normal items must wait.
	recent := insert(t, store, "https://example.com/recent", t0)
	setStatus(t, db, recent, "published", t0.Add(-5*time.Minute))

	claimed, ok, err := store.ClaimDue(ctx, t0, gap)
	if err != nil || !ok || claimed.ID != urgent || claimed.Status != newsroom.StatusProcessing || !claimed.PublishNow {
		t.Fatalf("claim = %+v ok=%v err=%v, want the publish-now candidate", claimed, ok, err)
	}
	if _, ok, _ := store.ClaimDue(ctx, t0, gap); ok {
		t.Fatal("must not claim while another candidate is processing")
	}
	if err := store.MarkPublished(ctx, urgent, seedArticle(t, db), t0); err != nil {
		t.Fatal(err)
	}
	if got := mustGet(t, store, urgent); got.PublishNow {
		t.Fatalf("published candidate keeps the flag: %+v", got)
	}
	if _, ok, _ := store.ClaimDue(ctx, t0.Add(time.Minute), gap); ok {
		t.Fatal("a normal candidate must still wait the minimum gap")
	}
	if got := mustGet(t, store, early); got.Status != newsroom.StatusQueued || scheduledFor(t, got) != "2026-09-20T11:00:00Z" {
		t.Fatalf("normal queued candidate changed: %+v", got)
	}
}

func TestClaimDueDoesNotRunPublishNowWhileAnotherIsProcessing(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	queueAt(t, store, "https://example.com/busy", t0)
	if _, ok, err := store.ClaimDue(ctx, t0, 0); err != nil || !ok {
		t.Fatalf("claim busy: ok=%v err=%v", ok, err)
	}
	urgent := insert(t, store, "https://example.com/urgent", t0)
	if err := store.MarkPublishNow(ctx, urgent, newsroom.StatusPending, t0); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.ClaimDue(ctx, t0, 0); ok {
		t.Fatal("publish now must not run while another candidate is processing")
	}
}

func TestPublishNowFlagClearsWhenTheCandidateLeavesTheQueue(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	flag := func(url string) int64 {
		t.Helper()
		id := insert(t, store, url, t0)
		if err := store.MarkPublishNow(ctx, id, newsroom.StatusPending, t0); err != nil {
			t.Fatal(err)
		}
		return id
	}
	claim := func(want int64) {
		t.Helper()
		claimed, ok, err := store.ClaimDue(ctx, t0, 0)
		if err != nil || !ok || claimed.ID != want {
			t.Fatalf("claim = %+v ok=%v err=%v, want %d", claimed, ok, err, want)
		}
	}
	assertFlag := func(label string, id int64, want bool) {
		t.Helper()
		if got := mustGet(t, store, id); got.PublishNow != want {
			t.Fatalf("%s: publish_now = %v, want %v (%+v)", label, got.PublishNow, want, got)
		}
	}

	// Transient requeue keeps the flag; failing clears it.
	failing := flag("https://example.com/failing")
	claim(failing)
	if err := store.Requeue(ctx, failing, "Gemini returned 503", t0, t0); err != nil {
		t.Fatal(err)
	}
	assertFlag("requeued", failing, true)
	claim(failing)
	if err := store.RequeueWithoutAttempt(ctx, failing, "gemini responded 403", t0, t0); err != nil {
		t.Fatal(err)
	}
	assertFlag("requeued without attempt", failing, true)
	claim(failing)
	if err := store.MarkFailed(ctx, failing, "source text too short", t0); err != nil {
		t.Fatal(err)
	}
	assertFlag("failed", failing, false)

	// Unqueue clears it.
	unqueued := flag("https://example.com/unqueued")
	if err := store.MarkPending(ctx, unqueued, t0); err != nil {
		t.Fatal(err)
	}
	assertFlag("unqueued", unqueued, false)

	// Rejection (after unqueue) keeps it clear, and a direct move to
	// rejected clears it too.
	if err := store.MarkRejected(ctx, unqueued, t0); err != nil {
		t.Fatal(err)
	}
	assertFlag("rejected", unqueued, false)
	rejected := flag("https://example.com/rejected")
	setStatus(t, db, rejected, "rejected", t0)
	assertFlag("forced rejected", rejected, false)

	// Crash recovery that fails a worn candidate clears it.
	worn := flag("https://example.com/worn")
	claim(worn)
	mustExec(t, db, `UPDATE candidates SET attempts = 3 WHERE id = ?`, worn)
	if _, failed, err := store.ResetProcessing(ctx, t0, 3); err != nil || failed != 1 {
		t.Fatalf("reset failed=%d err=%v", failed, err)
	}
	assertFlag("failed by recovery", worn, false)

	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM candidate_publish_now`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("flag rows = %d err=%v, want none left", rows, err)
	}
}
