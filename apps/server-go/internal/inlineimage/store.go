package inlineimage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
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
	// SourceURL is the news source's page. It is read only to find the
	// maker's official site and is never shown or credited.
	SourceURL string
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

// articleColumns are the columns scanArticle reads, for articles a.
const articleColumns = `a.id, a.slug, a.title, a.content, COALESCE(a.featured_image, ''), COALESCE(a.source_url, ''),
	COALESCE((SELECT c.source_image_url FROM candidates c WHERE c.article_id = a.id ORDER BY c.id LIMIT 1), ''),
	COALESCE((SELECT i.attempts FROM article_images i WHERE i.article_id = a.id), 0)`

type scanner interface{ Scan(dest ...any) error }

func scanArticle(row scanner) (Article, error) {
	var article Article
	err := row.Scan(&article.ID, &article.Slug, &article.Title, &article.Content, &article.FeaturedImage, &article.SourceURL, &article.SourceImage, &article.Attempts)
	return article, err
}

// Pending lists articles published since since, newest first, that have a
// featured image, no ready inline image and fewer than maxAttempts failed
// tries. Body length is checked by the caller.
func (s *SQLiteStore) Pending(ctx context.Context, since time.Time, maxAttempts int) ([]Article, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+articleColumns+`
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
		article, err := scanArticle(rows)
		if err != nil {
			return nil, fmt.Errorf("scan article for inline image: %w", err)
		}
		articles = append(articles, article)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate articles for inline images: %w", err)
	}
	return articles, nil
}

// MarkReady records the article's inline image and, for a real photo, its
// credit; a generated illustration clears any earlier credit.
func (s *SQLiteStore) MarkReady(ctx context.Context, articleID int64, url, alt string, afterParagraph, attempts int, credit *realphoto.Credit) error {
	stamp := s.now().UTC().Format(sqliteDateTime)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("record inline image: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var imageID int64
	err = tx.QueryRowContext(ctx, `INSERT INTO article_images (article_id, url, alt, after_paragraph, status, attempts, last_error, created_at, updated_at)
	VALUES (?, ?, ?, ?, 'ready', ?, NULL, ?, ?)
	ON CONFLICT(article_id) DO UPDATE SET url = excluded.url, alt = excluded.alt, after_paragraph = excluded.after_paragraph,
		status = 'ready', attempts = excluded.attempts, last_error = NULL, updated_at = excluded.updated_at
	RETURNING id`,
		articleID, url, alt, afterParagraph, attempts, stamp, stamp).Scan(&imageID)
	if err != nil {
		return fmt.Errorf("record inline image: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM article_image_credits WHERE article_image_id = ?`, imageID); err != nil {
		return fmt.Errorf("clear inline image credit: %w", err)
	}
	if credit != nil {
		if _, err := tx.ExecContext(ctx, `INSERT INTO article_image_credits (article_image_id, kind, credit, credit_url, license, license_url, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, imageID, credit.Kind, credit.Text, credit.URL, credit.License, credit.LicenseURL, stamp); err != nil {
			return fmt.Errorf("record inline image credit: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
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

// ErrNotFound means no published article has the slug.
var ErrNotFound = errors.New("article not found")

// BySlug reads one published article, for trying the pipeline by hand.
func (s *SQLiteStore) BySlug(ctx context.Context, slug string) (Article, error) {
	article, err := scanArticle(s.db.QueryRowContext(ctx, `SELECT `+articleColumns+`
	FROM articles a WHERE a.slug = ? AND a.status = 'published'`, slug))
	if errors.Is(err, sql.ErrNoRows) {
		return Article{}, ErrNotFound
	}
	if err != nil {
		return Article{}, fmt.Errorf("read article %q: %w", slug, err)
	}
	return article, nil
}

// Latest reads the newest limit published articles, for trying the photo
// finder by hand.
func (s *SQLiteStore) Latest(ctx context.Context, limit int) ([]Article, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+articleColumns+`
	FROM articles a WHERE a.status = 'published'
	ORDER BY datetime(a.published_at) DESC, a.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list latest articles: %w", err)
	}
	defer rows.Close()
	var articles []Article
	for rows.Next() {
		article, err := scanArticle(rows)
		if err != nil {
			return nil, fmt.Errorf("scan latest article: %w", err)
		}
		articles = append(articles, article)
	}
	return articles, rows.Err()
}
