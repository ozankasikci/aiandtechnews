package content

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// DashboardList mirrors GET /api/dashboard/articles: every status, an optional
// exact status filter, category slug filter and title/excerpt search, ordered
// by updated_at text descending. Like Node, the count query joins only
// categories while the page query also joins authors.
func (s *SQLiteStore) DashboardList(ctx context.Context, query DashboardQuery) (ListResult, error) {
	where, args := dashboardWhere(query)
	var result ListResult
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM articles a JOIN categories c ON a.category_id = c.id `+where, args...).Scan(&result.Total); err != nil {
		return ListResult{}, fmt.Errorf("count dashboard articles: %w", err)
	}
	args = append(args, query.Limit, pageOffset(query.Page, query.Limit))
	rows, err := s.db.QueryContext(ctx, `SELECT `+articleColumns+articleJoins+` `+where+` ORDER BY a.updated_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return ListResult{}, fmt.Errorf("list dashboard articles: %w", err)
	}
	defer rows.Close()
	result.Articles = make([]Article, 0)
	for rows.Next() {
		article, err := scanArticle(rows)
		if err != nil {
			return ListResult{}, fmt.Errorf("scan dashboard article: %w", err)
		}
		result.Articles = append(result.Articles, article)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, fmt.Errorf("iterate dashboard articles: %w", err)
	}
	return result, nil
}

func dashboardWhere(query DashboardQuery) (string, []any) {
	where := `WHERE 1=1`
	args := make([]any, 0, 6)
	switch query.Status {
	case "draft", "published", "scheduled":
		where += ` AND a.status = ?`
		args = append(args, query.Status)
	}
	if query.Category != "" {
		where += ` AND c.slug = ?`
		args = append(args, query.Category)
	}
	if query.Search != "" {
		where += ` AND (a.title LIKE ? OR a.excerpt LIKE ?)`
		term := "%" + query.Search + "%"
		args = append(args, term, term)
	}
	return where, args
}

// DashboardCategories mirrors GET /api/dashboard/categories.
func (s *SQLiteStore) DashboardCategories(ctx context.Context) ([]CategoryWithCount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id, c.name, c.slug, c.description, c.color,
		(SELECT COUNT(*) FROM articles WHERE category_id = c.id) AS article_count
		FROM categories c ORDER BY c.name`)
	if err != nil {
		return nil, fmt.Errorf("list dashboard categories: %w", err)
	}
	defer rows.Close()
	categories := make([]CategoryWithCount, 0)
	for rows.Next() {
		var category CategoryWithCount
		if err := rows.Scan(&category.ID, &category.Name, &category.Slug, &category.Description, &category.Color, &category.ArticleCount); err != nil {
			return nil, fmt.Errorf("scan dashboard category: %w", err)
		}
		categories = append(categories, category)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dashboard categories: %w", err)
	}
	return categories, nil
}

// inAdminTx runs one dashboard mutation in a transaction. The pool has a
// single connection, so the transaction also serializes mutations the way
// better-sqlite3's synchronous handlers did. fn must use only the adminTx it
// is given: touching s.db inside fn would wait forever for the connection.
func (s *SQLiteStore) inAdminTx(ctx context.Context, fn func(adminTx) error) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin dashboard transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = fn(sqliteAdminTx{tx: tx}); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit dashboard transaction: %w", err)
	}
	return nil
}

type sqliteAdminTx struct{ tx *sql.Tx }

func (t sqliteAdminTx) storedArticle(ctx context.Context, id string) (storedArticle, error) {
	var article storedArticle
	var source, sourceURL sql.NullString
	err := t.tx.QueryRowContext(ctx, `SELECT title, slug, excerpt, content, category_id, status, source, source_url FROM articles WHERE id = ?`, id).
		Scan(&article.Title, &article.Slug, &article.Excerpt, &article.Content, &article.CategoryID, &article.Status, &source, &sourceURL)
	if errors.Is(err, sql.ErrNoRows) {
		return storedArticle{}, ErrNotFound
	}
	if err != nil {
		return storedArticle{}, fmt.Errorf("read article %s: %w", id, err)
	}
	article.Source = nullString(source)
	article.SourceURL = nullString(sourceURL)
	return article, nil
}

func (t sqliteAdminTx) articleSlugStatus(ctx context.Context, id string) (slug, status string, found bool, err error) {
	err = t.tx.QueryRowContext(ctx, `SELECT slug, status FROM articles WHERE id = ?`, id).Scan(&slug, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("read article %s: %w", id, err)
	}
	return slug, status, true, nil
}

func (t sqliteAdminTx) categorySlug(ctx context.Context, id any) (string, bool, error) {
	var slug string
	err := t.tx.QueryRowContext(ctx, `SELECT slug FROM categories WHERE id = ?`, id).Scan(&slug)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read category: %w", err)
	}
	return slug, true, nil
}

// articleConflict ports Node's duplicate query. excludeID is nil on create.
func (t sqliteAdminTx) articleConflict(ctx context.Context, excludeID *string, slug any, sourceURL *string) (bool, error) {
	var url any
	if sourceURL != nil {
		url = *sourceURL
	}
	query := `SELECT id FROM articles WHERE slug = ? OR (? IS NOT NULL AND source_url = ?) LIMIT 1`
	args := []any{slug, url, url}
	if excludeID != nil {
		query = `SELECT id FROM articles WHERE id != ? AND (slug = ? OR (? IS NOT NULL AND source_url = ?)) LIMIT 1`
		args = append([]any{*excludeID}, args...)
	}
	var id int64
	err := t.tx.QueryRowContext(ctx, query, args...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check duplicate article: %w", err)
	}
	return true, nil
}

func (t sqliteAdminTx) editorialAuthorID(ctx context.Context) (int64, bool, error) {
	var id int64
	err := t.tx.QueryRowContext(ctx, `SELECT id FROM authors WHERE name = ?`, EditorialAuthor().Name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("find editorial author: %w", err)
	}
	return id, true, nil
}

// insertArticle binds values in Node's exact INSERT column order.
func (t sqliteAdminTx) insertArticle(ctx context.Context, values []any) (int64, error) {
	result, err := t.tx.ExecContext(ctx, `INSERT INTO articles (
		title, slug, excerpt, content, featured_image, category_id, author_id, status,
		published_at, meta_title, meta_description, source, source_url, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, values...)
	if err != nil {
		return 0, fmt.Errorf("insert article: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("inserted article id: %w", err)
	}
	return id, nil
}

func (t sqliteAdminTx) updateRow(ctx context.Context, table, id string, assignments []assignment) error {
	columns := make([]string, len(assignments))
	values := make([]any, 0, len(assignments)+1)
	for i, a := range assignments {
		columns[i] = a.column + " = ?"
		values = append(values, a.value)
	}
	values = append(values, id)
	if _, err := t.tx.ExecContext(ctx, `UPDATE `+table+` SET `+strings.Join(columns, ", ")+` WHERE id = ?`, values...); err != nil {
		return fmt.Errorf("update %s %s: %w", table, id, err)
	}
	return nil
}

func (t sqliteAdminTx) deleteRow(ctx context.Context, table, id string) (bool, error) {
	result, err := t.tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("delete %s %s: %w", table, id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete %s %s: %w", table, id, err)
	}
	return affected > 0, nil
}

func (t sqliteAdminTx) article(ctx context.Context, id any) (Article, error) {
	return queryArticle(ctx, t.tx, ` WHERE a.id = ?`, id)
}

func (t sqliteAdminTx) categoryExists(ctx context.Context, id string) (bool, error) {
	var found int64
	err := t.tx.QueryRowContext(ctx, `SELECT id FROM categories WHERE id = ?`, id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read category %s: %w", id, err)
	}
	return true, nil
}

func (t sqliteAdminTx) insertCategory(ctx context.Context, name, slug, description, color any) (int64, error) {
	result, err := t.tx.ExecContext(ctx, `INSERT INTO categories (name, slug, description, color) VALUES (?, ?, ?, ?)`, name, slug, description, color)
	if err != nil {
		return 0, fmt.Errorf("insert category: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("inserted category id: %w", err)
	}
	return id, nil
}

func (t sqliteAdminTx) category(ctx context.Context, id any) (Category, error) {
	var category Category
	if err := t.tx.QueryRowContext(ctx, `SELECT id, name, slug, description, color FROM categories WHERE id = ?`, id).
		Scan(&category.ID, &category.Name, &category.Slug, &category.Description, &category.Color); err != nil {
		return Category{}, fmt.Errorf("read category: %w", err)
	}
	return category, nil
}

func (t sqliteAdminTx) categoryArticleCount(ctx context.Context, id string) (int64, error) {
	var count int64
	if err := t.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM articles WHERE category_id = ?`, id).Scan(&count); err != nil {
		return 0, fmt.Errorf("count category articles: %w", err)
	}
	return count, nil
}
