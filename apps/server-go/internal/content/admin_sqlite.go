package content

import (
	"context"
	"fmt"
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
