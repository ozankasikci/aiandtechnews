package inlineimage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Article is a published article the worker may illustrate.
type Article struct {
	ID            int64
	Slug          string
	Title         string
	Content       string
	FeaturedImage string
	// SourceImage is the news source's image from the article's candidate,
	// when there is one; used only to refuse a copied featured image.
	SourceImage string
	// Attempts are the failed tries so far.
	Attempts int
}

// SQLiteStore reads articles and writes article_images rows.
type SQLiteStore struct {
	db  *sql.DB
	now func() time.Time
}

func NewSQLiteStore(db *sql.DB, now func() time.Time) *SQLiteStore {
	if now == nil {
		now = time.Now
	}
	return &SQLiteStore{db: db, now: now}
}

const sqliteDateTime = "2006-01-02 15:04:05"

// Pending lists articles published since since, newest first, that have a
// featured image, no ready inline image and fewer than maxAttempts failed
// tries. Body length is checked by the caller.
func (s *SQLiteStore) Pending(ctx context.Context, since time.Time, maxAttempts int) ([]Article, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.id, a.slug, a.title, a.content, a.featured_image,
		COALESCE((SELECT c.source_image_url FROM candidates c WHERE c.article_id = a.id ORDER BY c.id LIMIT 1), ''),
		COALESCE(i.attempts, 0)
	FROM articles a
	LEFT JOIN article_images i ON i.article_id = a.id
	WHERE a.status = 'published' AND datetime(a.published_at) >= datetime(?)
		AND a.featured_image IS NOT NULL AND a.featured_image <> ''
		AND (i.id IS NULL OR (i.status <> 'ready' AND i.attempts < ?))
	ORDER BY datetime(a.published_at) DESC, a.id DESC`, since.UTC().Format(sqliteDateTime), maxAttempts)
	if err != nil {
		return nil, fmt.Errorf("list articles for inline images: %w", err)
	}
	defer rows.Close()
	var articles []Article
	for rows.Next() {
		var article Article
		if err := rows.Scan(&article.ID, &article.Slug, &article.Title, &article.Content, &article.FeaturedImage, &article.SourceImage, &article.Attempts); err != nil {
			return nil, fmt.Errorf("scan article for inline image: %w", err)
		}
		articles = append(articles, article)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate articles for inline images: %w", err)
	}
	return articles, nil
}

// MarkReady records the article's inline image.
func (s *SQLiteStore) MarkReady(ctx context.Context, articleID int64, url, alt string, afterParagraph, attempts int) error {
	stamp := s.now().UTC().Format(sqliteDateTime)
	_, err := s.db.ExecContext(ctx, `INSERT INTO article_images (article_id, url, alt, after_paragraph, status, attempts, last_error, created_at, updated_at)
	VALUES (?, ?, ?, ?, 'ready', ?, NULL, ?, ?)
	ON CONFLICT(article_id) DO UPDATE SET url = excluded.url, alt = excluded.alt, after_paragraph = excluded.after_paragraph,
		status = 'ready', attempts = excluded.attempts, last_error = NULL, updated_at = excluded.updated_at`,
		articleID, url, alt, afterParagraph, attempts, stamp, stamp)
	if err != nil {
		return fmt.Errorf("record inline image: %w", err)
	}
	return nil
}

// MarkFailed records a failed try; attempts is the new total.
func (s *SQLiteStore) MarkFailed(ctx context.Context, articleID int64, message string, attempts int) error {
	stamp := s.now().UTC().Format(sqliteDateTime)
	_, err := s.db.ExecContext(ctx, `INSERT INTO article_images (article_id, status, attempts, last_error, created_at, updated_at)
	VALUES (?, 'failed', ?, ?, ?, ?)
	ON CONFLICT(article_id) DO UPDATE SET status = 'failed', attempts = excluded.attempts, last_error = excluded.last_error,
		updated_at = excluded.updated_at`,
		articleID, attempts, message, stamp, stamp)
	if err != nil {
		return fmt.Errorf("record inline image failure: %w", err)
	}
	return nil
}
