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
	skipped    map[int64]string
	notes      string
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

func (s *memoryStore) MarkSkipped(_ context.Context, articleID int64, reason string) error {
	if s.skipped == nil {
		s.skipped = map[int64]string{}
	}
	s.skipped[articleID] = reason
	s.sent[articleID] = 0 // never due again
	return nil
}

func (s *memoryStore) SelectNotes(context.Context) (string, error) { return s.notes, nil }

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

type scriptedSelector struct {
	answers map[int64]bool
	err     error
	notes   []string
}

func (s *scriptedSelector) Select(_ context.Context, post telegram.Post, notes string) (bool, string, error) {
	s.notes = append(s.notes, notes)
	if s.err != nil {
		return false, "", s.err
	}
	return s.answers[post.ID], "because", nil
}

func selectiveWorker(store *memoryStore, poster *fakePoster, now *clock, selector telegram.Selector) *telegram.Worker {
	return telegram.NewWorker(store, poster, telegram.WorkerConfig{
		Chat: "@channel", SiteURL: site, MinGap: 5 * time.Minute, Location: istanbul, Now: now.Now, Selector: selector,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestOnlySelectedArticlesArePosted(t *testing.T) {
	_, store, poster, now := newWorker(t, 1, 2)
	store.notes = "- no local news"
	selector := &scriptedSelector{answers: map[int64]bool{1: false, 2: true}}
	worker := selectiveWorker(store, poster, now, selector)
	if posted, err := worker.RunOnce(context.Background()); err != nil || posted {
		t.Fatalf("article 1 must be skipped: posted=%v err=%v", posted, err)
	}
	if store.skipped[1] != "because" || len(poster.posts) != 0 {
		t.Fatalf("skipped=%v posts=%v", store.skipped, poster.posts)
	}
	// A skip does not start the gap between posts: the next article goes out at once.
	if posted, err := worker.RunOnce(context.Background()); err != nil || !posted {
		t.Fatalf("article 2 must be posted: posted=%v err=%v", posted, err)
	}
	if len(poster.posts) != 1 || selector.notes[0] != "- no local news" {
		t.Fatalf("posts=%v notes=%v", poster.posts, selector.notes)
	}
}

func TestSelectorFailurePostsNothingAndWaits(t *testing.T) {
	_, store, poster, now := newWorker(t, 1)
	selector := &scriptedSelector{err: errors.New("model at capacity")}
	worker := selectiveWorker(store, poster, now, selector)
	for i := 0; i < 2; i++ {
		if posted, err := worker.RunOnce(context.Background()); err != nil || posted {
			t.Fatalf("posted=%v err=%v", posted, err)
		}
		now.now = now.now.Add(time.Minute)
	}
	if len(selector.notes) != 1 || len(poster.posts) != 0 || len(store.skipped) != 0 {
		t.Fatalf("asked %d times, posts=%v skipped=%v", len(selector.notes), poster.posts, store.skipped)
	}
	selector.err, selector.answers = nil, map[int64]bool{1: true}
	now.now = now.now.Add(10 * time.Minute)
	if posted, err := worker.RunOnce(context.Background()); err != nil || !posted {
		t.Fatalf("posted=%v err=%v", posted, err)
	}
}

type fixedText struct{ answer string }

func (f fixedText) GenerateJSON(context.Context, string) (string, error) { return f.answer, nil }

func TestTextSelectorReadsTheAnswer(t *testing.T) {
	post := telegram.Post{ID: 1, Title: "OpenAI releases a model"}
	ok, reason, err := telegram.TextSelector{Text: fixedText{"sure: {\"post\":true,\"reason\":\"A new model.\"}"}}.Select(context.Background(), post, "")
	if err != nil || !ok || reason != "A new model." {
		t.Fatalf("ok=%v reason=%q err=%v", ok, reason, err)
	}
	if _, _, err := (telegram.TextSelector{Text: fixedText{"no idea"}}).Select(context.Background(), post, ""); err == nil {
		t.Fatal("an unreadable answer must be an error, not a decision")
	}
	if !strings.Contains(telegram.SelectPrompt(post, "- no crypto"), "- no crypto") {
		t.Fatal("notes missing from the prompt")
	}
}
