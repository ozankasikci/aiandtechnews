package content

import (
	"context"
	"errors"
	"testing"
)

type categoryStoreStub struct {
	categories []Category
	err        error
	ctx        context.Context
}

func (s *categoryStoreStub) ListCategories(ctx context.Context) ([]Category, error) {
	s.ctx = ctx
	return s.categories, s.err
}

func TestCategoryServiceListsCategoriesAndPreservesErrors(t *testing.T) {
	want := []Category{{ID: 1, Name: "AI", Slug: "ai", Description: "Artificial intelligence", Color: "#111111"}}
	store := &categoryStoreStub{categories: want}
	service := NewCategoryService(store)
	ctx := context.WithValue(context.Background(), struct{}{}, "request")
	got, err := service.List(ctx)
	if err != nil || len(got) != 1 || got[0] != want[0] || store.ctx != ctx {
		t.Fatalf("List() = %#v, %v; context forwarded = %t", got, err, store.ctx == ctx)
	}

	cause := errors.New("database failed")
	_, err = NewCategoryService(&categoryStoreStub{err: cause}).List(context.Background())
	if !errors.Is(err, cause) {
		t.Fatalf("List() error = %v, want wrapped cause", err)
	}
}
