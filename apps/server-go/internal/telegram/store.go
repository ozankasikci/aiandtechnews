package telegram

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// StartAfterKey is the settings row holding the moment the channel started:
// only articles published at or after it are ever posted.
const StartAfterKey = "telegram.start_after"

// candidateLimit bounds how many of the newest unposted articles NextDue
// reads; the worker posts far faster than that many can pile up.
const candidateLimit = 200

// Post is a published article as the channel shows it.
type Post struct {
	ID            int64
	Slug          string
	Title         string
	Excerpt       string
	Category      string
	FeaturedImage string
	WhyItMatters  string
	TLDR          []string
	PublishedAt   time.Time
	// Attempts are the failed tries so far.
	Attempts int
}

// SQLiteStore reads the articles to post and records what happened.
type SQLiteStore struct {
	db    *sql.DB
	now   func() time.Time
	local *time.Location
}

// NewSQLiteStore builds a store; local is the zone of published_at values
// without an offset (nil means time.Local, the process zone, TZ).
func NewSQLiteStore(db *sql.DB, now func() time.Time, local *time.Location) *SQLiteStore {
	if now == nil {
		now = time.Now
	}
	if local == nil {
		local = time.Local
	}
	return &SQLiteStore{db: db, now: now, local: local}
}

const sqliteDateTime = "2006-01-02 15:04:05"

// StartAfter returns the channel's start marker, first setting it to now
// when it is missing, so that turning the feature on never posts the
// archive.
func (s *SQLiteStore) StartAfter(ctx context.Context) (time.Time, error) {
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO settings (key, value) VALUES (?, ?)`,
		StartAfterKey, s.now().UTC().Format(time.RFC3339)); err != nil {
		return time.Time{}, fmt.Errorf("set telegram start marker: %w", err)
	}
	var value string
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, StartAfterKey).Scan(&value); err != nil {
		return time.Time{}, fmt.Errorf("read telegram start marker: %w", err)
	}
	start, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("telegram start marker %q: want RFC 3339", value)
	}
	return start, nil
}

// NextDue returns the oldest published article with startAfter <=
// published_at <= now that was not sent and has failed fewer than
// maxAttempts times, or nil. published_at is stored in several formats, so
// the newest candidates are read and compared in Go.
func (s *SQLiteStore) NextDue(ctx context.Context, startAfter time.Time, maxAttempts int) (*Post, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.id, a.slug, a.title, COALESCE(a.excerpt, ''), COALESCE(c.name, ''),
		COALESCE(a.featured_image, ''), COALESCE(sm.why_it_matters, ''), COALESCE(sm.tldr, ''),
		COALESCE(a.published_at, ''), COALESCE(t.attempts, 0)
	FROM articles a
	LEFT JOIN categories c ON c.id = a.category_id
	LEFT JOIN article_summaries sm ON sm.article_id = a.id
	LEFT JOIN telegram_posts t ON t.article_id = a.id
	WHERE a.status = 'published' AND a.published_at IS NOT NULL
	  AND (t.article_id IS NULL OR (t.status = 'failed' AND t.attempts < ?))
	ORDER BY datetime(a.published_at) DESC, a.id DESC
	LIMIT ?`, maxAttempts, candidateLimit)
	if err != nil {
		return nil, fmt.Errorf("list articles to post: %w", err)
	}
	defer rows.Close()
	now := s.now()
	var due *Post
	for rows.Next() {
		var post Post
		var tldr, publishedAt string
		if err := rows.Scan(&post.ID, &post.Slug, &post.Title, &post.Excerpt, &post.Category,
			&post.FeaturedImage, &post.WhyItMatters, &tldr, &publishedAt, &post.Attempts); err != nil {
			return nil, fmt.Errorf("scan article to post: %w", err)
		}
		published, ok := parseTimestamp(publishedAt, s.local)
		if !ok || published.Before(startAfter) || published.After(now) {
			continue
		}
		post.PublishedAt = published.UTC()
		post.TLDR = parseTLDR(tldr)
		if due == nil || published.Before(due.PublishedAt) ||
			(published.Equal(due.PublishedAt) && post.ID < due.ID) {
			candidate := post
			due = &candidate
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate articles to post: %w", err)
	}
	return due, nil
}

// ArticleBySlug reads one published article for a manual post, whatever its
// channel history.
func (s *SQLiteStore) ArticleBySlug(ctx context.Context, slug string) (Post, error) {
	var post Post
	var tldr, publishedAt string
	err := s.db.QueryRowContext(ctx, `SELECT a.id, a.slug, a.title, COALESCE(a.excerpt, ''), COALESCE(c.name, ''),
		COALESCE(a.featured_image, ''), COALESCE(sm.why_it_matters, ''), COALESCE(sm.tldr, ''), COALESCE(a.published_at, '')
	FROM articles a
	LEFT JOIN categories c ON c.id = a.category_id
	LEFT JOIN article_summaries sm ON sm.article_id = a.id
	WHERE a.slug = ? AND a.status = 'published'`, slug).Scan(&post.ID, &post.Slug, &post.Title, &post.Excerpt, &post.Category,
		&post.FeaturedImage, &post.WhyItMatters, &tldr, &publishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Post{}, fmt.Errorf("no published article with slug %q", slug)
	}
	if err != nil {
		return Post{}, fmt.Errorf("read article %q: %w", slug, err)
	}
	post.TLDR = parseTLDR(tldr)
	post.PublishedAt, _ = parseTimestamp(publishedAt, s.local)
	return post, nil
}

// LastPostedAt is when the channel's latest post went out, or the zero time.
func (s *SQLiteStore) LastPostedAt(ctx context.Context) (time.Time, error) {
	var value sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(posted_at) FROM telegram_posts WHERE status = 'sent'`).Scan(&value); err != nil {
		return time.Time{}, fmt.Errorf("read last telegram post: %w", err)
	}
	if !value.Valid {
		return time.Time{}, nil
	}
	posted, err := time.Parse(sqliteDateTime, value.String)
	if err != nil {
		return time.Time{}, fmt.Errorf("last telegram post %q: %w", value.String, err)
	}
	return posted, nil
}

// MarkSent records the channel message for the article.
func (s *SQLiteStore) MarkSent(ctx context.Context, articleID, messageID int64, now time.Time) error {
	stamp := now.UTC().Format(sqliteDateTime)
	_, err := s.db.ExecContext(ctx, `INSERT INTO telegram_posts (article_id, status, attempts, message_id, last_error, posted_at, updated_at)
	VALUES (?, 'sent', 0, ?, NULL, ?, ?)
	ON CONFLICT(article_id) DO UPDATE SET status = 'sent', message_id = excluded.message_id, last_error = NULL,
		posted_at = excluded.posted_at, updated_at = excluded.updated_at`,
		articleID, messageID, stamp, stamp)
	if err != nil {
		return fmt.Errorf("record telegram post: %w", err)
	}
	return nil
}

// MarkFailed records a failed try, adding one to the article's attempts,
// and returns the new total.
func (s *SQLiteStore) MarkFailed(ctx context.Context, articleID int64, message string, now time.Time) (int, error) {
	var attempts int
	err := s.db.QueryRowContext(ctx, `INSERT INTO telegram_posts (article_id, status, attempts, last_error, updated_at)
	VALUES (?, 'failed', 1, ?, ?)
	ON CONFLICT(article_id) DO UPDATE SET status = 'failed', attempts = telegram_posts.attempts + 1,
		last_error = excluded.last_error, updated_at = excluded.updated_at
	RETURNING attempts`, articleID, message, now.UTC().Format(sqliteDateTime)).Scan(&attempts)
	if err != nil {
		return 0, fmt.Errorf("record telegram failure: %w", err)
	}
	return attempts, nil
}

// parseTLDR reads article_summaries.tldr, a JSON array of sentences; a
// missing or malformed value yields no points.
func parseTLDR(value string) []string {
	var points []string
	if json.Unmarshal([]byte(value), &points) != nil {
		return nil
	}
	kept := points[:0]
	for _, point := range points {
		if point = strings.TrimSpace(point); point != "" {
			kept = append(kept, point)
		}
	}
	return kept
}

// parseTimestamp reads published_at like the newsletter does: SQLite's
// "YYYY-MM-DD HH:MM:SS" is UTC, an ISO date-time with an offset or Z is
// exact, and one without is in local (the process zone).
func parseTimestamp(value string, local *time.Location) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if parsed, err := time.Parse(sqliteDateTime, value); err == nil {
		return parsed, true
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, true
	}
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04", "2006-01-02 15:04:05.999999999"} {
		if parsed, err := time.ParseInLocation(layout, value, local); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
