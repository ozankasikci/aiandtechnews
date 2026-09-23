package app

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/indexnow"
)

const (
	indexNowSubmitTimeout = 30 * time.Second
	// indexNowDrainDeadline bounds how long Close waits for in-flight
	// submissions during shutdown before abandoning them.
	indexNowDrainDeadline = 15 * time.Second
)

type slugSubmitter interface {
	SubmitSlugs(context.Context, []string) error
}

// indexNowQueue ports Node's queueIndexNowNotification for dashboard article
// changes: Notify returns at once, the submission runs in the background with
// a bounded lifetime, and failures are only logged. Close stops accepting new
// submissions and drains in-flight ones during shutdown.
type indexNowQueue struct {
	submitter     slugSubmitter
	logger        *slog.Logger
	timeout       time.Duration
	drainDeadline time.Duration

	mu       sync.Mutex
	closed   bool
	pending  sync.WaitGroup
	inFlight atomic.Int64
}

func newIndexNowQueue(submitter slugSubmitter, logger *slog.Logger) *indexNowQueue {
	return &indexNowQueue{
		submitter:     submitter,
		logger:        logger,
		timeout:       indexNowSubmitTimeout,
		drainDeadline: indexNowDrainDeadline,
	}
}

func (q *indexNowQueue) Notify(slugs []string) {
	if len(slugs) == 0 {
		return
	}
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		q.logger.Warn("IndexNow notification dropped after shutdown", "slugs", slugs)
		return
	}
	q.pending.Add(1)
	q.inFlight.Add(1)
	q.mu.Unlock()

	slugs = append([]string(nil), slugs...)
	go func() {
		defer q.pending.Done()
		defer q.inFlight.Add(-1)
		ctx, cancel := context.WithTimeout(context.Background(), q.timeout)
		defer cancel()
		if err := q.submitter.SubmitSlugs(ctx, slugs); err != nil {
			q.logger.Error("IndexNow notification failed", "slugs", slugs, "error", err)
			return
		}
		q.logger.Info("IndexNow accepted article URLs", "submitted", len(slugs))
	}()
}

// Close stops accepting new submissions (any late Notify is logged and
// dropped) and waits for in-flight ones to finish, up to an overall
// indexNowDrainDeadline. If the deadline expires, the still-running
// submissions are abandoned and their count is logged.
func (q *indexNowQueue) Close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()

	done := make(chan struct{})
	go func() {
		q.pending.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(q.drainDeadline):
		q.logger.Warn("IndexNow drain deadline exceeded; abandoning in-flight submissions",
			"abandoned", q.inFlight.Load())
	}
}

// disabledIndexNow drops dashboard notifications when INDEXNOW_ENABLED is off.
type disabledIndexNow struct{}

func (disabledIndexNow) Notify([]string) {}

// newDashboardIndexNow chooses the dashboard notifier. Node always notified;
// the Go API keeps INDEXNOW_ENABLED as the single switch so a local dashboard
// edit never pings IndexNow for a dev-only article. The returned drain closes
// the queue and waits for in-flight submissions.
func newDashboardIndexNow(cfg config.Config, logger *slog.Logger) (content.IndexNowNotifier, func()) {
	if !cfg.IndexNowEnabled {
		return disabledIndexNow{}, func() {}
	}
	queue := newIndexNowQueue(indexnow.New(), logger)
	return queue, queue.Close
}
