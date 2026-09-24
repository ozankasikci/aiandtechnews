package newsroom

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"
	_ "time/tzdata" // editorial day boundaries must not depend on the host's zoneinfo
)

const (
	DefaultPageLimit = 20
	MaxPageLimit     = 50
	// EditorialTimeZone defines "today" for published_today, matching the
	// scheduler the Node importer used.
	EditorialTimeZone = "Europe/Istanbul"
)

type candidateStore interface {
	Get(context.Context, int64) (Candidate, error)
	List(context.Context, []Status, int, int) (Page, error)
	MarkQueued(context.Context, int64, Status, time.Time, time.Time) error
	MarkPublishNow(context.Context, int64, Status, time.Time) error
	MarkPending(context.Context, int64, time.Time) error
	MarkRejected(context.Context, int64, time.Time) error
	LatestScheduled(context.Context) (*time.Time, error)
	Overview(context.Context, time.Time) (Overview, error)
	PublishDelay(context.Context) (PublishDelay, error)
	SetPublishDelay(context.Context, PublishDelay) error
}

// RandomMinutes returns a uniform integer in [min, max].
func RandomMinutes(min, max int) int { return min + rand.IntN(max-min+1) }

type Service struct {
	store       candidateStore
	now         func() time.Time
	randMinutes func(min, max int) int
	editorialTZ *time.Location
	// scheduling serializes queue appends so spacing is computed against a
	// stable tail. The API runs as a single process; the store's guarded
	// updates still prevent double transitions if that ever changes.
	// The publisher's ClaimDue also enforces a minimum gap since the last
	// publish, so retries that reschedule outside the queue tail (Requeue,
	// ResetProcessing) cannot publish two items back to back.
	scheduling sync.Mutex
	// wake carries at most one pending "look now" signal to the publisher.
	wake chan struct{}
}

func NewService(store candidateStore, now func() time.Time, randMinutes func(min, max int) int) (*Service, error) {
	if store == nil || now == nil || randMinutes == nil {
		return nil, errors.New("newsroom service requires store, clock, and random source")
	}
	location, err := time.LoadLocation(EditorialTimeZone)
	if err != nil {
		return nil, fmt.Errorf("load editorial time zone: %w", err)
	}
	return &Service{store: store, now: now, randMinutes: randMinutes, editorialTZ: location, wake: make(chan struct{}, 1)}, nil
}

// Wakeups is signalled when a candidate becomes due immediately, so the
// publisher can claim it without waiting for its next tick.
func (s *Service) Wakeups() <-chan struct{} { return s.wake }

func (s *Service) List(ctx context.Context, statuses []Status, page, limit int) (Page, error) {
	return s.store.List(ctx, statuses, page, limit)
}

func (s *Service) Overview(ctx context.Context) (Overview, error) {
	local := s.now().In(s.editorialTZ)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, s.editorialTZ)
	return s.store.Overview(ctx, midnight)
}

// Publish queues pending candidates in request order, each scheduled a random
// delay after the previous queue entry.
//
// A batch is not atomic: on an unexpected error, earlier entries stay queued
// and are returned alongside the error.
func (s *Service) Publish(ctx context.Context, ids []int64) ([]Candidate, []Skipped, error) {
	s.scheduling.Lock()
	defer s.scheduling.Unlock()

	tail, delay, err := s.queueTail(ctx)
	if err != nil {
		return nil, nil, err
	}
	queued := make([]Candidate, 0, len(ids))
	skipped := make([]Skipped, 0)
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			skipped = append(skipped, Skipped{ID: id, Reason: "duplicate id"})
			continue
		}
		seen[id] = true
		candidate, err := s.enqueue(ctx, id, StatusPending, &tail, delay)
		switch {
		case errors.Is(err, ErrNotFound):
			skipped = append(skipped, Skipped{ID: id, Reason: "not found"})
		case errors.Is(err, ErrStaleTransition):
			skipped = append(skipped, Skipped{ID: id, Reason: "not pending"})
		case err != nil:
			return queued, skipped, err
		default:
			queued = append(queued, candidate)
		}
	}
	return queued, skipped, nil
}

// Retry appends a failed candidate to the end of the queue.
func (s *Service) Retry(ctx context.Context, id int64) (Candidate, error) {
	s.scheduling.Lock()
	defer s.scheduling.Unlock()

	tail, delay, err := s.queueTail(ctx)
	if err != nil {
		return Candidate{}, err
	}
	return s.enqueue(ctx, id, StatusFailed, &tail, delay)
}

// PublishNow queues a pending, queued, or failed candidate due immediately
// with priority: the publisher claims it ahead of the other queue entries
// and without the minimum gap since the last publish, though never while
// another candidate is processing. Other queue entries keep their times.
func (s *Service) PublishNow(ctx context.Context, id int64) (Candidate, error) {
	s.scheduling.Lock()
	defer s.scheduling.Unlock()

	candidate, err := s.store.Get(ctx, id)
	if err != nil {
		return Candidate{}, err
	}
	switch candidate.Status {
	case StatusPending, StatusQueued, StatusFailed:
	default:
		return Candidate{}, ErrStaleTransition
	}
	if err := s.store.MarkPublishNow(ctx, id, candidate.Status, s.now()); err != nil {
		return Candidate{}, err
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return s.store.Get(ctx, id)
}

// Unqueue returns a queued candidate to review. Other queue entries keep their times.
func (s *Service) Unqueue(ctx context.Context, id int64) (Candidate, error) {
	if err := s.store.MarkPending(ctx, id, s.now()); err != nil {
		return Candidate{}, s.staleOrMissing(ctx, id, err)
	}
	return s.store.Get(ctx, id)
}

// Reject rejects pending or failed candidates, reporting the rest as skipped.
//
// A batch is not atomic: on an unexpected error, earlier entries stay
// rejected and are returned alongside the error.
func (s *Service) Reject(ctx context.Context, ids []int64) ([]int64, []Skipped, error) {
	rejected := make([]int64, 0, len(ids))
	skipped := make([]Skipped, 0)
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			skipped = append(skipped, Skipped{ID: id, Reason: "duplicate id"})
			continue
		}
		seen[id] = true
		err := s.store.MarkRejected(ctx, id, s.now())
		if err != nil {
			err = s.staleOrMissing(ctx, id, err)
		}
		switch {
		case errors.Is(err, ErrNotFound):
			skipped = append(skipped, Skipped{ID: id, Reason: "not found"})
		case errors.Is(err, ErrStaleTransition):
			skipped = append(skipped, Skipped{ID: id, Reason: "not pending or failed"})
		case err != nil:
			return rejected, skipped, err
		default:
			rejected = append(rejected, id)
		}
	}
	return rejected, skipped, nil
}

func (s *Service) PublishDelay(ctx context.Context) (PublishDelay, error) {
	return s.store.PublishDelay(ctx)
}

func (s *Service) SetPublishDelay(ctx context.Context, delay PublishDelay) (PublishDelay, error) {
	if err := delay.Validate(); err != nil {
		return PublishDelay{}, err
	}
	if err := s.store.SetPublishDelay(ctx, delay); err != nil {
		return PublishDelay{}, err
	}
	return delay, nil
}

// queueTail returns the instant new entries are spaced from (the latest
// scheduled entry, or now) and the configured delay window.
func (s *Service) queueTail(ctx context.Context) (time.Time, PublishDelay, error) {
	delay, err := s.store.PublishDelay(ctx)
	if err != nil {
		return time.Time{}, PublishDelay{}, err
	}
	tail := s.now()
	latest, err := s.store.LatestScheduled(ctx)
	if err != nil {
		return time.Time{}, PublishDelay{}, err
	}
	if latest != nil && latest.After(tail) {
		tail = *latest
	}
	return tail, delay, nil
}

// enqueue schedules one candidate after *tail and advances *tail on success.
func (s *Service) enqueue(ctx context.Context, id int64, from Status, tail *time.Time, delay PublishDelay) (Candidate, error) {
	candidate, err := s.store.Get(ctx, id)
	if err != nil {
		return Candidate{}, err
	}
	if candidate.Status != from {
		return Candidate{}, ErrStaleTransition
	}
	at := tail.Add(time.Duration(s.randMinutes(delay.MinMinutes, delay.MaxMinutes)) * time.Minute)
	if err := s.store.MarkQueued(ctx, id, from, at, s.now()); err != nil {
		return Candidate{}, err
	}
	scheduled := formatTime(at)
	candidate.Status = StatusQueued
	candidate.ScheduledFor = &scheduled
	candidate.Attempts = 0
	candidate.LastError = nil
	*tail = at
	return candidate, nil
}

// staleOrMissing distinguishes a missing candidate from a status mismatch
// after a guarded update affected no rows.
func (s *Service) staleOrMissing(ctx context.Context, id int64, err error) error {
	if !errors.Is(err, ErrStaleTransition) {
		return err
	}
	if _, getErr := s.store.Get(ctx, id); getErr != nil {
		if errors.Is(getErr, ErrNotFound) {
			return ErrNotFound
		}
		return getErr
	}
	return err
}
