package inlineimage

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

const (
	// MaxAttempts is how many failed tries an article gets.
	MaxAttempts = 2
	// Window is how far back the worker looks. Turning the worker on
	// therefore also illustrates the last week's articles.
	Window = 7 * 24 * time.Hour
	// DefaultInterval is how often the worker picks an article.
	DefaultInterval = 5 * time.Minute

	bookkeepingTimeout = 15 * time.Second
	maxErrorLength     = 500
)

// Store is the worker's view of the database.
type Store interface {
	Pending(ctx context.Context, since time.Time, maxAttempts int) ([]Article, error)
	// MarkReady records the image; credit is nil for a generated
	// illustration and set for a real photo.
	MarkReady(ctx context.Context, articleID int64, url, alt string, afterParagraph, attempts int, credit *realphoto.Credit) error
	MarkFailed(ctx context.Context, articleID int64, message string, attempts int) error
}

// Illustrator makes and stores one inline image (illustration.Pipeline).
type Illustrator interface {
	IllustrateInline(ctx context.Context, request illustration.InlineRequest) (illustration.InlineIllustration, error)
}

// PhotoFinder looks for a real photo of the article's subject
// (realphoto.Finder). It never fails; Result.Found says whether it found one.
type PhotoFinder interface {
	Find(ctx context.Context, request realphoto.Request) realphoto.Result
}

// PhotoStore stores a found photo as the inline image (illustration.Pipeline).
type PhotoStore interface {
	StoreInlinePhoto(ctx context.Context, slug string, photo []byte) (illustration.InlineIllustration, error)
}

// Worker gives recent long articles a second image, one per run: a real
// photo when one is found, otherwise a generated illustration.
type Worker struct {
	store       Store
	illustrator Illustrator
	photos      PhotoFinder
	photoStore  PhotoStore
	now         func() time.Time
	logger      *slog.Logger
	// ownPrefix is where our featured images live (public S3 URL and
	// prefix); a featured image elsewhere is not ours. Empty skips the check.
	ownPrefix string
	// revalidator, when set, asks the site to refresh an article's cached
	// page once its inline image is ready.
	revalidator Revalidator
}

// Revalidator queues article slugs whose site pages must be refreshed
// (siterevalidate.Notifier). Notify returns at once and never fails.
type Revalidator interface {
	Notify(slugs []string)
}

// WithRevalidator makes the worker ask the site to refresh an article's
// cached page after its inline image is recorded.
func (w *Worker) WithRevalidator(revalidator Revalidator) *Worker {
	w.revalidator = revalidator
	return w
}

func (w *Worker) revalidate(slug string) {
	if w.revalidator != nil && slug != "" {
		w.revalidator.Notify([]string{slug})
	}
}

func NewWorker(store Store, illustrator Illustrator, now func() time.Time, logger *slog.Logger, ownPrefix string) *Worker {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{store: store, illustrator: illustrator, now: now, logger: logger, ownPrefix: ownPrefix}
}

// WithPhotos makes the worker look for a real photo (Wikimedia Commons, then
// the maker's official image) before drawing an illustration.
func (w *Worker) WithPhotos(finder PhotoFinder, store PhotoStore) *Worker {
	w.photos, w.photoStore = finder, store
	return w
}

// PhotoRequest is what the photo finder is told about an article. The
// source URL is only read, never credited.
func PhotoRequest(article Article) realphoto.Request {
	return realphoto.Request{Slug: article.Slug, Title: article.Title, Text: strings.Join(Paragraphs(article.Content), "\n\n"), SourceURL: article.SourceURL}
}

// OwnImagePrefix is the URL prefix of the featured images the pipeline
// stores: the public S3 base URL and the key prefix.
func OwnImagePrefix(publicBaseURL, keyPrefix string) string {
	base := strings.TrimRight(publicBaseURL, "/")
	if base == "" {
		return ""
	}
	if prefix := strings.Trim(keyPrefix, "/"); prefix != "" {
		base += "/" + prefix
	}
	return base + "/"
}

// Loop runs RunOnce now and then every interval until ctx is done.
func (w *Worker) Loop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
			w.logger.ErrorContext(ctx, "inline image run failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunOnce illustrates the newest eligible article, if any, and reports
// whether it tried one. Articles too short for an image are passed over;
// ones whose featured image is not ours are marked failed for good.
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	articles, err := w.store.Pending(ctx, w.now().Add(-Window), MaxAttempts)
	if err != nil {
		return false, err
	}
	for _, article := range articles {
		photoSlot, ok := PhotoSlot(article.Content)
		if !ok {
			continue
		}
		if !w.searchesPhotos() {
			// Without a photo search only an illustration is possible:
			// pass over what can't get one without spending this run.
			if _, long := Slot(article.Content); !long {
				continue
			}
			if !w.ours(article) {
				if err := w.fail(ctx, article, "featured image is not one of ours: "+article.FeaturedImage, MaxAttempts); err != nil {
					return false, err
				}
				continue
			}
		}
		return true, w.illustrate(ctx, article, photoSlot)
	}
	return false, nil
}

// illustrate tries a real photo first. Without one, a body long enough for a
// generated illustration whose featured image is ours gets one; any other
// article is marked done, so it is not searched again.
func (w *Worker) illustrate(ctx context.Context, article Article, photoSlot int) error {
	logger := w.logger.With("article", article.ID, "slug", article.Slug, "after_paragraph", photoSlot)
	if done, err := w.tryPhoto(ctx, logger, article, photoSlot); done || err != nil {
		return err
	}
	slot, ok := Slot(article.Content)
	if !ok {
		logger.InfoContext(ctx, "inline image skipped", "reason", "no real photo and too short for an illustration")
		return w.fail(ctx, article, "no real photo, and too short for a generated illustration", MaxAttempts)
	}
	if !w.ours(article) {
		return w.fail(ctx, article, "featured image is not one of ours: "+article.FeaturedImage, MaxAttempts)
	}
	logger = w.logger.With("article", article.ID, "slug", article.Slug, "after_paragraph", slot)
	result, err := w.illustrator.IllustrateInline(ctx, illustration.InlineRequest{
		Slug: article.Slug, Title: article.Title, Section: SectionText(article.Content, slot),
		FeaturedImageURL: article.FeaturedImage, SourceImageURL: article.SourceImage,
	})
	if err != nil {
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case publisher.IsSystemFault(err):
			// No article is to blame (a Codex login, S3 credentials): keep
			// its attempts and try again next run.
			return err
		case errors.Is(err, illustration.ErrNotOurImage):
			logger.InfoContext(ctx, "inline image skipped", "reason", err)
			return w.fail(ctx, article, err.Error(), MaxAttempts)
		}
		logger.WarnContext(ctx, "inline image failed", "attempt", article.Attempts+1, "error", err)
		return w.fail(ctx, article, err.Error(), article.Attempts+1)
	}
	bookkeeping, cancel := context.WithTimeout(context.WithoutCancel(ctx), bookkeepingTimeout)
	defer cancel()
	if err := w.store.MarkReady(bookkeeping, article.ID, result.URL, result.Alt, slot, article.Attempts+1, nil); err != nil {
		if result.Discard != nil {
			result.Discard(bookkeeping)
		}
		return err
	}
	logger.InfoContext(ctx, "inline image added", "path", "generated", "url", result.URL)
	w.revalidate(article.Slug)
	return nil
}

func (w *Worker) searchesPhotos() bool { return w.photos != nil && w.photoStore != nil }

// ours reports whether the article's featured image is one of our own
// illustrations, the only kind an inline illustration may copy the style of.
func (w *Worker) ours(article Article) bool {
	return w.ownPrefix == "" || strings.HasPrefix(article.FeaturedImage, w.ownPrefix)
}

// tryPhoto looks for and stores a real photo. done is true when the article
// got one; every failure short of a cancelled context or a failed database
// write falls through to drawing an illustration.
func (w *Worker) tryPhoto(ctx context.Context, logger *slog.Logger, article Article, slot int) (bool, error) {
	if w.photos == nil || w.photoStore == nil {
		return false, nil
	}
	found := w.photos.Find(ctx, PhotoRequest(article))
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !found.Found || found.Credit == nil || len(found.Image) == 0 {
		logger.InfoContext(ctx, "inline image path", "path", "generated", "reason", found.Reason)
		return false, nil
	}
	stored, err := w.photoStore.StoreInlinePhoto(ctx, article.Slug, found.Image)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		logger.WarnContext(ctx, "inline photo not stored, drawing instead", "kind", found.Credit.Kind, "error", err)
		return false, nil
	}
	bookkeeping, cancel := context.WithTimeout(context.WithoutCancel(ctx), bookkeepingTimeout)
	defer cancel()
	if err := w.store.MarkReady(bookkeeping, article.ID, stored.URL, found.Alt, slot, article.Attempts+1, found.Credit); err != nil {
		if stored.Discard != nil {
			stored.Discard(bookkeeping)
		}
		return false, err
	}
	logger.InfoContext(ctx, "inline image added", "path", found.Credit.Kind, "reason", found.Reason, "credit", found.Credit.Text, "url", stored.URL)
	w.revalidate(article.Slug)
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
