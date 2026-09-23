# AI & Tech News Newsroom — Design

Date: 2026-09-23
Status: Approved; phase 1 implemented
Repos: `omni-control-app` (iOS), `aiandtechnews` (`apps/server-go`)

## Goal

Change AI & Tech News from "importer auto-publishes" to "importer collects
candidates, a human picks what gets published". The Omni Control iOS app is
the editorial surface: its whole job for this site is to list candidates and
mark which ones get published. Selected candidates enter a queue and go live
one at a time, spaced by a random delay (default 30–40 minutes).

The Go API (`apps/server-go`) is the backend for all of this, including the
pipeline work currently done in Node (collection, Gemini rewrite,
illustration, S3, IndexNow).

## Decisions

| Topic | Decision |
|---|---|
| App role | Editorial gate: list candidates, select to publish, reject |
| Backend | Go API only; pipeline fully ported to Go |
| Candidate content | Raw policy-passing RSS item; rewrite + illustration happen only after selection |
| Unpicked candidates | Stay `pending` until explicitly rejected; never expire |
| Go-live timing | Queue; each item goes live `random(min..max)` minutes after the previous scheduled item (spaced out) |
| Delay window | Stored server-side, default 30–40 min, editable from the app |
| Auth | Existing Go `POST /api/auth/login` JWT (7-day, no refresh); app signs in |
| Other apps on the board | Removed; this is the only module for now |

## 1. Data model and lifecycle

A new `candidates` table, separate from `articles`. `articles` requires
content, slug, category and author, which only exist after the rewrite, and
the public site and `articles.status` CHECK stay untouched.

### `candidates` columns

| Column | Type | Notes |
|---|---|---|
| `id` | INTEGER PK | |
| `source_url` | TEXT NOT NULL UNIQUE | Normalized with `content/policy.go` URL normalization; dedup key |
| `source_name` | TEXT NOT NULL | Feed display name, e.g. "The Verge" |
| `feed_url` | TEXT NOT NULL | Approved feed it came from |
| `title` | TEXT NOT NULL | |
| `feed_summary` | TEXT NOT NULL DEFAULT '' | Plain text, from the RSS item |
| `source_image_url` | TEXT | From RSS enclosure/media tags when present |
| `feed_published_at` | TEXT | RFC 3339 UTC |
| `discovered_at` | TEXT NOT NULL | RFC 3339 UTC |
| `status` | TEXT NOT NULL | CHECK in (`pending`,`queued`,`processing`,`published`,`failed`,`rejected`) |
| `scheduled_for` | TEXT | RFC 3339 UTC; set while `queued` |
| `attempts` | INTEGER NOT NULL DEFAULT 0 | Publisher attempts for the current queue entry |
| `last_error` | TEXT | Human-readable failure reason |
| `article_id` | INTEGER | FK → `articles.id` ON DELETE SET NULL, set when published |
| `published_at` | TEXT | RFC 3339 UTC; set when status becomes published |
| `updated_at` | TEXT NOT NULL | RFC 3339 UTC |

Indexes: `(status, scheduled_for)`, `(status, discovered_at)`, `(article_id)`.

### Lifecycle

```
pending ──select──▶ queued ──due──▶ processing ──ok──▶ published
   │  ▲                │                 │
reject │unqueue ────────┘               fail
   ▼                                     ▼
rejected                              failed ──retry──▶ queued
                                         └──reject──▶ rejected
```

- `pending` never expires.
- `rejected` rows are kept forever so the collector never re-adds the URL.
- A candidate whose `source_url` already exists in `articles.source_url` is
  also skipped by the collector (covers articles published before this change).
- All transitions are guarded updates (`UPDATE … WHERE id=? AND status=?`);
  zero rows affected means the transition is rejected as stale.
- A CHECK guarantees every `queued` row has `scheduled_for`.

### Queue scheduling

- On publish, pending ids are queued in the order given by the request.
- For each: `scheduled_for = max(now, latest scheduled_for among queued/processing) + random(min..max)` minutes, where `random` is a uniform integer-minute draw, inclusive.
- `retry` appends a failed item to the end of the queue with the same rule and resets `attempts` to 0.
- `unqueue` returns an item to `pending` and clears `scheduled_for`. Remaining items keep their times (no re-compaction).
- Settings live in the existing `settings` key/value table:
  `newsroom.publish_delay_min_minutes` = `30`, `newsroom.publish_delay_max_minutes` = `40`.
  Validation: min and max are between 1 and 1440, and min is not above max.
- All timestamps are written through one formatter (RFC 3339 UTC, second
  precision, `Z` suffix); comparisons such as `published_today` rely on text
  ordering.
- **Minimum gap (publisher):** before claiming the next due item, the
  publisher also requires `now - latest published_at ≥ min` minutes. This
  keeps spacing when transient retries (`now + 5 min`) or crash recovery
  reschedule items outside the queue tail, and prevents a burst after
  downtime (overdue items then go out one per `min` minutes).
- The publisher shares the same `newsroom.Service` instance as the HTTP
  handler (its scheduling mutex is per instance).

### Failure handling

- **Transient** (network errors, Gemini 5xx/429, S3 errors, source fetch timeouts):
  status returns to `queued` with `scheduled_for = now + 5 min` and `attempts++`.
  After 3 attempts the item becomes `failed`.
- **Validation** (rewrite fails `content/policy.go` validation, illustration fails
  the vision check, source page unusable): `failed` immediately.
- `last_error` always records the reason shown in the app.
- On startup the publisher resets any `processing` rows (left by a crash) to
  `queued` with `scheduled_for = now`.

## 2. Go server architecture

New code follows the existing capability-package pattern in
`apps/server-go/internal/`, each package owning its migrations.

- **`internal/newsroom`**
  - `migrations/003_newsroom.sql`: table, indexes, default settings rows.
  - `store.go` — `SQLiteStore`: queries and guarded transitions.
  - `service.go` — `Service`: publish (with spacing), unqueue, retry, reject, overview, settings. Clock and random source injected.
  - `collector.go` — `Collector`: fetch approved feeds, apply `content/policy.go`, insert new candidates.
  - `publisher.go` — `Publisher`: claim the earliest due item, fetch source page, rewrite, illustrate, upload, insert the `published` article, notify IndexNow, mark candidate `published`.
  - `http.go` — handlers for Section 3.
- **`internal/gemini`** — rewrite, image generation, vision check. Same prompts and models as the Node implementation (`apps/server/scripts/news-importer.ts`, `apps/server/src/feature-illustration.ts`).
- **`internal/media`** — S3 upload and WebP encoding with a pure-Go encoder (keeps the no-cgo `modernc.org/sqlite` build). Fallback if quality is insufficient: shell out to `cwebp`.
- **`internal/indexnow`** — port of `apps/server/src/indexnow.ts`.

Collaborators are interfaces (`Rewriter`, `Illustrator`, `ImageStore`, `Notifier`) so the publisher is testable with fakes.

### Background loops

`cmd/api` starts two loops bound to the server context:

- Collector: every `COLLECTOR_INTERVAL` (default 30m). Also triggerable via `POST /collect`; a mutex prevents overlapping runs.
- Publisher: every 1 minute; processes at most one due item per tick.

Each loop has an enable flag: `COLLECTOR_ENABLED`, `PUBLISHER_ENABLED` (default `false` outside production, so local runs never publish by accident).

### New configuration (names only)

`GEMINI_API_KEY`, `GEMINI_TEXT_MODEL`, `GEMINI_IMAGE_MODEL`, `AWS_REGION`,
`S3_FEATURE_IMAGE_BUCKET`, `S3_FEATURE_IMAGE_PREFIX`,
`S3_FEATURE_IMAGE_PUBLIC_URL`, `INDEXNOW_KEY`, `COLLECTOR_INTERVAL`,
`COLLECTOR_ENABLED`, `PUBLISHER_ENABLED`.

### Publishing policy

`NEWS_PUBLISHING_POLICY.md` is updated in the same change as the code to
describe the new flow: collection no longer publishes; publishing requires an
editor selection and is spaced by the configured delay. The approved-feed
allowlist, AI-only gate and rewrite validation are unchanged. The previous
"max 1 article per run / ~4 slots per day" rule is replaced by the queue
spacing. (Per `AGENTS.md`, this is the explicit decision required to change
publication limits.)

### Store seams for later phases

- Collector `Insert` must also skip URLs present in `articles.source_url`;
  do it inside the store with `NOT EXISTS`.
- `SetLastCollected(ctx, t)`.
- Publisher store methods:
  - Atomic claim: `UPDATE … SET status='processing' WHERE id = (SELECT id …
    WHERE status='queued' AND scheduled_for <= ? ORDER BY scheduled_for, id
    LIMIT 1) AND status='queued' RETURNING id`.
  - `MarkPublished(id, articleID, now)` setting `published_at`.
  - `MarkFailed(id, reason, now)` clearing `scheduled_for`.
  - `ResetProcessing`.

## 3. API contract

All routes under `/api/newsroom`, all require `Authorization: Bearer <jwt>`
via the existing editorial auth middleware (roles `admin` and `editor`).
Errors: `{"error":"…"}`. Timestamps: RFC 3339 UTC. JSON: snake_case, except
the pagination envelope's `totalPages` (matching existing Go endpoints).

| Method | Path | Request | Response |
|---|---|---|---|
| GET | `/overview` | — | `{pending, queued, processing, failed, published_today, next_publish_at, last_collected_at}` |
| GET | `/candidates` | `status` (comma list, default `pending`), `page` (default 1), `limit` (default 20, max 50) | `{candidates:[Candidate], total, page, totalPages}` |
| POST | `/candidates/publish` | `{ids:[int]}` | `{queued:[Candidate], skipped:[{id, reason}]}` |
| POST | `/candidates/reject` | `{ids:[int]}` | `{rejected:[int], skipped:[{id, reason}]}` |
| POST | `/candidates/{id}/unqueue` | — | `{candidate: Candidate}`; `409` if not `queued` |
| POST | `/candidates/{id}/retry` | — | `{candidate: Candidate}`; `409` if not `failed` |
| POST | `/collect` | — | `202 {started:true}`; `409` if a run is in progress |
| GET | `/settings` | — | `{publish_delay_min_minutes, publish_delay_max_minutes}` |
| PUT | `/settings` | same shape | same shape; `400` on invalid range |

Ordering: `pending` by `discovered_at` desc; `queued` by `scheduled_for` asc;
`failed`/`processing` by `updated_at` desc; `published` by `published_at` desc.

`published_today` counts candidates whose `published_at` falls since local
midnight in `Europe/Istanbul` (matching the existing scheduler's timezone).

### Candidate

```json
{
  "id": 42,
  "title": "…",
  "feed_summary": "…",
  "source_name": "The Verge",
  "source_url": "https://…",
  "source_image_url": "https://…",
  "feed_published_at": "2026-09-23T10:02:00Z",
  "discovered_at": "2026-09-23T10:30:00Z",
  "status": "queued",
  "scheduled_for": "2026-09-23T15:40:00Z",
  "attempts": 0,
  "last_error": null,
  "article_slug": null
}
```

The contract lives in `apps/server-go/contracts/newsroom.openapi.yaml`, with
example responses that the iOS tests reuse. It is separate from
`contracts/openapi.yaml` because that file's tests require it to match the Node
fixture exactly.

Until the collector exists (phase 3), `POST /collect` answers
`503 {"error":"Collector is not enabled"}`.

## 4. iOS app

### Shell

- `control-board.json`: a single server (the Go API) and a single module,
  `{"type":"newsroom","id":"aiandtechnews","name":"AI & Tech News","kind":"News site"}`.
- Delete `Features/Shop`, `Features/Blog`, `Features/Site` and their catalog
  entries. Rename `Features/News` to `Features/Newsroom`; it is the template
  for future modules. Update the README accordingly.
- **`Core/Auth`** (reusable by any JWT-login server):
  - Config auth type `{"type":"session","loginPath":"auth/login"}`.
  - `SessionStore`: Keychain-backed JWT per server id; reads `exp` to treat
    expired tokens as signed out.
  - `SessionInterceptor`: attaches the bearer token; a `401` clears the session.
  - `SessionState` per server (`signedIn` / `signedOut`) observed by the shell.
    A signed-out module's tile shows "Sign in"; tapping presents the sign-in
    sheet (email, password, primary action) instead of the module screen.

### Newsroom feature

```
Features/Newsroom/
  NewsroomModule.swift
  Domain/      Candidate, NewsroomOverview, PublishDelay, NewsroomRepository
  Data/        NewsroomEndpoints (+DTOs), RemoteNewsroomRepository, InMemoryNewsroomRepository
  Presentation/
    NewsroomView.swift            header + tab switch
    Candidates/                   CandidatesView, CandidatesViewModel, CandidateRow
    Queue/                        QueueView, QueueViewModel, QueueRow
    Settings/                     NewsroomSettingsSheet, NewsroomSettingsViewModel
```

- **Home tile**: kind "News site", name "AI & Tech News", stat = `pending`,
  label "to review", badge = `pending`. When `failed > 0` the label reads
  "to review · N failed" in the danger color.
- **Header**: back link, kicker "AI & Tech News · News", title, right-side
  "N pending / M published today", a two-tab switch **Candidates · Queue (n)**
  in blueprint style, and a gear that opens settings.
- **Candidates tab** (`status=pending`): the existing design, plus source
  image thumbnails, infinite scroll paging, pull-to-refresh, and a **Reject**
  ghost action in the bottom bar when the selection is non-empty. Long-press
  menu: "Open source" (in-app Safari), "Reject". After publishing, the toast
  reads "N queued · first goes live ~HH:mm".
- **Queue tab** (`status=processing,queued,failed`): sections Processing,
  Scheduled (go-live time; swipe/long-press "Unqueue"), Failed (`last_error`,
  "Retry", "Reject").
- **Settings sheet**: delay min/max steppers, "Collect now" with last-collected
  time.
- Mock data (`InMemoryNewsroomRepository`) keeps the app usable without the server.

## 5. Testing

- **Go** (`make check`, temp SQLite via `internal/testutil`):
  queue spacing with injected clock/random (empty and non-empty queue, bounds);
  every store transition and stale-transition rejection; collector against
  `httptest` feeds (policy rejects, dedup against candidates and articles);
  publisher with fakes (success, transient retry, 3-attempt failure,
  validation failure, crash recovery, article row written); handler tests
  (401, per-id results, 409s, settings validation); OpenAPI contract check.
- **iOS**: view-model tests for both tabs and settings with a stub repository;
  `RemoteNewsroomRepository` against the OpenAPI example payloads via URLProtocol
  stub; session tests (401 clears token, expired token = signed out, tile
  shows "Sign in").
- **End to end**: Go API locally on `127.0.0.1:4401` against a **copy** of the
  database, Debug app build pointed at it. The production database is never
  used for testing and never wiped or recreated.

## 6. Delivery order

Each phase gets its own implementation plan and ends in a working state.

1. **Go foundation**: migration, store, queue, `/api/newsroom` endpoints, contract. Loops disabled.
2. **iOS**: `Core/Auth` + Newsroom feature, first against the mock, then against the local Go API.
3. **Go collector**: candidates accumulate.
4. **Go publisher**: Gemini, media, IndexNow ports; queued items go live.
5. **Cutover**: run the Go API on the home machine behind the tunnel, disable the Node importer's external scheduler, enable the Go loops.

### Blocker for phase 5

The Go migration runner cannot yet adopt a Node-created database (the
verifier is deferred in `docs/plans/2026-09-20-go-api-migration.md`). That
verifier must exist before the Go API migrates the production database. It
must only add the new table and settings rows; it never drops, wipes or
recreates data.

## Out of scope

- Editing candidate or article text from the app.
- Reordering the queue or setting per-item publish times.
- Push notifications (e.g. "article published" / "item failed").
- Other controlled apps on the board.
- Porting the remaining Node dashboard routes (plan tasks 9–14) beyond what this feature needs.
