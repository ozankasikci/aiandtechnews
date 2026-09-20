package editorial

import (
	"context"
	"fmt"
)

type authorStore interface {
	ListAuthors(context.Context) ([]Author, error)
}

type Service struct{ store authorStore }

func NewService(store authorStore) *Service { return &Service{store: store} }

func (s *Service) List(ctx context.Context) ([]Author, error) {
	authors, err := s.store.ListAuthors(ctx)
	if err != nil {
		return nil, fmt.Errorf("list authors: %w", err)
	}
	return authors, nil
}
