package featuredreimage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/ownimage"
)

// Article is a published article whose featured image is not one of ours.
type Article struct {
	ID      int64
	Slug    string
	Title   string
	Excerpt string
	// FeaturedImage is the current value: empty, or a news source's photo
	// (any URL ownimage.Is rejects). It is never fetched.
	FeaturedImage string
	// Attempts are the failed tries so far.
	Attempts int
}

// SQLiteStore reads the articles to replace and updates their featured image.
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

// Pending lists the published articles whose featured image is empty or not
// one of ours (ownimage.Is) and that have failed fewer than maxAttempts
// times, newest first. The "ours" rule lives in Go, shared with the site's
// rule, so the rows are filtered here rather than in SQL.
func (s *SQLiteStore) Pending(ctx context.Context, maxAttempts int) ([]Article, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.id, a.slug, a.title, a.excerpt, COALESCE(a.featured_image, ''), COALESCE(f.attempts, 0)
	FROM articles a
	LEFT JOIN featured_reimage f ON f.article_id = a.id
	WHERE a.status = 'published' AND COALESCE(f.attempts, 0) < ?
	ORDER BY datetime(a.published_at) DESC, a.id DESC`, maxAttempts)
	if err != nil {
		return nil, fmt.Errorf("list articles to re-image: %w", err)
	}
	defer rows.Close()
	var articles []Article
	for rows.Next() {
		var article Article
		if err := rows.Scan(&article.ID, &article.Slug, &article.Title, &article.Excerpt, &article.FeaturedImage, &article.Attempts); err != nil {
			return nil, fmt.Errorf("scan article to re-image: %w", err)
		}
		if !ownimage.Is(article.FeaturedImage) {
			articles = append(articles, article)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate articles to re-image: %w", err)
	}
	return articles, nil
}

// Replace points the article at its new featured image, but only while it
// still has the image Pending saw: a concurrent edit wins. It reports whether
// a row changed. Nothing else about the article changes.
func (s *SQLiteStore) Replace(ctx context.Context, articleID int64, oldURL, newURL string) (bool, error) {
	// An empty old image is stored as NULL or ''.
	result, err := s.db.ExecContext(ctx, `UPDATE articles SET featured_image = ?, updated_at = ?
	WHERE id = ? AND COALESCE(featured_image, '') = ?`, newURL, s.now().UTC().Format(sqliteDateTime), articleID, oldURL)
	if err != nil {
		return false, fmt.Errorf("replace featured image: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("replace featured image: %w", err)
	}
	return changed == 1, nil
}

// MarkFailed records a failed try; attempts is the new total.
func (s *SQLiteStore) MarkFailed(ctx context.Context, articleID int64, message string, attempts int) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO featured_reimage (article_id, attempts, last_error, updated_at)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(article_id) DO UPDATE SET attempts = excluded.attempts, last_error = excluded.last_error, updated_at = excluded.updated_at`,
		articleID, attempts, message, s.now().UTC().Format(sqliteDateTime))
	if err != nil {
		return fmt.Errorf("record featured image failure: %w", err)
	}
	return nil
}
