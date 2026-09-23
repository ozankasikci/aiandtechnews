package publisher

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
)

// sqliteDateTime matches SQLite datetime('now'), which Node writes and the website reads.
const sqliteDateTime = "2006-01-02 15:04:05"

var (
	ErrDuplicateArticle = errors.New("article already published for this source URL or slug")
	// ErrCandidateNotProcessing means the candidate left processing (reset,
	// failed or published elsewhere) before its article could be committed.
	ErrCandidateNotProcessing = errors.New("candidate is no longer processing")
)

// NewArticle is an article to insert. When CandidateID is non-zero, the
// candidate is marked published in the same transaction as the insert.
type NewArticle struct {
	CandidateID   int64
	Title         string
	Slug          string
	Excerpt       string
	Content       string
	FeaturedImage string
	Source        string
	SourceURL     string
}

// Categories in Node's CATEGORY_KEYWORDS insertion order (the matching
// order); "tech" is the fallback.
var categoryKeywords = []struct {
	slug     string
	keywords []string
}{
	{"ai", []string{"artificial intelligence", "machine learning", "llm", "openai", "chatgpt", "anthropic", "gemini", "generative ai", "neural network", "ai model"}},
	{"science", []string{"science", "space", "nasa", "physics", "biology", "climate", "quantum", "telescope", "asteroid", "fusion", "genome"}},
	{"entertainment", []string{"game", "gaming", "movie", "film", "streaming", "playstation", "xbox", "nintendo", "netflix", "spotify"}},
	{"reviews", []string{"review", "hands-on", "benchmark", "comparison", "tested", "unboxing"}},
	{"creators", []string{"creator", "youtube", "tiktok", "influencer", "podcast", "twitch", "patreon"}},
	{"tech", nil},
}

// categoryColors is Node's CATEGORY_COLORS in insertion order, which is the
// order ensureCategories inserts them (and so their ids on a fresh database).
var categoryColors = []struct{ slug, color string }{
	{"tech", "#3b82f6"},
	{"reviews", "#f59e0b"},
	{"science", "#10b981"},
	{"entertainment", "#ec4899"},
	{"ai", "#8b5cf6"},
	{"creators", "#f97316"},
}

// Categorize ports categorize: a keyword in the headline wins; otherwise two
// keywords in the first 1000 characters of the body; otherwise "tech".
func Categorize(title, html string) string {
	headline := strings.ToLower(title)
	sample := truncateJS(strings.ToLower(content.StripHTML(html)), 1_000)
	for _, category := range categoryKeywords {
		for _, keyword := range category.keywords {
			if strings.Contains(headline, keyword) {
				return category.slug
			}
		}
	}
	for _, category := range categoryKeywords {
		matches := 0
		for _, keyword := range category.keywords {
			if strings.Contains(sample, keyword) {
				matches++
			}
		}
		if matches >= 2 {
			return category.slug
		}
	}
	return "tech"
}

// SQLiteArticles writes articles exactly as news-importer.ts does.
type SQLiteArticles struct {
	db  *sql.DB
	now func() time.Time
}

func NewSQLiteArticles(db *sql.DB, now func() time.Time) *SQLiteArticles {
	return &SQLiteArticles{db: db, now: now}
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func exists(ctx context.Context, q queryer, sourceURL, slug string) (bool, error) {
	var id int64
	err := q.QueryRowContext(ctx, `SELECT id FROM articles WHERE source_url = ? OR slug = ? LIMIT 1`, sourceURL, slug).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check existing article: %w", err)
	}
	return true, nil
}

// Exists ports isStored.
func (a *SQLiteArticles) Exists(ctx context.Context, sourceURL, slug string) (bool, error) {
	return exists(ctx, a.db, sourceURL, slug)
}

// Publish inserts a published article and verifies it by reading it back, in
// one transaction. It returns ErrDuplicateArticle if the source URL or slug
// already exists.
func (a *SQLiteArticles) Publish(ctx context.Context, article NewArticle) (id int64, err error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin article insert: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	for _, category := range categoryColors {
		name := strings.ToUpper(category.slug[:1]) + category.slug[1:]
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO categories (name, slug, description, color) VALUES (?, ?, ?, ?)`,
			name, category.slug, category.slug+" news", category.color); err != nil {
			return 0, fmt.Errorf("ensure category %s: %w", category.slug, err)
		}
	}
	var categoryID int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM categories WHERE slug = ?`, Categorize(article.Title, article.Content)).Scan(&categoryID); err != nil {
		return 0, fmt.Errorf("find category: %w", err)
	}
	authorID, err := ensureEditorialAuthor(ctx, tx)
	if err != nil {
		return 0, err
	}
	duplicate, err := exists(ctx, tx, article.SourceURL, article.Slug)
	if err != nil {
		return 0, err
	}
	if duplicate {
		return 0, ErrDuplicateArticle
	}

	stamp := a.now().UTC().Format(sqliteDateTime)
	result, err := tx.ExecContext(ctx, `INSERT INTO articles (
		title, slug, excerpt, content, featured_image, category_id, author_id,
		status, published_at, view_count, created_at, updated_at, source, source_url
	) VALUES (?, ?, ?, ?, ?, ?, ?, 'published', ?, 0, ?, ?, ?, ?)`,
		article.Title, article.Slug, article.Excerpt, article.Content, article.FeaturedImage, categoryID, authorID,
		stamp, stamp, stamp, article.Source, article.SourceURL)
	if err != nil {
		return 0, fmt.Errorf("insert article: %w", err)
	}
	if id, err = result.LastInsertId(); err != nil {
		return 0, fmt.Errorf("article id: %w", err)
	}

	var status, author, source string
	if err = tx.QueryRowContext(ctx, `SELECT a.status, au.name, a.source FROM articles a JOIN authors au ON au.id = a.author_id
		WHERE a.source_url = ? AND a.slug = ?`, article.SourceURL, article.Slug).Scan(&status, &author, &source); err != nil {
		return 0, fmt.Errorf("read back article: %w", err)
	}
	if status != "published" || author != content.EditorialAuthor().Name || source != article.Source {
		err = errors.New("post-insert readback did not match the publishing contract")
		return 0, err
	}
	if article.CandidateID != 0 {
		if err = markCandidatePublished(ctx, tx, article.CandidateID, id, a.now()); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit article: %w", err)
	}
	return id, nil
}

// markCandidatePublished links a processing candidate to its new article.
// Timestamps use newsroom's RFC3339 UTC format, not the articles' SQLite format.
func markCandidatePublished(ctx context.Context, tx *sql.Tx, candidateID, articleID int64, now time.Time) error {
	stamp := now.UTC().Format(time.RFC3339)
	result, err := tx.ExecContext(ctx, `UPDATE candidates SET status = 'published', article_id = ?, published_at = ?,
		scheduled_for = NULL, last_error = NULL, updated_at = ? WHERE id = ? AND status = 'processing'`,
		articleID, stamp, stamp, candidateID)
	if err != nil {
		return fmt.Errorf("mark candidate %d published: %w", candidateID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("mark candidate %d published: %w", candidateID, err)
	}
	if affected == 0 {
		return fmt.Errorf("mark candidate %d published: %w", candidateID, ErrCandidateNotProcessing)
	}
	return nil
}

// ensureEditorialAuthor ports ensureEditorialAuthor (by name, then by email, then insert).
func ensureEditorialAuthor(ctx context.Context, tx *sql.Tx) (int64, error) {
	editorial := content.EditorialAuthor()
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM authors WHERE name = ?`, editorial.Name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("find editorial author: %w", err)
	}
	err = tx.QueryRowContext(ctx, `SELECT id FROM authors WHERE email = ?`, editorial.Email).Scan(&id)
	if err == nil {
		if _, err := tx.ExecContext(ctx, `UPDATE authors SET name = ?, bio = ? WHERE id = ?`, editorial.Name, editorial.Bio, id); err != nil {
			return 0, fmt.Errorf("update editorial author: %w", err)
		}
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("find editorial author by email: %w", err)
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO authors (name, email, password_hash, avatar, bio, role)
		VALUES (?, ?, 'not-a-login', NULL, ?, 'editor')`, editorial.Name, editorial.Email, editorial.Bio)
	if err != nil {
		return 0, fmt.Errorf("insert editorial author: %w", err)
	}
	return result.LastInsertId()
}
