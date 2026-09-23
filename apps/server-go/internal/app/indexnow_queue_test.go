package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
)

type blockingSubmitter struct {
	mu      sync.Mutex
	release chan struct{}
	calls   [][]string
	err     error
}

func (s *blockingSubmitter) SubmitSlugs(ctx context.Context, slugs []string) error {
	<-s.release
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, slugs)
	return s.err
}

func TestIndexNowQueueNeverBlocksTheCallerAndDrainsOnWait(t *testing.T) {
	submitter := &blockingSubmitter{release: make(chan struct{})}
	queue := newIndexNowQueue(submitter, slog.New(slog.NewTextHandler(io.Discard, nil)))
	slugs := []string{"a", "b"}

	returned := make(chan struct{})
	go func() {
		queue.Notify(slugs)
		queue.Notify(nil) // empty lists are never submitted
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("Notify blocked on the submission")
	}
	slugs[0] = "mutated" // the queue must have copied the slice

	drained := make(chan struct{})
	go func() {
		queue.Wait()
		close(drained)
	}()
	select {
	case <-drained:
		t.Fatal("Wait returned before the submission finished")
	case <-time.After(50 * time.Millisecond):
	}
	close(submitter.release)
	<-drained
	if !reflect.DeepEqual(submitter.calls, [][]string{{"a", "b"}}) {
		t.Fatalf("submissions = %v", submitter.calls)
	}
}

func TestIndexNowQueueLogsFailuresWithoutPropagating(t *testing.T) {
	var logs bytes.Buffer
	submitter := &blockingSubmitter{release: make(chan struct{}), err: errors.New("IndexNow rejected 1 URL(s) with 403")}
	close(submitter.release)
	queue := newIndexNowQueue(submitter, slog.New(slog.NewTextHandler(&logs, nil)))
	queue.Notify([]string{"a"})
	queue.Wait()
	if !strings.Contains(logs.String(), "IndexNow notification failed") || !strings.Contains(logs.String(), "403") {
		t.Fatalf("logs = %q", logs.String())
	}
}

func TestNewDashboardIndexNowHonorsIndexNowEnabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	notifier, drain := newDashboardIndexNow(config.Config{}, logger)
	if _, ok := notifier.(disabledIndexNow); !ok {
		t.Fatalf("disabled notifier = %T", notifier)
	}
	notifier.Notify([]string{"a"}) // must be a harmless no-op
	drain()

	notifier, drain = newDashboardIndexNow(config.Config{IndexNowEnabled: true}, logger)
	if _, ok := notifier.(*indexNowQueue); !ok {
		t.Fatalf("enabled notifier = %T", notifier)
	}
	drain()
}
