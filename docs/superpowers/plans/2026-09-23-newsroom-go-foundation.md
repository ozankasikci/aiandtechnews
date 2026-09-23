# Newsroom Go Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the `candidates` data model, publish-queue scheduling, and authenticated `/api/newsroom` endpoints to the Go API so the Omni Control iOS app can list candidates and queue them for publishing.

**Architecture:** A new capability package `internal/newsroom` following the existing pattern (own migration, `SQLiteStore`, service, chi handler). The service owns queue spacing (`scheduled_for = max(now, queue tail) + random(min..max)` minutes) with an injected clock and random source. Routes mount under `/api/newsroom` behind the existing editorial JWT middleware. No collector or publisher yet: `POST /collect` returns 503 until phase 3.

**Tech Stack:** Go 1.25, chi v5, `modernc.org/sqlite`, `gopkg.in/yaml.v3` (tests), standard `testing`.

**Spec:** `docs/superpowers/specs/2026-09-23-ai-tech-news-newsroom-design.md` (phase 1 of 5).

> **Status: executed on branch `newsroom-go-foundation`.** Review follow-ups changed code after these tasks were written (migration adds `published_at`, `ON DELETE SET NULL` + `idx_candidates_article`, queued-schedule CHECK; store/service/handler hardening). The code in the repo is authoritative; do not copy SQL or Go from the task blocks below.

**Working directory for all commands:** `apps/server-go` (run from the repo root: `cd apps/server-go`).

**Commit style for this repo owner:** plain sentence messages, no `feat:`-style prefixes, no co-author trailers.

**Safety:** tests use temporary databases from `internal/testutil`. Never point anything at `apps/server/data/technews.db` or the production database.

---

## File structure

| File | Responsibility |
|---|---|
| Create `internal/newsroom/migrations/003_newsroom.sql` | `settings` (if absent) + default delay rows, `candidates` table, indexes |
| Create `internal/newsroom/migrations.go` | Embeds the SQL as migration descriptor version 3 |
| Create `internal/newsroom/model.go` | `Status`, `Candidate`, `NewCandidate`, `Page`, `Overview`, `PublishDelay`, `Skipped`, errors, time helpers |
| Create `internal/newsroom/store.go` | `SQLiteStore`: insert, get, list, guarded transitions, queue tail, overview counts, delay settings |
| Create `internal/newsroom/service.go` | `Service`: spacing, publish/reject/unqueue/retry, overview day boundary, settings validation |
| Create `internal/newsroom/http.go` | `Handler`: `/newsroom/*` routes, request parsing, error mapping |
| Create `internal/newsroom/helpers_test.go`, `store_test.go`, `service_test.go` | Package tests against temp SQLite |
| Modify `internal/editorial/http_auth.go` | Export `RequireAuth` for other capabilities |
| Modify `internal/app/app.go` | Wire newsroom store/service/handler; append migrations |
| Modify `internal/app/migrations_test.go` | Expect 3 descriptors |
| Create `internal/app/newsroom_http_test.go` | End-to-end HTTP tests through the composed app |
| Create `contracts/newsroom.openapi.yaml` | Newsroom contract (separate from the Node-parity `openapi.yaml`, whose tests require it to match the Node fixture exactly) |
| Modify `README.md` | Document the newsroom slice |

---

### Task 1: Migration and descriptor

**Files:**
- Create: `internal/newsroom/migrations/003_newsroom.sql`
- Create: `internal/newsroom/migrations.go`
- Modify: `internal/app/app.go` (`Migrations()` at the bottom of the file)
- Modify: `internal/app/migrations_test.go:14-18`

- [ ] **Step 1: Update the migration-order test to expect the new descriptor (failing test)**

In `internal/app/migrations_test.go`, replace the first `if` block of `TestApplicationMigrationsHaveStableGlobalOrderAndAreIdempotent`:

```go
	descriptors := app.Migrations()
	if len(descriptors) != 3 ||
		descriptors[0].Version != 1 || descriptors[0].Name != "editorial authors" ||
		descriptors[1].Version != 2 || descriptors[1].Name != "content categories and articles" ||
		descriptors[2].Version != 3 || descriptors[2].Name != "newsroom candidates" {
		t.Fatalf("descriptors = %#v", descriptors)
	}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/app -run TestApplicationMigrationsHaveStableGlobalOrderAndAreIdempotent`
Expected: FAIL with `descriptors = []migrate.Descriptor{...}` (only 2 entries).

- [ ] **Step 3: Write the migration SQL**

Create `internal/newsroom/migrations/003_newsroom.sql`:

```sql
CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT OR IGNORE INTO settings (key, value) VALUES
    ('newsroom.publish_delay_min_minutes', '30'),
    ('newsroom.publish_delay_max_minutes', '40');

CREATE TABLE candidates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_url TEXT NOT NULL UNIQUE,
    source_name TEXT NOT NULL,
    feed_url TEXT NOT NULL,
    title TEXT NOT NULL,
    feed_summary TEXT NOT NULL DEFAULT '',
    source_image_url TEXT,
    feed_published_at TEXT,
    discovered_at TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK(status IN ('pending', 'queued', 'processing', 'published', 'failed', 'rejected')),
    scheduled_for TEXT,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    article_id INTEGER,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (article_id) REFERENCES articles(id)
);

CREATE INDEX idx_candidates_status_scheduled ON candidates(status, scheduled_for);
CREATE INDEX idx_candidates_status_discovered ON candidates(status, discovered_at);
```

`settings` uses `IF NOT EXISTS` and the exact Node schema (`apps/server/src/db.ts`) so a future adoption of the Node database does not conflict. `INSERT OR IGNORE` never overwrites operator-set values.

- [ ] **Step 4: Write the descriptor**

Create `internal/newsroom/migrations.go`:

```go
package newsroom

import (
	_ "embed"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
)

//go:embed migrations/003_newsroom.sql
var newsroomSchema string

// Migrations returns a fresh slice containing newsroom's immutable schema descriptors.
func Migrations() []migrate.Descriptor {
	return []migrate.Descriptor{{Version: 3, Name: "newsroom candidates", SQL: newsroomSchema}}
}
```

- [ ] **Step 5: Append it in the app**

In `internal/app/app.go`, add the import `"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"` and replace `Migrations()`:

```go
// Migrations explicitly collects capability-owned descriptors in global order.
func Migrations() []migrate.Descriptor {
	descriptors := editorial.Migrations()
	descriptors = append(descriptors, content.Migrations()...)
	return append(descriptors, newsroom.Migrations()...)
}
```

- [ ] **Step 6: Run the app tests**

Run: `go test ./internal/app`
Expected: PASS (migration test plus existing contract tests; the new migration applies cleanly on fresh databases).

- [ ] **Step 7: Commit**

```bash
git add internal/newsroom/migrations internal/newsroom/migrations.go internal/app/app.go internal/app/migrations_test.go
git commit -m "Add newsroom candidates migration"
```

---

### Task 2: Domain model

**Files:**
- Create: `internal/newsroom/model.go`

No behavior to test in isolation beyond `Validate` and `ParseStatus`, which are covered by the service and handler tests in later tasks.

- [ ] **Step 1: Write the model**

Create `internal/newsroom/model.go`:

```go
package newsroom

import (
	"errors"
	"time"
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusQueued     Status = "queued"
	StatusProcessing Status = "processing"
	StatusPublished  Status = "published"
	StatusFailed     Status = "failed"
	StatusRejected   Status = "rejected"
)

var allStatuses = []Status{StatusPending, StatusQueued, StatusProcessing, StatusPublished, StatusFailed, StatusRejected}

// ParseStatus returns the Status named by value.
func ParseStatus(value string) (Status, bool) {
	for _, status := range allStatuses {
		if string(status) == value {
			return status, true
		}
	}
	return "", false
}

var (
	ErrNotFound        = errors.New("candidate not found")
	ErrStaleTransition = errors.New("candidate status changed")
	ErrInvalidDelay    = errors.New("publish delay minutes must be between 1 and 1440, with min not above max")
)

// Candidate is the API shape of a potential article.
type Candidate struct {
	ID              int64   `json:"id"`
	Title           string  `json:"title"`
	FeedSummary     string  `json:"feed_summary"`
	SourceName      string  `json:"source_name"`
	SourceURL       string  `json:"source_url"`
	SourceImageURL  *string `json:"source_image_url"`
	FeedPublishedAt *string `json:"feed_published_at"`
	DiscoveredAt    string  `json:"discovered_at"`
	Status          Status  `json:"status"`
	ScheduledFor    *string `json:"scheduled_for"`
	Attempts        int64   `json:"attempts"`
	LastError       *string `json:"last_error"`
	ArticleSlug     *string `json:"article_slug"`
}

// NewCandidate is what the collector stores for a policy-passing feed item.
type NewCandidate struct {
	SourceURL       string
	SourceName      string
	FeedURL         string
	Title           string
	FeedSummary     string
	SourceImageURL  *string
	FeedPublishedAt *time.Time
}

type Page struct {
	Candidates []Candidate `json:"candidates"`
	Total      int64       `json:"total"`
	Page       int         `json:"page"`
	TotalPages int64       `json:"totalPages"`
}

type Overview struct {
	Pending         int64   `json:"pending"`
	Queued          int64   `json:"queued"`
	Processing      int64   `json:"processing"`
	Failed          int64   `json:"failed"`
	PublishedToday  int64   `json:"published_today"`
	NextPublishAt   *string `json:"next_publish_at"`
	LastCollectedAt *string `json:"last_collected_at"`
}

type PublishDelay struct {
	MinMinutes int `json:"publish_delay_min_minutes"`
	MaxMinutes int `json:"publish_delay_max_minutes"`
}

func (d PublishDelay) Validate() error {
	if d.MinMinutes < 1 || d.MinMinutes > d.MaxMinutes || d.MaxMinutes > 1440 {
		return ErrInvalidDelay
	}
	return nil
}

// Skipped reports an id a batch operation did not apply, with a reason for the client.
type Skipped struct {
	ID     int64  `json:"id"`
	Reason string `json:"reason"`
}

// Timestamps are stored and served as RFC 3339 UTC with second precision so
// that SQLite's lexical ordering matches chronological ordering.
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339, value) }
```

- [ ] **Step 2: Verify it compiles**

Run: `go vet ./internal/newsroom`
Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/newsroom/model.go
git commit -m "Add newsroom domain model"
```

---

### Task 3: SQLite store

**Files:**
- Create: `internal/newsroom/helpers_test.go`
- Create: `internal/newsroom/store_test.go`
- Create: `internal/newsroom/store.go`

- [ ] **Step 1: Write the shared test helpers**

Create `internal/newsroom/helpers_test.go`:

```go
package newsroom_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/editorial"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

var t0 = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

func openStore(t *testing.T) (*newsroom.SQLiteStore, *sql.DB) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	descriptors := editorial.Migrations()
	descriptors = append(descriptors, content.Migrations()...)
	descriptors = append(descriptors, newsroom.Migrations()...)
	if err := migrate.Run(context.Background(), db, descriptors); err != nil {
		t.Fatal(err)
	}
	return newsroom.NewSQLiteStore(db), db
}

func insert(t *testing.T, store *newsroom.SQLiteStore, url string, at time.Time) int64 {
	t.Helper()
	id, inserted, err := store.Insert(context.Background(), newsroom.NewCandidate{
		SourceURL: url, SourceName: "The Verge", FeedURL: "https://www.theverge.com/rss/index.xml",
		Title: "Title " + url, FeedSummary: "Summary",
	}, at)
	if err != nil || !inserted {
		t.Fatalf("insert %s: inserted=%v err=%v", url, inserted, err)
	}
	return id
}

// setStatus forces a status the phase-1 API cannot produce (processing, published, failed).
func setStatus(t *testing.T, db *sql.DB, id int64, status string, updatedAt time.Time) {
	t.Helper()
	if _, err := db.Exec(`UPDATE candidates SET status = ?, updated_at = ? WHERE id = ?`,
		status, updatedAt.UTC().Format(time.RFC3339), id); err != nil {
		t.Fatal(err)
	}
}

func mustGet(t *testing.T, store *newsroom.SQLiteStore, id int64) newsroom.Candidate {
	t.Helper()
	candidate, err := store.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return candidate
}
```

- [ ] **Step 2: Write the failing store tests**

Create `internal/newsroom/store_test.go`:

```go
package newsroom_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
)

func TestInsertDedupesBySourceURL(t *testing.T) {
	store, _ := openStore(t)
	insert(t, store, "https://example.com/a", t0)
	_, inserted, err := store.Insert(context.Background(), newsroom.NewCandidate{
		SourceURL: "https://example.com/a", SourceName: "X", FeedURL: "https://x/rss", Title: "Again",
	}, t0)
	if err != nil || inserted {
		t.Fatalf("duplicate insert: inserted=%v err=%v", inserted, err)
	}
}

func TestGetReturnsPendingCandidate(t *testing.T) {
	store, _ := openStore(t)
	id := insert(t, store, "https://example.com/a", t0)
	got := mustGet(t, store, id)
	if got.Status != newsroom.StatusPending || got.DiscoveredAt != "2026-09-20T12:00:00Z" ||
		got.ScheduledFor != nil || got.ArticleSlug != nil || got.Attempts != 0 || got.Title != "Title https://example.com/a" {
		t.Fatalf("candidate = %#v", got)
	}
	if _, err := store.Get(context.Background(), 999); !errors.Is(err, newsroom.ErrNotFound) {
		t.Fatalf("missing err = %v", err)
	}
}

func TestTransitionsAreGuarded(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	id := insert(t, store, "https://example.com/a", t0)
	at := t0.Add(35 * time.Minute)

	if err := store.MarkQueued(ctx, id, newsroom.StatusPending, at, t0); err != nil {
		t.Fatal(err)
	}
	if got := mustGet(t, store, id); got.Status != newsroom.StatusQueued || got.ScheduledFor == nil || *got.ScheduledFor != "2026-09-20T12:35:00Z" {
		t.Fatalf("queued = %#v", got)
	}
	if err := store.MarkQueued(ctx, id, newsroom.StatusPending, at, t0); !errors.Is(err, newsroom.ErrStaleTransition) {
		t.Fatalf("second queue err = %v", err)
	}
	if err := store.MarkPending(ctx, id, t0); err != nil {
		t.Fatal(err)
	}
	if got := mustGet(t, store, id); got.Status != newsroom.StatusPending || got.ScheduledFor != nil {
		t.Fatalf("unqueued = %#v", got)
	}
	if err := store.MarkPending(ctx, id, t0); !errors.Is(err, newsroom.ErrStaleTransition) {
		t.Fatalf("second unqueue err = %v", err)
	}
	if err := store.MarkRejected(ctx, id, t0); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkRejected(ctx, id, t0); !errors.Is(err, newsroom.ErrStaleTransition) {
		t.Fatalf("second reject err = %v", err)
	}
}

func TestListFiltersOrdersAndPaginates(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	oldest := insert(t, store, "https://example.com/1", t0)
	middle := insert(t, store, "https://example.com/2", t0.Add(time.Minute))
	newest := insert(t, store, "https://example.com/3", t0.Add(2*time.Minute))
	later := insert(t, store, "https://example.com/4", t0)
	sooner := insert(t, store, "https://example.com/5", t0)
	if err := store.MarkQueued(ctx, later, newsroom.StatusPending, t0.Add(80*time.Minute), t0); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkQueued(ctx, sooner, newsroom.StatusPending, t0.Add(40*time.Minute), t0); err != nil {
		t.Fatal(err)
	}

	pending, err := store.List(ctx, []newsroom.Status{newsroom.StatusPending}, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, pending.Candidates, newest, middle, oldest)
	if pending.Total != 3 || pending.TotalPages != 1 || pending.Page != 1 {
		t.Fatalf("pending page = %+v", pending)
	}

	mixed, err := store.List(ctx, []newsroom.Status{newsroom.StatusPending, newsroom.StatusQueued}, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, mixed.Candidates, sooner, later, newest, middle, oldest)

	second, err := store.List(ctx, []newsroom.Status{newsroom.StatusPending}, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, second.Candidates, oldest)
	if second.Total != 3 || second.TotalPages != 2 || second.Page != 2 {
		t.Fatalf("second page = %+v", second)
	}
}

func TestOverviewCountsAndDelaySettings(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	insert(t, store, "https://example.com/pending", t0)
	queued := insert(t, store, "https://example.com/queued", t0)
	failed := insert(t, store, "https://example.com/failed", t0)
	today := insert(t, store, "https://example.com/today", t0)
	yesterday := insert(t, store, "https://example.com/yesterday", t0)
	if err := store.MarkQueued(ctx, queued, newsroom.StatusPending, t0.Add(33*time.Minute), t0); err != nil {
		t.Fatal(err)
	}
	setStatus(t, db, failed, "failed", t0)
	setStatus(t, db, today, "published", t0.Add(-time.Hour))
	setStatus(t, db, yesterday, "published", t0.Add(-48*time.Hour))

	overview, err := store.Overview(ctx, t0.Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if overview.Pending != 1 || overview.Queued != 1 || overview.Processing != 0 || overview.Failed != 1 ||
		overview.PublishedToday != 1 || overview.NextPublishAt == nil || *overview.NextPublishAt != "2026-09-20T12:33:00Z" ||
		overview.LastCollectedAt != nil {
		t.Fatalf("overview = %+v", overview)
	}

	delay, err := store.PublishDelay(ctx)
	if err != nil || delay != (newsroom.PublishDelay{MinMinutes: 30, MaxMinutes: 40}) {
		t.Fatalf("default delay = %+v err=%v", delay, err)
	}
	if err := store.SetPublishDelay(ctx, newsroom.PublishDelay{MinMinutes: 5, MaxMinutes: 9}); err != nil {
		t.Fatal(err)
	}
	if delay, err := store.PublishDelay(ctx); err != nil || delay != (newsroom.PublishDelay{MinMinutes: 5, MaxMinutes: 9}) {
		t.Fatalf("updated delay = %+v err=%v", delay, err)
	}
}

func TestLatestScheduledIncludesProcessing(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	if latest, err := store.LatestScheduled(ctx); err != nil || latest != nil {
		t.Fatalf("empty latest = %v err=%v", latest, err)
	}
	a := insert(t, store, "https://example.com/a", t0)
	b := insert(t, store, "https://example.com/b", t0)
	if err := store.MarkQueued(ctx, a, newsroom.StatusPending, t0.Add(30*time.Minute), t0); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkQueued(ctx, b, newsroom.StatusPending, t0.Add(70*time.Minute), t0); err != nil {
		t.Fatal(err)
	}
	setStatus(t, db, b, "processing", t0)
	latest, err := store.LatestScheduled(ctx)
	if err != nil || latest == nil || !latest.Equal(t0.Add(70*time.Minute)) {
		t.Fatalf("latest = %v err=%v", latest, err)
	}
}

func assertIDs(t *testing.T, candidates []newsroom.Candidate, want ...int64) {
	t.Helper()
	got := make([]int64, len(candidates))
	for i, candidate := range candidates {
		got[i] = candidate.ID
	}
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ids = %v, want %v", got, want)
		}
	}
}
```

- [ ] **Step 3: Run them to verify they fail**

Run: `go test ./internal/newsroom`
Expected: FAIL to compile: `undefined: newsroom.SQLiteStore` / `newsroom.NewSQLiteStore`.

- [ ] **Step 4: Implement the store**

Create `internal/newsroom/store.go`:

```go
package newsroom

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	settingDelayMin      = "newsroom.publish_delay_min_minutes"
	settingDelayMax      = "newsroom.publish_delay_max_minutes"
	settingLastCollected = "newsroom.last_collected_at"
)

var defaultDelay = PublishDelay{MinMinutes: 30, MaxMinutes: 40}

type SQLiteStore struct{ db *sql.DB }

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{db: db} }

const candidateSelect = `SELECT c.id, c.title, c.feed_summary, c.source_name, c.source_url,
	c.source_image_url, c.feed_published_at, c.discovered_at, c.status, c.scheduled_for,
	c.attempts, c.last_error, a.slug
	FROM candidates c LEFT JOIN articles a ON a.id = c.article_id`

// listOrder groups the queue view (processing, queued, failed) ahead of the
// review view, then orders within each status as the app displays it.
const listOrder = ` ORDER BY CASE c.status WHEN 'processing' THEN 0 WHEN 'queued' THEN 1 WHEN 'failed' THEN 2
	WHEN 'pending' THEN 3 WHEN 'published' THEN 4 ELSE 5 END,
	CASE WHEN c.status = 'queued' THEN c.scheduled_for END ASC,
	CASE WHEN c.status = 'pending' THEN c.discovered_at END DESC,
	c.updated_at DESC, c.id DESC`

type rowScanner interface{ Scan(...any) error }

func scanCandidate(row rowScanner) (Candidate, error) {
	var c Candidate
	err := row.Scan(&c.ID, &c.Title, &c.FeedSummary, &c.SourceName, &c.SourceURL,
		&c.SourceImageURL, &c.FeedPublishedAt, &c.DiscoveredAt, &c.Status, &c.ScheduledFor,
		&c.Attempts, &c.LastError, &c.ArticleSlug)
	return c, err
}

// Insert stores a new pending candidate. It reports false without error when
// the source URL is already known (including rejected candidates).
func (s *SQLiteStore) Insert(ctx context.Context, candidate NewCandidate, now time.Time) (int64, bool, error) {
	var feedPublished *string
	if candidate.FeedPublishedAt != nil {
		value := formatTime(*candidate.FeedPublishedAt)
		feedPublished = &value
	}
	stamp := formatTime(now)
	result, err := s.db.ExecContext(ctx, `INSERT INTO candidates
		(source_url, source_name, feed_url, title, feed_summary, source_image_url, feed_published_at, discovered_at, status, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?)
		ON CONFLICT(source_url) DO NOTHING`,
		candidate.SourceURL, candidate.SourceName, candidate.FeedURL, candidate.Title, candidate.FeedSummary,
		candidate.SourceImageURL, feedPublished, stamp, stamp)
	if err != nil {
		return 0, false, fmt.Errorf("insert candidate: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, false, fmt.Errorf("insert candidate: %w", err)
	}
	if affected == 0 {
		return 0, false, nil
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, false, fmt.Errorf("insert candidate id: %w", err)
	}
	return id, true, nil
}

func (s *SQLiteStore) Get(ctx context.Context, id int64) (Candidate, error) {
	candidate, err := scanCandidate(s.db.QueryRowContext(ctx, candidateSelect+` WHERE c.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Candidate{}, ErrNotFound
	}
	if err != nil {
		return Candidate{}, fmt.Errorf("get candidate %d: %w", id, err)
	}
	return candidate, nil
}

// List returns one page of candidates in any of statuses. statuses must be non-empty.
func (s *SQLiteStore) List(ctx context.Context, statuses []Status, page, limit int) (Page, error) {
	args := make([]any, 0, len(statuses)+2)
	for _, status := range statuses {
		args = append(args, string(status))
	}
	where := ` WHERE c.status IN (` + strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",") + `)`

	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM candidates c`+where, args...).Scan(&total); err != nil {
		return Page{}, fmt.Errorf("count candidates: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, candidateSelect+where+listOrder+` LIMIT ? OFFSET ?`,
		append(args, limit, (page-1)*limit)...)
	if err != nil {
		return Page{}, fmt.Errorf("list candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]Candidate, 0)
	for rows.Next() {
		candidate, err := scanCandidate(rows)
		if err != nil {
			return Page{}, fmt.Errorf("scan candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("iterate candidates: %w", err)
	}
	return Page{
		Candidates: candidates,
		Total:      total,
		Page:       page,
		TotalPages: (total + int64(limit) - 1) / int64(limit),
	}, nil
}

// transition runs a guarded UPDATE; zero affected rows means the candidate
// was missing or no longer in the expected status.
func (s *SQLiteStore) transition(ctx context.Context, id int64, query string, args ...any) error {
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update candidate %d: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update candidate %d: %w", id, err)
	}
	if affected == 0 {
		return ErrStaleTransition
	}
	return nil
}

// MarkQueued moves a candidate from `from` (pending or failed) into the queue.
func (s *SQLiteStore) MarkQueued(ctx context.Context, id int64, from Status, scheduledFor, now time.Time) error {
	return s.transition(ctx, id, `UPDATE candidates SET status = 'queued', scheduled_for = ?, attempts = 0,
		last_error = NULL, updated_at = ? WHERE id = ? AND status = ?`,
		formatTime(scheduledFor), formatTime(now), id, string(from))
}

// MarkPending returns a queued candidate to review.
func (s *SQLiteStore) MarkPending(ctx context.Context, id int64, now time.Time) error {
	return s.transition(ctx, id, `UPDATE candidates SET status = 'pending', scheduled_for = NULL, updated_at = ?
		WHERE id = ? AND status = 'queued'`, formatTime(now), id)
}

// MarkRejected rejects a pending or failed candidate. Rejected rows are kept
// so the collector never re-adds the URL.
func (s *SQLiteStore) MarkRejected(ctx context.Context, id int64, now time.Time) error {
	return s.transition(ctx, id, `UPDATE candidates SET status = 'rejected', scheduled_for = NULL, updated_at = ?
		WHERE id = ? AND status IN ('pending', 'failed')`, formatTime(now), id)
}

// LatestScheduled returns the tail of the queue, or nil when nothing is queued or processing.
func (s *SQLiteStore) LatestScheduled(ctx context.Context) (*time.Time, error) {
	var value sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(scheduled_for) FROM candidates
		WHERE status IN ('queued', 'processing')`).Scan(&value); err != nil {
		return nil, fmt.Errorf("read queue tail: %w", err)
	}
	if !value.Valid {
		return nil, nil
	}
	latest, err := parseTime(value.String)
	if err != nil {
		return nil, fmt.Errorf("parse queue tail %q: %w", value.String, err)
	}
	return &latest, nil
}

// Overview counts candidates by status; publishedSince bounds published_today.
func (s *SQLiteStore) Overview(ctx context.Context, publishedSince time.Time) (Overview, error) {
	var overview Overview
	err := s.db.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(status = 'pending'), 0),
		COALESCE(SUM(status = 'queued'), 0),
		COALESCE(SUM(status = 'processing'), 0),
		COALESCE(SUM(status = 'failed'), 0),
		COALESCE(SUM(status = 'published' AND updated_at >= ?), 0),
		MIN(CASE WHEN status = 'queued' THEN scheduled_for END)
		FROM candidates`, formatTime(publishedSince)).Scan(
		&overview.Pending, &overview.Queued, &overview.Processing, &overview.Failed,
		&overview.PublishedToday, &overview.NextPublishAt)
	if err != nil {
		return Overview{}, fmt.Errorf("count candidates by status: %w", err)
	}
	overview.LastCollectedAt, err = s.setting(ctx, settingLastCollected)
	if err != nil {
		return Overview{}, err
	}
	return overview, nil
}

func (s *SQLiteStore) setting(ctx context.Context, key string) (*string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read setting %s: %w", key, err)
	}
	return &value, nil
}

func (s *SQLiteStore) PublishDelay(ctx context.Context) (PublishDelay, error) {
	delay := defaultDelay
	for key, target := range map[string]*int{settingDelayMin: &delay.MinMinutes, settingDelayMax: &delay.MaxMinutes} {
		value, err := s.setting(ctx, key)
		if err != nil {
			return PublishDelay{}, err
		}
		if value == nil {
			continue
		}
		parsed, err := strconv.Atoi(*value)
		if err != nil {
			return PublishDelay{}, fmt.Errorf("parse setting %s=%q: %w", key, *value, err)
		}
		*target = parsed
	}
	return delay, nil
}

func (s *SQLiteStore) SetPublishDelay(ctx context.Context, delay PublishDelay) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delay update: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	for _, pair := range [][2]string{
		{settingDelayMin, strconv.Itoa(delay.MinMinutes)},
		{settingDelayMax, strconv.Itoa(delay.MaxMinutes)},
	} {
		if _, err = tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, pair[0], pair[1]); err != nil {
			return fmt.Errorf("write setting %s: %w", pair[0], err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit delay update: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Run the store tests**

Run: `go test ./internal/newsroom -run 'TestInsert|TestGet|TestTransitions|TestList|TestOverview|TestLatest' -v`
Expected: all 6 PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/newsroom/store.go internal/newsroom/store_test.go internal/newsroom/helpers_test.go
git commit -m "Add newsroom candidate store"
```

---

### Task 4: Queue service

**Files:**
- Create: `internal/newsroom/service_test.go`
- Create: `internal/newsroom/service.go`

- [ ] **Step 1: Write the failing service tests**

Create `internal/newsroom/service_test.go`:

```go
package newsroom_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
)

// sequence returns a deterministic random source cycling through minutes.
func sequence(minutes ...int) func(int, int) int {
	i := 0
	return func(int, int) int {
		value := minutes[i%len(minutes)]
		i++
		return value
	}
}

func newService(t *testing.T, store *newsroom.SQLiteStore, now time.Time, random func(int, int) int) *newsroom.Service {
	t.Helper()
	service, err := newsroom.NewService(store, func() time.Time { return now }, random)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func scheduledFor(t *testing.T, candidate newsroom.Candidate) string {
	t.Helper()
	if candidate.ScheduledFor == nil {
		t.Fatalf("candidate %d has no scheduled_for", candidate.ID)
	}
	return *candidate.ScheduledFor
}

func TestPublishSpacesItemsAfterEachOther(t *testing.T) {
	store, _ := openStore(t)
	a := insert(t, store, "https://example.com/a", t0)
	b := insert(t, store, "https://example.com/b", t0)
	c := insert(t, store, "https://example.com/c", t0)
	service := newService(t, store, t0, sequence(35, 31, 40))

	queued, skipped, err := service.Publish(context.Background(), []int64{a, b, c})
	if err != nil || len(skipped) != 0 || len(queued) != 3 {
		t.Fatalf("queued=%v skipped=%v err=%v", queued, skipped, err)
	}
	want := []string{"2026-09-20T12:35:00Z", "2026-09-20T13:06:00Z", "2026-09-20T13:46:00Z"}
	for i, candidate := range queued {
		if candidate.Status != newsroom.StatusQueued || scheduledFor(t, candidate) != want[i] {
			t.Fatalf("queued[%d] = %#v, want scheduled %s", i, candidate, want[i])
		}
	}
}

func TestPublishAppendsAfterExistingQueueTail(t *testing.T) {
	store, _ := openStore(t)
	a := insert(t, store, "https://example.com/a", t0)
	b := insert(t, store, "https://example.com/b", t0)
	service := newService(t, store, t0, sequence(35, 31))

	if _, _, err := service.Publish(context.Background(), []int64{a}); err != nil {
		t.Fatal(err)
	}
	queued, _, err := service.Publish(context.Background(), []int64{b})
	if err != nil || len(queued) != 1 || scheduledFor(t, queued[0]) != "2026-09-20T13:06:00Z" {
		t.Fatalf("queued=%v err=%v", queued, err)
	}
}

func TestPublishPassesConfiguredDelayToRandomSource(t *testing.T) {
	store, _ := openStore(t)
	a := insert(t, store, "https://example.com/a", t0)
	var gotMin, gotMax int
	service := newService(t, store, t0, func(min, max int) int { gotMin, gotMax = min, max; return min })

	if _, err := service.SetPublishDelay(context.Background(), newsroom.PublishDelay{MinMinutes: 10, MaxMinutes: 20}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Publish(context.Background(), []int64{a}); err != nil {
		t.Fatal(err)
	}
	if gotMin != 10 || gotMax != 20 {
		t.Fatalf("random called with %d..%d", gotMin, gotMax)
	}
}

func TestPublishSkipsDuplicateMissingAndNonPending(t *testing.T) {
	store, db := openStore(t)
	a := insert(t, store, "https://example.com/a", t0)
	rejected := insert(t, store, "https://example.com/r", t0)
	setStatus(t, db, rejected, "rejected", t0)
	service := newService(t, store, t0, sequence(30))

	queued, skipped, err := service.Publish(context.Background(), []int64{a, a, 999, rejected})
	if err != nil {
		t.Fatal(err)
	}
	if len(queued) != 1 || queued[0].ID != a {
		t.Fatalf("queued = %v", queued)
	}
	want := []newsroom.Skipped{{ID: a, Reason: "duplicate id"}, {ID: 999, Reason: "not found"}, {ID: rejected, Reason: "not pending"}}
	if len(skipped) != len(want) {
		t.Fatalf("skipped = %v", skipped)
	}
	for i := range want {
		if skipped[i] != want[i] {
			t.Fatalf("skipped = %v, want %v", skipped, want)
		}
	}
}

func TestRetryAppendsFailedCandidateToQueueTail(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	queuedID := insert(t, store, "https://example.com/q", t0)
	failed := insert(t, store, "https://example.com/f", t0)
	service := newService(t, store, t0, sequence(35, 32))
	if _, _, err := service.Publish(ctx, []int64{queuedID}); err != nil {
		t.Fatal(err)
	}
	setStatus(t, db, failed, "failed", t0)
	if _, err := db.Exec(`UPDATE candidates SET attempts = 3, last_error = 'boom' WHERE id = ?`, failed); err != nil {
		t.Fatal(err)
	}

	retried, err := service.Retry(ctx, failed)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Status != newsroom.StatusQueued || scheduledFor(t, retried) != "2026-09-20T13:07:00Z" ||
		retried.Attempts != 0 || retried.LastError != nil {
		t.Fatalf("retried = %#v", retried)
	}
	if _, err := service.Retry(ctx, queuedID); !errors.Is(err, newsroom.ErrStaleTransition) {
		t.Fatalf("retry queued err = %v", err)
	}
	if _, err := service.Retry(ctx, 999); !errors.Is(err, newsroom.ErrNotFound) {
		t.Fatalf("retry missing err = %v", err)
	}
}

func TestUnqueueReturnsCandidateToReview(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	a := insert(t, store, "https://example.com/a", t0)
	service := newService(t, store, t0, sequence(30))
	if _, _, err := service.Publish(ctx, []int64{a}); err != nil {
		t.Fatal(err)
	}

	unqueued, err := service.Unqueue(ctx, a)
	if err != nil || unqueued.Status != newsroom.StatusPending || unqueued.ScheduledFor != nil {
		t.Fatalf("unqueued = %#v err=%v", unqueued, err)
	}
	if _, err := service.Unqueue(ctx, a); !errors.Is(err, newsroom.ErrStaleTransition) {
		t.Fatalf("second unqueue err = %v", err)
	}
	if _, err := service.Unqueue(ctx, 999); !errors.Is(err, newsroom.ErrNotFound) {
		t.Fatalf("missing unqueue err = %v", err)
	}
}

func TestRejectAcceptsPendingAndFailedOnly(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	pending := insert(t, store, "https://example.com/p", t0)
	failed := insert(t, store, "https://example.com/f", t0)
	queued := insert(t, store, "https://example.com/q", t0)
	setStatus(t, db, failed, "failed", t0)
	service := newService(t, store, t0, sequence(30))
	if _, _, err := service.Publish(ctx, []int64{queued}); err != nil {
		t.Fatal(err)
	}

	rejected, skipped, err := service.Reject(ctx, []int64{pending, failed, queued, 999, pending})
	if err != nil {
		t.Fatal(err)
	}
	if len(rejected) != 2 || rejected[0] != pending || rejected[1] != failed {
		t.Fatalf("rejected = %v", rejected)
	}
	want := []newsroom.Skipped{
		{ID: queued, Reason: "not pending or failed"},
		{ID: 999, Reason: "not found"},
		{ID: pending, Reason: "duplicate id"},
	}
	if len(skipped) != len(want) {
		t.Fatalf("skipped = %v", skipped)
	}
	for i := range want {
		if skipped[i] != want[i] {
			t.Fatalf("skipped = %v, want %v", skipped, want)
		}
	}
}

func TestOverviewCountsPublishedSinceIstanbulMidnight(t *testing.T) {
	store, db := openStore(t)
	// 12:00 UTC on 2026-09-20 is 15:00 in Istanbul; local midnight is 2026-09-19T21:00:00Z.
	after := insert(t, store, "https://example.com/after", t0)
	before := insert(t, store, "https://example.com/before", t0)
	setStatus(t, db, after, "published", time.Date(2026, 9, 19, 21, 30, 0, 0, time.UTC))
	setStatus(t, db, before, "published", time.Date(2026, 9, 19, 20, 30, 0, 0, time.UTC))
	service := newService(t, store, t0, sequence(30))

	overview, err := service.Overview(context.Background())
	if err != nil || overview.PublishedToday != 1 {
		t.Fatalf("overview = %+v err=%v", overview, err)
	}
}

func TestSetPublishDelayValidatesRange(t *testing.T) {
	store, _ := openStore(t)
	service := newService(t, store, t0, sequence(30))
	for _, invalid := range []newsroom.PublishDelay{{MinMinutes: 0, MaxMinutes: 10}, {MinMinutes: 41, MaxMinutes: 40}, {MinMinutes: 30, MaxMinutes: 1441}} {
		if _, err := service.SetPublishDelay(context.Background(), invalid); !errors.Is(err, newsroom.ErrInvalidDelay) {
			t.Fatalf("SetPublishDelay(%+v) err = %v", invalid, err)
		}
	}
	saved, err := service.SetPublishDelay(context.Background(), newsroom.PublishDelay{MinMinutes: 1, MaxMinutes: 1440})
	if err != nil || saved != (newsroom.PublishDelay{MinMinutes: 1, MaxMinutes: 1440}) {
		t.Fatalf("saved = %+v err=%v", saved, err)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/newsroom -run 'TestPublish|TestRetry|TestUnqueue|TestReject|TestOverviewCounts|TestSetPublishDelay'`
Expected: FAIL to compile: `undefined: newsroom.NewService`.

- [ ] **Step 3: Implement the service**

Create `internal/newsroom/service.go`:

```go
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
	scheduling sync.Mutex
}

func NewService(store candidateStore, now func() time.Time, randMinutes func(min, max int) int) (*Service, error) {
	if store == nil || now == nil || randMinutes == nil {
		return nil, errors.New("newsroom service requires store, clock, and random source")
	}
	location, err := time.LoadLocation(EditorialTimeZone)
	if err != nil {
		return nil, fmt.Errorf("load editorial time zone: %w", err)
	}
	return &Service{store: store, now: now, randMinutes: randMinutes, editorialTZ: location}, nil
}

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
			return nil, nil, err
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

// Unqueue returns a queued candidate to review. Other queue entries keep their times.
func (s *Service) Unqueue(ctx context.Context, id int64) (Candidate, error) {
	if err := s.store.MarkPending(ctx, id, s.now()); err != nil {
		return Candidate{}, s.staleOrMissing(ctx, id, err)
	}
	return s.store.Get(ctx, id)
}

// Reject rejects pending or failed candidates, reporting the rest as skipped.
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
			return nil, nil, err
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
	*tail = at
	return s.store.Get(ctx, id)
}

// staleOrMissing distinguishes a missing candidate from a status mismatch
// after a guarded update affected no rows.
func (s *Service) staleOrMissing(ctx context.Context, id int64, err error) error {
	if !errors.Is(err, ErrStaleTransition) {
		return err
	}
	if _, getErr := s.store.Get(ctx, id); errors.Is(getErr, ErrNotFound) {
		return ErrNotFound
	}
	return err
}
```

- [ ] **Step 4: Run the package tests**

Run: `go test ./internal/newsroom -v`
Expected: all store and service tests PASS.

- [ ] **Step 5: Run with the race detector**

Run: `go test -race ./internal/newsroom`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/newsroom/service.go internal/newsroom/service_test.go
git commit -m "Add newsroom queue service with spaced scheduling"
```

---

### Task 5: Export the auth middleware

**Files:**
- Modify: `internal/editorial/http_auth.go` (after `requireAuth`)

- [ ] **Step 1: Add the exported wrapper**

Add below the existing `requireAuth` method in `internal/editorial/http_auth.go`:

```go
// RequireAuth lets other capabilities protect their routes with the same
// bearer-token check used by /auth/me.
func (h *AuthHandler) RequireAuth(next http.Handler) http.Handler { return h.requireAuth(next) }
```

- [ ] **Step 2: Verify existing auth tests still pass**

Run: `go test ./internal/editorial ./internal/app -run Auth`
Expected: `ok` for both packages.

- [ ] **Step 3: Commit**

```bash
git add internal/editorial/http_auth.go
git commit -m "Expose editorial auth middleware to other capabilities"
```

---

### Task 6: Contract file

**Files:**
- Create: `contracts/newsroom.openapi.yaml`

This is written before the handler so the HTTP tests in Task 7 can iterate over it. It is deliberately separate from `contracts/openapi.yaml`, which `internal/contracttest` requires to match the Node fixture's 32 operations exactly.

- [ ] **Step 1: Write the contract**

Create `contracts/newsroom.openapi.yaml`:

```yaml
openapi: 3.1.0
info:
  title: Technews newsroom API
  version: 1.0.0
  description: >-
    Editorial candidate review and publish queue used by the Omni Control iOS
    app. All operations require a bearer JWT from POST /api/auth/login.
    Timestamps are RFC 3339 UTC. Errors are {"error": string}.
servers:
  - url: /
security:
  - bearerAuth: []
paths:
  /api/newsroom/overview:
    get:
      operationId: newsroom.overview
      responses:
        '200':
          description: Counts by status
          content:
            application/json:
              schema: { $ref: '#/components/schemas/Overview' }
              example:
                pending: 12
                queued: 2
                processing: 0
                failed: 1
                published_today: 3
                next_publish_at: '2026-09-23T15:40:00Z'
                last_collected_at: '2026-09-23T15:00:00Z'
        '401': { $ref: '#/components/responses/Unauthorized' }
  /api/newsroom/candidates:
    get:
      operationId: newsroom.candidates.list
      parameters:
        - name: status
          in: query
          description: Comma-separated statuses; defaults to pending.
          schema: { type: string, example: 'queued,processing,failed' }
        - name: page
          in: query
          schema: { type: integer, minimum: 1, default: 1 }
        - name: limit
          in: query
          schema: { type: integer, minimum: 1, maximum: 50, default: 20 }
      responses:
        '200':
          description: One page of candidates
          content:
            application/json:
              schema: { $ref: '#/components/schemas/CandidatePage' }
              example:
                candidates:
                  - id: 42
                    title: OpenAI ships a new reasoning model
                    feed_summary: The model targets agentic coding tasks.
                    source_name: The Verge
                    source_url: https://www.theverge.com/ai/42
                    source_image_url: https://cdn.example.com/42.jpg
                    feed_published_at: '2026-09-23T10:02:00Z'
                    discovered_at: '2026-09-23T10:30:00Z'
                    status: pending
                    scheduled_for: null
                    attempts: 0
                    last_error: null
                    article_slug: null
                total: 1
                page: 1
                totalPages: 1
        '400': { $ref: '#/components/responses/BadRequest' }
        '401': { $ref: '#/components/responses/Unauthorized' }
  /api/newsroom/candidates/publish:
    post:
      operationId: newsroom.candidates.publish
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: '#/components/schemas/IDs' }
      responses:
        '200':
          description: Queued candidates and per-id skips
          content:
            application/json:
              schema:
                type: object
                required: [queued, skipped]
                properties:
                  queued: { type: array, items: { $ref: '#/components/schemas/Candidate' } }
                  skipped: { type: array, items: { $ref: '#/components/schemas/Skipped' } }
        '400': { $ref: '#/components/responses/BadRequest' }
        '401': { $ref: '#/components/responses/Unauthorized' }
  /api/newsroom/candidates/reject:
    post:
      operationId: newsroom.candidates.reject
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: '#/components/schemas/IDs' }
      responses:
        '200':
          description: Rejected ids and per-id skips
          content:
            application/json:
              schema:
                type: object
                required: [rejected, skipped]
                properties:
                  rejected: { type: array, items: { type: integer } }
                  skipped: { type: array, items: { $ref: '#/components/schemas/Skipped' } }
        '400': { $ref: '#/components/responses/BadRequest' }
        '401': { $ref: '#/components/responses/Unauthorized' }
  /api/newsroom/candidates/{id}/unqueue:
    post:
      operationId: newsroom.candidates.unqueue
      parameters: [{ $ref: '#/components/parameters/ID' }]
      responses:
        '200': { $ref: '#/components/responses/SingleCandidate' }
        '401': { $ref: '#/components/responses/Unauthorized' }
        '404': { $ref: '#/components/responses/NotFound' }
        '409': { $ref: '#/components/responses/Conflict' }
  /api/newsroom/candidates/{id}/retry:
    post:
      operationId: newsroom.candidates.retry
      parameters: [{ $ref: '#/components/parameters/ID' }]
      responses:
        '200': { $ref: '#/components/responses/SingleCandidate' }
        '401': { $ref: '#/components/responses/Unauthorized' }
        '404': { $ref: '#/components/responses/NotFound' }
        '409': { $ref: '#/components/responses/Conflict' }
  /api/newsroom/collect:
    post:
      operationId: newsroom.collect
      responses:
        '202':
          description: Collection started
          content:
            application/json:
              schema:
                type: object
                required: [started]
                properties: { started: { type: boolean, const: true } }
        '401': { $ref: '#/components/responses/Unauthorized' }
        '409': { $ref: '#/components/responses/Conflict' }
        '503':
          description: Collector not enabled on this server
          content:
            application/json:
              schema: { $ref: '#/components/schemas/Error' }
  /api/newsroom/settings:
    get:
      operationId: newsroom.settings.get
      responses:
        '200': { $ref: '#/components/responses/Settings' }
        '401': { $ref: '#/components/responses/Unauthorized' }
    put:
      operationId: newsroom.settings.update
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: '#/components/schemas/PublishDelay' }
      responses:
        '200': { $ref: '#/components/responses/Settings' }
        '400': { $ref: '#/components/responses/BadRequest' }
        '401': { $ref: '#/components/responses/Unauthorized' }
components:
  securitySchemes:
    bearerAuth: { type: http, scheme: bearer, bearerFormat: JWT }
  parameters:
    ID:
      name: id
      in: path
      required: true
      schema: { type: integer, minimum: 1 }
  responses:
    SingleCandidate:
      description: The updated candidate
      content:
        application/json:
          schema:
            type: object
            required: [candidate]
            properties: { candidate: { $ref: '#/components/schemas/Candidate' } }
    Settings:
      description: Publish delay window
      content:
        application/json:
          schema: { $ref: '#/components/schemas/PublishDelay' }
          example: { publish_delay_min_minutes: 30, publish_delay_max_minutes: 40 }
    BadRequest:
      description: Invalid input
      content: { application/json: { schema: { $ref: '#/components/schemas/Error' } } }
    Unauthorized:
      description: Missing, invalid or expired token
      content: { application/json: { schema: { $ref: '#/components/schemas/Error' } } }
    NotFound:
      description: Candidate not found
      content: { application/json: { schema: { $ref: '#/components/schemas/Error' } } }
    Conflict:
      description: Candidate is not in the required status, or a collection is running
      content: { application/json: { schema: { $ref: '#/components/schemas/Error' } } }
  schemas:
    Error:
      type: object
      required: [error]
      properties: { error: { type: string } }
    IDs:
      type: object
      required: [ids]
      properties:
        ids: { type: array, minItems: 1, maxItems: 100, items: { type: integer } }
    Skipped:
      type: object
      required: [id, reason]
      properties:
        id: { type: integer }
        reason: { type: string, enum: ['duplicate id', 'not found', 'not pending', 'not pending or failed'] }
    PublishDelay:
      type: object
      required: [publish_delay_min_minutes, publish_delay_max_minutes]
      properties:
        publish_delay_min_minutes: { type: integer, minimum: 1, maximum: 1440 }
        publish_delay_max_minutes: { type: integer, minimum: 1, maximum: 1440 }
    Overview:
      type: object
      required: [pending, queued, processing, failed, published_today, next_publish_at, last_collected_at]
      properties:
        pending: { type: integer }
        queued: { type: integer }
        processing: { type: integer }
        failed: { type: integer }
        published_today: { type: integer }
        next_publish_at: { type: [string, 'null'], format: date-time }
        last_collected_at: { type: [string, 'null'], format: date-time }
    CandidatePage:
      type: object
      required: [candidates, total, page, totalPages]
      properties:
        candidates: { type: array, items: { $ref: '#/components/schemas/Candidate' } }
        total: { type: integer }
        page: { type: integer }
        totalPages: { type: integer }
    Candidate:
      type: object
      required: [id, title, feed_summary, source_name, source_url, source_image_url, feed_published_at,
                 discovered_at, status, scheduled_for, attempts, last_error, article_slug]
      properties:
        id: { type: integer }
        title: { type: string }
        feed_summary: { type: string }
        source_name: { type: string }
        source_url: { type: string, format: uri }
        source_image_url: { type: [string, 'null'], format: uri }
        feed_published_at: { type: [string, 'null'], format: date-time }
        discovered_at: { type: string, format: date-time }
        status: { type: string, enum: [pending, queued, processing, published, failed, rejected] }
        scheduled_for: { type: [string, 'null'], format: date-time }
        attempts: { type: integer }
        last_error: { type: [string, 'null'] }
        article_slug: { type: [string, 'null'] }
```

- [ ] **Step 2: Verify the existing contract suite is unaffected**

Run: `make contracts-check`
Expected: passes (the new file is not read by `internal/contracttest`).

- [ ] **Step 3: Commit**

```bash
git add contracts/newsroom.openapi.yaml
git commit -m "Add newsroom API contract"
```

---

### Task 7: HTTP handler and app wiring

**Files:**
- Create: `internal/app/newsroom_http_test.go`
- Create: `internal/newsroom/http.go`
- Modify: `internal/app/app.go` (`NewWithDatabaseAt`)

- [ ] **Step 1: Write the failing HTTP tests**

Create `internal/app/newsroom_http_test.go` (package `app_test`; reuses `request` and `authTestSecret` from `auth_http_test.go`):

```go
package app_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/editorial"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

var newsroomNow = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

func newsroomApplication(t *testing.T) (http.Handler, *sql.DB, string) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4402", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), JWTSecret: authTestSecret}
	clock := func() time.Time { return newsroomNow }
	application, err := app.NewWithDatabaseAt(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db, clock)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := editorial.NewJWT(authTestSecret, clock)
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.Sign(editorial.Identity{ID: 1, Email: "editor@example.invalid", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	return application.Handler(), db, "Bearer " + token
}

func seedCandidates(t *testing.T, db *sql.DB, urls ...string) {
	t.Helper()
	store := newsroom.NewSQLiteStore(db)
	for _, url := range urls {
		if _, inserted, err := store.Insert(context.Background(), newsroom.NewCandidate{
			SourceURL: url, SourceName: "The Verge", FeedURL: "https://www.theverge.com/rss/index.xml", Title: "Title " + url,
		}, newsroomNow); err != nil || !inserted {
			t.Fatalf("seed %s: inserted=%v err=%v", url, inserted, err)
		}
	}
}

func decodeBody[T any](t *testing.T, body string) T {
	t.Helper()
	var value T
	if err := json.Unmarshal([]byte(body), &value); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return value
}

func expectStatus(t *testing.T, method, path string, code int, got int, body string) {
	t.Helper()
	if got != code {
		t.Fatalf("%s %s = %d %s, want %d", method, path, got, body, code)
	}
}

// Every operation in the newsroom contract must exist and require auth.
func TestNewsroomContractOperationsRequireAuth(t *testing.T) {
	handler, _, _ := newsroomApplication(t)
	raw, err := os.ReadFile(filepath.Join("..", "..", "contracts", "newsroom.openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	operations := 0
	for path, methods := range document.Paths {
		for method := range methods {
			concrete := strings.ReplaceAll(path, "{id}", "1")
			response := request(t, handler, strings.ToUpper(method), concrete, "", "")
			if response.Code != http.StatusUnauthorized || response.Body.String() != `{"error":"Authentication required"}` {
				t.Errorf("%s %s = %d %s, want 401", method, concrete, response.Code, response.Body.String())
			}
			operations++
		}
	}
	if operations != 9 {
		t.Fatalf("contract operations = %d, want 9", operations)
	}
}

func TestNewsroomReviewAndQueueFlow(t *testing.T) {
	handler, db, auth := newsroomApplication(t)
	seedCandidates(t, db, "https://example.com/1", "https://example.com/2")

	list := request(t, handler, http.MethodGet, "/api/newsroom/candidates", "", auth)
	expectStatus(t, "GET", "/candidates", http.StatusOK, list.Code, list.Body.String())
	page := decodeBody[newsroom.Page](t, list.Body.String())
	if page.Total != 2 || page.TotalPages != 1 || len(page.Candidates) != 2 {
		t.Fatalf("page = %+v", page)
	}

	publish := request(t, handler, http.MethodPost, "/api/newsroom/candidates/publish", `{"ids":[1,2,99]}`, auth)
	expectStatus(t, "POST", "/publish", http.StatusOK, publish.Code, publish.Body.String())
	published := decodeBody[struct {
		Queued  []newsroom.Candidate `json:"queued"`
		Skipped []newsroom.Skipped   `json:"skipped"`
	}](t, publish.Body.String())
	if len(published.Queued) != 2 || len(published.Skipped) != 1 || published.Skipped[0] != (newsroom.Skipped{ID: 99, Reason: "not found"}) {
		t.Fatalf("publish = %+v", published)
	}
	first, err := time.Parse(time.RFC3339, *published.Queued[0].ScheduledFor)
	if err != nil || first.Before(newsroomNow.Add(30*time.Minute)) || first.After(newsroomNow.Add(40*time.Minute)) {
		t.Fatalf("first scheduled_for = %v err=%v", published.Queued[0].ScheduledFor, err)
	}

	overview := decodeBody[newsroom.Overview](t, request(t, handler, http.MethodGet, "/api/newsroom/overview", "", auth).Body.String())
	if overview.Pending != 0 || overview.Queued != 2 || overview.NextPublishAt == nil || *overview.NextPublishAt != *published.Queued[0].ScheduledFor {
		t.Fatalf("overview = %+v", overview)
	}

	queue := decodeBody[newsroom.Page](t, request(t, handler, http.MethodGet, "/api/newsroom/candidates?status=queued,processing,failed", "", auth).Body.String())
	if queue.Total != 2 || queue.Candidates[0].ID != 1 {
		t.Fatalf("queue = %+v", queue)
	}

	unqueue := request(t, handler, http.MethodPost, "/api/newsroom/candidates/1/unqueue", "", auth)
	expectStatus(t, "POST", "/1/unqueue", http.StatusOK, unqueue.Code, unqueue.Body.String())
	if got := decodeBody[struct{ Candidate newsroom.Candidate }](t, unqueue.Body.String()).Candidate; got.Status != newsroom.StatusPending {
		t.Fatalf("unqueued = %+v", got)
	}
	again := request(t, handler, http.MethodPost, "/api/newsroom/candidates/1/unqueue", "", auth)
	if again.Code != http.StatusConflict || again.Body.String() != `{"error":"Candidate is not queued"}` {
		t.Fatalf("second unqueue = %d %s", again.Code, again.Body.String())
	}

	reject := request(t, handler, http.MethodPost, "/api/newsroom/candidates/reject", `{"ids":[1,2]}`, auth)
	expectStatus(t, "POST", "/reject", http.StatusOK, reject.Code, reject.Body.String())
	rejected := decodeBody[struct {
		Rejected []int64            `json:"rejected"`
		Skipped  []newsroom.Skipped `json:"skipped"`
	}](t, reject.Body.String())
	if len(rejected.Rejected) != 1 || rejected.Rejected[0] != 1 || len(rejected.Skipped) != 1 || rejected.Skipped[0].Reason != "not pending or failed" {
		t.Fatalf("reject = %+v", rejected)
	}

	retry := request(t, handler, http.MethodPost, "/api/newsroom/candidates/2/retry", "", auth)
	if retry.Code != http.StatusConflict || retry.Body.String() != `{"error":"Candidate is not failed"}` {
		t.Fatalf("retry queued = %d %s", retry.Code, retry.Body.String())
	}
}

func TestNewsroomInputValidation(t *testing.T) {
	handler, _, auth := newsroomApplication(t)
	cases := []struct {
		method, path, body string
		code               int
		response           string
	}{
		{http.MethodGet, "/api/newsroom/candidates?status=bogus", "", http.StatusBadRequest, `{"error":"unknown status \"bogus\""}`},
		{http.MethodPost, "/api/newsroom/candidates/publish", `{"ids":[]}`, http.StatusBadRequest, `{"error":"ids must contain 1 to 100 candidate ids"}`},
		{http.MethodPost, "/api/newsroom/candidates/publish", `{"ids":[1]} {}`, http.StatusBadRequest, `{"error":"Invalid request body"}`},
		{http.MethodPost, "/api/newsroom/candidates/reject", `{"id":1}`, http.StatusBadRequest, `{"error":"Invalid request body"}`},
		{http.MethodPost, "/api/newsroom/candidates/abc/retry", "", http.StatusNotFound, `{"error":"Candidate not found"}`},
		{http.MethodPost, "/api/newsroom/candidates/7/unqueue", "", http.StatusNotFound, `{"error":"Candidate not found"}`},
		{http.MethodPut, "/api/newsroom/settings", `{"publish_delay_min_minutes":50,"publish_delay_max_minutes":40}`, http.StatusBadRequest, `{"error":"publish delay minutes must be between 1 and 1440, with min not above max"}`},
		{http.MethodPut, "/api/newsroom/settings", `{"publish_delay_min_minutes":10}`, http.StatusBadRequest, `{"error":"Invalid request body"}`},
		{http.MethodPost, "/api/newsroom/collect", "", http.StatusServiceUnavailable, `{"error":"Collector is not enabled"}`},
	}
	for _, tc := range cases {
		response := request(t, handler, tc.method, tc.path, tc.body, auth)
		if response.Code != tc.code || response.Body.String() != tc.response {
			t.Errorf("%s %s %s = %d %s, want %d %s", tc.method, tc.path, tc.body, response.Code, response.Body.String(), tc.code, tc.response)
		}
	}
}

func TestNewsroomSettingsRoundTrip(t *testing.T) {
	handler, _, auth := newsroomApplication(t)
	get := request(t, handler, http.MethodGet, "/api/newsroom/settings", "", auth)
	if get.Code != http.StatusOK || get.Body.String() != `{"publish_delay_min_minutes":30,"publish_delay_max_minutes":40}` {
		t.Fatalf("default settings = %d %s", get.Code, get.Body.String())
	}
	put := request(t, handler, http.MethodPut, "/api/newsroom/settings", `{"publish_delay_min_minutes":10,"publish_delay_max_minutes":20}`, auth)
	if put.Code != http.StatusOK || put.Body.String() != `{"publish_delay_min_minutes":10,"publish_delay_max_minutes":20}` {
		t.Fatalf("put settings = %d %s", put.Code, put.Body.String())
	}
	if again := request(t, handler, http.MethodGet, "/api/newsroom/settings", "", auth); again.Body.String() != put.Body.String() {
		t.Fatalf("settings after put = %s", again.Body.String())
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/app -run Newsroom`
Expected: FAIL. The contract test reports every operation as `404` (routes not mounted), e.g. `get /api/newsroom/overview = 404 {"error":"Not found"}, want 401`.

- [ ] **Step 3: Implement the handler**

Create `internal/newsroom/http.go`:

```go
package newsroom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

const (
	maxBodyBytes     = 100 * 1024
	maxIDsPerRequest = 100
)

// ErrCollectInProgress is returned by a Collector when a run is already active.
var ErrCollectInProgress = errors.New("collection already running")

// Collector starts a collection run in the background. It is nil until the
// Go collector exists, and /collect answers 503 in the meantime.
type Collector interface {
	Start(context.Context) error
}

type Handler struct {
	service   *Service
	collector Collector
	logger    *slog.Logger
}

func NewHandler(service *Service, collector Collector, logger *slog.Logger) *Handler {
	return &Handler{service: service, collector: collector, logger: logger}
}

// Mount registers the /newsroom routes behind requireAuth.
func (h *Handler) Mount(router chi.Router, requireAuth func(http.Handler) http.Handler) {
	router.Route("/newsroom", func(r chi.Router) {
		r.Use(requireAuth)
		r.Get("/overview", h.overview)
		r.Get("/candidates", h.list)
		r.Post("/candidates/publish", h.publish)
		r.Post("/candidates/reject", h.reject)
		r.Post("/candidates/{id}/unqueue", h.unqueue)
		r.Post("/candidates/{id}/retry", h.retry)
		r.Post("/collect", h.collect)
		r.Get("/settings", h.settings)
		r.Put("/settings", h.updateSettings)
	})
}

func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	overview, err := h.service.Overview(r.Context())
	if err != nil {
		h.internalError(w, r, "newsroom overview", err)
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	statuses, err := parseStatuses(query.Get("status"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	page := parsePositive(query.Get("page"), 1, 0)
	limit := parsePositive(query.Get("limit"), DefaultPageLimit, MaxPageLimit)
	result, err := h.service.List(r.Context(), statuses, page, limit)
	if err != nil {
		h.internalError(w, r, "list candidates", err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) publish(w http.ResponseWriter, r *http.Request) {
	ids, ok := decodeIDs(w, r)
	if !ok {
		return
	}
	queued, skipped, err := h.service.Publish(r.Context(), ids)
	if err != nil {
		h.internalError(w, r, "publish candidates", err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Queued  []Candidate `json:"queued"`
		Skipped []Skipped   `json:"skipped"`
	}{queued, skipped})
}

func (h *Handler) reject(w http.ResponseWriter, r *http.Request) {
	ids, ok := decodeIDs(w, r)
	if !ok {
		return
	}
	rejected, skipped, err := h.service.Reject(r.Context(), ids)
	if err != nil {
		h.internalError(w, r, "reject candidates", err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Rejected []int64   `json:"rejected"`
		Skipped  []Skipped `json:"skipped"`
	}{rejected, skipped})
}

func (h *Handler) unqueue(w http.ResponseWriter, r *http.Request) {
	h.single(w, r, h.service.Unqueue, "Candidate is not queued")
}

func (h *Handler) retry(w http.ResponseWriter, r *http.Request) {
	h.single(w, r, h.service.Retry, "Candidate is not failed")
}

func (h *Handler) single(w http.ResponseWriter, r *http.Request, action func(context.Context, int64) (Candidate, error), staleMessage string) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusNotFound, "Candidate not found")
		return
	}
	candidate, err := action(r.Context(), id)
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "Candidate not found")
	case errors.Is(err, ErrStaleTransition):
		writeError(w, http.StatusConflict, staleMessage)
	case err != nil:
		h.internalError(w, r, "update candidate", err)
	default:
		writeJSON(w, http.StatusOK, struct {
			Candidate Candidate `json:"candidate"`
		}{candidate})
	}
}

func (h *Handler) collect(w http.ResponseWriter, r *http.Request) {
	if h.collector == nil {
		writeError(w, http.StatusServiceUnavailable, "Collector is not enabled")
		return
	}
	err := h.collector.Start(r.Context())
	if errors.Is(err, ErrCollectInProgress) {
		writeError(w, http.StatusConflict, "Collection already running")
		return
	}
	if err != nil {
		h.internalError(w, r, "start collection", err)
		return
	}
	writeJSON(w, http.StatusAccepted, struct {
		Started bool `json:"started"`
	}{true})
}

func (h *Handler) settings(w http.ResponseWriter, r *http.Request) {
	delay, err := h.service.PublishDelay(r.Context())
	if err != nil {
		h.internalError(w, r, "read newsroom settings", err)
		return
	}
	writeJSON(w, http.StatusOK, delay)
}

func (h *Handler) updateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MinMinutes *int `json:"publish_delay_min_minutes"`
		MaxMinutes *int `json:"publish_delay_max_minutes"`
	}
	if err := decodeJSON(w, r, &body); err != nil || body.MinMinutes == nil || body.MaxMinutes == nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	delay, err := h.service.SetPublishDelay(r.Context(), PublishDelay{MinMinutes: *body.MinMinutes, MaxMinutes: *body.MaxMinutes})
	if errors.Is(err, ErrInvalidDelay) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		h.internalError(w, r, "update newsroom settings", err)
		return
	}
	writeJSON(w, http.StatusOK, delay)
}

func decodeIDs(w http.ResponseWriter, r *http.Request) ([]int64, bool) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return nil, false
	}
	if len(body.IDs) == 0 || len(body.IDs) > maxIDsPerRequest {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("ids must contain 1 to %d candidate ids", maxIDsPerRequest))
		return nil, false
	}
	return body.IDs, true
}

// decodeJSON reads exactly one JSON value with no unknown fields.
func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("trailing data after JSON body")
	}
	return nil
}

func parseStatuses(value string) ([]Status, error) {
	if strings.TrimSpace(value) == "" {
		return []Status{StatusPending}, nil
	}
	statuses := make([]Status, 0)
	seen := make(map[Status]bool)
	for _, part := range strings.Split(value, ",") {
		name := strings.TrimSpace(part)
		status, ok := ParseStatus(name)
		if !ok {
			return nil, fmt.Errorf("unknown status %q", name)
		}
		if !seen[status] {
			seen[status] = true
			statuses = append(statuses, status)
		}
	}
	return statuses, nil
}

// parsePositive returns fallback for missing or invalid values and clamps to max when max > 0.
func parsePositive(value string, fallback, max int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 1 {
		return fallback
	}
	if max > 0 && parsed > max {
		return max
	}
	return parsed
}

func (h *Handler) internalError(w http.ResponseWriter, r *http.Request, message string, err error) {
	h.logger.ErrorContext(r.Context(), message, "error", err)
	writeError(w, http.StatusInternalServerError, "Internal server error")
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		body = []byte(`{"error":"Internal server error"}`)
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
```

- [ ] **Step 4: Wire it into the app**

In `internal/app/app.go`, inside `NewWithDatabaseAt`, after the line that builds `auth := editorial.NewAuthHandler(...)`, add:

```go
	newsroomService, err := newsroom.NewService(newsroom.NewSQLiteStore(db), now, newsroom.RandomMinutes)
	if err != nil {
		return nil, err
	}
	newsroomHandler := newsroom.NewHandler(newsroomService, nil, logger)
```

and inside the `httpserver.NewRouter` callback, after `auth.Mount(router)`:

```go
		newsroomHandler.Mount(router, auth.RequireAuth)
```

(The `newsroom` import was added in Task 1.)

- [ ] **Step 5: Run the newsroom HTTP tests**

Run: `go test ./internal/app -run Newsroom -v`
Expected: all 4 tests PASS.

- [ ] **Step 6: Run the whole module**

Run: `make check && make test-race && make contracts-check`
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/newsroom/http.go internal/app/app.go internal/app/newsroom_http_test.go
git commit -m "Serve newsroom candidate and queue endpoints"
```

---

### Task 8: Local smoke test and docs

**Files:**
- Modify: `README.md` (append a section)

- [ ] **Step 1: Smoke-test against a throwaway database**

From `apps/server-go`, using a temp database only (never the Node or production database). `htpasswd` ships with macOS; Go's bcrypt accepts its `$2y$` hashes.

```bash
export DATABASE_PATH="$(mktemp -d)/newsroom-smoke.db" JWT_SECRET=local-smoke-secret
go run ./cmd/migrate
HASH=$(htpasswd -bnBC 10 "" smoke-password | tr -d ':\n')
sqlite3 "$DATABASE_PATH" "INSERT INTO authors (name, email, password_hash, role) VALUES ('Smoke', 'smoke@example.invalid', '$HASH', 'admin');"
sqlite3 "$DATABASE_PATH" "INSERT INTO candidates (source_url, source_name, feed_url, title, discovered_at, updated_at) VALUES ('https://example.com/smoke', 'Smoke', 'https://example.com/rss', 'Smoke candidate', '2026-09-23T10:00:00Z', '2026-09-23T10:00:00Z');"
go run ./cmd/api &
sleep 3
TOKEN=$(curl -s -X POST -H 'Content-Type: application/json' -d '{"email":"smoke@example.invalid","password":"smoke-password"}' http://127.0.0.1:4401/api/auth/login | sed -E 's/.*"token":"([^"]+)".*/\1/')
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:4401/api/newsroom/candidates; echo
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"ids":[1]}' http://127.0.0.1:4401/api/newsroom/candidates/publish; echo
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:4401/api/newsroom/overview; echo
kill %1
```

Expected: the list shows the smoke candidate as `pending`; publish returns it `queued` with `scheduled_for` 30–40 minutes from now; overview shows `"queued":1`.

- [ ] **Step 2: Append a README section**

Append to `README.md`:

```markdown
## Newsroom (editorial queue)

`internal/newsroom` owns the `candidates` table (migration 3) and the
authenticated `/api/newsroom/*` routes described in
`contracts/newsroom.openapi.yaml`. Candidates are policy-passing feed items
awaiting an editor's decision. Publishing queues them one after another,
each `random(min..max)` minutes after the previous queue entry (defaults 30–40,
stored in `settings`). Rejected candidates are kept so their URLs are never
re-collected.

The collector and publisher are not implemented yet: `POST /api/newsroom/collect`
answers `503` and queued items are not processed. Design:
`docs/superpowers/specs/2026-09-23-ai-tech-news-newsroom-design.md`.
```

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "Document the newsroom slice"
```

---

## Out of scope for this plan (later phases)

- Collector (phase 3), publisher and Gemini/S3/IndexNow ports (phase 4), cutover and Node-database adoption verifier (phase 5).
- iOS client (phase 2) — consumes `contracts/newsroom.openapi.yaml` examples in its tests.
