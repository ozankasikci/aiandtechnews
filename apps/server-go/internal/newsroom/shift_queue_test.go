package newsroom_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
)

func scheduleOf(t *testing.T, store *newsroom.SQLiteStore, id int64) string {
	t.Helper()
	return scheduledFor(t, mustGet(t, store, id))
}

func shiftedIDs(result newsroom.QueueShift) []int64 {
	ids := make([]int64, len(result.Candidates))
	for i, candidate := range result.Candidates {
		ids[i] = candidate.ID
	}
	return ids
}

func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestShiftQueuePostponesKeepingSpacingAndSkipsFlaggedAndProcessing(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	a := queueAt(t, store, "https://example.com/a", t0.Add(10*time.Minute))
	b := queueAt(t, store, "https://example.com/b", t0.Add(45*time.Minute))
	flagged := insert(t, store, "https://example.com/flagged", t0)
	if err := store.MarkPublishNow(ctx, flagged, newsroom.StatusPending, t0); err != nil {
		t.Fatal(err)
	}
	busy := queueAt(t, store, "https://example.com/busy", t0.Add(5*time.Minute))
	setStatus(t, db, busy, "processing", t0)
	pending := insert(t, store, "https://example.com/pending", t0)

	result, err := store.ShiftQueue(ctx, 30, t0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Shifted != 2 || result.Minutes != 30 {
		t.Fatalf("result = shifted %d minutes %d", result.Shifted, result.Minutes)
	}
	if got := scheduleOf(t, store, a); got != "2026-09-20T12:40:00Z" {
		t.Fatalf("a = %s", got)
	}
	if got := scheduleOf(t, store, b); got != "2026-09-20T13:15:00Z" {
		t.Fatalf("b = %s", got)
	}
	if got := mustGet(t, store, flagged); !got.PublishNow || scheduledFor(t, got) != "2026-09-20T12:00:00Z" {
		t.Fatalf("flagged candidate moved: %+v", got)
	}
	if got := mustGet(t, store, busy); got.Status != newsroom.StatusProcessing || scheduledFor(t, got) != "2026-09-20T12:05:00Z" {
		t.Fatalf("processing candidate moved: %+v", got)
	}
	if got := mustGet(t, store, pending); got.Status != newsroom.StatusPending || got.ScheduledFor != nil {
		t.Fatalf("pending candidate changed: %+v", got)
	}
	if want := []int64{flagged, a, b}; !equalIDs(shiftedIDs(result), want) {
		t.Fatalf("candidates = %v, want %v (queued only, schedule order)", shiftedIDs(result), want)
	}
	for _, candidate := range result.Candidates {
		if candidate.Status != newsroom.StatusQueued {
			t.Fatalf("non-queued candidate in result: %+v", candidate)
		}
	}
}

func TestShiftQueueBringsForward(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	a := queueAt(t, store, "https://example.com/a", t0.Add(time.Hour))
	b := queueAt(t, store, "https://example.com/b", t0.Add(90*time.Minute))

	result, err := store.ShiftQueue(ctx, -20, t0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Shifted != 2 || result.Minutes != -20 {
		t.Fatalf("result = shifted %d minutes %d", result.Shifted, result.Minutes)
	}
	if got := scheduleOf(t, store, a); got != "2026-09-20T12:40:00Z" {
		t.Fatalf("a = %s", got)
	}
	if got := scheduleOf(t, store, b); got != "2026-09-20T13:10:00Z" {
		t.Fatalf("b = %s", got)
	}
}

func TestShiftQueueClampsBringingForwardAtNow(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	a := queueAt(t, store, "https://example.com/a", t0.Add(25*time.Minute))
	b := queueAt(t, store, "https://example.com/b", t0.Add(55*time.Minute))

	result, err := store.ShiftQueue(ctx, -60, t0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Shifted != 2 || result.Minutes != -25 {
		t.Fatalf("result = shifted %d minutes %d, want 2 and -25", result.Shifted, result.Minutes)
	}
	if got := scheduleOf(t, store, a); got != "2026-09-20T12:00:00Z" {
		t.Fatalf("a = %s", got)
	}
	if got := scheduleOf(t, store, b); got != "2026-09-20T12:30:00Z" {
		t.Fatalf("b = %s", got)
	}
}

func TestShiftQueueClampRoundsPartialMinutesUp(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	a := queueAt(t, store, "https://example.com/a", t0.Add(90*time.Second))

	result, err := store.ShiftQueue(ctx, -10, t0)
	if err != nil {
		t.Fatal(err)
	}
	// ceil(-1.5) = -1: the item lands 30 seconds after now, never before it.
	if result.Shifted != 1 || result.Minutes != -1 || scheduleOf(t, store, a) != "2026-09-20T12:00:30Z" {
		t.Fatalf("result = shifted %d minutes %d, a = %s", result.Shifted, result.Minutes, scheduleOf(t, store, a))
	}
}

func TestShiftQueueLeavesAnAlreadyDueQueueWhereItIs(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	overdue := queueAt(t, store, "https://example.com/overdue", t0.Add(-10*time.Minute))
	later := queueAt(t, store, "https://example.com/later", t0.Add(20*time.Minute))
	for _, minutes := range []int{-30, -1} {
		result, err := store.ShiftQueue(ctx, minutes, t0)
		if err != nil {
			t.Fatal(err)
		}
		if result.Shifted != 0 || result.Minutes != 0 || len(result.Candidates) != 2 {
			t.Fatalf("%d: result = %+v", minutes, result)
		}
	}
	if scheduleOf(t, store, overdue) != "2026-09-20T11:50:00Z" || scheduleOf(t, store, later) != "2026-09-20T12:20:00Z" {
		t.Fatal("an overdue queue must not move")
	}
}

func TestShiftQueueEmptyAndFlaggedOnlyQueues(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()

	empty, err := store.ShiftQueue(ctx, 30, t0)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(empty)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"shifted":0,"minutes":0,"candidates":[]}` {
		t.Fatalf("empty queue = %s", body)
	}

	flagged := insert(t, store, "https://example.com/flagged", t0)
	if err := store.MarkPublishNow(ctx, flagged, newsroom.StatusPending, t0); err != nil {
		t.Fatal(err)
	}
	result, err := store.ShiftQueue(ctx, 30, t0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Shifted != 0 || result.Minutes != 0 || !equalIDs(shiftedIDs(result), []int64{flagged}) {
		t.Fatalf("flagged-only queue = %+v", result)
	}
	if got := scheduleOf(t, store, flagged); got != "2026-09-20T12:00:00Z" {
		t.Fatalf("flagged = %s", got)
	}
}

func TestServiceShiftQueueValidatesAndWakesWhenBringingForward(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	a := queueAt(t, store, "https://example.com/a", t0.Add(time.Hour))
	service := newService(t, store, t0, sequence(30))

	for _, minutes := range []int{0, 1441, -1441} {
		if _, err := service.ShiftQueue(ctx, minutes); !errors.Is(err, newsroom.ErrInvalidShift) {
			t.Fatalf("%d: err = %v, want ErrInvalidShift", minutes, err)
		}
	}
	if got := scheduleOf(t, store, a); got != "2026-09-20T13:00:00Z" {
		t.Fatalf("invalid shifts moved the queue: %s", got)
	}

	postponed, err := service.ShiftQueue(ctx, 1440)
	if err != nil || postponed.Minutes != 1440 || postponed.Shifted != 1 {
		t.Fatalf("postpone = %+v, %v", postponed, err)
	}
	if woken(service) {
		t.Fatal("postponing must not wake the publisher")
	}
	forward, err := service.ShiftQueue(ctx, -1440)
	if err != nil || forward.Minutes != -1440 || forward.Shifted != 1 {
		t.Fatalf("bring forward = %+v, %v", forward, err)
	}
	if !woken(service) {
		t.Fatal("bringing the queue forward must wake the publisher")
	}
	if got := scheduleOf(t, store, a); got != "2026-09-20T13:00:00Z" {
		t.Fatalf("a = %s", got)
	}
}

func serveBody(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func TestShiftQueueHTTP(t *testing.T) {
	store, _ := openStore(t)
	service := newService(t, store, t0, sequence(30))
	router := chi.NewRouter()
	newsroom.NewHandler(service, nil, discardLogger()).Mount(router, passThroughAuth)
	const path = "/newsroom/queue/shift"
	const invalid = `{"error":"minutes must be a non-zero integer between -1440 and 1440"}`

	for body, want := range map[string]string{
		`{"minutes":0}`:               invalid,
		`{"minutes":1441}`:            invalid,
		`{"minutes":-1441}`:           invalid,
		`{}`:                          invalid,
		`{"minutes":null}`:            invalid,
		`{"minutes":1.5}`:             invalid,
		`{"minutes":"30"}`:            invalid,
		``:                            `{"error":"Invalid request body"}`,
		`{"minutes":30,"extra":true}`: `{"error":"Invalid request body"}`,
		`{"minutes":30} {}`:           `{"error":"Invalid request body"}`,
	} {
		response := serveBody(t, router, http.MethodPost, path, body)
		if response.Code != http.StatusBadRequest || response.Body.String() != want {
			t.Errorf("%q = %d %s, want 400 %s", body, response.Code, response.Body.String(), want)
		}
	}

	empty := serveBody(t, router, http.MethodPost, path, `{"minutes":30}`)
	if empty.Code != http.StatusOK || empty.Body.String() != `{"shifted":0,"minutes":0,"candidates":[]}` {
		t.Fatalf("empty queue = %d %s", empty.Code, empty.Body.String())
	}

	id := queueAt(t, store, "https://example.com/a", t0.Add(15*time.Minute))
	response := serveBody(t, router, http.MethodPost, path, `{"minutes":-30}`)
	if response.Code != http.StatusOK {
		t.Fatalf("shift = %d %s", response.Code, response.Body.String())
	}
	var result newsroom.QueueShift
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Shifted != 1 || result.Minutes != -15 || len(result.Candidates) != 1 || result.Candidates[0].ID != id ||
		scheduledFor(t, result.Candidates[0]) != "2026-09-20T12:00:00Z" {
		t.Fatalf("shift result = %+v", result)
	}
}
