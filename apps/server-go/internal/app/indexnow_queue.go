package app

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/indexnow"
)

const indexNowSubmitTimeout = 30 * time.Second

type slugSubmitter interface {
	SubmitSlugs(context.Context, []string) error
}

// indexNowQueue ports Node's queueIndexNowNotification for dashboard article
// changes: Notify returns at once, the submission runs in the background with
// a bounded lifetime, and failures are only logged. Wait drains in-flight
// submissions during shutdown.
type indexNowQueue struct {
	submitter slugSubmitter
	logger    *slog.Logger
	timeout   time.Duration
	pending   sync.WaitGroup
}

func newIndexNowQueue(submitter slugSubmitter, logger *slog.Logger) *indexNowQueue {
	return &indexNowQueue{submitter: submitter, logger: logger, timeout: indexNowSubmitTimeout}
}

func (q *indexNowQueue) Notify(slugs []string) {
	if len(slugs) == 0 {
		return
	}
	slugs = append([]string(nil), slugs...)
	q.pending.Add(1)
	go func() {
		defer q.pending.Done()
		ctx, cancel := context.WithTimeout(context.Background(), q.timeout)
		defer cancel()
		if err := q.submitter.SubmitSlugs(ctx, slugs); err != nil {
			q.logger.Error("IndexNow notification failed", "slugs", slugs, "error", err)
			return
		}
		q.logger.Info("IndexNow accepted article URLs", "submitted", len(slugs))
	}()
}

func (q *indexNowQueue) Wait() { q.pending.Wait() }

// disabledIndexNow drops dashboard notifications when INDEXNOW_ENABLED is off.
type disabledIndexNow struct{}

func (disabledIndexNow) Notify([]string) {}

// newDashboardIndexNow chooses the dashboard notifier. Node always notified;
// the Go API keeps INDEXNOW_ENABLED as the single switch so a local dashboard
// edit never pings IndexNow for a dev-only article. The returned drain waits
// for in-flight submissions.
func newDashboardIndexNow(cfg config.Config, logger *slog.Logger) (content.IndexNowNotifier, func()) {
	if !cfg.IndexNowEnabled {
		return disabledIndexNow{}, func() {}
	}
	queue := newIndexNowQueue(indexnow.New(), logger)
	return queue, queue.Wait
}
