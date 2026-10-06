// Package autopick is the automatic editor: every few hours it reads all
// pending candidates next to what the site published and turned down lately,
// queues the ones worth publishing (like an editor would by hand) and rejects
// the rest. A run that fails does nothing.
package autopick

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
)

const (
	// DefaultInterval is how long a successful run waits for the next one.
	DefaultInterval = 6 * time.Hour
	// retryAfter is how long a failed run waits.
	retryAfter = 30 * time.Minute
	// checkEvery is how often the loop checks whether a run is due.
	checkEvery = 5 * time.Minute
	// historyWindow is how far back published and rejected stories are shown.
	historyWindow = 3 * 24 * time.Hour
	// historyLimit caps each history list.
	historyLimit = 150
	// maxCandidates caps one run; older pending candidates wait for the next.
	maxCandidates = 200
	// maxPicks caps how many one run may queue.
	maxPicks = 25
)

// Candidate is a pending story the editor decides on.
type Candidate struct {
	ID          int64  `json:"id"`
	Source      string `json:"source"`
	Title       string `json:"title"`
	Summary     string `json:"summary,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
}

// Story is a recently published or rejected title, shown as context.
type Story struct {
	Title string `json:"title"`
	At    string `json:"at"`
}

// History is what the site did lately.
type History struct {
	Published []Story
	Rejected  []Story
	// Notes are the owner's standing instructions (what not to publish, what
	// to favor), one per line; empty when there are none.
	Notes string
}

// Decision is the editor's verdict on one candidate. Priority orders the
// picks (1 first); it is ignored when Publish is false.
type Decision struct {
	ID       int64  `json:"id"`
	Publish  bool   `json:"publish"`
	Priority int    `json:"priority"`
	Reason   string `json:"reason"`
}

// Editor decides on a batch of candidates.
type Editor interface {
	Decide(ctx context.Context, candidates []Candidate, history History) ([]Decision, error)
}

// Store reads candidates and history and keeps the run schedule.
type Store interface {
	Pending(ctx context.Context, limit int) ([]Candidate, error)
	History(ctx context.Context, since time.Time, limit int) (History, error)
	NextRun(ctx context.Context) (*time.Time, error)
	SaveRun(ctx context.Context, run Run, next time.Time) error
}

// Queue is the newsroom service: the same publish and reject an editor uses.
type Queue interface {
	Publish(ctx context.Context, ids []int64) ([]newsroom.Candidate, []newsroom.Skipped, error)
	Reject(ctx context.Context, ids []int64) ([]int64, []newsroom.Skipped, error)
}

// Pick is one queued candidate with the editor's reason.
type Pick struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

// Run is the outcome of one run, kept for the dashboard.
type Run struct {
	At         string     `json:"at"`
	Considered int        `json:"considered"`
	Picked     []Pick     `json:"picked"`
	Rejected   int        `json:"rejected"`
	Undecided  int        `json:"undecided"`
	Error      string     `json:"error,omitempty"`
	Decisions  []Decision `json:"-"`
}

// Picker runs the automatic editor.
type Picker struct {
	Store    Store
	Queue    Queue
	Editor   Editor
	Interval time.Duration
	Now      func() time.Time
	Logger   Logger
}

// Logger is the part of slog.Logger the picker uses.
type Logger interface {
	InfoContext(ctx context.Context, msg string, args ...any)
	ErrorContext(ctx context.Context, msg string, args ...any)
}

// Plan asks the editor and returns the run without queueing or rejecting
// anything: Picked in queue order, Rejected the ids to reject.
func (p *Picker) Plan(ctx context.Context) (Run, []int64, error) {
	now := p.Now()
	run := Run{At: now.UTC().Format(time.RFC3339), Picked: []Pick{}}
	candidates, err := p.Store.Pending(ctx, maxCandidates)
	if err != nil || len(candidates) == 0 {
		return run, nil, err
	}
	run.Considered = len(candidates)
	history, err := p.Store.History(ctx, now.Add(-historyWindow), historyLimit)
	if err != nil {
		return run, nil, err
	}
	decisions, err := p.Editor.Decide(ctx, candidates, history)
	if err != nil {
		return run, nil, err
	}
	run.Decisions = decisions
	byID := make(map[int64]Candidate, len(candidates))
	for _, candidate := range candidates {
		byID[candidate.ID] = candidate
	}
	decided := map[int64]bool{}
	var picks []Decision
	var rejected []int64
	for _, decision := range decisions {
		if _, ok := byID[decision.ID]; !ok || decided[decision.ID] {
			continue
		}
		decided[decision.ID] = true
		if decision.Publish {
			picks = append(picks, decision)
		} else {
			rejected = append(rejected, decision.ID)
		}
	}
	slices.SortStableFunc(picks, func(a, b Decision) int { return priority(a) - priority(b) })
	if len(picks) > maxPicks {
		// Past the cap stays pending for the next run.
		for _, extra := range picks[maxPicks:] {
			decided[extra.ID] = false
		}
		picks = picks[:maxPicks]
	}
	for _, pick := range picks {
		run.Picked = append(run.Picked, Pick{ID: pick.ID, Title: byID[pick.ID].Title, Reason: pick.Reason})
	}
	run.Rejected = len(rejected)
	for _, candidate := range candidates {
		if !decided[candidate.ID] {
			run.Undecided++
		}
	}
	return run, rejected, nil
}

func priority(d Decision) int {
	if d.Priority < 1 {
		return 1 << 30
	}
	return d.Priority
}

// RunOnce plans, queues the picks, rejects the rest and records the run.
// Candidates the editor did not answer for stay pending.
func (p *Picker) RunOnce(ctx context.Context) (Run, error) {
	run, rejected, err := p.Plan(ctx)
	if err == nil {
		err = p.apply(ctx, &run, rejected)
	}
	next := p.Now().Add(p.interval())
	if err != nil {
		run.Error = err.Error()
		next = p.Now().Add(retryAfter)
	}
	if saveErr := p.Store.SaveRun(ctx, run, next); saveErr != nil && err == nil {
		err = saveErr
	}
	return run, err
}

func (p *Picker) apply(ctx context.Context, run *Run, rejected []int64) error {
	if len(run.Picked) > 0 {
		ids := make([]int64, len(run.Picked))
		for i, pick := range run.Picked {
			ids[i] = pick.ID
		}
		queued, _, err := p.Queue.Publish(ctx, ids)
		if err != nil {
			return err
		}
		ok := map[int64]bool{}
		for _, candidate := range queued {
			ok[candidate.ID] = true
		}
		// Keep only what was really queued (a candidate may have changed status).
		run.Picked = slices.DeleteFunc(run.Picked, func(pick Pick) bool { return !ok[pick.ID] })
	}
	if len(rejected) > 0 {
		done, _, err := p.Queue.Reject(ctx, rejected)
		run.Rejected = len(done)
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *Picker) interval() time.Duration {
	if p.Interval <= 0 {
		return DefaultInterval
	}
	return p.Interval
}

// Loop runs whenever the saved next-run time has passed (at once when none
// was saved) until ctx is done.
func (p *Picker) Loop(ctx context.Context) {
	ticker := time.NewTicker(checkEvery)
	defer ticker.Stop()
	for {
		p.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p *Picker) tick(ctx context.Context) {
	next, err := p.Store.NextRun(ctx)
	if err != nil {
		p.Logger.ErrorContext(ctx, "autopick schedule", "error", err)
		return
	}
	if next != nil && p.Now().Before(*next) {
		return
	}
	run, err := p.RunOnce(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		p.Logger.ErrorContext(ctx, "autopick run failed", "error", err)
		return
	}
	p.Logger.InfoContext(ctx, "autopick run", "considered", run.Considered, "picked", len(run.Picked), "rejected", run.Rejected, "undecided", run.Undecided)
}
