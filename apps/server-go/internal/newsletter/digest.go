package newsletter

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// digestWindow is how far back the digest looks: 30 hours.
	digestWindow = 30 * time.Hour
	// digestCandidates and digestSize are Node's LIMIT 20 and slice(0, 5).
	digestCandidates = 20
	digestSize       = 5
	// deliveryWriteTimeout bounds recording a delivery outcome once the
	// send has returned, even when the digest is being cancelled.
	deliveryWriteTimeout = 10 * time.Second
)

// DigestResult is the digest route's counts (Node's DigestResult).
type DigestResult struct {
	Edition  string `json:"edition"`
	Articles int    `json:"articles"`
	Sent     int    `json:"sent"`
	Skipped  int    `json:"skipped"`
	Failed   int    `json:"failed"`
}

type articleRow struct {
	title, slug, excerpt, category string
	content, publishedAt           sql.NullString
}

// RecentPublishedArticles is sendDailyDigest's article query: the twenty
// published articles with the greatest published_at text.
func (s *SQLiteStore) RecentPublishedArticles(ctx context.Context) ([]articleRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.title, a.slug, a.excerpt, a.content, a.published_at, c.name AS category_name
         FROM articles a
         JOIN categories c ON c.id = a.category_id
         WHERE a.status = 'published'
         ORDER BY a.published_at DESC
         LIMIT `+strconv.Itoa(digestCandidates))
	if err != nil {
		return nil, fmt.Errorf("select digest articles: %w", err)
	}
	defer rows.Close()
	var articles []articleRow
	for rows.Next() {
		var row articleRow
		if err := rows.Scan(&row.title, &row.slug, &row.excerpt, &row.content, &row.publishedAt, &row.category); err != nil {
			return nil, fmt.Errorf("scan digest article: %w", err)
		}
		articles = append(articles, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("select digest articles: %w", err)
	}
	return articles, nil
}

// SaveEdition records the edition before any delivery, so the archive
// shows what it contained even if sends fail; a rerun replaces the subject
// and articles but keeps the first created_at.
func (s *SQLiteStore) SaveEdition(ctx context.Context, key, subject, articles, createdAt string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO newsletter_editions (edition_key, subject, articles, created_at)
         VALUES (?, ?, ?, ?)
         ON CONFLICT(edition_key) DO UPDATE SET subject = excluded.subject, articles = excluded.articles`,
		key, subject, articles, createdAt)
	if err != nil {
		return fmt.Errorf("save edition: %w", err)
	}
	return nil
}

// ActiveSubscribers returns the digest's recipients in id order.
func (s *SQLiteStore) ActiveSubscribers(ctx context.Context) ([]subscriber, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, email, status FROM subscribers WHERE status = 'active' ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("select subscribers: %w", err)
	}
	defer rows.Close()
	var subscribers []subscriber
	for rows.Next() {
		var row subscriber
		if err := rows.Scan(&row.id, &row.email, &row.status); err != nil {
			return nil, fmt.Errorf("scan subscriber: %w", err)
		}
		subscribers = append(subscribers, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("select subscribers: %w", err)
	}
	return subscribers, nil
}

// ClaimDelivery is Node's check-then-upsert as one statement: a delivery
// already 'sent' for this edition is left alone (claimed is false, the
// subscriber is skipped); otherwise the row is inserted, or reset to
// 'sending' with its error cleared, exactly as Node's upsert does.
func (s *SQLiteStore) ClaimDelivery(ctx context.Context, subscriberID int64, edition, createdAt string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO newsletter_deliveries (subscriber_id, edition_key, status, created_at)
           VALUES (?, ?, 'sending', ?)
           ON CONFLICT(subscriber_id, edition_key)
           DO UPDATE SET status = 'sending', error = NULL
           WHERE newsletter_deliveries.status <> 'sent'`, subscriberID, edition, createdAt)
	if err != nil {
		return false, fmt.Errorf("claim delivery: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("claim delivery: %w", err)
	}
	return changed > 0, nil
}

func (s *SQLiteStore) MarkSent(ctx context.Context, subscriberID int64, edition, providerMessageID, sentAt string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE newsletter_deliveries
             SET status = 'sent', provider_message_id = ?, sent_at = ?, error = NULL
             WHERE subscriber_id = ? AND edition_key = ?`, providerMessageID, sentAt, subscriberID, edition)
	if err != nil {
		return fmt.Errorf("mark delivery sent: %w", err)
	}
	return nil
}

func (s *SQLiteStore) MarkFailed(ctx context.Context, subscriberID int64, edition, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE newsletter_deliveries
             SET status = 'failed', error = ?
             WHERE subscriber_id = ? AND edition_key = ?`, message, subscriberID, edition)
	if err != nil {
		return fmt.Errorf("mark delivery failed: %w", err)
	}
	return nil
}

// articlesJSON is JSON.stringify(articles) for the stored edition.
func articlesJSON(articles []DigestArticle) string {
	var builder strings.Builder
	builder.WriteByte('[')
	for i, article := range articles {
		if i > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(`{"title":`)
		writeJSONString(&builder, article.Title)
		builder.WriteString(`,"slug":`)
		writeJSONString(&builder, article.Slug)
		builder.WriteString(`,"excerpt":`)
		writeJSONString(&builder, article.Excerpt)
		builder.WriteString(`,"category":`)
		writeJSONString(&builder, article.Category)
		builder.WriteString(`,"readingMinutes":`)
		builder.WriteString(strconv.Itoa(article.ReadingMinutes))
		builder.WriteByte('}')
	}
	builder.WriteByte(']')
	return builder.String()
}

// Bind sets the lifecycle context that bounds digest runs. App.Run binds it
// to the process's shutdown context.
func (s *Service) Bind(ctx context.Context) {
	s.lifecycle.Lock()
	s.lifecycle.ctx = ctx
	s.lifecycle.Unlock()
}

func (s *Service) boundLifecycle() context.Context {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if s.lifecycle.ctx != nil {
		return s.lifecycle.ctx
	}
	return context.Background()
}

// begin starts work that outlives its caller's context (Node never stops
// it when the client goes away) but ends with the bound lifecycle, and
// counts it as in flight until finish is called.
func (s *Service) begin(ctx context.Context) (context.Context, func()) {
	s.inflight.Lock()
	s.inflight.count++
	s.inflight.Unlock()
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stop := context.AfterFunc(s.boundLifecycle(), cancel)
	return runCtx, func() {
		stop()
		cancel()
		s.inflight.Lock()
		s.inflight.count--
		if s.inflight.count == 0 && s.inflight.idle != nil {
			close(s.inflight.idle)
			s.inflight.idle = nil
		}
		s.inflight.Unlock()
	}
}

// Wait blocks until no digest run or welcome email is in flight, or until
// ctx ends. App.Run calls it after the HTTP server stops, so the outcome of
// the last delivery is recorded before the process exits.
func (s *Service) Wait(ctx context.Context) error {
	s.inflight.Lock()
	if s.inflight.count == 0 {
		s.inflight.Unlock()
		return nil
	}
	if s.inflight.idle == nil {
		s.inflight.idle = make(chan struct{})
	}
	idle := s.inflight.idle
	s.inflight.Unlock()
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SendDailyDigest is Node's sendDailyDigest. Like Node, a started digest is
// not stopped when the caller (the cron's HTTP request) goes away: it runs
// on a context detached from ctx and bounded only by the bound lifecycle
// (shutdown), which stops it between deliveries. Runs are serialized, so two
// overlapping requests never deliver the same edition twice from this
// process (Node relied on Resend's idempotency key for that); a run waiting
// for its turn gives up at shutdown.
func (s *Service) SendDailyDigest(ctx context.Context, now time.Time) (DigestResult, error) {
	runCtx, finish := s.begin(ctx)
	defer finish()
	select {
	case s.digestSlot <- struct{}{}:
	case <-runCtx.Done():
		return DigestResult{}, runCtx.Err()
	}
	defer func() { <-s.digestSlot }()
	if err := runCtx.Err(); err != nil {
		return DigestResult{}, err
	}
	return s.sendDailyDigest(runCtx, now)
}

func (s *Service) sendDailyDigest(ctx context.Context, now time.Time) (DigestResult, error) {
	secret, err := s.requireTokenSecret()
	if err != nil {
		return DigestResult{}, err
	}
	edition := editionKey(now)
	nowMS := now.UnixMilli()
	cutoff := nowMS - digestWindow.Milliseconds()
	rows, err := s.store.RecentPublishedArticles(ctx)
	if err != nil {
		return DigestResult{}, err
	}
	articles := make([]DigestArticle, 0, digestSize)
	for _, row := range rows {
		publishedAt, ok := parseArticleTimestamp(row.publishedAt.String, s.local)
		if !row.publishedAt.Valid || !ok || publishedAt > nowMS || publishedAt < cutoff {
			continue
		}
		articles = append(articles, DigestArticle{
			Title: row.title, Slug: row.slug, Excerpt: row.excerpt, Category: row.category,
			ReadingMinutes: readingMinutes(row.content.String),
		})
		if len(articles) == digestSize {
			break
		}
	}

	result := DigestResult{Edition: edition, Articles: len(articles)}
	if len(articles) == 0 {
		return result, nil
	}
	timestamp := isoTimestamp(now)
	if err := s.store.SaveEdition(ctx, edition, DigestSubject(articles), articlesJSON(articles), timestamp); err != nil {
		return result, err
	}
	subscribers, err := s.store.ActiveSubscribers(ctx)
	if err != nil {
		return result, err
	}
	for index, recipient := range subscribers {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		claimed, err := s.store.ClaimDelivery(ctx, recipient.id, edition, timestamp)
		if err != nil {
			return result, err
		}
		if !claimed {
			result.Skipped++
			continue
		}
		key := "newsletter-digest-" + edition + "-" + strconv.FormatInt(recipient.id, 10)
		providerID, sendErr := s.sender.Send(ctx, DigestEmail(recipient.email, articles, s.siteURL, s.unsubscribeURL(recipient.id, secret)), key)
		// The outcome is recorded even while shutting down, so a claimed row
		// never stays 'sending' because of cancellation.
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deliveryWriteTimeout)
		if sendErr != nil {
			err = s.store.MarkFailed(writeCtx, recipient.id, edition, sliceUTF16(sendErr.Error(), 500))
			result.Failed++
			s.logger.WarnContext(ctx, "newsletter digest delivery failed", "edition", edition, "subscriber_id", recipient.id, "error", sendErr)
		} else {
			err = s.store.MarkSent(writeCtx, recipient.id, edition, providerID, timestamp)
			result.Sent++
		}
		cancel()
		if err != nil {
			return result, err
		}
		if index < len(subscribers)-1 {
			if err := s.pace(ctx); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}
