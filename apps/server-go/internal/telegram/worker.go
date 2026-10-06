// Package telegram posts every newly published article to a Telegram
// channel through a bot: the featured image (when it is one of ours) with an
// HTML caption, or a text message with a link preview. A background worker
// posts at most one article per tick, oldest first, outside quiet hours and
// never sooner than a minimum gap after the previous post, so a morning
// backlog is spread out. Each article's outcome is kept in telegram_posts;
// only articles published after the channel's start marker are posted.
package telegram

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/ownimage"
)

const (
	// MaxAttempts is how many failed tries an article gets.
	MaxAttempts = 5
	// DefaultInterval is how often the worker looks for an article.
	DefaultInterval = time.Minute
	// MinInterval is the shortest TELEGRAM_INTERVAL allowed.
	MinInterval = 30 * time.Second
	// DefaultMinGap is the shortest time between two posts.
	DefaultMinGap = 5 * time.Minute

	bookkeepingTimeout = 15 * time.Second
	// selectRetryDelay is the pause after the selector's model failed.
	selectRetryDelay = 10 * time.Minute
	maxErrorLength   = 500
)

// Store is the worker's view of the database (SQLiteStore).
type Store interface {
	StartAfter(ctx context.Context) (time.Time, error)
	LastPostedAt(ctx context.Context) (time.Time, error)
	NextDue(ctx context.Context, startAfter time.Time, maxAttempts int) (*Post, error)
	MarkSent(ctx context.Context, articleID, messageID int64, now time.Time) error
	MarkFailed(ctx context.Context, articleID int64, message string, now time.Time) (int, error)
	MarkSkipped(ctx context.Context, articleID int64, reason string) error
	SelectNotes(ctx context.Context) (string, error)
}

// Poster sends one post to a chat (Client.Post).
type Poster interface {
	Post(ctx context.Context, chat, text, imageURL string) (int64, error)
}

// WorkerConfig is the worker's settings.
type WorkerConfig struct {
	// Chat is the channel: "@name" or a numeric id.
	Chat string
	// SiteURL is the public site origin the links point to.
	SiteURL    string
	QuietHours QuietHours
	MinGap     time.Duration
	// Location is the zone quiet hours are read in (nil means time.Local).
	Location *time.Location
	// Now is the clock (nil means time.Now).
	Now func() time.Time
	// Selector decides which articles the channel carries; nil posts all.
	Selector Selector
}

type Worker struct {
	store  Store
	poster Poster
	cfg    WorkerConfig
	logger *slog.Logger

	loaded     bool
	startAfter time.Time
	lastPost   time.Time
	// notBefore is when Telegram's flood control lets the next call through.
	notBefore time.Time
}

func NewWorker(store Store, poster Poster, cfg WorkerConfig, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Location == nil {
		cfg.Location = time.Local
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Worker{store: store, poster: poster, cfg: cfg, logger: logger}
}

// Loop runs RunOnce now and then every interval until ctx is done.
func (w *Worker) Loop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
			w.logger.ErrorContext(ctx, "telegram post run failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunOnce posts the oldest due article, unless it is quiet hours, the last
// post was less than MinGap ago or Telegram asked to wait. It reports whether
// it posted one.
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	now := w.cfg.Now()
	if w.cfg.QuietHours.Contains(now.In(w.cfg.Location)) || now.Before(w.notBefore) {
		return false, nil
	}
	if !w.loaded {
		// The start marker is set on the first run, so enabling the channel
		// never posts the archive; the last post survives restarts.
		startAfter, err := w.store.StartAfter(ctx)
		if err != nil {
			return false, err
		}
		lastPost, err := w.store.LastPostedAt(ctx)
		if err != nil {
			return false, err
		}
		w.startAfter, w.lastPost, w.loaded = startAfter, lastPost, true
	}
	if !w.lastPost.IsZero() && now.Sub(w.lastPost) < w.cfg.MinGap {
		return false, nil
	}
	post, err := w.store.NextDue(ctx, w.startAfter, MaxAttempts)
	if err != nil || post == nil {
		return false, err
	}
	logger := w.logger.With("article", post.ID, "slug", post.Slug, "attempt", post.Attempts+1)
	if w.cfg.Selector != nil && post.Attempts == 0 {
		notes, err := w.store.SelectNotes(ctx)
		if err != nil {
			return false, err
		}
		wanted, reason, err := w.cfg.Selector.Select(ctx, *post, notes)
		if err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			// The model is to blame, not the article: ask again later.
			w.notBefore = now.Add(selectRetryDelay)
			logger.WarnContext(ctx, "telegram selection postponed", "error", err)
			return false, nil
		}
		if !wanted {
			if err := w.store.MarkSkipped(ctx, post.ID, reason); err != nil {
				return false, err
			}
			logger.InfoContext(ctx, "telegram post skipped", "title", post.Title, "reason", reason)
			return false, nil
		}
		logger.InfoContext(ctx, "telegram post selected", "reason", reason)
	}
	messageID, err := w.poster.Post(ctx, w.cfg.Chat, Caption(*post, w.cfg.SiteURL), PhotoURL(post.FeaturedImage, w.cfg.SiteURL))
	bookkeeping, cancel := context.WithTimeout(context.WithoutCancel(ctx), bookkeepingTimeout)
	defer cancel()
	if err != nil {
		switch {
		case ctx.Err() != nil:
			return false, ctx.Err()
		case IsTransient(err):
			// Telegram or the network is to blame, not the article: keep its
			// attempts and try again next tick (or after retry_after).
			if wait := RetryAfter(err); wait > 0 {
				w.notBefore = now.Add(wait)
			}
			logger.WarnContext(ctx, "telegram post postponed", "error", err)
			return false, nil
		}
		message := err.Error()
		if len(message) > maxErrorLength {
			message = message[:maxErrorLength]
		}
		attempts, markErr := w.store.MarkFailed(bookkeeping, post.ID, message, now)
		if markErr != nil {
			return false, markErr
		}
		if attempts >= MaxAttempts {
			logger.ErrorContext(ctx, "telegram post failed for good", "attempts", attempts, "error", err)
		} else {
			logger.WarnContext(ctx, "telegram post failed", "attempts", attempts, "error", err)
		}
		return false, nil
	}
	w.lastPost = now
	if err := w.store.MarkSent(bookkeeping, post.ID, messageID, now); err != nil {
		return true, err
	}
	logger.InfoContext(ctx, "telegram post sent", "message_id", messageID)
	return true, nil
}

// PhotoURL is the absolute URL of the featured image when it is one of our
// own images (ownimage.Is), else "": a news source's photo is never posted,
// and the site's generic placeholder is left to the link preview.
func PhotoURL(image, siteURL string) string {
	image = strings.TrimSpace(image)
	if !ownimage.Is(image) || strings.Contains(image, "/images/default-article") {
		return ""
	}
	if strings.HasPrefix(image, "/") {
		return strings.TrimRight(siteURL, "/") + image
	}
	return image
}
