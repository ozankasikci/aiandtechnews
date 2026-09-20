package content

import (
	"context"
	"fmt"
	"math"
)

type articleStore interface {
	List(context.Context, ListQuery) (ListResult, error)
	Trending(context.Context, int) ([]Article, error)
	PublishedBySlugAndIncrement(context.Context, string) (Article, error)
	ByID(context.Context, string) (Article, error)
}

type Service struct{ store articleStore }

func NewService(store articleStore) *Service { return &Service{store: store} }

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

func (s *Service) Trending(ctx context.Context, limit int) ([]Article, error) {
	articles, err := s.store.Trending(ctx, clamp(limit, 1, 20))
	if err != nil {
		return nil, fmt.Errorf("trending articles: %w", err)
	}
	return articles, nil
}

func (s *Service) BySlug(ctx context.Context, slug string) (Article, error) {
	article, err := s.store.PublishedBySlugAndIncrement(ctx, slug)
	if err != nil {
		return Article{}, fmt.Errorf("article by slug: %w", err)
	}
	return article, nil
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
