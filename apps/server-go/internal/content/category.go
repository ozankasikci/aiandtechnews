package content

import (
	"context"
	"fmt"
)

type categoryStore interface {
	ListCategories(context.Context) ([]Category, error)
}

// CategoryService provides public category reads independently of article use cases.
type CategoryService struct{ store categoryStore }

func NewCategoryService(store categoryStore) *CategoryService {
	return &CategoryService{store: store}
}

func (s *CategoryService) List(ctx context.Context) ([]Category, error) {
	categories, err := s.store.ListCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	return categories, nil
}
