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

func TestIndexNowQueueNeverBlocksTheCallerAndDrainsOnClose(t *testing.T) {
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
		queue.Close()
		close(drained)
	}()
	select {
	case <-drained:
		t.Fatal("Close returned before the submission finished")
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
	queue.Close()
	if !strings.Contains(logs.String(), "IndexNow notification failed") || !strings.Contains(logs.String(), "403") {
		t.Fatalf("logs = %q", logs.String())
	}
}

func TestIndexNowQueueDropsNotifyAfterClose(t *testing.T) {
	var logs bytes.Buffer
	submitter := &blockingSubmitter{release: make(chan struct{})}
	close(submitter.release)
	queue := newIndexNowQueue(submitter, slog.New(slog.NewTextHandler(&logs, nil)))
	queue.Close()

	queue.Notify([]string{"late"})

	if !strings.Contains(logs.String(), "IndexNow notification dropped after shutdown") {
		t.Fatalf("logs = %q", logs.String())
	}
	submitter.mu.Lock()
	defer submitter.mu.Unlock()
	if len(submitter.calls) != 0 {
		t.Fatalf("submitter.calls = %v, want none for a post-close Notify", submitter.calls)
	}
}

func TestIndexNowQueueCloseAbandonsAfterDeadline(t *testing.T) {
	var logs bytes.Buffer
	submitter := &blockingSubmitter{release: make(chan struct{})}
	queue := newIndexNowQueue(submitter, slog.New(slog.NewTextHandler(&logs, nil)))
	queue.drainDeadline = 20 * time.Millisecond
	queue.Notify([]string{"a"})

	closed := make(chan struct{})
	go func() {
		queue.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not return after its drain deadline expired")
	}
	if !strings.Contains(logs.String(), "IndexNow drain deadline exceeded") || !strings.Contains(logs.String(), "abandoned") {
		t.Fatalf("logs = %q", logs.String())
	}
	close(submitter.release) // let the leaked goroutine finish so it doesn't outlive the test
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
