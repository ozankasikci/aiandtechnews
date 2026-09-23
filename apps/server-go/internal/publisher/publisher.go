package publisher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/collector"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
)

const (
	// MaxAttempts is how many times a candidate is tried before it is marked failed.
	MaxAttempts = 3
	// RetryDelay is how long a transient failure waits before the next attempt.
	RetryDelay         = 5 * time.Minute
	bookkeepingTimeout = 10 * time.Second
)

type Store interface {
	PublishDelay(ctx context.Context) (newsroom.PublishDelay, error)
	ClaimDue(ctx context.Context, now time.Time, minGap time.Duration) (newsroom.Candidate, bool, error)
	MarkPublished(ctx context.Context, id, articleID int64, now time.Time) error
	MarkFailed(ctx context.Context, id int64, reason string, now time.Time) error
	Requeue(ctx context.Context, id int64, reason string, at, now time.Time) error
	ResetProcessing(ctx context.Context, now time.Time) (int64, error)
}

type SourceFetcher interface {
	FetchText(ctx context.Context, url, expectedSource string) (body, finalURL string, err error)
}

type ArticleRewriter interface {
	Rewrite(ctx context.Context, input RewriteInput) (content.RewrittenArticle, error)
}

type IllustrationRequest struct {
	Slug              string
	Title             string
	Excerpt           string
	Content           string
	ReferenceImageURL string
}

// Illustration is a stored featured image. Discard removes it when the
// article could not be published after the image was uploaded.
type Illustration struct {
	URL     string
	Discard func(context.Context)
}

// Illustrator produces the featured image (phase 4b: Gemini illustration + S3).
type Illustrator interface {
	Illustrate(ctx context.Context, request IllustrationRequest) (Illustration, error)
}

type ArticleStore interface {
	Exists(ctx context.Context, sourceURL, slug string) (bool, error)
	Publish(ctx context.Context, article NewArticle) (int64, error)
}

type Notifier interface {
	SubmitSlugs(ctx context.Context, slugs []string) error
}

type Deps struct {
	Store       Store
	Fetcher     SourceFetcher
	Rewriter    ArticleRewriter
	Illustrator Illustrator
	Articles    ArticleStore
	Notifier    Notifier
	Now         func() time.Time
	Logger      *slog.Logger
}

// Publisher moves due candidates to published articles, one per call.
type Publisher struct{ Deps }

func New(deps Deps) *Publisher { return &Publisher{Deps: deps} }

// Recover requeues candidates left processing by a crash.
func (p *Publisher) Recover(ctx context.Context) error {
	count, err := p.Store.ResetProcessing(ctx, p.Now())
	if err != nil {
		return err
	}
	if count > 0 {
		p.Logger.WarnContext(ctx, "requeued interrupted candidates", "count", count)
	}
	return nil
}

// Loop recovers, then tries one publish per interval until ctx is done.
func (p *Publisher) Loop(ctx context.Context, interval time.Duration) {
	if err := p.Recover(ctx); err != nil {
		p.Logger.ErrorContext(ctx, "publisher recovery failed", "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := p.PublishNext(ctx); err != nil && ctx.Err() == nil {
			p.Logger.ErrorContext(ctx, "publisher tick failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// PublishNext claims the next due candidate (respecting the minimum gap since
// the last publish) and publishes it. It reports whether a candidate was
// claimed; publishing failures are recorded on the candidate, not returned.
func (p *Publisher) PublishNext(ctx context.Context) (bool, error) {
	delay, err := p.Store.PublishDelay(ctx)
	if err != nil {
		return false, err
	}
	candidate, ok, err := p.Store.ClaimDue(ctx, p.Now(), time.Duration(delay.MinMinutes)*time.Minute)
	if err != nil || !ok {
		return false, err
	}
	articleID, slug, err := p.publish(ctx, candidate)
	if err != nil {
		return true, p.recordFailure(ctx, candidate, err)
	}
	bookkeeping, cancel := context.WithTimeout(context.WithoutCancel(ctx), bookkeepingTimeout)
	defer cancel()
	if err := p.Store.MarkPublished(bookkeeping, candidate.ID, articleID, p.Now()); err != nil {
		return true, fmt.Errorf("mark candidate %d published: %w", candidate.ID, err)
	}
	p.Logger.InfoContext(ctx, "published candidate", "candidate", candidate.ID, "article", articleID, "slug", slug)
	if err := p.Notifier.SubmitSlugs(bookkeeping, []string{slug}); err != nil {
		p.Logger.WarnContext(ctx, "IndexNow notification failed", "slug", slug, "error", err)
	}
	return true, nil
}

func (p *Publisher) publish(ctx context.Context, candidate newsroom.Candidate) (int64, string, error) {
	source := candidate.SourceName
	now := p.Now()
	if reason := content.AutomaticItemRejectionReason(candidate.Title, candidate.SourceURL, source, now); reason != "" {
		return 0, "", Permanent(fmt.Errorf("rejected by publishing policy: %s", reason))
	}
	initialSlug := content.Slugify(candidate.Title)
	if initialSlug == "" {
		return 0, "", Permanent(errors.New("headline produces an empty slug"))
	}
	if err := p.rejectDuplicate(ctx, candidate.SourceURL, initialSlug); err != nil {
		return 0, "", err
	}

	body, finalURL, err := p.Fetcher.FetchText(ctx, candidate.SourceURL, source)
	if err != nil {
		if errors.Is(err, collector.ErrRedirectOutsideSource) {
			return 0, "", Permanent(err)
		}
		return 0, "", fmt.Errorf("fetch source page: %w", err)
	}
	canonicalURL := ExtractCanonicalURL(body, finalURL)
	if canonicalSource, ok := content.SourceForURL(canonicalURL); !ok || canonicalSource != source {
		return 0, "", Permanent(fmt.Errorf("canonical URL is outside %s: %s", source, canonicalURL))
	}
	if reason := content.AutomaticItemRejectionReason(candidate.Title, canonicalURL, source, now); reason != "" {
		return 0, "", Permanent(fmt.Errorf("canonical URL rejected by publishing policy: %s", reason))
	}
	if err := p.rejectDuplicate(ctx, canonicalURL, initialSlug); err != nil {
		return 0, "", err
	}
	sourceText := ExtractSourceText(body)
	if jsLength(sourceText) < MinSourceTextLength {
		return 0, "", Permanent(errors.New("source text is too short for an accurate rewrite"))
	}

	article, err := p.Rewriter.Rewrite(ctx, RewriteInput{Source: source, Title: candidate.Title, CanonicalURL: canonicalURL, SourceText: sourceText})
	if err != nil {
		return 0, "", err
	}
	finalSlug := content.Slugify(article.Title)
	if finalSlug == "" {
		return 0, "", Permanent(errors.New("rewritten headline produces an empty slug"))
	}
	if err := p.rejectDuplicate(ctx, canonicalURL, finalSlug); err != nil {
		return 0, "", err
	}

	reference := ExtractOGImage(body, canonicalURL)
	if reference == "" && candidate.SourceImageURL != nil {
		reference = *candidate.SourceImageURL
	}
	illustration, err := p.Illustrator.Illustrate(ctx, IllustrationRequest{
		Slug: finalSlug, Title: article.Title, Excerpt: article.Excerpt, Content: article.Content, ReferenceImageURL: reference,
	})
	if err != nil {
		return 0, "", fmt.Errorf("featured image: %w", err)
	}
	articleID, err := p.Articles.Publish(ctx, NewArticle{
		Title: article.Title, Slug: finalSlug, Excerpt: article.Excerpt, Content: article.Content,
		FeaturedImage: illustration.URL, Source: source, SourceURL: canonicalURL,
	})
	if err != nil {
		if illustration.Discard != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), bookkeepingTimeout)
			illustration.Discard(cleanup)
			cancel()
		}
		if errors.Is(err, ErrDuplicateArticle) {
			return 0, "", Permanent(err)
		}
		return 0, "", err
	}
	return articleID, finalSlug, nil
}

func (p *Publisher) rejectDuplicate(ctx context.Context, sourceURL, slug string) error {
	duplicate, err := p.Articles.Exists(ctx, sourceURL, slug)
	if err != nil {
		return err
	}
	if duplicate {
		return Permanent(ErrDuplicateArticle)
	}
	return nil
}

// recordFailure retries transient failures (up to MaxAttempts) and marks the
// rest failed with a readable reason for the app.
func (p *Publisher) recordFailure(ctx context.Context, candidate newsroom.Candidate, cause error) error {
	bookkeeping, cancel := context.WithTimeout(context.WithoutCancel(ctx), bookkeepingTimeout)
	defer cancel()
	now := p.Now()
	reason := cause.Error()
	if !IsPermanent(cause) && candidate.Attempts < MaxAttempts {
		retryAt := now.Add(RetryDelay)
		if ctx.Err() != nil {
			retryAt = now // interrupted by shutdown, not by the source
		}
		p.Logger.WarnContext(ctx, "publish attempt failed; retrying", "candidate", candidate.ID, "attempt", candidate.Attempts, "error", cause)
		return p.Store.Requeue(bookkeeping, candidate.ID, reason, retryAt, now)
	}
	p.Logger.ErrorContext(ctx, "publishing failed", "candidate", candidate.ID, "attempts", candidate.Attempts, "error", cause)
	return p.Store.MarkFailed(bookkeeping, candidate.ID, reason, now)
}
