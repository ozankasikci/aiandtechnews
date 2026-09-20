package content

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var ErrNotFound = errors.New("article not found")

type SQLiteStore struct{ db *sql.DB }

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{db: db} }

const articleColumns = `a.id, a.title, a.slug, a.excerpt, a.content, a.featured_image,
	a.category_id, a.author_id, a.status, a.published_at, a.meta_title, a.meta_description,
	a.source, a.source_url, a.view_count, a.created_at, a.updated_at,
	c.name, c.slug, c.description, c.color,
	au.name, au.email, au.avatar, au.bio, au.role`

const articleJoins = ` FROM articles a
	JOIN categories c ON a.category_id = c.id
	JOIN authors au ON a.author_id = au.id`

func (s *SQLiteStore) List(ctx context.Context, query ListQuery) (ListResult, error) {
	where, args := listWhere(query)
	var result ListResult
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM articles a JOIN categories c ON a.category_id = c.id `+where, args...).Scan(&result.Total); err != nil {
		return ListResult{}, fmt.Errorf("count published articles: %w", err)
	}
	args = append(args, query.Limit, pageOffset(query.Page, query.Limit))
	rows, err := s.db.QueryContext(ctx, `SELECT `+articleColumns+articleJoins+` `+where+` ORDER BY a.published_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return ListResult{}, fmt.Errorf("list published articles: %w", err)
	}
	defer rows.Close()
	result.Articles = make([]Article, 0)
	for rows.Next() {
		article, err := scanArticle(rows)
		if err != nil {
			return ListResult{}, fmt.Errorf("scan published article: %w", err)
		}
		result.Articles = append(result.Articles, article)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, fmt.Errorf("iterate published articles: %w", err)
	}
	return result, nil
}

func pageOffset(page float64, limit int) float64 {
	return (page - 1) * float64(limit)
}

func listWhere(query ListQuery) (string, []any) {
	where := `WHERE a.status = 'published'`
	args := make([]any, 0, 5)
	if query.Category != "" {
		where += ` AND c.slug = ?`
		args = append(args, query.Category)
	}
	if query.Search != "" {
		where += ` AND (a.title LIKE ? OR a.excerpt LIKE ? OR a.content LIKE ?)`
		term := "%" + query.Search + "%"
		args = append(args, term, term, term)
	}
	return where, args
}

func (s *SQLiteStore) Trending(ctx context.Context, limit int) ([]Article, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+articleColumns+articleJoins+` WHERE a.status = 'published' ORDER BY a.view_count DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list trending articles: %w", err)
	}
	defer rows.Close()
	articles := make([]Article, 0)
	for rows.Next() {
		article, err := scanArticle(rows)
		if err != nil {
			return nil, fmt.Errorf("scan trending article: %w", err)
		}
		articles = append(articles, article)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trending articles: %w", err)
	}
	return articles, nil
}

func (s *SQLiteStore) PublishedBySlugAndIncrement(ctx context.Context, slug string) (article Article, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Article{}, fmt.Errorf("begin article view transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	article, err = queryArticle(ctx, tx, ` WHERE a.slug = ? AND a.status = 'published'`, slug)
	if err != nil {
		return Article{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE articles SET view_count = view_count + 1 WHERE id = ?`, article.ID); err != nil {
		return Article{}, fmt.Errorf("increment article view count: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return Article{}, fmt.Errorf("commit article view transaction: %w", err)
	}
	return article, nil
}

func (s *SQLiteStore) ByID(ctx context.Context, id string) (Article, error) {
	return queryArticle(ctx, s.db, ` WHERE a.id = ?`, id)
}

type rowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type scanner interface{ Scan(...any) error }

func queryArticle(ctx context.Context, queryer rowQueryer, where string, arg any) (Article, error) {
	article, err := scanArticle(queryer.QueryRowContext(ctx, `SELECT `+articleColumns+articleJoins+where, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return Article{}, ErrNotFound
	}
	if err != nil {
		return Article{}, fmt.Errorf("query article: %w", err)
	}
	return article, nil
}

func scanArticle(row scanner) (Article, error) {
	var article Article
	var featuredImage, publishedAt, metaTitle, metaDescription, source, sourceURL sql.NullString
	var authorAvatar, authorBio sql.NullString
	err := row.Scan(
		&article.ID, &article.Title, &article.Slug, &article.Excerpt, &article.Content, &featuredImage,
		&article.CategoryID, &article.AuthorID, &article.Status, &publishedAt, &metaTitle, &metaDescription,
		&source, &sourceURL, &article.ViewCount, &article.CreatedAt, &article.UpdatedAt,
		&article.Category.Name, &article.Category.Slug, &article.Category.Description, &article.Category.Color,
		&article.Author.Name, &article.Author.Email, &authorAvatar, &authorBio, &article.Author.Role,
	)
	if err != nil {
		return Article{}, err
	}
	article.Category.ID = article.CategoryID
	article.Author.ID = article.AuthorID
	article.FeaturedImage = nullString(featuredImage)
	article.PublishedAt = nullString(publishedAt)
	article.MetaTitle = nullString(metaTitle)
	article.MetaDescription = nullString(metaDescription)
	article.Source = nullString(source)
	article.SourceURL = nullString(sourceURL)
	article.Author.Avatar = nullString(authorAvatar)
	article.Author.Bio = nullString(authorBio)
	return article, nil
}

func nullString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

// normalizeSearch preserves whitespace and wildcard characters just like the
// Node endpoint; it exists to make the intentional no-trimming behavior clear.
func normalizeSearch(value string) string { return strings.Clone(value) }
