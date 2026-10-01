package content

import (
	"context"
	"fmt"
	"math"
	"time"
)

type articleStore interface {
	List(context.Context, ListQuery) (ListResult, error)
	Trending(context.Context, int, time.Time) ([]Article, error)
	PublishedBySlug(context.Context, string) (Article, error)
	RecordView(context.Context, string) error
	ByID(context.Context, string) (Article, error)
}

type Service struct {
	store articleStore
	now   func() time.Time
}

func NewService(store articleStore) *Service { return &Service{store: store, now: time.Now} }

// trendingWindows are the ?window= values Trending accepts. Anything else
// ranks by all-time views.
var trendingWindows = map[string]time.Duration{"24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour}

type Page struct {
	Articles   []Article `json:"articles"`
	Total      int64     `json:"total"`
	Page       float64   `json:"page"`
	TotalPages int64     `json:"totalPages"`
}

func (s *Service) List(ctx context.Context, query ListQuery) (Page, error) {
	query.Page = math.Max(1, query.Page)
	query.Limit = clamp(query.Limit, 1, 50)
	query.Search = normalizeSearch(query.Search)
	result, err := s.store.List(ctx, query)
	if err != nil {
		return Page{}, fmt.Errorf("list articles: %w", err)
	}
	return Page{Articles: result.Articles, Total: result.Total, Page: query.Page, TotalPages: (result.Total + int64(query.Limit) - 1) / int64(query.Limit)}, nil
}

func (s *Service) Trending(ctx context.Context, limit int, window string) ([]Article, error) {
	var since time.Time
	if span, ok := trendingWindows[window]; ok {
		since = s.now().Add(-span)
	}
	articles, err := s.store.Trending(ctx, clamp(limit, 1, 20), since)
	if err != nil {
		return nil, fmt.Errorf("trending articles: %w", err)
	}
	return articles, nil
}

func (s *Service) BySlug(ctx context.Context, slug string) (Article, error) {
	article, err := s.store.PublishedBySlug(ctx, slug)
	if err != nil {
		return Article{}, fmt.Errorf("article by slug: %w", err)
	}
	return article, nil
}

// RecordView counts one reader view of a published article.
func (s *Service) RecordView(ctx context.Context, slug string) error {
	if err := s.store.RecordView(ctx, slug); err != nil {
		return fmt.Errorf("record article view: %w", err)
	}
	return nil
}

func (s *Service) ByID(ctx context.Context, id string) (Article, error) {
	article, err := s.store.ByID(ctx, id)
	if err != nil {
		return Article{}, fmt.Errorf("article by ID: %w", err)
	}
	return article, nil
}

func clamp(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
