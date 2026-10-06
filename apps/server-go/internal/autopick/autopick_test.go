package autopick

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
)

type fakeStore struct {
	pending []Candidate
	next    *time.Time
	saved   []Run
	nextAt  time.Time
}

func (s *fakeStore) Pending(context.Context, int) ([]Candidate, error) { return s.pending, nil }
func (s *fakeStore) History(context.Context, time.Time, int) (History, error) {
	return History{}, nil
}
func (s *fakeStore) NextRun(context.Context) (*time.Time, error) { return s.next, nil }
func (s *fakeStore) SaveRun(_ context.Context, run Run, next time.Time) error {
	s.saved = append(s.saved, run)
	s.nextAt = next
	return nil
}

type fakeQueue struct{ published, rejected []int64 }

func (q *fakeQueue) Publish(_ context.Context, ids []int64) ([]newsroom.Candidate, []newsroom.Skipped, error) {
	q.published = append(q.published, ids...)
	out := make([]newsroom.Candidate, len(ids))
	for i, id := range ids {
		out[i] = newsroom.Candidate{ID: id}
	}
	return out, nil, nil
}

func (q *fakeQueue) Reject(_ context.Context, ids []int64) ([]int64, []newsroom.Skipped, error) {
	q.rejected = append(q.rejected, ids...)
	return ids, nil, nil
}

type fakeEditor struct {
	decisions []Decision
	err       error
}

func (e fakeEditor) Decide(context.Context, []Candidate, History) ([]Decision, error) {
	return e.decisions, e.err
}

type quietLogger struct{}

func (quietLogger) InfoContext(context.Context, string, ...any)  {}
func (quietLogger) ErrorContext(context.Context, string, ...any) {}

var now = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

func picker(store *fakeStore, queue *fakeQueue, editor Editor) *Picker {
	return &Picker{Store: store, Queue: queue, Editor: editor, Interval: 6 * time.Hour, Now: func() time.Time { return now }, Logger: quietLogger{}}
}

func TestRunQueuesPicksInPriorityOrderAndRejectsTheRest(t *testing.T) {
	store := &fakeStore{pending: []Candidate{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}, {ID: 5}}}
	queue := &fakeQueue{}
	editor := fakeEditor{decisions: []Decision{
		{ID: 1, Publish: false},
		{ID: 2, Publish: true, Priority: 2},
		{ID: 3, Publish: true, Priority: 1},
		{ID: 4, Publish: false},
		{ID: 99, Publish: true, Priority: 1}, // not a candidate
		// 5 has no answer and stays pending
	}}
	run, err := picker(store, queue, editor).RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(queue.published, []int64{3, 2}) {
		t.Errorf("published %v, want [3 2]", queue.published)
	}
	if !slices.Equal(queue.rejected, []int64{1, 4}) {
		t.Errorf("rejected %v, want [1 4]", queue.rejected)
	}
	if run.Undecided != 1 || run.Rejected != 2 || len(run.Picked) != 2 {
		t.Errorf("run %+v", run)
	}
	if !store.nextAt.Equal(now.Add(6 * time.Hour)) {
		t.Errorf("next run %s", store.nextAt)
	}
}

func TestRunMayPickNothing(t *testing.T) {
	store := &fakeStore{pending: []Candidate{{ID: 1}, {ID: 2}}}
	queue := &fakeQueue{}
	editor := fakeEditor{decisions: []Decision{{ID: 1}, {ID: 2}}}
	if _, err := picker(store, queue, editor).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(queue.published) != 0 || len(queue.rejected) != 2 {
		t.Errorf("published %v rejected %v", queue.published, queue.rejected)
	}
}

func TestFailedRunChangesNothingAndRetriesSooner(t *testing.T) {
	store := &fakeStore{pending: []Candidate{{ID: 1}}}
	queue := &fakeQueue{}
	_, err := picker(store, queue, fakeEditor{err: errors.New("codex down")}).RunOnce(context.Background())
	if err == nil {
		t.Fatal("want error")
	}
	if len(queue.published)+len(queue.rejected) != 0 {
		t.Errorf("queue touched: %+v", queue)
	}
	if !store.nextAt.Equal(now.Add(retryAfter)) || store.saved[0].Error == "" {
		t.Errorf("next %s run %+v", store.nextAt, store.saved)
	}
}

func TestTickWaitsForTheNextRun(t *testing.T) {
	later := now.Add(time.Hour)
	store := &fakeStore{pending: []Candidate{{ID: 1}}, next: &later}
	queue := &fakeQueue{}
	picker(store, queue, fakeEditor{decisions: []Decision{{ID: 1}}}).tick(context.Background())
	if len(store.saved) != 0 || len(queue.rejected) != 0 {
		t.Fatal("ran before it was due")
	}
	store.next = nil
	picker(store, queue, fakeEditor{decisions: []Decision{{ID: 1}}}).tick(context.Background())
	if len(store.saved) != 1 {
		t.Fatal("did not run when due")
	}
}

func TestParseAnswer(t *testing.T) {
	decisions, err := ParseAnswer([]byte("noise {\"decisions\":[{\"id\":7,\"publish\":true,\"priority\":1,\"reason\":\" big \"}]}"))
	if err != nil || len(decisions) != 1 || decisions[0].ID != 7 || decisions[0].Reason != "big" {
		t.Fatalf("%+v %v", decisions, err)
	}
}

func TestPromptCarriesTheOwnersStandingInstructions(t *testing.T) {
	prompt, err := Prompt([]Candidate{{ID: 1, Title: "A story"}}, History{Notes: "- No local records disputes."})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Standing instructions from the site's owner") || !strings.Contains(prompt, "No local records disputes.") {
		t.Fatalf("prompt: %s", prompt)
	}
	plain, _ := Prompt([]Candidate{{ID: 1}}, History{})
	if strings.Contains(plain, "Standing instructions") {
		t.Fatal("no notes, no instructions section")
	}
}
