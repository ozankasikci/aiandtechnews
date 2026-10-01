// Package featuredreimage gives articles whose featured image is a news
// source's own photo an image of our own. The site never shows or copies a
// source's images; older articles (before the pipeline stopped copying) still
// point at them. The worker makes a new image one article at a time, newest
// first, with the featured-image pipeline's generating providers only, and
// switches the article over to it.
package featuredreimage

import (
	"context"
	"log/slog"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

const (
	// MaxAttempts is how many failed tries an article gets.
	MaxAttempts = 3
	// DefaultInterval is how often the worker picks an article.
	DefaultInterval = 10 * time.Minute

	bookkeepingTimeout = 15 * time.Second
	maxErrorLength     = 500
)

// Store is the worker's view of the database (SQLiteStore).
type Store interface {
	Pending(ctx context.Context, maxAttempts int) ([]Article, error)
	Replace(ctx context.Context, articleID int64, oldURL, newURL string) (bool, error)
	MarkFailed(ctx context.Context, articleID int64, message string, attempts int) error
}

// Illustrator draws and stores a new featured image without ever copying a
// source photo (illustration.Pipeline.IllustrateGenerated). It shares the
// pipeline's one-image-at-a-time lock with the publisher and inline worker.
type Illustrator interface {
	IllustrateGenerated(ctx context.Context, request publisher.IllustrationRequest) (publisher.Illustration, error)
}

// Revalidator queues article slugs whose site pages must be refreshed
// (siterevalidate.Notifier). Notify returns at once and never fails.
type Revalidator interface {
	Notify(slugs []string)
}

type Worker struct {
	store       Store
	illustrator Illustrator
	revalidator Revalidator
	logger      *slog.Logger
}

func NewWorker(store Store, illustrator Illustrator, revalidator Revalidator, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{store: store, illustrator: illustrator, revalidator: revalidator, logger: logger}
}

// Loop runs RunOnce now and then every interval until ctx is done.
func (w *Worker) Loop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
			w.logger.ErrorContext(ctx, "featured image re-image run failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunOnce replaces the featured image of the newest article that needs one,
// and reports whether it tried one. The article's source photo is never
// fetched: the new image is drawn from the headline and excerpt alone.
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	articles, err := w.store.Pending(ctx, MaxAttempts)
	if err != nil || len(articles) == 0 {
		return false, err
	}
	article := articles[0]
	logger := w.logger.With("article", article.ID, "slug", article.Slug, "attempt", article.Attempts+1)
	made, err := w.illustrator.IllustrateGenerated(ctx, publisher.IllustrationRequest{
		Slug: article.Slug, Title: article.Title, Excerpt: article.Excerpt,
	})
	if err != nil {
		switch {
		case ctx.Err() != nil:
			return true, ctx.Err()
		case publisher.IsSystemFault(err):
			// No article is to blame (a Codex login, S3 credentials): keep
			// its attempts and try again next run.
			logger.WarnContext(ctx, "featured image re-image postponed", "error", err)
			return true, err
		case publisher.IsPermanent(err):
			logger.WarnContext(ctx, "featured image re-image failed for good", "error", err)
			return true, w.fail(ctx, article, err.Error(), MaxAttempts)
		}
		logger.WarnContext(ctx, "featured image re-image failed", "error", err)
		return true, w.fail(ctx, article, err.Error(), article.Attempts+1)
	}
	bookkeeping, cancel := context.WithTimeout(context.WithoutCancel(ctx), bookkeepingTimeout)
	defer cancel()
	changed, err := w.store.Replace(bookkeeping, article.ID, article.FeaturedImage, made.URL)
	if err != nil {
		// The new image stays in storage unused; nothing is ever deleted here.
		return true, err
	}
	if !changed {
		logger.InfoContext(ctx, "featured image changed meanwhile; left alone", "unused_image", made.URL)
		return true, nil
	}
	logger.InfoContext(ctx, "featured image replaced", "old", article.FeaturedImage, "new", made.URL)
	if w.revalidator != nil {
		w.revalidator.Notify([]string{article.Slug})
	}
	return true, nil
}

func (w *Worker) fail(ctx context.Context, article Article, message string, attempts int) error {
	if len(message) > maxErrorLength {
		message = message[:maxErrorLength]
	}
	bookkeeping, cancel := context.WithTimeout(context.WithoutCancel(ctx), bookkeepingTimeout)
	defer cancel()
	return w.store.MarkFailed(bookkeeping, article.ID, message, attempts)
}
