package editorial

import (
	"context"
	"errors"
	"testing"
)

type authorStoreStub struct {
	authors []Author
	err     error
	ctx     context.Context
}

func (s *authorStoreStub) ListAuthors(ctx context.Context) ([]Author, error) {
	s.ctx = ctx
	return s.authors, s.err
}

func TestServiceListsAuthorsAndPreservesErrors(t *testing.T) {
	want := []Author{{ID: 1, Name: "Editor", Email: "editor@example.invalid", Role: "editor"}}
	store := &authorStoreStub{authors: want}
	service := NewService(store)
	ctx := context.WithValue(context.Background(), struct{}{}, "request")
	got, err := service.List(ctx)
	if err != nil || len(got) != 1 || got[0] != want[0] || store.ctx != ctx {
		t.Fatalf("List() = %#v, %v; context forwarded = %t", got, err, store.ctx == ctx)
	}

	cause := errors.New("database failed")
	_, err = NewService(&authorStoreStub{err: cause}).List(context.Background())
	if !errors.Is(err, cause) {
		t.Fatalf("List() error = %v, want wrapped cause", err)
	}
}
