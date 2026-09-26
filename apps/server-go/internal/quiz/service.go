package quiz

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	// generateFromHour is the UTC hour from which the loop writes the day's quiz.
	generateFromHour = 4
	// retryAfter is the least time between loop attempts after a failure.
	retryAfter = time.Hour
	// articleWindow and maxArticles bound the articles a quiz is written from.
	articleWindow = 7 * 24 * time.Hour
	maxArticles   = 25
)

type store interface {
	RecentArticles(ctx context.Context, since time.Time, limit int) ([]Article, error)
	Save(ctx context.Context, day string, questions []Question, at time.Time) (Quiz, error)
	Latest(ctx context.Context) (Quiz, error)
	Pull(ctx context.Context, day string) (bool, error)
	Exists(ctx context.Context, day string) (bool, error)
}

type Service struct {
	store     store
	generator *Generator
	now       func() time.Time
	logger    *slog.Logger

	// generating serialises the loop and dashboard regenerations.
	generating sync.Mutex
	// lastFailure is when the loop's last attempt failed (zero after a success).
	lastFailure time.Time
}

// NewService builds the quiz service. A nil generator disables generation:
// the loop does nothing and Regenerate returns ErrGenerationDisabled.
func NewService(store store, generator *Generator, now func() time.Time, logger *slog.Logger) *Service {
	return &Service{store: store, generator: generator, now: now, logger: logger}
}

// Latest returns the most recent published quiz, or ErrNoQuiz.
func (s *Service) Latest(ctx context.Context) (Quiz, error) { return s.store.Latest(ctx) }

func (s *Service) today() string { return s.now().UTC().Format(time.DateOnly) }

// PullToday hides today's (UTC) quiz and reports whether there was one.
func (s *Service) PullToday(ctx context.Context) (string, bool, error) {
	day := s.today()
	pulled, err := s.store.Pull(ctx, day)
	return day, pulled, err
}

// Regenerate writes a new quiz for today and replaces today's quiz with it.
// The old quiz stays in place when generation fails.
func (s *Service) Regenerate(ctx context.Context) (Quiz, error) {
	if s.generator == nil {
		return Quiz{}, ErrGenerationDisabled
	}
	s.generating.Lock()
	defer s.generating.Unlock()
	return s.generate(ctx, s.today())
}

// GenerateIfDue writes today's quiz when it is at or after 04:00 UTC and no
// quiz row (published or pulled) exists for today. After a failed attempt it
// waits an hour before trying again. It reports whether a quiz was written.
func (s *Service) GenerateIfDue(ctx context.Context) (bool, error) {
	if s.generator == nil {
		return false, nil
	}
	s.generating.Lock()
	defer s.generating.Unlock()
	now := s.now().UTC()
	if now.Hour() < generateFromHour {
		return false, nil
	}
	if !s.lastFailure.IsZero() && now.Sub(s.lastFailure) < retryAfter {
		return false, nil
	}
	day := now.Format(time.DateOnly)
	exists, err := s.store.Exists(ctx, day)
	if err != nil || exists {
		return false, err
	}
	if _, err := s.generate(ctx, day); err != nil {
		s.lastFailure = now
		return false, err
	}
	s.lastFailure = time.Time{}
	return true, nil
}

func (s *Service) generate(ctx context.Context, day string) (Quiz, error) {
	now := s.now()
	articles, err := s.store.RecentArticles(ctx, now.Add(-articleWindow), maxArticles)
	if err != nil {
		return Quiz{}, err
	}
	questions, err := s.generator.Generate(ctx, day, articles)
	if err != nil {
		return Quiz{}, generationError{err}
	}
	return s.store.Save(ctx, day, questions, now)
}

// Loop checks every interval whether today's quiz is due, until ctx is done.
// Failures are logged; the loop never stops on one.
func (s *Service) Loop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		generated, err := s.GenerateIfDue(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			s.logger.ErrorContext(ctx, "daily quiz generation failed", "error", err)
		case generated:
			s.logger.InfoContext(ctx, "daily quiz published", "day", s.today())
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// generationError marks a failure of the model or its quiz, as opposed to a
// storage failure, so the dashboard can answer 502 instead of 500.
type generationError struct{ err error }

func (e generationError) Error() string { return e.err.Error() }
func (e generationError) Unwrap() error { return e.err }
