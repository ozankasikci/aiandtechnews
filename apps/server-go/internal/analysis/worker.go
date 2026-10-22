package analysis

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"
)

const (
	MaxAttempts     = 2
	Window          = 14 * 24 * time.Hour
	DefaultInterval = 10 * time.Minute
)

type Store interface {
	Pending(context.Context, time.Time) (Article, error)
	Related(context.Context, int64) ([]Related, error)
	Save(context.Context, int64, Result, int) error
}

type Revalidator interface{ Notify([]string) }

type Worker struct {
	store       Store
	writer      Writer
	checker     Checker
	now         func() time.Time
	logger      *slog.Logger
	revalidator Revalidator
}

func NewWorker(store Store, writer Writer, checker Checker, now func() time.Time, logger *slog.Logger) *Worker {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{store: store, writer: writer, checker: checker, now: now, logger: logger}
}

func (w *Worker) WithRevalidator(site Revalidator) *Worker { w.revalidator = site; return w }

// RunOnce drafts and checks one recent article. Failures consume an attempt;
// explicit skips and checker rejections are final and contain no text.
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	article, err := w.store.Pending(ctx, w.now().Add(-Window))
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	related, err := w.store.Related(ctx, article.ID)
	result := Result{Status: "failed"}
	if err == nil {
		_, result, err = Evaluate(ctx, w.writer, w.checker, article, related)
	} else {
		result.Reason = err.Error()
	}
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	bookkeeping, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if saveErr := w.store.Save(bookkeeping, article.ID, result, article.Attempts+1); saveErr != nil {
		return true, errors.Join(err, saveErr)
	}
	if result.Status == "ready" && w.revalidator != nil {
		w.revalidator.Notify([]string{article.Slug})
	}
	return true, err
}

func (w *Worker) Loop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
			w.logger.ErrorContext(ctx, "analysis run failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
