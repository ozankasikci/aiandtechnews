package quiz_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/quiz"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) now() time.Time   { c.mu.Lock(); defer c.mu.Unlock(); return c.at }
func (c *clock) set(at time.Time) { c.mu.Lock(); c.at = at; c.mu.Unlock() }

// seedTestArticles stores testArticles as published articles from yesterday.
func seedTestArticles(t *testing.T, db *sql.DB) {
	t.Helper()
	for i, article := range testArticles {
		published := time.Date(2026, 9, 26, 20-i, 0, 0, 0, time.UTC).Format(time.RFC3339)
		mustExec(t, db, `INSERT INTO articles (id, title, slug, content, category_id, author_id, status, published_at)
			VALUES (?, ?, ?, ?, 1, 1, 'published', ?)`, article.ID, article.Title, article.Slug, "<p>"+article.Text+"</p>", published)
	}
}

// lockedText is a concurrency-safe scriptedText that repeats its last response.
type lockedText struct {
	mu        sync.Mutex
	responses []string
	err       error
	calls     int
}

func (l *lockedText) GenerateJSON(context.Context, string) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	if l.err != nil {
		return "", l.err
	}
	return l.responses[min(l.calls-1, len(l.responses)-1)], nil
}

func (l *lockedText) count() int { l.mu.Lock(); defer l.mu.Unlock(); return l.calls }

func newService(t *testing.T, text quiz.TextGenerator, at time.Time) (*quiz.Service, *quiz.SQLiteStore, *clock) {
	t.Helper()
	store, db := openStore(t)
	seedTestArticles(t, db)
	c := &clock{at: at}
	var generator *quiz.Generator
	if text != nil {
		generator = quiz.NewGenerator(text)
	}
	return quiz.NewService(store, generator, c.now, discard), store, c
}

func TestGenerateIfDueWaitsForFourUTCThenGeneratesOncePerDay(t *testing.T) {
	text := &lockedText{responses: []string{encode(t, validQuestions())}}
	service, store, c := newService(t, text, time.Date(2026, 9, 27, 3, 59, 0, 0, time.UTC))
	ctx := context.Background()

	if generated, err := service.GenerateIfDue(ctx); err != nil || generated || text.count() != 0 {
		t.Fatalf("before 04:00: generated = %v, err = %v, calls = %d", generated, err, text.count())
	}
	c.set(time.Date(2026, 9, 27, 4, 0, 0, 0, time.UTC))
	if generated, err := service.GenerateIfDue(ctx); err != nil || !generated {
		t.Fatalf("at 04:00: generated = %v, err = %v", generated, err)
	}
	latest, err := store.Latest(ctx)
	if err != nil || latest.Day != "2026-09-27" || len(latest.Questions) != 3 {
		t.Fatalf("latest = %+v, %v", latest, err)
	}
	c.set(time.Date(2026, 9, 27, 18, 0, 0, 0, time.UTC))
	if generated, err := service.GenerateIfDue(ctx); err != nil || generated || text.count() != 1 {
		t.Fatalf("same day: generated = %v, err = %v, calls = %d", generated, err, text.count())
	}

	// A pulled day is not regenerated.
	if _, err := store.Pull(ctx, "2026-09-27"); err != nil {
		t.Fatal(err)
	}
	if generated, err := service.GenerateIfDue(ctx); err != nil || generated || text.count() != 1 {
		t.Fatalf("pulled day: generated = %v, err = %v, calls = %d", generated, err, text.count())
	}
}

func TestGenerateIfDueRetriesAtMostOncePerHourAfterAFailure(t *testing.T) {
	text := &lockedText{err: errors.New("gemini down")}
	service, _, c := newService(t, text, time.Date(2026, 9, 27, 4, 0, 0, 0, time.UTC))
	ctx := context.Background()

	if _, err := service.GenerateIfDue(ctx); err == nil || text.count() != 1 {
		t.Fatalf("first attempt: err = %v, calls = %d", err, text.count())
	}
	c.set(time.Date(2026, 9, 27, 4, 59, 0, 0, time.UTC))
	if generated, err := service.GenerateIfDue(ctx); err != nil || generated || text.count() != 1 {
		t.Fatalf("within the hour: generated = %v, err = %v, calls = %d", generated, err, text.count())
	}
	c.set(time.Date(2026, 9, 27, 5, 0, 0, 0, time.UTC))
	text.mu.Lock()
	text.err, text.responses = nil, []string{encode(t, validQuestions())}
	text.mu.Unlock()
	if generated, err := service.GenerateIfDue(ctx); err != nil || !generated || text.count() != 2 {
		t.Fatalf("after an hour: generated = %v, err = %v, calls = %d", generated, err, text.count())
	}
}

func TestGenerateIfDueDoesNothingWithoutAGenerator(t *testing.T) {
	service, _, _ := newService(t, nil, time.Date(2026, 9, 27, 5, 0, 0, 0, time.UTC))
	if generated, err := service.GenerateIfDue(context.Background()); err != nil || generated {
		t.Fatalf("generated = %v, err = %v", generated, err)
	}
	if _, err := service.Regenerate(context.Background()); !errors.Is(err, quiz.ErrGenerationDisabled) {
		t.Fatalf("Regenerate err = %v", err)
	}
}

func TestRegenerateReplacesTodaysQuizOnlyWhenTheNewOneIsValid(t *testing.T) {
	good := encode(t, validQuestions())
	bad := validQuestions()
	bad[0].Answer = 1
	text := &lockedText{responses: []string{good}}
	service, store, _ := newService(t, text, time.Date(2026, 9, 27, 6, 0, 0, 0, time.UTC))
	ctx := context.Background()

	first, err := service.Regenerate(ctx)
	if err != nil || first.Day != "2026-09-27" || len(first.Questions) != 3 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	text.mu.Lock()
	text.calls, text.responses = 0, []string{encode(t, bad)}
	text.mu.Unlock()
	if _, err := service.Regenerate(ctx); !errors.Is(err, quiz.ErrInvalidQuiz) {
		t.Fatalf("invalid regenerate err = %v", err)
	}
	latest, err := store.Latest(ctx)
	if err != nil || latest.Number != first.Number {
		t.Fatalf("a failed regenerate changed today's quiz: %+v, %v", latest, err)
	}
}

func TestPullTodayPullsOnlyTodaysQuiz(t *testing.T) {
	service, store, _ := newService(t, nil, time.Date(2026, 9, 27, 6, 0, 0, 0, time.UTC))
	ctx := context.Background()
	if day, pulled, err := service.PullToday(ctx); err != nil || pulled || day != "2026-09-27" {
		t.Fatalf("empty: day = %q, pulled = %v, err = %v", day, pulled, err)
	}
	if _, err := store.Save(ctx, "2026-09-27", sampleQuestions, time.Now()); err != nil {
		t.Fatal(err)
	}
	if day, pulled, err := service.PullToday(ctx); err != nil || !pulled || day != "2026-09-27" {
		t.Fatalf("day = %q, pulled = %v, err = %v", day, pulled, err)
	}
	if _, err := service.Latest(ctx); !errors.Is(err, quiz.ErrNoQuiz) {
		t.Fatalf("Latest after pull = %v", err)
	}
}

func TestLoopGeneratesOnItsFirstTickAndStopsOnCancel(t *testing.T) {
	text := &lockedText{responses: []string{encode(t, validQuestions())}}
	service, store, _ := newService(t, text, time.Date(2026, 9, 27, 4, 30, 0, 0, time.UTC))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		service.Loop(ctx, time.Hour)
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if exists, _ := store.Exists(context.Background(), "2026-09-27"); exists {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("loop did not generate a quiz")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not stop")
	}
}
