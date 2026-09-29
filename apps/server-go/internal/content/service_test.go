package content

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubStore struct {
	query ListQuery
	limit int
	since time.Time
	err   error
}

func (s *stubStore) List(_ context.Context, query ListQuery) (ListResult, error) {
	s.query = query
	return ListResult{Articles: []Article{}, Total: 51}, s.err
}
func (s *stubStore) Trending(_ context.Context, limit int, since time.Time) ([]Article, error) {
	s.limit, s.since = limit, since
	return []Article{}, s.err
}
func (s *stubStore) PublishedBySlug(context.Context, string) (Article, error) {
	return Article{}, s.err
}
func (s *stubStore) RecordView(context.Context, string) error      { return s.err }
func (s *stubStore) ByID(context.Context, string) (Article, error) { return Article{}, s.err }

func TestServiceClampsQueriesAndCalculatesPages(t *testing.T) {
	store := &stubStore{}
	service := NewService(store)
	page, err := service.List(context.Background(), ListQuery{Page: -1, Limit: 999})
	if err != nil {
		t.Fatal(err)
	}
	if store.query.Page != 1 || store.query.Limit != 50 || page.TotalPages != 2 || page.Articles == nil {
		t.Fatalf("query/page = %#v / %#v", store.query, page)
	}
	if _, err := service.Trending(context.Background(), 999, ""); err != nil || store.limit != 20 {
		t.Fatalf("trending = %d, %v", store.limit, err)
	}
}

func TestServicePreservesNotFoundAndInternalCauses(t *testing.T) {
	for _, cause := range []error{ErrNotFound, context.Canceled, errors.New("query failed")} {
		service := NewService(&stubStore{err: cause})
		_, err := service.BySlug(context.Background(), "slug")
		if !errors.Is(err, cause) {
			t.Errorf("BySlug error = %v, want cause %v", err, cause)
		}
		if err := service.RecordView(context.Background(), "slug"); !errors.Is(err, cause) {
			t.Errorf("RecordView error = %v, want cause %v", err, cause)
		}
	}
}

func TestServiceTrendingWindows(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	store := &stubStore{}
	service := NewService(store)
	service.now = func() time.Time { return now }
	for window, want := range map[string]time.Time{
		"":      {},
		"all":   {},
		"bogus": {},
		"24h":   now.Add(-24 * time.Hour),
		"7d":    now.Add(-7 * 24 * time.Hour),
	} {
		if _, err := service.Trending(context.Background(), 5, window); err != nil {
			t.Fatal(err)
		}
		if !store.since.Equal(want) {
			t.Errorf("window %q since = %v, want %v", window, store.since, want)
		}
	}
}
