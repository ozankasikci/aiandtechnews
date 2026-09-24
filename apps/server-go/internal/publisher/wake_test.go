package publisher_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

// claimCountingStore claims nothing and reports each ClaimDue call.
type claimCountingStore struct {
	publisher.Store
	claims chan struct{}
}

func (s *claimCountingStore) PublishDelay(context.Context) (newsroom.PublishDelay, error) {
	return newsroom.PublishDelay{MinMinutes: 30, MaxMinutes: 40}, nil
}

func (s *claimCountingStore) ResetProcessing(context.Context, time.Time, int) (int64, int64, error) {
	return 0, 0, nil
}

func (s *claimCountingStore) ClaimDue(context.Context, time.Time, time.Duration) (newsroom.Candidate, bool, error) {
	s.claims <- struct{}{}
	return newsroom.Candidate{}, false, nil
}

func TestLoopClaimsImmediatelyWhenWoken(t *testing.T) {
	store := &claimCountingStore{claims: make(chan struct{}, 4)}
	wake := make(chan struct{}, 1)
	pub := publisher.New(publisher.Deps{
		Store:  store,
		Now:    time.Now,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Wake:   wake,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		pub.Loop(ctx, time.Hour)
		close(done)
	}()
	defer func() {
		cancel()
		<-done
	}()

	waitClaim := func(label string) {
		t.Helper()
		select {
		case <-store.claims:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: no claim attempt", label)
		}
	}
	waitClaim("first tick")
	wake <- struct{}{}
	waitClaim("after wake")
}
