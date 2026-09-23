package content

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"
)

type adminTx interface {
	storedArticle(context.Context, string) (storedArticle, error)
	articleSlugStatus(context.Context, string) (string, string, bool, error)
	categorySlug(context.Context, any) (string, bool, error)
	articleConflict(context.Context, *string, any, *string) (bool, error)
	editorialAuthorID(context.Context) (int64, bool, error)
	insertArticle(context.Context, []any) (int64, error)
	updateRow(context.Context, string, string, []assignment) error
	deleteRow(context.Context, string, string) (bool, error)
	article(context.Context, any) (Article, error)
	categoryExists(context.Context, string) (bool, error)
	insertCategory(context.Context, any, any, any, any) (int64, error)
	category(context.Context, any) (Category, error)
	categoryArticleCount(context.Context, string) (int64, error)
}

type adminStore interface {
	DashboardList(context.Context, DashboardQuery) (ListResult, error)
	ByID(context.Context, string) (Article, error)
	DashboardCategories(context.Context) ([]CategoryWithCount, error)
	inAdminTx(context.Context, func(adminTx) error) error
}

// AdminService ports the authenticated article and category handlers of
// apps/server/src/routes/dashboard.ts rule for rule.
type AdminService struct {
	store adminStore
	now   func() time.Time
}

func NewAdminService(store adminStore, now func() time.Time) *AdminService {
	return &AdminService{store: store, now: now}
}

// ListArticles ports dashboard.ts:98-151.
func (s *AdminService) ListArticles(ctx context.Context, query DashboardQuery) (Page, error) {
	query.Page = math.Max(1, query.Page)
	query.Limit = clamp(query.Limit, 1, 50)
	result, err := s.store.DashboardList(ctx, query)
	if err != nil {
		return Page{}, fmt.Errorf("list dashboard articles: %w", err)
	}
	return Page{Articles: result.Articles, Total: result.Total, Page: query.Page, TotalPages: (result.Total + int64(query.Limit) - 1) / int64(query.Limit)}, nil
}

// GetArticle ports dashboard.ts:153-171 (any status).
func (s *AdminService) GetArticle(ctx context.Context, id string) (Article, error) {
	article, err := s.store.ByID(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return Article{}, articleNotFound()
	}
	if err != nil {
		return Article{}, fmt.Errorf("get dashboard article: %w", err)
	}
	return article, nil
}

// ListCategories ports dashboard.ts:483-491.
func (s *AdminService) ListCategories(ctx context.Context) ([]CategoryWithCount, error) {
	categories, err := s.store.DashboardCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("list dashboard categories: %w", err)
	}
	return categories, nil
}
