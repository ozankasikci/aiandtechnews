package content

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"
)

type adminStore interface {
	DashboardList(context.Context, DashboardQuery) (ListResult, error)
	ByID(context.Context, string) (Article, error)
	DashboardCategories(context.Context) ([]CategoryWithCount, error)
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
