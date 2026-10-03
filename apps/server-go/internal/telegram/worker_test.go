package telegram_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/telegram"
)

type memoryStore struct {
	startAfter time.Time
	lastPosted time.Time
	due        []*telegram.Post
	sent       map[int64]int64
	attempts   map[int64]int
}

func (s *memoryStore) StartAfter(context.Context) (time.Time, error) { return s.startAfter, nil }
func (s *memoryStore) LastPostedAt(context.Context) (time.Time, error) {
	return s.lastPosted, nil
}

func (s *memoryStore) NextDue(_ context.Context, _ time.Time, maxAttempts int) (*telegram.Post, error) {
	for _, post := range s.due {
		if _, sent := s.sent[post.ID]; !sent && s.attempts[post.ID] < maxAttempts {
			copied := *post
			copied.Attempts = s.attempts[post.ID]
			return &copied, nil
		}
	}
	return nil, nil
}

func (s *memoryStore) MarkSent(_ context.Context, articleID, messageID int64, _ time.Time) error {
	s.sent[articleID] = messageID
	return nil
}

func (s *memoryStore) MarkFailed(_ context.Context, articleID int64, _ string, _ time.Time) (int, error) {
	s.attempts[articleID]++
	return s.attempts[articleID], nil
}

type fakePoster struct {
	posts []string // "chat|imageURL"
	err   error
}

func (p *fakePoster) Post(_ context.Context, chat, text, imageURL string) (int64, error) {
	if p.err != nil {
		return 0, p.err
	}
	p.posts = append(p.posts, chat+"|"+imageURL)
	if !strings.Contains(text, "utm_source=telegram") {
		return 0, errors.New("caption without link")
	}
	return int64(100 + len(p.posts)), nil
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

var istanbul = time.FixedZone("TRT", 3*60*60)

func newWorker(t *testing.T, posts ...int64) (*telegram.Worker, *memoryStore, *fakePoster, *clock) {
	t.Helper()
	store := &memoryStore{sent: map[int64]int64{}, attempts: map[int64]int{}}
	for _, id := range posts {
		store.due = append(store.due, &telegram.Post{ID: id, Slug: "a", Title: "T",
			FeaturedImage: "https://aiandtech.news/images/x.webp"})
	}
	poster := &fakePoster{}
	quiet, err := telegram.ParseQuietHours("00:00-08:00")
	if err != nil {
		t.Fatal(err)
	}
	// 09:00 in Istanbul.
	now := &clock{now: time.Date(2026, 10, 3, 6, 0, 0, 0, time.UTC)}
	worker := telegram.NewWorker(store, poster, telegram.WorkerConfig{
		Chat: "@channel", SiteURL: site, QuietHours: quiet, MinGap: 5 * time.Minute, Location: istanbul, Now: now.Now,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return worker, store, poster, now
}

func runOnce(t *testing.T, worker *telegram.Worker) bool {
	t.Helper()
	posted, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return posted
}

func TestWorkerPostsOnePerTickAndKeepsTheGap(t *testing.T) {
	worker, store, poster, now := newWorker(t, 1, 2)
	if !runOnce(t, worker) || store.sent[1] != 101 {
		t.Fatalf("sent = %v", store.sent)
	}
	if poster.posts[0] != "@channel|https://aiandtech.news/images/x.webp" {
		t.Fatalf("posts = %v", poster.posts)
	}
	now.now = now.now.Add(4 * time.Minute)
	if runOnce(t, worker) {
		t.Fatal("posted inside the minimum gap")
	}
	now.now = now.now.Add(time.Minute)
	if !runOnce(t, worker) || store.sent[2] != 102 {
		t.Fatalf("sent = %v", store.sent)
	}
	now.now = now.now.Add(time.Hour)
	if runOnce(t, worker) {
		t.Fatal("posted with nothing due")
	}
}

func TestWorkerGapSurvivesARestart(t *testing.T) {
	worker, store, _, now := newWorker(t, 1)
	store.lastPosted = now.now.Add(-2 * time.Minute)
	if runOnce(t, worker) {
		t.Fatal("posted within the gap of the previous run's last post")
	}
	now.now = now.now.Add(3 * time.Minute)
	if !runOnce(t, worker) {
		t.Fatal("did not post after the gap")
	}
}

func TestWorkerIsSilentDuringQuietHoursInItsZone(t *testing.T) {
	worker, store, _, now := newWorker(t, 1)
	// 04:59 UTC is 07:59 in Istanbul: quiet. 05:00 UTC is 08:00: not.
	now.now = time.Date(2026, 10, 3, 4, 59, 0, 0, time.UTC)
	if runOnce(t, worker) {
		t.Fatal("posted during quiet hours")
	}
	now.now = time.Date(2026, 10, 2, 21, 0, 0, 0, time.UTC) // midnight in Istanbul
	if runOnce(t, worker) {
		t.Fatal("posted at midnight")
	}
	now.now = time.Date(2026, 10, 3, 5, 0, 0, 0, time.UTC)
	if !runOnce(t, worker) || len(store.sent) != 1 {
		t.Fatal("did not post when quiet hours ended")
	}
}

func TestWorkerRetriesPermanentFailuresUpToTheCap(t *testing.T) {
	worker, store, poster, now := newWorker(t, 1, 2)
	poster.err = &telegram.Error{Method: "sendMessage", StatusCode: 400}
	for i := 1; i <= telegram.MaxAttempts; i++ {
		if runOnce(t, worker) || store.attempts[1] != i {
			t.Fatalf("try %d: attempts = %v", i, store.attempts)
		}
		now.now = now.now.Add(time.Minute) // failures do not start the gap
	}
	if runOnce(t, worker) || store.attempts[2] != 1 {
		t.Fatalf("attempts = %v, want article 2 next", store.attempts)
	}
	poster.err = nil
	if !runOnce(t, worker) || store.sent[2] == 0 {
		t.Fatalf("sent = %v", store.sent)
	}
}

func TestWorkerKeepsAttemptsOnTransientErrorsAndHonoursRetryAfter(t *testing.T) {
	worker, store, poster, now := newWorker(t, 1)
	poster.err = &telegram.Error{Method: "sendPhoto", StatusCode: 429, RetryAfter: 90 * time.Second}
	if runOnce(t, worker) || store.attempts[1] != 0 {
		t.Fatalf("attempts = %v", store.attempts)
	}
	poster.err = nil
	now.now = now.now.Add(time.Minute)
	if runOnce(t, worker) {
		t.Fatal("posted before retry_after passed")
	}
	now.now = now.now.Add(30 * time.Second)
	if !runOnce(t, worker) {
		t.Fatal("did not post after retry_after")
	}
}
