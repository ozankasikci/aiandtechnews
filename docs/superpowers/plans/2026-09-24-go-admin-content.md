# Go Admin Content Port Implementation Plan (Plan 5a)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port every authenticated, non-media route of `apps/server/src/routes/dashboard.ts` to the Go API with the same behavior: dashboard article list, get, create, update, and delete; category list, create, update, and delete; settings get and put. The dashboard client (`apps/dashboard/lib/api.ts`) and the recorded Node contracts must not be able to tell the two servers apart. This covers migration tasks 9 and 10 in `docs/plans/2026-09-20-go-api-migration.md`.

**Architecture:** One new infrastructure package and one new capability package, plus additions to `content` and `app`:
- `internal/jsonbody` reproduces the request semantics the Node handlers depend on: `express.json()` parsing, JavaScript truthiness, `??` and `String()` coercion, and better-sqlite3 parameter binding. Without it Go cannot tell a missing field from a `null` one, and cannot reproduce Node's `x || default` rules.
- `internal/content` gains `AdminService` and `AdminHandler`. `AdminService` holds the rules. The `SQLiteStore` runs each mutation through a transaction interface (`adminTx`). `AdminHandler` does the HTTP mapping and queues IndexNow notifications after it writes the response.
- `internal/settings` is a new capability for `GET/PUT /api/dashboard/settings`. It has no migration of its own: newsroom migration 3 already creates the `settings` table with Node's exact schema (`CREATE TABLE IF NOT EXISTS`).
- `internal/app` mounts both handlers in one `/api/dashboard` chi group behind `auth.RequireAuth`, which also makes unknown dashboard paths answer 401, as the Node contract observation requires. It also adds a non-blocking IndexNow queue, gated by `INDEXNOW_ENABLED`, that drains on shutdown.

No schema changes. No new migration versions. `app.Migrations()` is unchanged.

**Tech Stack:** Go 1.25, `net/http`, `github.com/go-chi/chi/v5`, `database/sql` + `modernc.org/sqlite`, and the existing `content` policy, `editorial` auth, `indexnow` client, `contracttest` replay, and `testutil`.

**Working directory:** `apps/server-go` (repo `/Users/ozan/Projects/aiandtechnews`, branch `main`). Every `go` command below runs from there.

**Build and test commands:** always pass `-p 2`, because a heavy parallel compile once crashed this machine: `go build -p 2 ./...`, `go vet -p 2 ./...`, `go test -p 2 ./...`. Run the race detector only on the touched packages: `go test -race -p 2 ./internal/jsonbody/ ./internal/content/ ./internal/settings/ ./internal/app/`.

**Commits:** plain sentences, no `feat:`-style prefixes, no co-author trailers.

**Safety:** tests use temporary SQLite databases (`testutil.OpenDatabase`), in-process `httptest`, and fake IndexNow notifiers. No test contacts IndexNow or opens a real database. Never start the API on ports 3001, 3002, or 4001.

**Verified:** every task's code was built and tested in a scratch copy of this module. Each task's new tests fail against the previous task's tree (compile error or wrong status) and pass once the task's implementation is added. After Task 10: `go vet -p 2 ./...` is clean, `go test -p 2 ./...` passes (760 tests), the race detector passes on the touched packages, and `scripts/sync-contracts.sh --check` passes. The expected Node behaviors in the tests were recorded by running the real Node handlers (`createApp` in `apps/server/src/app.ts`, on an in-memory database and an ephemeral port) against the same seed rows. They are not inferred.

---

## Node reference (line numbers are in `apps/server/src/routes/dashboard.ts` unless noted)

| Behavior | Node source |
|---|---|
| Body parsing: `express.json()` default (100 kb, strict, only `application/json`, otherwise `req.body = {}`) runs **before** routing and auth | `app.ts:29`, Express 4.22 / body-parser 1.20 |
| Auth guard for every route after it, **including unknown `/api/*` paths**: `router.use(requireAuth)` | `:94`; `auth.ts:32-49` |
| `requireAuth`: header must start with `Bearer ` (else 401 `Authentication required`); signature/expiry failure is 401 `Invalid or expired token`; **no role check, no DB lookup** | `auth.ts:32-49` |
| Dashboard list: `page = max(1, parseInt||1)`, `limit = min(50, max(1, parseInt||20))`, optional exact `status` in {draft,published,scheduled} (other values ignored), `category` = `c.slug`, `search` on title/excerpt `LIKE %s%`, `ORDER BY a.updated_at DESC`; count joins only categories | `:98-151` |
| Get by id (any status); 404 `Article not found` | `:153-171` |
| Editorial author = first `authors` row named `TechNews Editorial` | `:173-178`, `news-policy.ts:23-27` |
| Published validation = `validateRewrittenArticle` + source/URL match, deduped | `:180-196`, `news-policy.ts:132-159, 220-273` |
| Create: required check, URL normalize, published policy, category/deals, duplicate slug or source_url, editorial author, insert, 201, IndexNow if published | `:198-314` |
| Update: 404, URL, merged `??` next state, category/deals, policy, duplicate (excluding self), `published_at` rule, defined-only field list, editorial author when next is published, `No fields to update`, IndexNow old/new slug | `:316-462` |
| Delete article (IndexNow if it was published) | `:464-479` |
| Categories list (`c.*` + `article_count`, `ORDER BY c.name`), create, update, delete | `:483-573` |
| Settings get (all rows, `newsletter_enabled` → boolean) and put (8-key allowlist, booleans → `"true"`/`"false"`, one transaction) | `:632-696` |
| `formatSqliteTimestamp` (`YYYY-MM-DD HH:MM:SS`, UTC) and `formatArticleRow` | `:700-739` |
| Fire-and-forget IndexNow after the response is sent | `:36-45`, `:313`, `:458-461`, `:478`; `indexnow.ts:41-74` |
| Schema: `settings(key TEXT PRIMARY KEY, value TEXT NOT NULL)`, `categories.slug UNIQUE`, `articles.status CHECK`, NOT NULL columns | `db.ts:18-70` |

### Response shapes (unchanged Go types are reused)

- Article: the existing `content.Article` JSON, identical to `formatArticleRow`: 17 row fields plus nested `category{id,name,slug,description,color}` and `author{id,name,email,avatar,bio,role}`. Nullable: `featured_image`, `published_at`, `meta_title`, `meta_description`, `source`, `source_url`, `author.avatar`, `author.bio`.
- List: the existing `content.Page` (`{"articles":[...],"total":n,"page":n,"totalPages":n}`), where `totalPages = ceil(total/limit)`, so an empty result has `totalPages` 0 and `page` 1.
- Category list rows: `{"id","name","slug","description","color","article_count"}`. Category create and update return `{"category":{id,name,slug,description,color}}`.
- Deletes return `{"success":true}`. Settings return `{"settings":{...}}` in SQLite row order.
- Errors: `{"error":"..."}`. The policy failure adds `"details":[...]`.

## Known Node behaviors kept (parity, deliberately not fixed)

Each item is contract-visible or data-visible. Each is kept as-is and pinned by a test. Changing any of them needs a separately reviewed contract change, ideally after cutover.

1. **changed: blank settings are stored as empty strings.** `apps/dashboard/app/(dashboard)/settings/page.tsx:67-75` sends `null` for blank `social_*` and `newsletter_webhook_url`. Node upserts `null` into `settings.value NOT NULL`, the transaction rolls back, and the result is a 500 (`:666-678`), so today the dashboard cannot save settings while any of those fields is blank. This is a reviewed contract change, not kept parity: Go stores an empty string for a null value instead of failing (`internal/settings/settings.go`, `Service.Update`). Pinned by `internal/settings/settings_test.go` (`TestSettingsMatchNode/null_value_is_stored_as_an_empty_string`).
2. **Duplicate category slugs, `null` category fields, `null` article `title`/`slug`/`excerpt`/`content`/`category_id`, a `status` outside the CHECK list, and booleans or objects in any bound field all return 500, not 4xx.** They are SQLite constraint or bind errors thrown from `better-sqlite3` (`:500-504`, `:538-541`, `:276-299`, `:441-444`).
3. **The author is always "TechNews Editorial".** Create ignores the signed-in user (`:266-290`). Any update whose next status is `published`, even a title-only edit, sets `author_id` to the editorial author again (`:423-431`). Unpublishing keeps the existing author.
4. **No roles.** Any valid JWT, whether admin, editor, or an author id that no longer exists, can use every dashboard route (`auth.ts:32-49`).
5. **`published_at` defaults use JavaScript ISO with milliseconds** (`2026-09-20T12:00:00.000Z`, `:273`, `:402`), while `created_at` and `updated_at` use the SQLite format (`2026-09-20 12:00:00`). Note that `internal/publisher` writes `published_at` in SQLite format, so after cutover the column will hold both formats, as it already does in Node.
6. **Re-sending `status: "published"` without a truthy `published_at` re-stamps `published_at` to now**, even on an article that is already published (`:400-405`). The dashboard edit page always sends `published_at`, so the UI does not hit this.
7. **Any update of a published article re-runs the full publishing policy on the merged state.** Legacy published articles without `source`/`source_url` (400 `Published articles require source and source_url`) or with copy that fails the current policy (400 with details) cannot be edited at all, not even their title, unless they are unpublished in the same request (`:373-389`).
8. **Numbers bind as REAL:** `{"title":5}` stores `"5.0"`, and a numeric setting stores `"5.0"`. A one-element array is spread into its element (`{"newsletter_provider":["resend"]}` stores `"resend"`).
9. **changed: `newsroom.*` keys are hidden from the dashboard settings surface.** Node's `GET /api/dashboard/settings` returns every row of the table, so on a Go-migrated database it would also return newsroom's own keys (`newsroom.publish_delay_min_minutes`, `newsroom.publish_delay_max_minutes`, `newsroom.last_collected_at`) as strings. This is a reviewed contract change, not kept parity: Go's `Service.Get` filters out any key with the `newsroom.` prefix, and `PUT` can never write them because they are not in the allowlist (`internal/settings/settings.go`). Pinned by `internal/settings/settings_test.go` (`TestSettingsHideNewsroomKeys`) and `internal/app/dashboard_http_test.go` (`TestDashboardSettingsHideNewsroomKeysFromTheSharedTable`). The dashboard reads only the eight known keys (`settings/page.tsx:45-52`).
10. **List quirks:** search covers only title and excerpt (the public list also searches content). `%` and `_` in search are not escaped. Invalid `status` filters are silently ignored. Ordering is by the `updated_at` **text**, with no tie-breaker, so mixed formats sort lexicographically (`T` sorts after space).
11. **Field write rules differ between create and update.** Create stores `featured_image`, `meta_title`, `meta_description`, and `source` as `value || null`, and `excerpt`/`content` as `value || ""`. Update writes every defined field as sent (`""` stays `""`), except `source`, which is written as `source || null` (`:410-421`).
12. **Category delete checks usage before existence.** A missing id that has no articles returns 404, and a category in use returns 400 even when the request is otherwise valid (`:550-573`).
13. **Path ids are bound as strings**, so SQLite numeric affinity applies: `/articles/%20303` matches id 303 and `/articles/303abc` returns 404.
14. **Non-published articles accept any `source_url` that WHATWG URL parsing accepts.** Only published ones must come from an approved publication.
15. **IndexNow side effects:** publishing create notifies `[slug]`. Update notifies the old slug if the article was published before and the next slug if it is published after, deduplicated, so a rename notifies both and an unpublish notifies the old URL. Deleting a published article notifies its slug. Nothing is sent on 4xx or 500. Notifications are sent after the response and never affect it.

## Go transport differences (inherited conventions, documented rather than contract-visible)

These follow decisions the Go slices already made (see README "HTTP compatibility" and the auth slice). The recorded fixture does not exercise any of them.

| Situation | Node/Express | Go |
|---|---|---|
| Unexpected 500 (constraint, bind error) | Express HTML error page (stack trace outside production) | `{"error":"Internal server error"}`, cause logged. The dashboard shows "Network error" for Node and "Internal server error" for Go |
| Malformed JSON, non-object JSON scalar, body over 100 kb, non-UTF-8 charset | HTML 400 / 413 / 415 | `400 {"error":"Invalid request body"}` (same as auth login) |
| Unauthenticated request with a malformed body | 400 (body parsed before auth) | 401 (auth first) |
| Authenticated unknown `/api/dashboard/*` path | 404 HTML `Cannot GET ...` | 404 `{"error":"Not found"}` |
| Known path, unsupported method (e.g. `PATCH /api/dashboard/articles/1`) | 404 HTML | 405 JSON with `Allow` (router-wide existing behavior) |
| Unknown **non-dashboard** `/api/*` path without a token | 401 (the dashboard router's `router.use(requireAuth)` catches it) | 404 `{"error":"Not found"}`, pre-existing and out of scope (see Open questions) |
| Repeated query parameters (`?search=a&search=b`) | `qs` array coerced to string `"a,b"` | first value (same as the public-read slice) |
| `<`, `>`, `&` in JSON strings | literal | `<`-style escapes from `encoding/json` (semantically identical, existing convention) |
| IndexNow | always on | only when `INDEXNOW_ENABLED=1`, like the publisher, so a local dashboard edit never pings IndexNow for a dev-only article |
| Mutation atomicity | synchronous better-sqlite3 handlers, nothing interleaves | each mutation runs in one SQLite transaction on the single pooled connection. Same observable results; concurrent writers serialize |

## Out of scope

- **Media (plan 5b):** `GET /api/dashboard/media`, `POST /api/dashboard/media/upload`, `DELETE /api/dashboard/media/:id`, and static `/uploads/*` serving (`app.ts:30`). Plan 5b must mount its handler **inside the same `router.Route("/dashboard", ...)` closure** added in Task 10 (chi allows the pattern only once) and extend `dashboardContentOperations` / the replay to include `dashboard.media.*`.
- **Newsletter** (migration task 12), including `newsletter.*` operations. Nothing in Node reads the settings table outside these dashboard endpoints: the `newsletter_*` settings keys are stored but have no runtime consumer.
- **Adoption/cutover** (migration task 14): the unmanaged-Node-database verifier/adoption command, production data, and switching the dashboard proxy to Go.
- **Unknown non-dashboard `/api/*` → 401** parity (see the table above).
- **Seeding default site settings** (`db.ts:187-198` seeds `site_name` and the other seven keys only when a fresh database is initialized with `seedDefaults`). No Go migration or devseed writes them (see Open questions).

## Findings that affect later plans

- **Media URLs in content:** `articles.featured_image` and `authors.avatar` hold `/uploads/<file>` paths from Node's local media library (see the contract seed). New publisher articles hold `https://` S3 URLs. The dashboard lets editors paste any string into `featured_image`. Plan 5b must keep `/uploads/*` URLs stable and serving, or migrate those rows. Node's media delete does not check whether an article references the file (`:609-628`), so dangling images are possible today.
- **Mixed `published_at` formats** (item 5 above) must be accepted by every future reader, including newsletter digest selection and cutover verification.
- **Settings table ownership:** it is created by newsroom migration 3 and read and written by both `newsroom` (`newsroom.*` keys) and `settings` (site keys). The cutover schema verifier must treat Node's `settings` DDL as compatible (it is identical).
- **Dashboard routes all live under one authenticated chi group** (`/api/dashboard`). Anything else Node serves under `/api/dashboard/*` must be added inside it.

## Open questions for the user

1. **Settings `null` → 500 (Known behavior 1):** keep parity (this plan) or, as a reviewed contract change, store `""` for `null`? As things stand, the dashboard cannot save settings while any social or webhook field is blank.
2. **`newsroom.*` keys in `GET /api/dashboard/settings`:** keep parity (this plan) or filter them out? Filtering is safe for the current dashboard but is a contract change.
3. **Default site settings for fresh Go databases:** add `INSERT OR IGNORE` of Node's eight defaults (`site_name='TechNews'`, `site_description='AI & Tech News, Daily.'`, empty socials, `newsletter_enabled='false'`, `newsletter_provider='none'`, `newsletter_webhook_url=''`) as migration 5, add them to `cmd/devseed` only, or leave them out? This plan does neither.
4. **Unknown `/api/*` without a token:** should Go answer 401 like Node for every unknown `/api` path, or keep today's 404 outside `/api/dashboard`?
5. **500s for duplicate category slugs and bad input:** acceptable to keep until after cutover, then change to 409/400 in a reviewed contract change?

---

## File structure

| File | Responsibility |
|---|---|
| Create `internal/jsonbody/value.go` | `Value`: undefined/null/defined, truthiness, `??`, `String()`, better-sqlite3 `Bind` |
| Create `internal/jsonbody/decode.go` | `Decode`: `express.json()` defaults, 100 KiB limit, `ErrInvalid` |
| Create `internal/jsonbody/jsonbody_test.go` | Operator, binding, and decoding vectors |
| Create `internal/content/admin_model.go` | `AdminError`, `DashboardQuery`, `CategoryWithCount`, `ArticleChange`, internal row types |
| Create `internal/content/admin_sqlite.go` | Dashboard list/categories reads; `inAdminTx` + `sqliteAdminTx` SQL seams |
| Create `internal/content/admin_service.go` | `AdminService` construction, store interfaces, read use cases |
| Create `internal/content/admin_articles.go` | Article create/update/delete rules, binding helpers, time formats |
| Create `internal/content/admin_categories.go` | Category create/update/delete rules |
| Create `internal/content/http_admin.go` | `AdminHandler`, routes, error mapping, `IndexNowNotifier` |
| Create `internal/content/admin_*_test.go`, `admin_tx_internal_test.go` | Node-recorded HTTP vectors and SQL seam tests |
| Create `internal/settings/settings.go`, `http.go`, `settings_test.go` | Settings store/service/handler and Node vectors |
| Create `internal/app/indexnow_queue.go` (+ test) | Non-blocking, drained IndexNow queue gated by `INDEXNOW_ENABLED` |
| Modify `internal/app/app.go` | Authenticated `/api/dashboard` group, drains in `Run` |
| Create `internal/app/dashboard_contract_test.go`, `dashboard_http_test.go` | Contract replay for all 11 operations, unknown-route observation, auth, shared settings table |
| Modify `README.md`, `../../docs/plans/2026-09-20-go-api-migration.md` | Capability and status documentation |

---

### Task 1: `jsonbody`: Express request semantics

Node reads loosely typed `req.body` properties and applies `!x`, `x || d`, `x ?? d`, `x !== undefined`, `typeof x === "string"`, `String(x)`, and better-sqlite3 binding. Go needs the same operators on raw JSON so it can keep Node's validation order and error behavior exactly.

**Files:** Create `internal/jsonbody/value.go`, `internal/jsonbody/decode.go`, `internal/jsonbody/jsonbody_test.go`

- [ ] **Step 1: Write the failing test** at `internal/jsonbody/jsonbody_test.go`:

```go
package jsonbody_test

import (
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/jsonbody"
)

func raw(s string) jsonbody.Value { return jsonbody.FromRaw([]byte(s)) }

func TestValueJavaScriptOperators(t *testing.T) {
	undefined := jsonbody.Value{}
	tests := []struct {
		name          string
		value         jsonbody.Value
		defined, null bool
		truthy        bool
		jsString      string
		stringOrEmpty string
	}{
		{"undefined", undefined, false, false, false, "undefined", ""},
		{"null", raw("null"), true, true, false, "null", ""},
		{"empty string", raw(`""`), true, false, false, "", ""},
		{"string", raw(`"a b"`), true, false, true, "a b", "a b"},
		{"zero", raw("0"), true, false, false, "0", ""},
		{"negative zero", raw("-0"), true, false, false, "0", ""},
		{"integer", raw("101"), true, false, true, "101", ""},
		{"fraction", raw("1.5"), true, false, true, "1.5", ""},
		{"large", raw("1e21"), true, false, true, "1e+21", ""},
		{"small", raw("1.5e-7"), true, false, true, "1.5e-7", ""},
		{"true", raw("true"), true, false, true, "true", ""},
		{"false", raw("false"), true, false, false, "false", ""},
		{"object", raw(`{"a":1}`), true, false, true, "[object Object]", ""},
		{"empty array", raw(`[]`), true, false, true, "", ""},
		{"array", raw(`["https://a.example/x", null, 2]`), true, false, true, "https://a.example/x,,2", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Defined(); got != tt.defined {
				t.Errorf("Defined = %v", got)
			}
			if got := tt.value.IsNull(); got != tt.null {
				t.Errorf("IsNull = %v", got)
			}
			if got := tt.value.Nullish(); got != (!tt.defined || tt.null) {
				t.Errorf("Nullish = %v", got)
			}
			if got := tt.value.Truthy(); got != tt.truthy {
				t.Errorf("Truthy = %v", got)
			}
			if got := tt.value.JSString(); got != tt.jsString {
				t.Errorf("JSString = %q", got)
			}
			if got := tt.value.StringOrEmpty(); got != tt.stringOrEmpty {
				t.Errorf("StringOrEmpty = %q", got)
			}
		})
	}
	if got := undefined.Or(jsonbody.String("x")); !got.Is("x") {
		t.Error("undefined ?? x != x")
	}
	if got := raw("null").Or(jsonbody.String("x")); !got.Is("x") {
		t.Error("null ?? x != x")
	}
	if got := raw(`""`).Or(jsonbody.String("x")); !got.Is("") {
		t.Error(`"" ?? x != ""`)
	}
	if b, ok := raw("true").Bool(); !ok || !b {
		t.Error("Bool(true)")
	}
	if _, ok := raw(`"true"`).Bool(); ok {
		t.Error(`Bool("true") reported a boolean`)
	}
}

func TestValueBindMatchesBetterSQLite3(t *testing.T) {
	for name, tt := range map[string]struct {
		value jsonbody.Value
		want  any
	}{
		"null":             {raw("null"), nil},
		"string":           {raw(`"x"`), "x"},
		"integer is REAL":  {raw("5"), float64(5)},
		"fraction":         {raw("1.5"), 1.5},
		"one element":      {raw(`["x"]`), "x"},
		"one null element": {raw(`[null]`), nil},
	} {
		got, err := tt.value.Bind()
		if err != nil || got != tt.want {
			t.Errorf("%s: Bind = %#v, %v; want %#v", name, got, err, tt.want)
		}
	}
	for name, value := range map[string]jsonbody.Value{
		"undefined": {}, "true": raw("true"), "false": raw("false"), "object": raw(`{}`),
		"empty array": raw(`[]`), "two elements": raw(`[1,2]`), "nested array": raw(`[[1]]`), "boolean element": raw(`[true]`),
	} {
		if _, err := value.Bind(); !errors.Is(err, jsonbody.ErrUnbindable) {
			t.Errorf("%s: Bind error = %v, want ErrUnbindable", name, err)
		}
	}
	if got, err := raw("1e400").Bind(); err != nil || !math.IsInf(got.(float64), 1) {
		t.Errorf("overflow Bind = %v, %v", got, err)
	}
}

func decode(t *testing.T, contentType, body string) (jsonbody.Object, error) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	return jsonbody.Decode(httptest.NewRecorder(), request)
}

func TestDecodeMatchesExpressJSON(t *testing.T) {
	object, err := decode(t, "application/json; charset=utf-8", ` {"title":"T","n":null,"k":5} `)
	if err != nil || !object.Get("title").Is("T") || !object.Get("n").IsNull() || object.Get("missing").Defined() {
		t.Fatalf("object = %#v, %v", object, err)
	}
	for name, tt := range map[string]struct{ contentType, body string }{
		"no content type":   {"", `{"title":"T"}`},
		"text content type": {"text/plain", `{"title":"T"}`},
		"vendor json":       {"application/vnd.api+json", `{"title":"T"}`},
		"empty body":        {"application/json", ""},
		"whitespace body":   {"application/json", " \n"},
		"array body":        {"application/json", `["title"]`},
	} {
		object, err := decode(t, tt.contentType, tt.body)
		if err != nil || len(object) != 0 {
			t.Errorf("%s: object = %#v, %v; want {}", name, object, err)
		}
	}
	for name, body := range map[string]string{
		"malformed": `{`, "scalar string": `"x"`, "number": `5`, "null": `null`, "trailing value": `{"a":1}{}`,
		"oversized": `{"a":"` + strings.Repeat("x", jsonbody.MaxBytes) + `"}`,
	} {
		if _, err := decode(t, "application/json", body); !errors.Is(err, jsonbody.ErrInvalid) {
			t.Errorf("%s: error = %v, want ErrInvalid", name, err)
		}
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test -p 2 ./internal/jsonbody/`
Expected: build failure. The package has no non-test Go files, and `jsonbody.FromRaw`, `jsonbody.Decode`, and the other identifiers are undefined.

- [ ] **Step 3: Implement `internal/jsonbody/value.go`**

```go
// Package jsonbody reproduces the request-body semantics the Node dashboard
// relies on: express.json() parsing, JavaScript truthiness, nullish and
// String() coercion, and better-sqlite3 parameter binding. It lets ported
// handlers keep Node's exact validation order and error behavior without
// guessing at Go struct types for loosely typed JSON input.
package jsonbody

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
)

// ErrUnbindable reports a value better-sqlite3 refuses to bind (booleans,
// objects, and arrays that do not spread into exactly one scalar). Node turns
// that exception into an HTTP 500.
var ErrUnbindable = errors.New("value cannot be bound as a SQLite parameter")

// Value is one property of a parsed JSON body. The zero Value is JavaScript
// undefined (the property was absent).
type Value struct{ raw json.RawMessage }

// FromRaw wraps one JSON value.
func FromRaw(raw json.RawMessage) Value {
	return Value{raw: bytes.TrimSpace(raw)}
}

// String returns the JSON string value s.
func String(s string) Value {
	raw, _ := json.Marshal(s)
	return Value{raw: raw}
}

// Null returns the JSON null value.
func Null() Value { return Value{raw: json.RawMessage("null")} }

// Defined reports whether the property was present (value !== undefined).
func (v Value) Defined() bool { return len(v.raw) > 0 }

// IsNull reports value === null.
func (v Value) IsNull() bool { return string(v.raw) == "null" }

// Nullish reports value === null || value === undefined.
func (v Value) Nullish() bool { return !v.Defined() || v.IsNull() }

// Or implements the JavaScript ?? operator.
func (v Value) Or(fallback Value) Value {
	if v.Nullish() {
		return fallback
	}
	return v
}

func (v Value) kind() byte {
	if !v.Defined() {
		return 0
	}
	return v.raw[0]
}

// Str returns the value when typeof value === "string".
func (v Value) Str() (string, bool) {
	if v.kind() != '"' {
		return "", false
	}
	var s string
	if json.Unmarshal(v.raw, &s) != nil {
		return "", false
	}
	return s, true
}

// StringOrEmpty implements typeof value === "string" ? value : "".
func (v Value) StringOrEmpty() string {
	s, _ := v.Str()
	return s
}

// Is reports value === s for a string s.
func (v Value) Is(s string) bool {
	got, ok := v.Str()
	return ok && got == s
}

// Bool returns the value when typeof value === "boolean".
func (v Value) Bool() (bool, bool) {
	switch v.kind() {
	case 't':
		return true, true
	case 'f':
		return false, true
	}
	return false, false
}

func (v Value) number() (float64, bool) {
	switch k := v.kind(); {
	case k == '-' || (k >= '0' && k <= '9'):
		f, err := strconv.ParseFloat(string(v.raw), 64)
		var numErr *strconv.NumError
		if err != nil && !(errors.As(err, &numErr) && numErr.Err == strconv.ErrRange) {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

// Truthy implements JavaScript truthiness for JSON values.
func (v Value) Truthy() bool {
	switch v.kind() {
	case 0, 'n', 'f':
		return false
	case 't', '{', '[':
		return true
	case '"':
		s, _ := v.Str()
		return s != ""
	default:
		f, ok := v.number()
		return ok && f != 0
	}
}

// JSString implements JavaScript String(value).
func (v Value) JSString() string {
	switch v.kind() {
	case 0:
		return "undefined"
	case 'n':
		return "null"
	case 't':
		return "true"
	case 'f':
		return "false"
	case '"':
		s, _ := v.Str()
		return s
	case '{':
		return "[object Object]"
	case '[':
		var items []json.RawMessage
		if json.Unmarshal(v.raw, &items) != nil {
			return ""
		}
		parts := make([]string, len(items))
		for i, item := range items {
			// Array.prototype.join renders null and undefined elements as "".
			if element := FromRaw(item); !element.Nullish() {
				parts[i] = element.JSString()
			}
		}
		return strings.Join(parts, ",")
	default:
		f, _ := v.number()
		return jsNumber(f)
	}
}

// Bind converts the value the way better-sqlite3 binds a JavaScript argument:
// strings bind as TEXT, every number binds as REAL (so SQLite stores 5 in a
// TEXT column as "5.0"), null binds NULL, and a one-element array is spread
// into its element. Booleans, objects, undefined and other arrays fail.
func (v Value) Bind() (any, error) {
	switch v.kind() {
	case 'n':
		return nil, nil
	case '"':
		s, _ := v.Str()
		return s, nil
	case '[':
		var items []json.RawMessage
		if json.Unmarshal(v.raw, &items) != nil || len(items) != 1 {
			return nil, ErrUnbindable
		}
		element := FromRaw(items[0])
		if element.kind() == '[' {
			return nil, ErrUnbindable
		}
		return element.Bind()
	case 0, 't', 'f', '{':
		return nil, ErrUnbindable
	default:
		f, ok := v.number()
		if !ok {
			return nil, ErrUnbindable
		}
		return f, nil
	}
}

// jsNumber implements Number.prototype.toString() for finite and infinite values.
func jsNumber(f float64) string {
	switch {
	case f == 0:
		return "0"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	if abs := math.Abs(f); abs >= 1e-6 && abs < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	mantissa, exponent, _ := strings.Cut(strconv.FormatFloat(f, 'e', -1, 64), "e")
	return mantissa + "e" + exponent[:1] + strings.TrimLeft(exponent[1:], "0")
}
```

- [ ] **Step 4: Implement `internal/jsonbody/decode.go`**

```go
package jsonbody

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

// MaxBytes is express.json()'s default "100kb" body limit.
const MaxBytes = 100 * 1024

// ErrInvalid reports a body express.json() would reject. Express answers with
// an HTML 400/413 page; the Go API answers {"error":"Invalid request body"}
// like the ported auth routes.
var ErrInvalid = errors.New("invalid request body")

// Object is a parsed JSON object body. Missing properties are undefined.
type Object map[string]Value

// Get returns the named property, or undefined.
func (o Object) Get(name string) Value { return o[name] }

// Decode reads r's body with express.json() default semantics: a request
// whose Content-Type is not application/json, or whose body is empty, parses
// as {}; strict mode rejects anything that does not start with { or [; a
// JSON array parses but has no named properties.
func Decode(w http.ResponseWriter, r *http.Request) (Object, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || r.Body == nil {
		return Object{}, nil
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBytes))
	if err != nil {
		return nil, ErrInvalid
	}
	trimmed := bytes.TrimLeft(data, " \t\n\r")
	if len(trimmed) == 0 {
		return Object{}, nil
	}
	if trimmed[0] != '{' && trimmed[0] != '[' {
		return nil, ErrInvalid
	}
	if !json.Valid(data) {
		return nil, ErrInvalid
	}
	if trimmed[0] == '[' {
		return Object{}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, ErrInvalid
	}
	object := make(Object, len(fields))
	for name, raw := range fields {
		object[name] = FromRaw(raw)
	}
	return object, nil
}
```

- [ ] **Step 5: Run it and confirm it passes**

Run: `gofmt -l internal/jsonbody; go vet -p 2 ./internal/jsonbody/ && go test -p 2 ./internal/jsonbody/`
Expected: no gofmt output, vet clean, `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/jsonbody
git commit -m "Add Express-compatible JSON body and value semantics for dashboard ports"
```

---

### Task 2: Dashboard reads (article list/get, category list)

**Node:** `:98-171`, `:483-491`. **Files:** Create `internal/content/admin_model.go`, `admin_sqlite.go`, `admin_service.go`, `http_admin.go`, `admin_helpers_test.go`, `admin_read_test.go`

The test helpers mount `AdminHandler` on a bare chi router (auth is wired in Task 10) over the public-read seed (`seed` in `sqlite_test.go`: categories 101-103, authors 201-202, articles 301-303). They add a `deals` category (104) and a published article (305) whose copy passes the publishing policy. A `recordingIndexNow` fake records how much of the response body had been written when `Notify` ran, which proves notifications come after the response.

- [ ] **Step 1: Write the shared test helpers** at `internal/content/admin_helpers_test.go`:

```go
package content_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

// adminNow is the fixed clock of every dashboard test (the Node fixture clock).
var adminNow = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

// A rewritten article that passes the publishing policy: 5 paragraphs, 230 words.
const (
	validTitle    = "Researchers release a new language model"
	validExcerpt  = "Researchers released new benchmark results for a language model."
	validSentence = "The research team shared new benchmark results for the language model and explained how the training data was collected and filtered before release."
)

var validContent = strings.Repeat("<p>"+validSentence+" "+validSentence+"</p>", 5)

// recordingIndexNow records Notify calls and how much of the response body
// had been written when each call happened.
type recordingIndexNow struct {
	calls         [][]string
	bodyAtNotify  []int
	currentWriter *httptest.ResponseRecorder
}

func (r *recordingIndexNow) Notify(slugs []string) {
	r.calls = append(r.calls, append([]string(nil), slugs...))
	r.bodyAtNotify = append(r.bodyAtNotify, r.currentWriter.Body.Len())
}

// seedAdmin extends the public-read seed (301-303) with a deals category and
// a published article (305) that passes the publishing policy.
func seedAdmin(t *testing.T, db *sql.DB) {
	t.Helper()
	seed(t, db)
	if _, err := db.Exec(`INSERT INTO categories(id,name,slug,description,color) VALUES (104,'Deals','deals','Promotions','#999999')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO articles(id,title,slug,excerpt,content,featured_image,category_id,author_id,status,published_at,meta_title,meta_description,source,source_url,view_count,created_at,updated_at)
		VALUES (305,?,'approved-published',?,?,NULL,101,202,'published','2026-09-01T00:00:00.000Z',NULL,NULL,'TechCrunch','https://techcrunch.com/2026/09/01/model',5,'2026-09-01 00:00:00','2026-09-01 00:00:00')`,
		validTitle, validExcerpt, validContent); err != nil {
		t.Fatal(err)
	}
}

func adminServer(t *testing.T) (http.Handler, *sql.DB, *recordingIndexNow) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	seedAdmin(t, db)
	notifier := &recordingIndexNow{}
	service := content.NewAdminService(content.NewSQLiteStore(db), func() time.Time { return adminNow })
	router := chi.NewRouter()
	content.NewAdminHandler(service, notifier, slog.New(slog.NewTextHandler(io.Discard, nil))).Mount(router)
	return router, db, notifier
}

func adminRequest(t *testing.T, handler http.Handler, notifier *recordingIndexNow, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	notifier.currentWriter = response
	handler.ServeHTTP(response, request)
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("%s %s Content-Type = %q", method, target, got)
	}
	return response
}

func expectBody(t *testing.T, response *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	if response.Code != status || response.Body.String() != body {
		t.Fatalf("response = %d %s, want %d %s", response.Code, response.Body.String(), status, body)
	}
}

// articleFields decodes {"article": {...}} and returns the article object.
func articleFields(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope struct {
		Article map[string]any `json:"article"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Article == nil {
		t.Fatalf("decode article from %d %s: %v", response.Code, response.Body.String(), err)
	}
	return envelope.Article
}

func expectFields(t *testing.T, got map[string]any, want map[string]any) {
	t.Helper()
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %#v, want %#v", key, got[key], value)
		}
	}
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

const internalError = `{"error":"Internal server error"}`
```

- [ ] **Step 2: Write the failing read tests** at `internal/content/admin_read_test.go`. The expected ids, totals, and pages were recorded from Node on the same rows:

```go
package content_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

func TestDashboardArticleListFiltersPaginatesAndOrdersByUpdatedAt(t *testing.T) {
	handler, _, notifier := adminServer(t)
	type page struct {
		Articles []struct {
			ID int64 `json:"id"`
		} `json:"articles"`
		Total      int64   `json:"total"`
		Page       float64 `json:"page"`
		TotalPages int64   `json:"totalPages"`
	}
	// Expected values were recorded from the Node server on the same seed.
	for _, tt := range []struct {
		target       string
		ids          []int64
		total, pages int64
		pageNumber   float64
	}{
		{"/articles", []int64{301, 302, 303, 305}, 4, 1, 1},
		{"/articles?status=draft", []int64{303}, 1, 1, 1},
		{"/articles?status=zzz&limit=2&page=2", []int64{303, 305}, 4, 2, 2},
		{"/articles?search=excerpt", []int64{301, 302, 303}, 3, 1, 1},
		{"/articles?search=synthetic%20contract", nil, 0, 0, 1},
		{"/articles?category=synthetic-code&limit=0&page=-3", []int64{302}, 1, 1, 1},
		{"/articles?limit=999", []int64{301, 302, 303, 305}, 4, 1, 1},
	} {
		response := adminRequest(t, handler, notifier, http.MethodGet, tt.target, "")
		var got page
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &got) != nil {
			t.Fatalf("GET %s = %d %s", tt.target, response.Code, response.Body.String())
		}
		ids := make([]int64, 0)
		for _, article := range got.Articles {
			ids = append(ids, article.ID)
		}
		if tt.ids == nil {
			tt.ids = []int64{}
		}
		if !reflect.DeepEqual(ids, tt.ids) || got.Total != tt.total || got.TotalPages != tt.pages || got.Page != tt.pageNumber {
			t.Errorf("GET %s = ids %v total %d page %v pages %d", tt.target, ids, got.Total, got.Page, got.TotalPages)
		}
	}
	empty := adminRequest(t, handler, notifier, http.MethodGet, "/articles?search=nothing-matches", "")
	expectBody(t, empty, http.StatusOK, `{"articles":[],"total":0,"page":1,"totalPages":0}`)
}

func TestDashboardArticleGetReturnsAnyStatusOr404(t *testing.T) {
	handler, _, notifier := adminServer(t)
	response := adminRequest(t, handler, notifier, http.MethodGet, "/articles/303", "")
	expectFields(t, articleFields(t, response), map[string]any{"id": float64(303), "status": "draft", "published_at": nil})
	for _, target := range []string{"/articles/999", "/articles/abc", "/articles/303abc"} {
		expectBody(t, adminRequest(t, handler, notifier, http.MethodGet, target, ""), http.StatusNotFound, `{"error":"Article not found"}`)
	}
}

func TestDashboardCategoryListCountsArticlesInNameOrder(t *testing.T) {
	handler, _, notifier := adminServer(t)
	expectBody(t, adminRequest(t, handler, notifier, http.MethodGet, "/categories", ""), http.StatusOK,
		`{"categories":[`+
			`{"id":104,"name":"Deals","slug":"deals","description":"Promotions","color":"#999999","article_count":0},`+
			`{"id":101,"name":"Synthetic AI","slug":"synthetic-ai","description":"Synthetic artificial intelligence fixtures","color":"#111111","article_count":2},`+
			`{"id":102,"name":"Synthetic Code","slug":"synthetic-code","description":"Synthetic programming fixtures","color":"#222222","article_count":1},`+
			`{"id":103,"name":"Synthetic Startups","slug":"synthetic-startups","description":"Synthetic startup fixtures","color":"#333333","article_count":1}]}`)
}

func TestDashboardReadsHideDatabaseErrors(t *testing.T) {
	handler, db, notifier := adminServer(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"/articles", "/articles/301", "/categories"} {
		expectBody(t, adminRequest(t, handler, notifier, http.MethodGet, target, ""), http.StatusInternalServerError, internalError)
	}
}
```

- [ ] **Step 3: Run them and confirm they fail**

Run: `go test -p 2 ./internal/content/`
Expected: build failure: `undefined: content.NewAdminService`, `undefined: content.NewAdminHandler`.

- [ ] **Step 4: Create `internal/content/admin_model.go`**

```go
package content

import "net/http"

// AdminError is a dashboard request failure carrying Node's exact status and
// JSON error body ({"error": Message} plus "details" when present).
type AdminError struct {
	Status  int
	Message string
	Details []string
}

func (e *AdminError) Error() string { return e.Message }

func badRequest(message string) *AdminError {
	return &AdminError{Status: http.StatusBadRequest, Message: message}
}

func articleNotFound() *AdminError {
	return &AdminError{Status: http.StatusNotFound, Message: "Article not found"}
}

func categoryNotFound() *AdminError {
	return &AdminError{Status: http.StatusNotFound, Message: "Category not found"}
}

// DashboardQuery is GET /api/dashboard/articles after Node's query parsing.
type DashboardQuery struct {
	Page     float64
	Limit    int
	Status   string
	Category string
	Search   string
}

// CategoryWithCount is one GET /api/dashboard/categories row (c.* plus article_count).
type CategoryWithCount struct {
	Category
	ArticleCount int64 `json:"article_count"`
}

// ArticleChange is the result of a dashboard article mutation: the response
// article and the public slugs Node hands to IndexNow after responding.
type ArticleChange struct {
	Article       Article
	IndexNowSlugs []string
}

// storedArticle is the subset of `SELECT * FROM articles` the Node update
// handler reads from the existing row.
type storedArticle struct {
	Title      string
	Slug       string
	Excerpt    string
	Content    string
	CategoryID int64
	Status     string
	Source     *string
	SourceURL  *string
}

// assignment is one `column = ?` of a partial UPDATE. Columns always come
// from fixed allowlists in this package, never from request input.
type assignment struct {
	column string
	value  any
}
```

- [ ] **Step 5: Create `internal/content/admin_sqlite.go`** (reads only; Task 3 adds the transaction seams)

```go
package content

import (
	"context"
	"fmt"
)

// DashboardList mirrors GET /api/dashboard/articles: every status, an optional
// exact status filter, category slug filter and title/excerpt search, ordered
// by updated_at text descending. Like Node, the count query joins only
// categories while the page query also joins authors.
func (s *SQLiteStore) DashboardList(ctx context.Context, query DashboardQuery) (ListResult, error) {
	where, args := dashboardWhere(query)
	var result ListResult
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM articles a JOIN categories c ON a.category_id = c.id `+where, args...).Scan(&result.Total); err != nil {
		return ListResult{}, fmt.Errorf("count dashboard articles: %w", err)
	}
	args = append(args, query.Limit, pageOffset(query.Page, query.Limit))
	rows, err := s.db.QueryContext(ctx, `SELECT `+articleColumns+articleJoins+` `+where+` ORDER BY a.updated_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return ListResult{}, fmt.Errorf("list dashboard articles: %w", err)
	}
	defer rows.Close()
	result.Articles = make([]Article, 0)
	for rows.Next() {
		article, err := scanArticle(rows)
		if err != nil {
			return ListResult{}, fmt.Errorf("scan dashboard article: %w", err)
		}
		result.Articles = append(result.Articles, article)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, fmt.Errorf("iterate dashboard articles: %w", err)
	}
	return result, nil
}

func dashboardWhere(query DashboardQuery) (string, []any) {
	where := `WHERE 1=1`
	args := make([]any, 0, 6)
	switch query.Status {
	case "draft", "published", "scheduled":
		where += ` AND a.status = ?`
		args = append(args, query.Status)
	}
	if query.Category != "" {
		where += ` AND c.slug = ?`
		args = append(args, query.Category)
	}
	if query.Search != "" {
		where += ` AND (a.title LIKE ? OR a.excerpt LIKE ?)`
		term := "%" + query.Search + "%"
		args = append(args, term, term)
	}
	return where, args
}

// DashboardCategories mirrors GET /api/dashboard/categories.
func (s *SQLiteStore) DashboardCategories(ctx context.Context) ([]CategoryWithCount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id, c.name, c.slug, c.description, c.color,
		(SELECT COUNT(*) FROM articles WHERE category_id = c.id) AS article_count
		FROM categories c ORDER BY c.name`)
	if err != nil {
		return nil, fmt.Errorf("list dashboard categories: %w", err)
	}
	defer rows.Close()
	categories := make([]CategoryWithCount, 0)
	for rows.Next() {
		var category CategoryWithCount
		if err := rows.Scan(&category.ID, &category.Name, &category.Slug, &category.Description, &category.Color, &category.ArticleCount); err != nil {
			return nil, fmt.Errorf("scan dashboard category: %w", err)
		}
		categories = append(categories, category)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dashboard categories: %w", err)
	}
	return categories, nil
}
```

- [ ] **Step 6: Create `internal/content/admin_service.go`**

```go
package content

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"
)

type adminStore interface {
	DashboardList(context.Context, DashboardQuery) (ListResult, error)
	ByID(context.Context, string) (Article, error)
	DashboardCategories(context.Context) ([]CategoryWithCount, error)
}

// AdminService ports the authenticated article and category handlers of
// apps/server/src/routes/dashboard.ts rule for rule.
type AdminService struct {
	store adminStore
	now   func() time.Time
}

func NewAdminService(store adminStore, now func() time.Time) *AdminService {
	return &AdminService{store: store, now: now}
}

// ListArticles ports dashboard.ts:98-151.
func (s *AdminService) ListArticles(ctx context.Context, query DashboardQuery) (Page, error) {
	query.Page = math.Max(1, query.Page)
	query.Limit = clamp(query.Limit, 1, 50)
	result, err := s.store.DashboardList(ctx, query)
	if err != nil {
		return Page{}, fmt.Errorf("list dashboard articles: %w", err)
	}
	return Page{Articles: result.Articles, Total: result.Total, Page: query.Page, TotalPages: (result.Total + int64(query.Limit) - 1) / int64(query.Limit)}, nil
}

// GetArticle ports dashboard.ts:153-171 (any status).
func (s *AdminService) GetArticle(ctx context.Context, id string) (Article, error) {
	article, err := s.store.ByID(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return Article{}, articleNotFound()
	}
	if err != nil {
		return Article{}, fmt.Errorf("get dashboard article: %w", err)
	}
	return article, nil
}

// ListCategories ports dashboard.ts:483-491.
func (s *AdminService) ListCategories(ctx context.Context) ([]CategoryWithCount, error) {
	categories, err := s.store.DashboardCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("list dashboard categories: %w", err)
	}
	return categories, nil
}
```

- [ ] **Step 7: Create `internal/content/http_admin.go`.** `decode`, `notify`, and the mutation envelopes are used from Task 4 onward. The dashboard list reuses the public slice's `parseNodeNumber`/`parseNodeLimit` (`parseInt(x) || default` semantics) with default limit 20:

```go
package content

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/jsonbody"
)

type adminService interface {
	ListArticles(context.Context, DashboardQuery) (Page, error)
	GetArticle(context.Context, string) (Article, error)
	ListCategories(context.Context) ([]CategoryWithCount, error)
}

// IndexNowNotifier queues public article slugs for IndexNow. Notify must not
// block: Node sends the HTTP response first and never lets IndexNow delay or
// fail a dashboard request.
type IndexNowNotifier interface {
	Notify(slugs []string)
}

// AdminHandler serves the authenticated dashboard article and category routes.
type AdminHandler struct {
	service  adminService
	indexNow IndexNowNotifier
	logger   *slog.Logger
}

func NewAdminHandler(service adminService, indexNow IndexNowNotifier, logger *slog.Logger) *AdminHandler {
	return &AdminHandler{service: service, indexNow: indexNow, logger: logger}
}

// Mount registers the routes relative to /api/dashboard. The caller owns
// authentication: every route here must sit behind RequireAuth.
func (h *AdminHandler) Mount(router chi.Router) {
	router.Get("/articles", h.listArticles)
	router.Get("/articles/{id}", h.getArticle)
	router.Get("/categories", h.listCategories)
}

type articleEnvelope struct {
	Article Article `json:"article"`
}

type categoryEnvelope struct {
	Category Category `json:"category"`
}

type successEnvelope struct {
	Success bool `json:"success"`
}

func (h *AdminHandler) listArticles(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	page, err := h.service.ListArticles(r.Context(), DashboardQuery{
		Page:     parseNodeNumber(values.Get("page"), 1),
		Limit:    parseNodeLimit(values.Get("limit"), 20, 50),
		Status:   values.Get("status"),
		Category: values.Get("category"),
		Search:   values.Get("search"),
	})
	if err != nil {
		h.fail(w, r, "list dashboard articles", err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (h *AdminHandler) getArticle(w http.ResponseWriter, r *http.Request) {
	article, err := h.service.GetArticle(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, "get dashboard article", err)
		return
	}
	writeJSON(w, http.StatusOK, articleEnvelope{article})
}

func (h *AdminHandler) listCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := h.service.ListCategories(r.Context())
	if err != nil {
		h.fail(w, r, "list dashboard categories", err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Categories []CategoryWithCount `json:"categories"`
	}{categories})
}

func (h *AdminHandler) decode(w http.ResponseWriter, r *http.Request) (jsonbody.Object, bool) {
	body, err := jsonbody.Decode(w, r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return nil, false
	}
	return body, true
}

// notify runs after the response is written, like Node's queueIndexNowNotification.
func (h *AdminHandler) notify(slugs []string) {
	if len(slugs) > 0 && h.indexNow != nil {
		h.indexNow.Notify(slugs)
	}
}

// fail maps AdminError to Node's status and body; anything else is an
// unexpected failure (Express's default 500) logged without leaking detail.
func (h *AdminHandler) fail(w http.ResponseWriter, r *http.Request, message string, err error) {
	var adminErr *AdminError
	if errors.As(err, &adminErr) {
		if adminErr.Status >= http.StatusInternalServerError {
			h.logger.ErrorContext(r.Context(), message, "error", err)
		}
		if len(adminErr.Details) > 0 {
			writeJSON(w, adminErr.Status, struct {
				Error   string   `json:"error"`
				Details []string `json:"details"`
			}{adminErr.Message, adminErr.Details})
			return
		}
		writeJSON(w, adminErr.Status, map[string]string{"error": adminErr.Message})
		return
	}
	h.logger.ErrorContext(r.Context(), message, "error", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Internal server error"})
}
```

- [ ] **Step 8: Run them and confirm they pass**

Run: `gofmt -l internal/content; go vet -p 2 ./internal/content/ && go test -p 2 ./internal/content/`
Expected: vet clean, `ok` (all existing content tests still pass).

- [ ] **Step 9: Commit**

```bash
git add internal/content/admin_model.go internal/content/admin_sqlite.go internal/content/admin_service.go internal/content/http_admin.go internal/content/admin_helpers_test.go internal/content/admin_read_test.go
git commit -m "Port the dashboard article and category reads to the Go content module"
```

---

### Task 3: Transactional SQL seams for dashboard mutations

Every mutation runs inside one transaction on the single pooled connection. This serializes writers the way better-sqlite3's synchronous handlers did, and rolls back partial work. Inside `fn`, only the given `adminTx` may be used, because touching `s.db` would wait forever for the one connection.

**Files:** Modify `internal/content/admin_sqlite.go`, `internal/content/admin_service.go`; Create `internal/content/admin_tx_internal_test.go`

- [ ] **Step 1: Write the failing test** at `internal/content/admin_tx_internal_test.go`:

```go
package content

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

// openTxStore builds the v1 authors + v2 content schema without importing the
// editorial capability (the authors DDL mirrors editorial migration 1).
func openTxStore(t *testing.T) (*SQLiteStore, *sql.DB) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	descriptors := append([]migrate.Descriptor{{Version: 1, Name: "authors", SQL: `CREATE TABLE authors (
		id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, email TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL, avatar TEXT, bio TEXT,
		role TEXT NOT NULL DEFAULT 'editor' CHECK(role IN ('admin', 'editor')))`}}, Migrations()...)
	if err := migrate.Run(context.Background(), db, descriptors); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO categories (id, name, slug) VALUES (101, 'AI', 'ai'), (104, 'Deals', 'deals')`,
		`INSERT INTO authors (id, name, email, password_hash, role) VALUES (201, 'TechNews Editorial', 'e@example.invalid', 'x', 'admin')`,
		`INSERT INTO articles (id, title, slug, category_id, author_id, status, source, source_url) VALUES
			(301, 'A', 'a', 101, 201, 'published', 'TechCrunch', 'https://techcrunch.com/a'),
			(302, 'B', 'b', 101, 201, 'draft', NULL, NULL)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	return NewSQLiteStore(db), db
}

func TestAdminTxLookupsUseNodeSQLSemantics(t *testing.T) {
	store, _ := openTxStore(t)
	ctx := context.Background()
	url := "https://techcrunch.com/a"
	err := store.inAdminTx(ctx, func(tx adminTx) error {
		for _, id := range []any{"101", float64(101), " 101", int64(101)} {
			if slug, found, err := tx.categorySlug(ctx, id); err != nil || !found || slug != "ai" {
				t.Errorf("categorySlug(%#v) = %q %v %v", id, slug, found, err)
			}
		}
		if _, found, _ := tx.categorySlug(ctx, "101abc"); found {
			t.Error("categorySlug matched a non-numeric id")
		}
		for name, tt := range map[string]struct {
			exclude *string
			slug    any
			url     *string
			want    bool
		}{
			"slug":                        {nil, "a", nil, true},
			"source url":                  {nil, "new", &url, true},
			"null url never matches null": {nil, "new", nil, false},
			"own row excluded":            {ptr("301"), "a", &url, false},
			"other row not excluded":      {ptr("302"), "a", nil, true},
		} {
			if got, err := tx.articleConflict(ctx, tt.exclude, tt.slug, tt.url); err != nil || got != tt.want {
				t.Errorf("%s: articleConflict = %v %v, want %v", name, got, err, tt.want)
			}
		}
		if id, found, err := tx.editorialAuthorID(ctx); err != nil || !found || id != 201 {
			t.Errorf("editorialAuthorID = %d %v %v", id, found, err)
		}
		if _, err := tx.storedArticle(ctx, "999"); !errors.Is(err, ErrNotFound) {
			t.Errorf("storedArticle(999) error = %v", err)
		}
		stored, err := tx.storedArticle(ctx, "302")
		if err != nil || stored.Slug != "b" || stored.Source != nil || stored.SourceURL != nil || stored.CategoryID != 101 {
			t.Errorf("storedArticle(302) = %+v %v", stored, err)
		}
		if deleted, err := tx.deleteRow(ctx, "articles", "999"); err != nil || deleted {
			t.Errorf("deleteRow(999) = %v %v", deleted, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAdminTxRollsBackWhenTheMutationFails(t *testing.T) {
	store, db := openTxStore(t)
	ctx := context.Background()
	failure := errors.New("later check failed")
	err := store.inAdminTx(ctx, func(tx adminTx) error {
		if err := tx.updateRow(ctx, "articles", "302", []assignment{{"title", "Changed"}}); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("inAdminTx error = %v", err)
	}
	var title string
	if err := db.QueryRow(`SELECT title FROM articles WHERE id = 302`).Scan(&title); err != nil || title != "B" {
		t.Fatalf("title after rollback = %q %v", title, err)
	}
}

func ptr(value string) *string { return &value }
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test -p 2 ./internal/content/ -run AdminTx`
Expected: build failure: `store.inAdminTx undefined`, `undefined: adminTx`.

- [ ] **Step 3: In `internal/content/admin_sqlite.go` replace the import block with**

```go
import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)
```

**and append:**

```go
// inAdminTx runs one dashboard mutation in a transaction. The pool has a
// single connection, so the transaction also serializes mutations the way
// better-sqlite3's synchronous handlers did. fn must use only the adminTx it
// is given: touching s.db inside fn would wait forever for the connection.
func (s *SQLiteStore) inAdminTx(ctx context.Context, fn func(adminTx) error) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin dashboard transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = fn(sqliteAdminTx{tx: tx}); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit dashboard transaction: %w", err)
	}
	return nil
}

type sqliteAdminTx struct{ tx *sql.Tx }

func (t sqliteAdminTx) storedArticle(ctx context.Context, id string) (storedArticle, error) {
	var article storedArticle
	var source, sourceURL sql.NullString
	err := t.tx.QueryRowContext(ctx, `SELECT title, slug, excerpt, content, category_id, status, source, source_url FROM articles WHERE id = ?`, id).
		Scan(&article.Title, &article.Slug, &article.Excerpt, &article.Content, &article.CategoryID, &article.Status, &source, &sourceURL)
	if errors.Is(err, sql.ErrNoRows) {
		return storedArticle{}, ErrNotFound
	}
	if err != nil {
		return storedArticle{}, fmt.Errorf("read article %s: %w", id, err)
	}
	article.Source = nullString(source)
	article.SourceURL = nullString(sourceURL)
	return article, nil
}

func (t sqliteAdminTx) articleSlugStatus(ctx context.Context, id string) (slug, status string, found bool, err error) {
	err = t.tx.QueryRowContext(ctx, `SELECT slug, status FROM articles WHERE id = ?`, id).Scan(&slug, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("read article %s: %w", id, err)
	}
	return slug, status, true, nil
}

func (t sqliteAdminTx) categorySlug(ctx context.Context, id any) (string, bool, error) {
	var slug string
	err := t.tx.QueryRowContext(ctx, `SELECT slug FROM categories WHERE id = ?`, id).Scan(&slug)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read category: %w", err)
	}
	return slug, true, nil
}

// articleConflict ports Node's duplicate query. excludeID is nil on create.
func (t sqliteAdminTx) articleConflict(ctx context.Context, excludeID *string, slug any, sourceURL *string) (bool, error) {
	var url any
	if sourceURL != nil {
		url = *sourceURL
	}
	query := `SELECT id FROM articles WHERE slug = ? OR (? IS NOT NULL AND source_url = ?) LIMIT 1`
	args := []any{slug, url, url}
	if excludeID != nil {
		query = `SELECT id FROM articles WHERE id != ? AND (slug = ? OR (? IS NOT NULL AND source_url = ?)) LIMIT 1`
		args = append([]any{*excludeID}, args...)
	}
	var id int64
	err := t.tx.QueryRowContext(ctx, query, args...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check duplicate article: %w", err)
	}
	return true, nil
}

func (t sqliteAdminTx) editorialAuthorID(ctx context.Context) (int64, bool, error) {
	var id int64
	err := t.tx.QueryRowContext(ctx, `SELECT id FROM authors WHERE name = ?`, EditorialAuthor().Name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("find editorial author: %w", err)
	}
	return id, true, nil
}

// insertArticle binds values in Node's exact INSERT column order.
func (t sqliteAdminTx) insertArticle(ctx context.Context, values []any) (int64, error) {
	result, err := t.tx.ExecContext(ctx, `INSERT INTO articles (
		title, slug, excerpt, content, featured_image, category_id, author_id, status,
		published_at, meta_title, meta_description, source, source_url, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, values...)
	if err != nil {
		return 0, fmt.Errorf("insert article: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("inserted article id: %w", err)
	}
	return id, nil
}

func (t sqliteAdminTx) updateRow(ctx context.Context, table, id string, assignments []assignment) error {
	columns := make([]string, len(assignments))
	values := make([]any, 0, len(assignments)+1)
	for i, a := range assignments {
		columns[i] = a.column + " = ?"
		values = append(values, a.value)
	}
	values = append(values, id)
	if _, err := t.tx.ExecContext(ctx, `UPDATE `+table+` SET `+strings.Join(columns, ", ")+` WHERE id = ?`, values...); err != nil {
		return fmt.Errorf("update %s %s: %w", table, id, err)
	}
	return nil
}

func (t sqliteAdminTx) deleteRow(ctx context.Context, table, id string) (bool, error) {
	result, err := t.tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("delete %s %s: %w", table, id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete %s %s: %w", table, id, err)
	}
	return affected > 0, nil
}

func (t sqliteAdminTx) article(ctx context.Context, id any) (Article, error) {
	return queryArticle(ctx, t.tx, ` WHERE a.id = ?`, id)
}

func (t sqliteAdminTx) categoryExists(ctx context.Context, id string) (bool, error) {
	var found int64
	err := t.tx.QueryRowContext(ctx, `SELECT id FROM categories WHERE id = ?`, id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read category %s: %w", id, err)
	}
	return true, nil
}

func (t sqliteAdminTx) insertCategory(ctx context.Context, name, slug, description, color any) (int64, error) {
	result, err := t.tx.ExecContext(ctx, `INSERT INTO categories (name, slug, description, color) VALUES (?, ?, ?, ?)`, name, slug, description, color)
	if err != nil {
		return 0, fmt.Errorf("insert category: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("inserted category id: %w", err)
	}
	return id, nil
}

func (t sqliteAdminTx) category(ctx context.Context, id any) (Category, error) {
	var category Category
	if err := t.tx.QueryRowContext(ctx, `SELECT id, name, slug, description, color FROM categories WHERE id = ?`, id).
		Scan(&category.ID, &category.Name, &category.Slug, &category.Description, &category.Color); err != nil {
		return Category{}, fmt.Errorf("read category: %w", err)
	}
	return category, nil
}

func (t sqliteAdminTx) categoryArticleCount(ctx context.Context, id string) (int64, error) {
	var count int64
	if err := t.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM articles WHERE category_id = ?`, id).Scan(&count); err != nil {
		return 0, fmt.Errorf("count category articles: %w", err)
	}
	return count, nil
}
```

- [ ] **Step 4: In `internal/content/admin_service.go` replace the `adminStore` interface with**

```go
type adminTx interface {
	storedArticle(context.Context, string) (storedArticle, error)
	articleSlugStatus(context.Context, string) (string, string, bool, error)
	categorySlug(context.Context, any) (string, bool, error)
	articleConflict(context.Context, *string, any, *string) (bool, error)
	editorialAuthorID(context.Context) (int64, bool, error)
	insertArticle(context.Context, []any) (int64, error)
	updateRow(context.Context, string, string, []assignment) error
	deleteRow(context.Context, string, string) (bool, error)
	article(context.Context, any) (Article, error)
	categoryExists(context.Context, string) (bool, error)
	insertCategory(context.Context, any, any, any, any) (int64, error)
	category(context.Context, any) (Category, error)
	categoryArticleCount(context.Context, string) (int64, error)
}

type adminStore interface {
	DashboardList(context.Context, DashboardQuery) (ListResult, error)
	ByID(context.Context, string) (Article, error)
	DashboardCategories(context.Context) ([]CategoryWithCount, error)
	inAdminTx(context.Context, func(adminTx) error) error
}
```

- [ ] **Step 5: Run it and confirm it passes**

Run: `gofmt -l internal/content; go vet -p 2 ./internal/content/ && go test -p 2 ./internal/content/`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/content/admin_sqlite.go internal/content/admin_service.go internal/content/admin_tx_internal_test.go
git commit -m "Add transactional SQL seams for dashboard content mutations"
```

---

### Task 4: Create article

**Node:** `:198-314`. Order of checks (each check is exact, and the first failure wins): required `title`/`slug`/`category_id` truthy → `source_url` truthy ⇒ `normalizeSourceUrl(String(x))` or 400 → if `status || "draft"` is `"published"`: truthy `source` and normalized URL, then the policy (the `details` array) → category lookup binds `category_id` (400 not found / 400 deals) → duplicate on `slug` or normalized `source_url` (409) → editorial author (500 with message) → insert (`excerpt||""`, `content||""`, others `||null`, `published_at` = given or ISO now when published, SQLite timestamps) → 201 → IndexNow `[slug]` if published.

**Files:** Create `internal/content/admin_articles.go`, `internal/content/admin_create_test.go`; Modify `internal/content/http_admin.go`

- [ ] **Step 1: Write the failing test** at `internal/content/admin_create_test.go`. Every status and body was recorded from Node:

```go
package content_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

func publishedBody(extra map[string]any) string {
	body := map[string]any{
		"title": validTitle, "excerpt": validExcerpt, "content": validContent,
		"slug": "new-published", "category_id": 101, "status": "published", "source": "TechCrunch",
	}
	for key, value := range extra {
		body[key] = value
	}
	return jsonObject(body)
}

func jsonObject(body map[string]any) string {
	data, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// Every expected status and body below was recorded from the Node server
// (apps/server/src/routes/dashboard.ts) against the same seed.
func TestDashboardCreateArticleRejectsLikeNode(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		status     int
		want       string
	}{
		{"missing fields", `{}`, 400, `{"error":"Title, slug, and category_id are required"}`},
		{"falsy category", `{"title":"T","slug":"s","category_id":0}`, 400, `{"error":"Title, slug, and category_id are required"}`},
		{"empty slug", `{"title":"T","slug":"","category_id":101}`, 400, `{"error":"Title, slug, and category_id are required"}`},
		{"invalid source url", `{"title":"T","slug":"s","category_id":101,"source_url":"nope"}`, 400, `{"error":"Source URL is invalid"}`},
		{"published without source", `{"title":"T","slug":"s","category_id":101,"status":"published","source_url":"https://techcrunch.com/a"}`, 400, `{"error":"Published articles require source and source_url"}`},
		{"published without source url", `{"title":"T","slug":"s","category_id":101,"status":"published","source":"TechCrunch"}`, 400, `{"error":"Published articles require source and source_url"}`},
		{"policy failure", `{"title":"Big news!","slug":"s","category_id":101,"status":"published","source":"The Verge","source_url":"https://techcrunch.com/a"}`, 400,
			`{"error":"Article failed publishing policy","details":["headline appears clickbait-like","excerpt is missing","article must contain 5 to 12 paragraphs","article must contain 150 to 800 words, found 0","source does not match source URL"]}`},
		{"unapproved source", publishedBody(map[string]any{"slug": "s", "source": "Synthetic Wire", "source_url": "https://news.example.invalid/x"}), 400,
			`{"error":"Article failed publishing policy","details":["source URL is not from an approved publication"]}`},
		{"unknown category", `{"title":"T","slug":"s","category_id":999}`, 400, `{"error":"Category not found"}`},
		{"deals category", `{"title":"T","slug":"s","category_id":104}`, 400, `{"error":"Deals articles are not allowed"}`},
		{"duplicate slug", `{"title":"T","slug":"synthetic-draft","category_id":101}`, 409, `{"error":"Article slug or source_url already exists"}`},
		{"duplicate normalized source url", `{"title":"T","slug":"s","category_id":101,"source_url":"https://techcrunch.com/2026/09/01/model/?utm_source=x#top"}`, 409, `{"error":"Article slug or source_url already exists"}`},
		{"unbindable title", `{"title":true,"slug":"s","category_id":101}`, 500, internalError},
		{"status outside CHECK", `{"title":"T","slug":"s","category_id":101,"status":"archived"}`, 500, internalError},
		{"malformed json", `{`, 400, `{"error":"Invalid request body"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler, db, notifier := adminServer(t)
			expectBody(t, adminRequest(t, handler, notifier, http.MethodPost, "/articles", tt.body), tt.status, tt.want)
			if count := countRows(t, db, `SELECT COUNT(*) FROM articles`); count != 4 {
				t.Errorf("articles = %d, want 4 (nothing written)", count)
			}
			if len(notifier.calls) != 0 {
				t.Errorf("IndexNow calls = %v", notifier.calls)
			}
		})
	}
}

func TestDashboardCreateArticleRequiresEditorialAuthor(t *testing.T) {
	handler, db, notifier := adminServer(t)
	if _, err := db.Exec(`UPDATE authors SET name = 'Former Editorial' WHERE id = 201`); err != nil {
		t.Fatal(err)
	}
	expectBody(t, adminRequest(t, handler, notifier, http.MethodPost, "/articles", `{"title":"T","slug":"s","category_id":101}`),
		http.StatusInternalServerError, `{"error":"TechNews Editorial author is missing"}`)
}

func TestDashboardCreateDraftAppliesNodeDefaults(t *testing.T) {
	handler, _, notifier := adminServer(t)
	response := adminRequest(t, handler, notifier, http.MethodPost, "/articles",
		`{"title":"T","slug":"new-draft","category_id":"101","featured_image":"","meta_title":"","source":""}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
	expectFields(t, articleFields(t, response), map[string]any{
		"id": float64(306), "title": "T", "slug": "new-draft", "excerpt": "", "content": "", "featured_image": nil,
		"category_id": float64(101), "author_id": float64(201), "status": "draft", "published_at": nil,
		"meta_title": nil, "meta_description": nil, "source": nil, "source_url": nil, "view_count": float64(0),
		"created_at": "2026-09-20 12:00:00", "updated_at": "2026-09-20 12:00:00",
	})
	if len(notifier.calls) != 0 {
		t.Errorf("draft create notified IndexNow: %v", notifier.calls)
	}
}

func TestDashboardCreatePublishedStampsNormalizesAndNotifiesAfterResponse(t *testing.T) {
	handler, _, notifier := adminServer(t)
	response := adminRequest(t, handler, notifier, http.MethodPost, "/articles",
		publishedBody(map[string]any{"source_url": "https://www.techcrunch.com/2026/09/20/new/?utm_campaign=x"}))
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
	expectFields(t, articleFields(t, response), map[string]any{
		"status": "published", "published_at": "2026-09-20T12:00:00.000Z", "author_id": float64(201),
		"source": "TechCrunch", "source_url": "https://www.techcrunch.com/2026/09/20/new",
		"created_at": "2026-09-20 12:00:00", "updated_at": "2026-09-20 12:00:00",
	})
	if !reflect.DeepEqual(notifier.calls, [][]string{{"new-published"}}) {
		t.Fatalf("IndexNow calls = %v", notifier.calls)
	}
	if notifier.bodyAtNotify[0] != response.Body.Len() {
		t.Errorf("IndexNow was notified before the response body was written")
	}
}

func TestDashboardCreatePublishedKeepsExplicitPublishedAt(t *testing.T) {
	handler, _, notifier := adminServer(t)
	response := adminRequest(t, handler, notifier, http.MethodPost, "/articles",
		publishedBody(map[string]any{"source_url": "https://techcrunch.com/2026/09/20/new", "published_at": "2026-09-19T08:00:00.000Z"}))
	expectFields(t, articleFields(t, response), map[string]any{"published_at": "2026-09-19T08:00:00.000Z"})
}

func TestDashboardCreateBindsNumbersLikeBetterSQLite3(t *testing.T) {
	handler, _, notifier := adminServer(t)
	response := adminRequest(t, handler, notifier, http.MethodPost, "/articles", `{"title":5,"slug":"numeric","category_id":101.0}`)
	expectFields(t, articleFields(t, response), map[string]any{"title": "5.0", "category_id": float64(101)})
}
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test -p 2 ./internal/content/ -run DashboardCreate`
Expected: FAIL. `POST /articles` is not routed yet, so it returns `405` (e.g. `response = 405 , want 400 {"error":"Title, slug, and category_id are required"}`).

- [ ] **Step 3: Create `internal/content/admin_articles.go`**

```go
package content

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/jsonbody"
)

const (
	// sqliteTimestamp is Node's formatSqliteTimestamp (toISOString, "T"->" ", first 19 chars).
	sqliteTimestamp = "2006-01-02 15:04:05"
	// javaScriptISO is Date.prototype.toISOString, used by Node for default published_at values.
	javaScriptISO = "2006-01-02T15:04:05.000Z"
)

// orEmpty is `value || ""` and orNull is `value || null`, bound like better-sqlite3.
func orEmpty(v jsonbody.Value) (any, error) {
	if v.Truthy() {
		return v.Bind()
	}
	return "", nil
}

func orNull(v jsonbody.Value) (any, error) {
	if v.Truthy() {
		return v.Bind()
	}
	return nil, nil
}

// validatePublishedArticle ports dashboard.ts:180-196. validateRewrittenArticle
// treats non-string input as "", which StringOrEmpty reproduces at the call sites.
func validatePublishedArticle(title, excerpt, content string, source jsonbody.Value, sourceURL string) []string {
	errs := ValidateRewrittenArticle(RewrittenArticle{Title: title, Excerpt: excerpt, Content: content}, ArticleValidationOptions{})
	expected, ok := SourceForURL(sourceURL)
	if !ok {
		errs = append(errs, "source URL is not from an approved publication")
	}
	if ok && !source.Is(expected) {
		errs = append(errs, "source does not match source URL")
	}
	return uniqueStrings(errs)
}

// uniqueStrings is JavaScript [...new Set(values)].
func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			unique = append(unique, value)
		}
	}
	return unique
}

// checkCategory ports the category lookup and deals guard (dashboard.ts:248-256, 363-371).
func checkCategory(ctx context.Context, tx adminTx, categoryID any) error {
	slug, found, err := tx.categorySlug(ctx, categoryID)
	if err != nil {
		return err
	}
	if !found {
		return badRequest("Category not found")
	}
	if slug == "deals" {
		return badRequest("Deals articles are not allowed")
	}
	return nil
}

func requireEditorialAuthor(ctx context.Context, tx adminTx) (int64, error) {
	id, found, err := tx.editorialAuthorID(ctx)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, &AdminError{Status: http.StatusInternalServerError, Message: "TechNews Editorial author is missing"}
	}
	return id, nil
}

func duplicateArticle() *AdminError {
	return &AdminError{Status: http.StatusConflict, Message: "Article slug or source_url already exists"}
}

func policyFailure(details []string) *AdminError {
	return &AdminError{Status: http.StatusBadRequest, Message: "Article failed publishing policy", Details: details}
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

// bindAll binds values in order, failing like better-sqlite3's .run() would.
func bindAll(binders ...func() (any, error)) ([]any, error) {
	values := make([]any, 0, len(binders))
	for _, bind := range binders {
		value, err := bind()
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func constant(value any) func() (any, error) {
	return func() (any, error) { return value, nil }
}

// CreateArticle ports dashboard.ts:198-314. The author is always the
// TechNews Editorial account, whoever is signed in.
func (s *AdminService) CreateArticle(ctx context.Context, body jsonbody.Object) (ArticleChange, error) {
	title, slug, categoryID := body.Get("title"), body.Get("slug"), body.Get("category_id")
	if !title.Truthy() || !slug.Truthy() || !categoryID.Truthy() {
		return ArticleChange{}, badRequest("Title, slug, and category_id are required")
	}
	status := body.Get("status")
	if !status.Truthy() {
		status = jsonbody.String("draft")
	}
	published := status.Is("published")

	var normalizedSourceURL *string
	if sourceURL := body.Get("source_url"); sourceURL.Truthy() {
		normalized, err := NormalizeSourceURL(sourceURL.JSString())
		if err != nil {
			return ArticleChange{}, badRequest("Source URL is invalid")
		}
		normalizedSourceURL = &normalized
	}
	source := body.Get("source")
	if published {
		if !source.Truthy() || normalizedSourceURL == nil {
			return ArticleChange{}, badRequest("Published articles require source and source_url")
		}
		errs := validatePublishedArticle(title.StringOrEmpty(), body.Get("excerpt").StringOrEmpty(), body.Get("content").StringOrEmpty(), source, *normalizedSourceURL)
		if len(errs) > 0 {
			return ArticleChange{}, policyFailure(errs)
		}
	}

	var change ArticleChange
	err := s.store.inAdminTx(ctx, func(tx adminTx) error {
		boundCategoryID, err := categoryID.Bind()
		if err != nil {
			return fmt.Errorf("bind category_id: %w", err)
		}
		if err := checkCategory(ctx, tx, boundCategoryID); err != nil {
			return err
		}
		boundSlug, err := slug.Bind()
		if err != nil {
			return fmt.Errorf("bind slug: %w", err)
		}
		duplicate, err := tx.articleConflict(ctx, nil, boundSlug, normalizedSourceURL)
		if err != nil {
			return err
		}
		if duplicate {
			return duplicateArticle()
		}
		authorID, err := requireEditorialAuthor(ctx, tx)
		if err != nil {
			return err
		}

		now := s.now().UTC()
		publishedAt := body.Get("published_at")
		if published && !publishedAt.Truthy() {
			publishedAt = jsonbody.String(now.Format(javaScriptISO))
		}
		stamp := now.Format(sqliteTimestamp)
		values, err := bindAll(
			title.Bind, slug.Bind,
			func() (any, error) { return orEmpty(body.Get("excerpt")) },
			func() (any, error) { return orEmpty(body.Get("content")) },
			func() (any, error) { return orNull(body.Get("featured_image")) },
			categoryID.Bind, constant(authorID), status.Bind,
			func() (any, error) { return orNull(publishedAt) },
			func() (any, error) { return orNull(body.Get("meta_title")) },
			func() (any, error) { return orNull(body.Get("meta_description")) },
			func() (any, error) { return orNull(source) },
			constant(nullableString(normalizedSourceURL)), constant(stamp), constant(stamp),
		)
		if err != nil {
			return fmt.Errorf("bind article insert: %w", err)
		}
		id, err := tx.insertArticle(ctx, values)
		if err != nil {
			return err
		}
		if change.Article, err = tx.article(ctx, id); err != nil {
			return err
		}
		if published {
			change.IndexNowSlugs = []string{slug.JSString()}
		}
		return nil
	})
	if err != nil {
		return ArticleChange{}, err
	}
	return change, nil
}
```

- [ ] **Step 4: In `internal/content/http_admin.go` replace the `adminService` interface with**

```go
type adminService interface {
	ListArticles(context.Context, DashboardQuery) (Page, error)
	GetArticle(context.Context, string) (Article, error)
	CreateArticle(context.Context, jsonbody.Object) (ArticleChange, error)
	ListCategories(context.Context) ([]CategoryWithCount, error)
}
```

**replace `Mount` with**

```go
// Mount registers the routes relative to /api/dashboard. The caller owns
// authentication: every route here must sit behind RequireAuth.
func (h *AdminHandler) Mount(router chi.Router) {
	router.Get("/articles", h.listArticles)
	router.Get("/articles/{id}", h.getArticle)
	router.Post("/articles", h.createArticle)
	router.Get("/categories", h.listCategories)
}
```

**and append:**

```go
func (h *AdminHandler) createArticle(w http.ResponseWriter, r *http.Request) {
	body, ok := h.decode(w, r)
	if !ok {
		return
	}
	change, err := h.service.CreateArticle(r.Context(), body)
	if err != nil {
		h.fail(w, r, "create dashboard article", err)
		return
	}
	writeJSON(w, http.StatusCreated, articleEnvelope{change.Article})
	h.notify(change.IndexNowSlugs)
}
```

- [ ] **Step 5: Run it and confirm it passes**

Run: `gofmt -l internal/content; go vet -p 2 ./internal/content/ && go test -p 2 ./internal/content/`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/content/admin_articles.go internal/content/http_admin.go internal/content/admin_create_test.go
git commit -m "Port dashboard article creation with Node's validation order and defaults"
```

---

### Task 5: Update article

**Node:** `:316-462`. Existing row (404) → `source_url` if defined (`null`/`""` clears it, otherwise normalize or 400) → next values via `??` (`title`, `slug`, `excerpt`, `content`, `source`, `status`, `category_id`) → category/deals on the next category → policy when the next status is published → duplicate excluding this id (409) → `published_at` = ISO now when **request** `status === "published"` and `published_at` is falsy, otherwise as sent if defined → every defined field written as sent (`source` as `source || null`), plus `source_url` if defined, plus `author_id` = editorial when next status is published (500 if missing) → `No fields to update` (400) → `updated_at` → 200 → IndexNow old slug (if it was published) + next slug (if it is published).

**Files:** Modify `internal/content/admin_articles.go`, `internal/content/http_admin.go`; Create `internal/content/admin_update_test.go`

- [ ] **Step 1: Write the failing test** at `internal/content/admin_update_test.go` (Node-recorded vectors):

```go
package content_test

import (
	"net/http"
	"reflect"
	"testing"
)

// Expected statuses and bodies were recorded from the Node server on the same seed.
func TestDashboardUpdateArticleRejectsLikeNode(t *testing.T) {
	for _, tt := range []struct {
		name, target, body string
		status             int
		want               string
	}{
		{"missing article", "/articles/999", `{"title":"x"}`, 404, `{"error":"Article not found"}`},
		{"invalid source url", "/articles/303", `{"source_url":"nope"}`, 400, `{"error":"Source URL is invalid"}`},
		{"empty draft update", "/articles/303", `{}`, 400, `{"error":"No fields to update"}`},
		{"non-json draft update", "/articles/303", ``, 400, `{"error":"No fields to update"}`},
		{"deals category", "/articles/303", `{"category_id":104}`, 400, `{"error":"Deals articles are not allowed"}`},
		{"unknown category", "/articles/303", `{"category_id":999}`, 400, `{"error":"Category not found"}`},
		{"duplicate slug", "/articles/303", `{"slug":"synthetic-published-newer"}`, 409, `{"error":"Article slug or source_url already exists"}`},
		{"published legacy article fails policy", "/articles/301", `{}`, 400,
			`{"error":"Article failed publishing policy","details":["excerpt must be exactly one sentence","article must contain 5 to 12 paragraphs","article must contain 150 to 800 words, found 5","source URL is not from an approved publication"]}`},
		{"published without source", "/articles/302", `{"title":"x"}`, 400, `{"error":"Published articles require source and source_url"}`},
		{"publishing a draft runs the policy", "/articles/303", `{"status":"published"}`, 400,
			`{"error":"Article failed publishing policy","details":["excerpt must be exactly one sentence","article must contain 5 to 12 paragraphs","article must contain 150 to 800 words, found 2","source URL is not from an approved publication"]}`},
		{"null title hits NOT NULL", "/articles/303", `{"title":null}`, 500, internalError},
		{"null category hits NOT NULL", "/articles/303", `{"category_id":null}`, 500, internalError},
		{"malformed json", "/articles/303", `{"title":`, 400, `{"error":"Invalid request body"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler, db, notifier := adminServer(t)
			expectBody(t, adminRequest(t, handler, notifier, http.MethodPut, tt.target, tt.body), tt.status, tt.want)
			if changed := countRows(t, db, `SELECT COUNT(*) FROM articles WHERE updated_at = '2026-09-20 12:00:00'`); changed != 0 {
				t.Errorf("%d rows were updated", changed)
			}
			if len(notifier.calls) != 0 {
				t.Errorf("IndexNow calls = %v", notifier.calls)
			}
		})
	}
}

func TestDashboardUpdateArticleWritesOnlyDefinedFields(t *testing.T) {
	handler, _, notifier := adminServer(t)
	response := adminRequest(t, handler, notifier, http.MethodPut, "/articles/303", `{"title":"Draft Updated","meta_description":null}`)
	expectFields(t, articleFields(t, response), map[string]any{
		"title": "Draft Updated", "slug": "synthetic-draft", "excerpt": "Draft excerpt", "meta_description": nil,
		"source": "Synthetic Wire", "source_url": "https://news.example.invalid/draft", "author_id": float64(201),
		"status": "draft", "published_at": nil, "created_at": "2026-09-17T00:00:00.000Z", "updated_at": "2026-09-20 12:00:00",
	})
	cleared := adminRequest(t, handler, notifier, http.MethodPut, "/articles/303", `{"source_url":""}`)
	expectFields(t, articleFields(t, cleared), map[string]any{"source_url": nil, "source": "Synthetic Wire"})
	padded := adminRequest(t, handler, notifier, http.MethodPut, "/articles/%20303", `{"title":"Padded"}`)
	expectFields(t, articleFields(t, padded), map[string]any{"id": float64(303), "title": "Padded"})
	if len(notifier.calls) != 0 {
		t.Errorf("draft updates notified IndexNow: %v", notifier.calls)
	}
}

func TestDashboardUpdatePublishTransitionsMatchNode(t *testing.T) {
	for _, tt := range []struct {
		name, target, body string
		fields             map[string]any
		indexNow           [][]string
	}{
		{"empty update of a published article reassigns the editorial author", "/articles/305", `{}`,
			map[string]any{"author_id": float64(201), "published_at": "2026-09-01T00:00:00.000Z", "updated_at": "2026-09-20 12:00:00"},
			[][]string{{"approved-published"}}},
		{"publishing a valid draft stamps published_at", "/articles/303",
			jsonObject(map[string]any{"title": validTitle, "excerpt": validExcerpt, "content": validContent, "status": "published",
				"source": "TechCrunch", "source_url": "https://techcrunch.com/2026/09/20/draft"}),
			map[string]any{"status": "published", "published_at": "2026-09-20T12:00:00.000Z", "author_id": float64(201), "source_url": "https://techcrunch.com/2026/09/20/draft"},
			[][]string{{"synthetic-draft"}}},
		{"unpublishing notifies the old URL and keeps the author", "/articles/305", `{"status":"draft"}`,
			map[string]any{"status": "draft", "author_id": float64(202), "published_at": "2026-09-01T00:00:00.000Z"},
			[][]string{{"approved-published"}}},
		{"renaming a published article notifies both URLs", "/articles/305", `{"slug":"renamed"}`,
			map[string]any{"slug": "renamed", "published_at": "2026-09-01T00:00:00.000Z"},
			[][]string{{"approved-published", "renamed"}}},
		{"re-sending status published re-stamps published_at", "/articles/305", `{"status":"published"}`,
			map[string]any{"published_at": "2026-09-20T12:00:00.000Z"},
			[][]string{{"approved-published"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler, _, notifier := adminServer(t)
			response := adminRequest(t, handler, notifier, http.MethodPut, tt.target, tt.body)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d %s", response.Code, response.Body.String())
			}
			expectFields(t, articleFields(t, response), tt.fields)
			if !reflect.DeepEqual(notifier.calls, tt.indexNow) {
				t.Errorf("IndexNow calls = %v, want %v", notifier.calls, tt.indexNow)
			}
			if notifier.bodyAtNotify[0] != response.Body.Len() {
				t.Error("IndexNow was notified before the response body was written")
			}
		})
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test -p 2 ./internal/content/ -run DashboardUpdate`
Expected: FAIL with `405` responses (e.g. `response = 405 , want 404 {"error":"Article not found"}`).

- [ ] **Step 3: In `internal/content/admin_articles.go` add `"errors"` to the import block (after `"context"`) and append:**

```go
// UpdateArticle ports dashboard.ts:316-462: a partial update in which every
// defined property is written (null included), with the publishing policy
// applied to the merged next state.
func (s *AdminService) UpdateArticle(ctx context.Context, id string, body jsonbody.Object) (ArticleChange, error) {
	var change ArticleChange
	err := s.store.inAdminTx(ctx, func(tx adminTx) error {
		existing, err := tx.storedArticle(ctx, id)
		if errors.Is(err, ErrNotFound) {
			return articleNotFound()
		}
		if err != nil {
			return err
		}

		sourceURL := body.Get("source_url")
		normalizedSourceURL := existing.SourceURL
		if sourceURL.Defined() {
			if sourceURL.IsNull() || sourceURL.Is("") {
				normalizedSourceURL = nil
			} else {
				normalized, err := NormalizeSourceURL(sourceURL.JSString())
				if err != nil {
					return badRequest("Source URL is invalid")
				}
				normalizedSourceURL = &normalized
			}
		}

		existingSource := jsonbody.Null()
		if existing.Source != nil {
			existingSource = jsonbody.String(*existing.Source)
		}
		status, categoryID := body.Get("status"), body.Get("category_id")
		nextTitle := body.Get("title").Or(jsonbody.String(existing.Title))
		nextSlug := body.Get("slug").Or(jsonbody.String(existing.Slug))
		nextExcerpt := body.Get("excerpt").Or(jsonbody.String(existing.Excerpt))
		nextContent := body.Get("content").Or(jsonbody.String(existing.Content))
		nextSource := body.Get("source").Or(existingSource)
		nextPublished := status.Or(jsonbody.String(existing.Status)).Is("published")

		var nextCategoryID any = existing.CategoryID
		if !categoryID.Nullish() {
			if nextCategoryID, err = categoryID.Bind(); err != nil {
				return fmt.Errorf("bind category_id: %w", err)
			}
		}
		if err := checkCategory(ctx, tx, nextCategoryID); err != nil {
			return err
		}

		if nextPublished {
			if !nextSource.Truthy() || normalizedSourceURL == nil {
				return badRequest("Published articles require source and source_url")
			}
			errs := validatePublishedArticle(nextTitle.StringOrEmpty(), nextExcerpt.StringOrEmpty(), nextContent.StringOrEmpty(), nextSource, *normalizedSourceURL)
			if len(errs) > 0 {
				return policyFailure(errs)
			}
		}

		boundSlug, err := nextSlug.Bind()
		if err != nil {
			return fmt.Errorf("bind slug: %w", err)
		}
		duplicate, err := tx.articleConflict(ctx, &id, boundSlug, normalizedSourceURL)
		if err != nil {
			return err
		}
		if duplicate {
			return duplicateArticle()
		}

		now := s.now().UTC()
		publishedAt := body.Get("published_at")
		if status.Is("published") && !publishedAt.Truthy() {
			publishedAt = jsonbody.String(now.Format(javaScriptISO))
		}
		source := body.Get("source")
		if source.Defined() && !source.Truthy() {
			source = jsonbody.Null() // source || null
		}

		type field struct {
			column string
			value  jsonbody.Value
		}
		fields := make([]field, 0, 11)
		for _, candidate := range []field{
			{"title", body.Get("title")}, {"slug", body.Get("slug")}, {"excerpt", body.Get("excerpt")},
			{"content", body.Get("content")}, {"featured_image", body.Get("featured_image")},
			{"category_id", categoryID}, {"status", status}, {"published_at", publishedAt},
			{"meta_title", body.Get("meta_title")}, {"meta_description", body.Get("meta_description")},
			{"source", source},
		} {
			if candidate.value.Defined() {
				fields = append(fields, candidate)
			}
		}
		var authorID int64
		if nextPublished {
			if authorID, err = requireEditorialAuthor(ctx, tx); err != nil {
				return err
			}
		}
		if len(fields) == 0 && !sourceURL.Defined() && !nextPublished {
			return badRequest("No fields to update")
		}

		assignments := make([]assignment, 0, len(fields)+3)
		for _, f := range fields {
			value, err := f.value.Bind()
			if err != nil {
				return fmt.Errorf("bind %s: %w", f.column, err)
			}
			assignments = append(assignments, assignment{f.column, value})
		}
		if sourceURL.Defined() {
			assignments = append(assignments, assignment{"source_url", nullableString(normalizedSourceURL)})
		}
		if nextPublished {
			assignments = append(assignments, assignment{"author_id", authorID})
		}
		assignments = append(assignments, assignment{"updated_at", now.Format(sqliteTimestamp)})
		if err := tx.updateRow(ctx, "articles", id, assignments); err != nil {
			return err
		}
		if change.Article, err = tx.article(ctx, id); err != nil {
			return err
		}

		var slugs []string
		if existing.Status == "published" {
			slugs = append(slugs, existing.Slug)
		}
		if nextPublished {
			slugs = append(slugs, nextSlug.JSString())
		}
		change.IndexNowSlugs = uniqueStrings(slugs)
		return nil
	})
	if err != nil {
		return ArticleChange{}, err
	}
	return change, nil
}
```

- [ ] **Step 4: In `internal/content/http_admin.go` replace the `adminService` interface with**

```go
type adminService interface {
	ListArticles(context.Context, DashboardQuery) (Page, error)
	GetArticle(context.Context, string) (Article, error)
	CreateArticle(context.Context, jsonbody.Object) (ArticleChange, error)
	UpdateArticle(context.Context, string, jsonbody.Object) (ArticleChange, error)
	ListCategories(context.Context) ([]CategoryWithCount, error)
}
```

**replace `Mount` with**

```go
// Mount registers the routes relative to /api/dashboard. The caller owns
// authentication: every route here must sit behind RequireAuth.
func (h *AdminHandler) Mount(router chi.Router) {
	router.Get("/articles", h.listArticles)
	router.Get("/articles/{id}", h.getArticle)
	router.Post("/articles", h.createArticle)
	router.Put("/articles/{id}", h.updateArticle)
	router.Get("/categories", h.listCategories)
}
```

**and append:**

```go
func (h *AdminHandler) updateArticle(w http.ResponseWriter, r *http.Request) {
	body, ok := h.decode(w, r)
	if !ok {
		return
	}
	change, err := h.service.UpdateArticle(r.Context(), chi.URLParam(r, "id"), body)
	if err != nil {
		h.fail(w, r, "update dashboard article", err)
		return
	}
	writeJSON(w, http.StatusOK, articleEnvelope{change.Article})
	h.notify(change.IndexNowSlugs)
}
```

- [ ] **Step 5: Run it and confirm it passes**

Run: `gofmt -l internal/content; go vet -p 2 ./internal/content/ && go test -p 2 ./internal/content/`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/content/admin_articles.go internal/content/http_admin.go internal/content/admin_update_test.go
git commit -m "Port dashboard article updates with partial writes and publish transitions"
```

---

### Task 6: Delete article

**Node:** `:464-479`. Reads `slug, status` (may be absent), deletes, 404 if nothing was deleted, otherwise `{"success":true}` and IndexNow `[slug]` when the article was published. On the Go schema, `candidates.article_id` is `ON DELETE SET NULL`. The test pins that the delete succeeds and unlinks the candidate.

**Files:** Modify `internal/content/admin_articles.go`, `internal/content/http_admin.go`; Create `internal/content/admin_delete_test.go`

- [ ] **Step 1: Write the failing test** at `internal/content/admin_delete_test.go`:

```go
package content_test

import (
	"net/http"
	"reflect"
	"testing"
)

func TestDashboardDeleteArticleMatchesNode(t *testing.T) {
	for _, tt := range []struct {
		name, target string
		status       int
		body         string
		remaining    int
		indexNow     [][]string
	}{
		{"published article notifies its URL", "/articles/305", 200, `{"success":true}`, 3, [][]string{{"approved-published"}}},
		{"draft article does not notify", "/articles/303", 200, `{"success":true}`, 3, nil},
		{"missing article", "/articles/999", 404, `{"error":"Article not found"}`, 4, nil},
		{"non-numeric id", "/articles/abc", 404, `{"error":"Article not found"}`, 4, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler, db, notifier := adminServer(t)
			expectBody(t, adminRequest(t, handler, notifier, http.MethodDelete, tt.target, ""), tt.status, tt.body)
			if remaining := countRows(t, db, `SELECT COUNT(*) FROM articles`); remaining != tt.remaining {
				t.Errorf("articles = %d, want %d", remaining, tt.remaining)
			}
			if !reflect.DeepEqual(notifier.calls, tt.indexNow) {
				t.Errorf("IndexNow calls = %v, want %v", notifier.calls, tt.indexNow)
			}
		})
	}
}

func TestDashboardDeleteArticleUnlinksNewsroomCandidates(t *testing.T) {
	handler, db, notifier := adminServer(t)
	if _, err := db.Exec(`INSERT INTO candidates (source_url, source_name, feed_url, title, discovered_at, status, article_id, published_at, updated_at)
		VALUES ('https://techcrunch.com/2026/09/01/model', 'TechCrunch', 'https://techcrunch.com/feed/', 'T', '2026-09-01T00:00:00Z', 'published', 305, '2026-09-01T00:00:00Z', '2026-09-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	expectBody(t, adminRequest(t, handler, notifier, http.MethodDelete, "/articles/305", ""), http.StatusOK, `{"success":true}`)
	if linked := countRows(t, db, `SELECT COUNT(*) FROM candidates WHERE article_id IS NOT NULL`); linked != 0 {
		t.Errorf("candidates still linked to the deleted article: %d", linked)
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test -p 2 ./internal/content/ -run DashboardDelete`
Expected: FAIL, `response = 405 , want 200 {"success":true}`.

- [ ] **Step 3: Append to `internal/content/admin_articles.go`:**

```go
// DeleteArticle ports dashboard.ts:464-479.
func (s *AdminService) DeleteArticle(ctx context.Context, id string) (ArticleChange, error) {
	var change ArticleChange
	err := s.store.inAdminTx(ctx, func(tx adminTx) error {
		slug, status, found, err := tx.articleSlugStatus(ctx, id)
		if err != nil {
			return err
		}
		deleted, err := tx.deleteRow(ctx, "articles", id)
		if err != nil {
			return err
		}
		if !deleted {
			return articleNotFound()
		}
		if found && status == "published" {
			change.IndexNowSlugs = []string{slug}
		}
		return nil
	})
	if err != nil {
		return ArticleChange{}, err
	}
	return change, nil
}
```

- [ ] **Step 4: In `internal/content/http_admin.go` replace the `adminService` interface with**

```go
type adminService interface {
	ListArticles(context.Context, DashboardQuery) (Page, error)
	GetArticle(context.Context, string) (Article, error)
	CreateArticle(context.Context, jsonbody.Object) (ArticleChange, error)
	UpdateArticle(context.Context, string, jsonbody.Object) (ArticleChange, error)
	DeleteArticle(context.Context, string) (ArticleChange, error)
	ListCategories(context.Context) ([]CategoryWithCount, error)
}
```

**replace `Mount` with**

```go
// Mount registers the routes relative to /api/dashboard. The caller owns
// authentication: every route here must sit behind RequireAuth.
func (h *AdminHandler) Mount(router chi.Router) {
	router.Get("/articles", h.listArticles)
	router.Get("/articles/{id}", h.getArticle)
	router.Post("/articles", h.createArticle)
	router.Put("/articles/{id}", h.updateArticle)
	router.Delete("/articles/{id}", h.deleteArticle)
	router.Get("/categories", h.listCategories)
}
```

**and append:**

```go
func (h *AdminHandler) deleteArticle(w http.ResponseWriter, r *http.Request) {
	change, err := h.service.DeleteArticle(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, "delete dashboard article", err)
		return
	}
	writeJSON(w, http.StatusOK, successEnvelope{true})
	h.notify(change.IndexNowSlugs)
}
```

- [ ] **Step 5: Run it and confirm it passes**

Run: `gofmt -l internal/content; go vet -p 2 ./internal/content/ && go test -p 2 ./internal/content/`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/content/admin_articles.go internal/content/http_admin.go internal/content/admin_delete_test.go
git commit -m "Port dashboard article deletion with IndexNow notice for published articles"
```

---

### Task 7: Category create, update, delete

**Node:** `:493-573`. Create: `name`/`slug` truthy (400) → insert `description||""`, `color||"#6366f1"` (UNIQUE violation = 500) → 201. Update: exists (404) → every defined field among name, slug, description, color → none gives 400 `No fields to update` → 200 with the row. Delete: article count > 0 gives 400 → delete → 0 rows gives 404 → `{"success":true}`.

**Files:** Create `internal/content/admin_categories.go`, `internal/content/admin_categories_test.go`; Modify `internal/content/http_admin.go`

- [ ] **Step 1: Write the failing test** at `internal/content/admin_categories_test.go`:

```go
package content_test

import (
	"net/http"
	"testing"
)

// Expected statuses and bodies were recorded from the Node server on the same seed.
func TestDashboardCategoryMutationsMatchNode(t *testing.T) {
	for _, tt := range []struct {
		name, method, target, body string
		status                     int
		want                       string
	}{
		{"create requires name and slug", "POST", "/categories", `{"slug":"x"}`, 400, `{"error":"Name and slug are required"}`},
		{"create applies defaults", "POST", "/categories", `{"name":"New","slug":"new"}`, 201, `{"category":{"id":105,"name":"New","slug":"new","description":"","color":"#6366f1"}}`},
		{"create duplicate slug is a 500", "POST", "/categories", `{"name":"Dup","slug":"synthetic-ai"}`, 500, internalError},
		{"update one field", "PUT", "/categories/101", `{"color":"#000000"}`, 200, `{"category":{"id":101,"name":"Synthetic AI","slug":"synthetic-ai","description":"Synthetic artificial intelligence fixtures","color":"#000000"}}`},
		{"update with no fields", "PUT", "/categories/101", `{}`, 400, `{"error":"No fields to update"}`},
		{"update missing category", "PUT", "/categories/999", `{"name":"x"}`, 404, `{"error":"Category not found"}`},
		{"update null name is a 500", "PUT", "/categories/101", `{"name":null}`, 500, internalError},
		{"update duplicate slug is a 500", "PUT", "/categories/102", `{"slug":"synthetic-ai"}`, 500, internalError},
		{"delete category in use", "DELETE", "/categories/101", ``, 400, `{"error":"Cannot delete category with existing articles. Reassign articles first."}`},
		{"delete missing category", "DELETE", "/categories/999", ``, 404, `{"error":"Category not found"}`},
		{"delete unused category", "DELETE", "/categories/104", ``, 200, `{"success":true}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler, _, notifier := adminServer(t)
			expectBody(t, adminRequest(t, handler, notifier, tt.method, tt.target, tt.body), tt.status, tt.want)
		})
	}
}

func TestDashboardCategoryFailuresLeaveDataUnchanged(t *testing.T) {
	handler, db, notifier := adminServer(t)
	adminRequest(t, handler, notifier, http.MethodPut, "/categories/102", `{"name":"Renamed","slug":"synthetic-ai"}`)
	if renamed := countRows(t, db, `SELECT COUNT(*) FROM categories WHERE name = 'Renamed'`); renamed != 0 {
		t.Errorf("failed update partially applied")
	}
	adminRequest(t, handler, notifier, http.MethodDelete, "/categories/101", "")
	if remaining := countRows(t, db, `SELECT COUNT(*) FROM categories`); remaining != 4 {
		t.Errorf("categories = %d, want 4", remaining)
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test -p 2 ./internal/content/ -run DashboardCategory`
Expected: FAIL, `response = 405 , want 400 {"error":"Name and slug are required"}`.

- [ ] **Step 3: Create `internal/content/admin_categories.go`** (`ListCategories` already lives in `admin_service.go`):

```go
package content

import (
	"context"
	"fmt"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/jsonbody"
)

// CreateCategory ports dashboard.ts:493-511. A duplicate slug violates the
// UNIQUE constraint and surfaces as a 500, exactly like Node.
func (s *AdminService) CreateCategory(ctx context.Context, body jsonbody.Object) (Category, error) {
	name, slug := body.Get("name"), body.Get("slug")
	if !name.Truthy() || !slug.Truthy() {
		return Category{}, badRequest("Name and slug are required")
	}
	description, color := body.Get("description"), body.Get("color")
	var category Category
	err := s.store.inAdminTx(ctx, func(tx adminTx) error {
		values, err := bindAll(name.Bind, slug.Bind,
			func() (any, error) { return orEmpty(description) },
			func() (any, error) {
				if color.Truthy() {
					return color.Bind()
				}
				return "#6366f1", nil
			})
		if err != nil {
			return fmt.Errorf("bind category insert: %w", err)
		}
		id, err := tx.insertCategory(ctx, values[0], values[1], values[2], values[3])
		if err != nil {
			return err
		}
		category, err = tx.category(ctx, id)
		return err
	})
	if err != nil {
		return Category{}, err
	}
	return category, nil
}

// UpdateCategory ports dashboard.ts:513-548: every defined property is written,
// so null values and duplicate slugs fail in SQLite and surface as a 500.
func (s *AdminService) UpdateCategory(ctx context.Context, id string, body jsonbody.Object) (Category, error) {
	var category Category
	err := s.store.inAdminTx(ctx, func(tx adminTx) error {
		exists, err := tx.categoryExists(ctx, id)
		if err != nil {
			return err
		}
		if !exists {
			return categoryNotFound()
		}
		assignments := make([]assignment, 0, 4)
		for _, column := range []string{"name", "slug", "description", "color"} {
			value := body.Get(column)
			if !value.Defined() {
				continue
			}
			bound, err := value.Bind()
			if err != nil {
				return fmt.Errorf("bind %s: %w", column, err)
			}
			assignments = append(assignments, assignment{column, bound})
		}
		if len(assignments) == 0 {
			return badRequest("No fields to update")
		}
		if err := tx.updateRow(ctx, "categories", id, assignments); err != nil {
			return err
		}
		category, err = tx.category(ctx, id)
		return err
	})
	if err != nil {
		return Category{}, err
	}
	return category, nil
}

// DeleteCategory ports dashboard.ts:550-573. The in-use check runs before the
// existence check, so a missing id with no articles is a 404.
func (s *AdminService) DeleteCategory(ctx context.Context, id string) error {
	return s.store.inAdminTx(ctx, func(tx adminTx) error {
		count, err := tx.categoryArticleCount(ctx, id)
		if err != nil {
			return err
		}
		if count > 0 {
			return badRequest("Cannot delete category with existing articles. Reassign articles first.")
		}
		deleted, err := tx.deleteRow(ctx, "categories", id)
		if err != nil {
			return err
		}
		if !deleted {
			return categoryNotFound()
		}
		return nil
	})
}
```

- [ ] **Step 4: In `internal/content/http_admin.go` replace the `adminService` interface with**

```go
type adminService interface {
	ListArticles(context.Context, DashboardQuery) (Page, error)
	GetArticle(context.Context, string) (Article, error)
	CreateArticle(context.Context, jsonbody.Object) (ArticleChange, error)
	UpdateArticle(context.Context, string, jsonbody.Object) (ArticleChange, error)
	DeleteArticle(context.Context, string) (ArticleChange, error)
	ListCategories(context.Context) ([]CategoryWithCount, error)
	CreateCategory(context.Context, jsonbody.Object) (Category, error)
	UpdateCategory(context.Context, string, jsonbody.Object) (Category, error)
	DeleteCategory(context.Context, string) error
}
```

**replace `Mount` with**

```go
// Mount registers the routes relative to /api/dashboard. The caller owns
// authentication: every route here must sit behind RequireAuth.
func (h *AdminHandler) Mount(router chi.Router) {
	router.Get("/articles", h.listArticles)
	router.Get("/articles/{id}", h.getArticle)
	router.Post("/articles", h.createArticle)
	router.Put("/articles/{id}", h.updateArticle)
	router.Delete("/articles/{id}", h.deleteArticle)
	router.Get("/categories", h.listCategories)
	router.Post("/categories", h.createCategory)
	router.Put("/categories/{id}", h.updateCategory)
	router.Delete("/categories/{id}", h.deleteCategory)
}
```

**and append:**

```go
func (h *AdminHandler) createCategory(w http.ResponseWriter, r *http.Request) {
	body, ok := h.decode(w, r)
	if !ok {
		return
	}
	category, err := h.service.CreateCategory(r.Context(), body)
	if err != nil {
		h.fail(w, r, "create dashboard category", err)
		return
	}
	writeJSON(w, http.StatusCreated, categoryEnvelope{category})
}

func (h *AdminHandler) updateCategory(w http.ResponseWriter, r *http.Request) {
	body, ok := h.decode(w, r)
	if !ok {
		return
	}
	category, err := h.service.UpdateCategory(r.Context(), chi.URLParam(r, "id"), body)
	if err != nil {
		h.fail(w, r, "update dashboard category", err)
		return
	}
	writeJSON(w, http.StatusOK, categoryEnvelope{category})
}

func (h *AdminHandler) deleteCategory(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteCategory(r.Context(), chi.URLParam(r, "id")); err != nil {
		h.fail(w, r, "delete dashboard category", err)
		return
	}
	writeJSON(w, http.StatusOK, successEnvelope{true})
}
```

- [ ] **Step 5: Run it and confirm it passes**

Run: `gofmt -l internal/content; go vet -p 2 ./internal/content/ && go test -p 2 ./internal/content/ && go test -race -p 2 ./internal/content/`
Expected: `ok` twice.

- [ ] **Step 6: Commit**

```bash
git add internal/content/admin_categories.go internal/content/http_admin.go internal/content/admin_categories_test.go
git commit -m "Port dashboard category create, update and delete"
```

---

### Task 8: `settings` capability

**Node:** `:632-696`, `db.ts:67-70`. GET returns every row in SQLite row order, with `newsletter_enabled` as `value === "true"`. PUT upserts each **defined** allowlisted key in Node's key order (booleans become `"true"`/`"false"`, other values are bound like better-sqlite3) in one transaction. Unknown keys are ignored. A non-JSON content type is treated as `{}`. The response is all rows. `Values.MarshalJSON` keeps row order, so newly inserted keys appear last, as in Node.

**Files:** Create `internal/settings/settings.go`, `internal/settings/http.go`, `internal/settings/settings_test.go`

- [ ] **Step 1: Write the failing test** at `internal/settings/settings_test.go` (Node-recorded vectors):

```go
package settings_test

import (
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/settings"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

// settingsServer creates Node's settings table (db.ts:67-70) with the rows the
// recorded Node vectors started from. The table's migration is owned by
// newsroom; the app-level test covers the migrated schema.
func settingsServer(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if _, err := db.Exec(`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES ('site_name','Synthetic TechNews'),('newsletter_enabled','true'),('social_twitter','')`); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	settings.NewHandler(settings.NewService(settings.NewSQLiteStore(db)), slog.New(slog.NewTextHandler(io.Discard, nil))).Mount(router)
	return router, db
}

func serve(t *testing.T, handler http.Handler, method, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/settings", strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	return response
}

const unchanged = `{"settings":{"site_name":"Synthetic TechNews","newsletter_enabled":true,"social_twitter":""}}`

// Expected bodies were recorded from the Node server on the same rows; keys
// keep SQLite row order, so newly inserted keys come last.
func TestSettingsMatchNode(t *testing.T) {
	for _, tt := range []struct {
		name, method, contentType, body string
		status                          int
		want                            string
	}{
		{"get", "GET", "", "", 200, unchanged},
		{"allowlisted update with boolean", "PUT", "application/json", `{"site_name":"New","newsletter_enabled":false,"ignored":"x"}`, 200,
			`{"settings":{"site_name":"New","newsletter_enabled":false,"social_twitter":""}}`},
		{"number is stored as REAL text", "PUT", "application/json", `{"site_description":5}`, 200,
			`{"settings":{"site_name":"Synthetic TechNews","newsletter_enabled":true,"social_twitter":"","site_description":"5.0"}}`},
		{"one-element array spreads", "PUT", "application/json", `{"newsletter_provider":["resend"]}`, 200,
			`{"settings":{"site_name":"Synthetic TechNews","newsletter_enabled":true,"social_twitter":"","newsletter_provider":"resend"}}`},
		{"non-json body is ignored", "PUT", "text/plain", `{"site_name":"T"}`, 200, unchanged},
		{"empty body", "PUT", "application/json", ``, 200, unchanged},
		{"null value rolls back the whole update", "PUT", "application/json", `{"site_name":"Z","social_twitter":null}`, 500, `{"error":"Internal server error"}`},
		{"object value", "PUT", "application/json", `{"site_description":{"a":1}}`, 500, `{"error":"Internal server error"}`},
		{"malformed json", "PUT", "application/json", `{"site_name":`, 400, `{"error":"Invalid request body"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler, _ := settingsServer(t)
			response := serve(t, handler, tt.method, tt.contentType, tt.body)
			if response.Code != tt.status || response.Body.String() != tt.want {
				t.Fatalf("%s = %d %s, want %d %s", tt.name, response.Code, response.Body.String(), tt.status, tt.want)
			}
			if tt.status != http.StatusOK {
				if after := serve(t, handler, "GET", "", ""); after.Body.String() != unchanged {
					t.Errorf("failed update changed settings: %s", after.Body.String())
				}
			}
		})
	}
}

func TestSettingsEmptyTableAndDatabaseFailure(t *testing.T) {
	handler, db := settingsServer(t)
	if _, err := db.Exec(`DELETE FROM settings`); err != nil {
		t.Fatal(err)
	}
	if response := serve(t, handler, "GET", "", ""); response.Body.String() != `{"settings":{}}` {
		t.Fatalf("empty settings = %s", response.Body.String())
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "PUT"} {
		if response := serve(t, handler, method, "application/json", `{}`); response.Code != 500 || response.Body.String() != `{"error":"Internal server error"}` {
			t.Errorf("%s after close = %d %s", method, response.Code, response.Body.String())
		}
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test -p 2 ./internal/settings/`
Expected: build failure: `no non-test Go files in .../internal/settings`.

- [ ] **Step 3: Create `internal/settings/settings.go`**

```go
// Package settings ports the dashboard site-settings endpoints
// (GET/PUT /api/dashboard/settings). It owns no migration: the settings
// table is created, with Node's exact schema, by newsroom migration 3
// (CREATE TABLE IF NOT EXISTS), and newsroom stores its own "newsroom.*"
// keys in the same table.
package settings

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/jsonbody"
)

// allowedKeys is Node's validKeys allowlist, in the order Node upserts them.
var allowedKeys = [...]string{
	"site_name",
	"site_description",
	"social_twitter",
	"social_linkedin",
	"social_github",
	"newsletter_enabled",
	"newsletter_provider",
	"newsletter_webhook_url",
}

// Entry is one settings row.
type Entry struct {
	Key   string
	Value string
}

// Values is every settings row in SQLite's row order. It encodes as one JSON
// object in that order, with newsletter_enabled decoded to a boolean
// (value === "true") and every other value kept as its stored string.
type Values []Entry

func (v Values) MarshalJSON() ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	for i, entry := range v {
		if i > 0 {
			buffer.WriteByte(',')
		}
		key, err := json.Marshal(entry.Key)
		if err != nil {
			return nil, err
		}
		buffer.Write(key)
		buffer.WriteByte(':')
		if entry.Key == "newsletter_enabled" {
			buffer.WriteString(strconv.FormatBool(entry.Value == "true"))
			continue
		}
		value, err := json.Marshal(entry.Value)
		if err != nil {
			return nil, err
		}
		buffer.Write(value)
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

// Update is one allowlisted upsert, already bound like better-sqlite3 would.
type Update struct {
	Key   string
	Value any
}

type SQLiteStore struct{ db *sql.DB }

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{db: db} }

// All reads every row with Node's unordered query (SQLite row order).
func (s *SQLiteStore) All(ctx context.Context) (Values, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, fmt.Errorf("read settings: %w", err)
	}
	defer rows.Close()
	values := make(Values, 0)
	for rows.Next() {
		var entry Entry
		if err := rows.Scan(&entry.Key, &entry.Value); err != nil {
			return nil, fmt.Errorf("scan setting: %w", err)
		}
		values = append(values, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate settings: %w", err)
	}
	return values, nil
}

// Upsert applies every update in one transaction; any failure (for example a
// null value hitting NOT NULL) rolls all of them back, like Node's db.transaction.
func (s *SQLiteStore) Upsert(ctx context.Context, updates []Update) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin settings transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	for _, update := range updates {
		if _, err = tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, update.Key, update.Value); err != nil {
			return fmt.Errorf("upsert setting %s: %w", update.Key, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit settings transaction: %w", err)
	}
	return nil
}

type store interface {
	All(context.Context) (Values, error)
	Upsert(context.Context, []Update) error
}

type Service struct{ store store }

func NewService(store store) *Service { return &Service{store: store} }

// Get ports dashboard.ts:632-648.
func (s *Service) Get(ctx context.Context) (Values, error) {
	values, err := s.store.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("get settings: %w", err)
	}
	return values, nil
}

// Update ports dashboard.ts:650-696: every defined allowlisted key is
// upserted (booleans as "true"/"false", other values bound like
// better-sqlite3), unknown keys are ignored, and all rows are returned.
func (s *Service) Update(ctx context.Context, body jsonbody.Object) (Values, error) {
	updates := make([]Update, 0, len(allowedKeys))
	for _, key := range allowedKeys {
		value := body.Get(key)
		if !value.Defined() {
			continue
		}
		if b, ok := value.Bool(); ok {
			updates = append(updates, Update{Key: key, Value: strconv.FormatBool(b)})
			continue
		}
		bound, err := value.Bind()
		if err != nil {
			return nil, fmt.Errorf("bind setting %s: %w", key, err)
		}
		updates = append(updates, Update{Key: key, Value: bound})
	}
	if err := s.store.Upsert(ctx, updates); err != nil {
		return nil, fmt.Errorf("update settings: %w", err)
	}
	return s.Get(ctx)
}
```

- [ ] **Step 4: Create `internal/settings/http.go`**

```go
package settings

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/jsonbody"
)

type settingsService interface {
	Get(context.Context) (Values, error)
	Update(context.Context, jsonbody.Object) (Values, error)
}

type Handler struct {
	service settingsService
	logger  *slog.Logger
}

func NewHandler(service settingsService, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

// Mount registers the routes relative to /api/dashboard; the caller applies RequireAuth.
func (h *Handler) Mount(router chi.Router) {
	router.Get("/settings", h.get)
	router.Put("/settings", h.update)
}

type envelope struct {
	Settings Values `json:"settings"`
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	values, err := h.service.Get(r.Context())
	if err != nil {
		h.internalError(w, r, "get settings", err)
		return
	}
	writeJSON(w, http.StatusOK, envelope{values})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	body, err := jsonbody.Decode(w, r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}
	values, err := h.service.Update(r.Context(), body)
	if err != nil {
		h.internalError(w, r, "update settings", err)
		return
	}
	writeJSON(w, http.StatusOK, envelope{values})
}

func (h *Handler) internalError(w http.ResponseWriter, r *http.Request, message string, err error) {
	h.logger.ErrorContext(r.Context(), message, "error", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Internal server error"})
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

- [ ] **Step 5: Run it and confirm it passes**

Run: `gofmt -l internal/settings; go vet -p 2 ./internal/settings/ && go test -p 2 ./internal/settings/`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/settings
git commit -m "Add the settings capability with Node's allowlisted upsert and boolean serialization"
```

---

### Task 9: Non-blocking IndexNow queue for dashboard changes

Node calls `notifyIndexNow` without awaiting it and only logs the result (`:36-45`). The Go queue keeps that behavior: `Notify` returns immediately, each submission runs in the background with a 30 s bound (the client's own HTTP timeout is 15 s), failures are logged, and `Wait` drains in-flight submissions at shutdown. `INDEXNOW_ENABLED` is the single switch shared with the publisher, so a local dashboard edit never pings IndexNow.

**Files:** Create `internal/app/indexnow_queue.go`, `internal/app/indexnow_queue_test.go`

- [ ] **Step 1: Write the failing test** at `internal/app/indexnow_queue_test.go`:

```go
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
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test -p 2 ./internal/app/ -run IndexNow`
Expected: build failure: `undefined: newIndexNowQueue`, `undefined: newDashboardIndexNow`.

- [ ] **Step 3: Create `internal/app/indexnow_queue.go`**

```go
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
```

- [ ] **Step 4: Run it and confirm it passes**

Run: `go vet -p 2 ./internal/app/ && go test -p 2 ./internal/app/ && go test -race -p 2 ./internal/app/ -run IndexNow`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/app/indexnow_queue.go internal/app/indexnow_queue_test.go
git commit -m "Add a drained, non-blocking IndexNow queue for dashboard article changes"
```

---

### Task 10: Mount the authenticated dashboard group and prove contract parity

This task replays all 11 non-media dashboard operations from the canonical Node fixture in canonical order, over the same synthetic rows, at the fixture clock (`2026-09-20T12:00:00Z`). It replays the six public reads first, because `articles.getBySlug` moves `view_count` from 42 to 43 before `dashboard.articles.list` is recorded. Newsletter (fixture operations 7-14) and media (27-29) are skipped: they touch no table these operations read. The replay authenticates through the recorded `auth.login` bindings, exactly like the auth replay. The test also asserts the fixture's dashboard operation set, so a newly captured Node operation cannot go unported silently, and replays the recorded `dashboardUnknownRoute` observation (401).

The Go-migrated database also contains the two `newsroom.*` keys that migration 3 seeds. The Node capture database never had them, so the contract seed deletes them to reproduce the captured state. A separate test pins that they are returned when present (Known behavior 9).

**Files:** Create `internal/app/dashboard_contract_test.go`, `internal/app/dashboard_http_test.go`; Modify `internal/app/app.go`

- [ ] **Step 1: Write the failing contract test** at `internal/app/dashboard_contract_test.go` (it reuses `seedContractArticles`, `authBinding`, `resolveAuthSecret`, `resolveAuthOperation`, `resolveAuthResponse`, `resolveAuthTemplate`, `verifyAuthBindingVector`, `executeAuthOperation`, and `operationIndex` from the existing app tests):

```go
package app_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/contracttest"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

// dashboardContentOperations is every non-media dashboard operation in the
// canonical Node fixture, in canonical order. Media (27-29) is plan 5b.
var dashboardContentOperations = []string{
	"dashboard.articles.list", "dashboard.articles.get", "dashboard.articles.create",
	"dashboard.articles.update", "dashboard.articles.delete",
	"dashboard.categories.list", "dashboard.categories.create",
	"dashboard.categories.update", "dashboard.categories.delete",
	"dashboard.settings.get", "dashboard.settings.update",
}

// seedContractSettings reproduces the synthetic Node capture database's
// settings (apps/server/scripts/contracts/synthetic-seed.ts). The Node
// capture schema has no newsroom keys, so the rows newsroom migration 3
// seeds are removed to match the captured database state.
func seedContractSettings(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		`DELETE FROM settings WHERE key LIKE 'newsroom.%'`,
		`INSERT INTO settings (key, value) VALUES
('site_name','Synthetic TechNews'),
('site_description','Synthetic contract fixture'),
('social_twitter','https://social.example.invalid/synthetic'),
('social_linkedin',''),
('social_github','https://code.example.invalid/synthetic'),
('newsletter_enabled','true'),
('newsletter_provider','recorder'),
('newsletter_webhook_url','https://hooks.example.invalid/newsletter')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}

// dashboardApplication composes the app at the fixture's fixed clock over the
// synthetic capture data.
func dashboardApplication(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	seedContractArticles(t, db)
	seedContractSettings(t, db)
	fixed := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	cfg := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4402", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), JWTSecret: authTestSecret}
	application, err := app.NewWithDatabaseAt(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db, func() time.Time { return fixed })
	if err != nil {
		t.Fatal(err)
	}
	return application.Handler(), db
}

// contractBindings logs in through the recorded auth.login operation and
// resolves $PASSWORD, $JWT and $AUTHORIZATION exactly like the auth replay.
func contractBindings(t *testing.T, handler http.Handler, contract *contracttest.Contract) map[string]string {
	t.Helper()
	passwordBinding, err := authBinding(contract.Replay.Bindings, "$PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	password, err := resolveAuthSecret(passwordBinding, map[string]string{"CONTRACT_TEST_PASSWORD": authTestPassword})
	if err != nil {
		t.Fatal(err)
	}
	login, ok := contract.Operation("auth.login")
	if !ok {
		t.Fatal("operation auth.login missing")
	}
	resolvedLogin, err := resolveAuthOperation(login, map[string]string{"$PASSWORD": password})
	if err != nil {
		t.Fatal(err)
	}
	loginResponse := executeAuthOperation(t, handler, resolvedLogin)
	jwtBinding, err := authBinding(contract.Replay.Bindings, "$JWT")
	if err != nil {
		t.Fatal(err)
	}
	token, err := resolveAuthResponse(jwtBinding, login.OperationID, loginResponse.Body.Bytes())
	if err != nil {
		t.Fatalf("resolve $JWT: %v", err)
	}
	if err := verifyAuthBindingVector(jwtBinding, token); err != nil {
		t.Fatal(err)
	}
	bindings := map[string]string{"$PASSWORD": password, "$JWT": token}
	authorizationBinding, err := authBinding(contract.Replay.Bindings, "$AUTHORIZATION")
	if err != nil {
		t.Fatal(err)
	}
	if bindings["$AUTHORIZATION"], err = resolveAuthTemplate(authorizationBinding, bindings); err != nil {
		t.Fatal(err)
	}
	return bindings
}

func TestDashboardContentMatchesApprovedNodeContractSequence(t *testing.T) {
	handler, _ := dashboardApplication(t)
	contract, err := contracttest.Load(filepath.Join("..", "..", "contracts", "fixtures", "node-contracts.json"))
	if err != nil {
		t.Fatal(err)
	}

	var fixtureOperations []string
	for _, operation := range contract.Operations {
		if strings.HasPrefix(operation.OperationID, "dashboard.") && !strings.HasPrefix(operation.OperationID, "dashboard.media.") {
			fixtureOperations = append(fixtureOperations, operation.OperationID)
		}
	}
	if !reflect.DeepEqual(fixtureOperations, dashboardContentOperations) {
		t.Fatalf("fixture dashboard operations = %v, want %v", fixtureOperations, dashboardContentOperations)
	}

	// Canonical state: articles.getBySlug increments view_count 42 -> 43 before
	// dashboard.articles.list is recorded. Newsletter (7-14) and media (27-29)
	// operations touch no table these operations read, so they are skipped.
	previous := -1
	for _, id := range []string{"articles.list", "articles.trending", "articles.getBySlug", "articles.getById", "categories.list", "authors.list"} {
		op, _ := contract.Operation(id)
		previous = assertAfter(t, contract, id, previous)
		if err := contracttest.Replay(handler, op); err != nil {
			t.Fatalf("%s replay: %v", id, err)
		}
	}
	bindings := contractBindings(t, handler, contract)
	previous = assertAfter(t, contract, "auth.login", previous)
	for _, id := range dashboardContentOperations {
		previous = assertAfter(t, contract, id, previous)
		op, _ := contract.Operation(id)
		resolved, err := resolveAuthOperation(op, bindings)
		if err != nil {
			t.Fatalf("resolve %s: %v", id, err)
		}
		// Dashboard replay errors print only response bodies, never the JWT.
		if err := contracttest.Replay(handler, resolved); err != nil {
			t.Fatalf("%s replay: %v", id, err)
		}
	}
}

func assertAfter(t *testing.T, contract *contracttest.Contract, id string, previous int) int {
	t.Helper()
	index := operationIndex(contract.Operations, id)
	if index <= previous {
		t.Fatalf("operation %s missing or not in canonical order", id)
	}
	return index
}

func TestDashboardUnknownRouteMatchesRecordedObservation(t *testing.T) {
	handler, _ := dashboardApplication(t)
	contract, err := contracttest.Load(filepath.Join("..", "..", "contracts", "fixtures", "node-contracts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var observations struct {
		DashboardUnknownRoute struct {
			Request struct {
				Method  string            `json:"method"`
				Path    string            `json:"path"`
				Headers map[string]string `json:"headers"`
			} `json:"request"`
			Status int             `json:"status"`
			Body   json.RawMessage `json:"body"`
		} `json:"dashboardUnknownRoute"`
	}
	if err := json.Unmarshal(contract.Observations, &observations); err != nil {
		t.Fatal(err)
	}
	observed := observations.DashboardUnknownRoute
	request := httptest.NewRequest(observed.Request.Method, observed.Request.Path, nil)
	for name, value := range observed.Request.Headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var got, want any
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(observed.Body, &want); err != nil {
		t.Fatal(err)
	}
	if response.Code != observed.Status || !reflect.DeepEqual(got, want) {
		t.Fatalf("unknown dashboard route = %d %s, want %d %s", response.Code, response.Body.String(), observed.Status, observed.Body)
	}
}
```

- [ ] **Step 2: Write the failing auth/HTTP test** at `internal/app/dashboard_http_test.go`:

```go
package app_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/editorial"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

var dashboardRoutes = []struct{ method, path string }{
	{http.MethodGet, "/api/dashboard/articles"},
	{http.MethodGet, "/api/dashboard/articles/303"},
	{http.MethodPost, "/api/dashboard/articles"},
	{http.MethodPut, "/api/dashboard/articles/303"},
	{http.MethodDelete, "/api/dashboard/articles/303"},
	{http.MethodGet, "/api/dashboard/categories"},
	{http.MethodPost, "/api/dashboard/categories"},
	{http.MethodPut, "/api/dashboard/categories/101"},
	{http.MethodDelete, "/api/dashboard/categories/104"},
	{http.MethodGet, "/api/dashboard/settings"},
	{http.MethodPut, "/api/dashboard/settings"},
	{http.MethodGet, "/api/dashboard/not-a-route"},
}

// Node mounts requireAuth for the whole dashboard router (dashboard.ts:94),
// so every route, known or not, answers 401 before any other check.
func TestDashboardRoutesRequireAuthentication(t *testing.T) {
	handler, db := dashboardApplication(t)
	expired, err := editorial.NewJWT(authTestSecret, func() time.Time { return time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	stale, err := expired.Sign(editorial.Identity{ID: 201, Email: "editorial@example.invalid", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range dashboardRoutes {
		for authorization, want := range map[string]string{
			"":                 `{"error":"Authentication required"}`,
			"Basic abc":        `{"error":"Authentication required"}`,
			"Bearer not-a-jwt": `{"error":"Invalid or expired token"}`,
			"Bearer " + stale:  `{"error":"Invalid or expired token"}`,
		} {
			// A malformed body must not be parsed before authentication.
			assertAuthResponse(t, request(t, handler, route.method, route.path, `{`, authorization), http.StatusUnauthorized, want)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM articles`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("unauthenticated requests changed data: %d articles, %v", count, err)
	}
}

func TestDashboardAcceptsAnyValidTokenRegardlessOfRole(t *testing.T) {
	handler, _ := dashboardApplication(t)
	tokens, err := editorial.NewJWT(authTestSecret, func() time.Time { return time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	// Node checks only the signature and expiry: an editor, even one whose
	// author row no longer exists, may use every dashboard route.
	token, err := tokens.Sign(editorial.Identity{ID: 999, Email: "gone@example.invalid", Role: "editor"})
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, handler, http.MethodDelete, "/api/dashboard/categories/104", "", "Bearer "+token)
	assertAuthResponse(t, response, http.StatusOK, `{"success":true}`)
	unknown := request(t, handler, http.MethodGet, "/api/dashboard/not-a-route", "", "Bearer "+token)
	assertAuthResponse(t, unknown, http.StatusNotFound, `{"error":"Not found"}`)
}

func TestDashboardSettingsExposeNewsroomKeysFromTheSharedTable(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	cfg := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4402", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), JWTSecret: authTestSecret}
	application, err := app.NewWithDatabaseAt(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db, func() time.Time { return fixed })
	if err != nil {
		t.Fatal(err)
	}
	tokens, _ := editorial.NewJWT(authTestSecret, func() time.Time { return fixed })
	token, _ := tokens.Sign(editorial.Identity{ID: 1, Email: "editor@example.invalid", Role: "admin"})
	// Node's SELECT key, value FROM settings has no filter, so a fresh Go
	// database returns only the keys newsroom migration 3 seeds.
	response := request(t, application.Handler(), http.MethodGet, "/api/dashboard/settings", "", "Bearer "+token)
	assertAuthResponse(t, response, http.StatusOK,
		`{"settings":{"newsroom.publish_delay_min_minutes":"30","newsroom.publish_delay_max_minutes":"40"}}`)
}
```

- [ ] **Step 3: Run them and confirm they fail**

Run: `go test -p 2 ./internal/app/ -run Dashboard`
Expected: FAIL, e.g. `dashboard.articles.list replay: replay dashboard.articles.list: status = 404, want 200` and `unknown dashboard route = 404 {"error":"Not found"}, want 401`.

- [ ] **Step 4: Wire the group in `internal/app/app.go`.** Make these four edits.

Add the import (after the `publisher` import):

```go
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/settings"
```

Add a field to `App` (after `feedCollector *collector.Collector`):

```go
	// drains run after the server and background tasks stop, to finish
	// fire-and-forget work such as dashboard IndexNow submissions.
	drains []func()
```

Replace this block:

```go
	newsroomHandler := newsroom.NewHandler(newsroomService, newsroomCollector, logger)
	handler := httpserver.NewRouter(logger, func(router chi.Router) {
		health.MountPublic(router)
		articles.MountPublic(router)
		categories.MountPublic(router)
		authors.MountPublic(router)
		auth.Mount(router)
		newsroomHandler.Mount(router, auth.RequireAuth)
	})
	server := httpserver.NewServer(cfg.Address, handler, logger)
	application := &App{address: cfg.Address, handler: handler, server: server}
```

with:

```go
	newsroomHandler := newsroom.NewHandler(newsroomService, newsroomCollector, logger)
	dashboardIndexNow, drainIndexNow := newDashboardIndexNow(cfg, logger)
	dashboardContent := content.NewAdminHandler(content.NewAdminService(contentStore, now), dashboardIndexNow, logger)
	dashboardSettings := settings.NewHandler(settings.NewService(settings.NewSQLiteStore(db)), logger)
	handler := httpserver.NewRouter(logger, func(router chi.Router) {
		health.MountPublic(router)
		articles.MountPublic(router)
		categories.MountPublic(router)
		authors.MountPublic(router)
		auth.Mount(router)
		newsroomHandler.Mount(router, auth.RequireAuth)
		// Node guards every dashboard path, including unknown ones, with
		// requireAuth before routing (dashboard.ts:94).
		router.Route("/dashboard", func(dashboard chi.Router) {
			dashboard.Use(auth.RequireAuth)
			dashboardContent.Mount(dashboard)
			dashboardSettings.Mount(dashboard)
		})
	})
	server := httpserver.NewServer(cfg.Address, handler, logger)
	application := &App{address: cfg.Address, handler: handler, server: server, drains: []func(){drainIndexNow}}
```

In `Run`, replace:

```go
	if a.feedCollector != nil {
		a.feedCollector.Wait()
	}
	return err
}
```

with:

```go
	if a.feedCollector != nil {
		a.feedCollector.Wait()
	}
	for _, drain := range a.drains {
		drain()
	}
	return err
}
```

- [ ] **Step 5: Run them and confirm they pass, then run the whole module**

Run: `gofmt -l .; go vet -p 2 ./... && go test -p 2 ./internal/app/ -run Dashboard -v`
Expected: `TestDashboardContentMatchesApprovedNodeContractSequence`, `TestDashboardUnknownRouteMatchesRecordedObservation`, `TestDashboardRoutesRequireAuthentication`, `TestDashboardAcceptsAnyValidTokenRegardlessOfRole`, `TestDashboardSettingsExposeNewsroomKeysFromTheSharedTable` PASS.

Run: `go build -p 2 ./... && go test -p 2 ./... && go test -race -p 2 ./internal/jsonbody/ ./internal/content/ ./internal/settings/ ./internal/app/ && ./scripts/sync-contracts.sh --check && go test -p 2 ./internal/contracttest && go test -p 2 ./internal/app -run Contract`
Expected: everything `ok`; `contract mirror matches canonical Node fixture`. (`make contracts-check` runs the same checks without `-p 2`; prefer the explicit commands.)

- [ ] **Step 6: Commit**

```bash
git add internal/app/app.go internal/app/dashboard_contract_test.go internal/app/dashboard_http_test.go
git commit -m "Mount the authenticated dashboard content and settings routes and replay their Node contracts"
```

---

### Task 11: Documentation and final verification

**Files:** Modify `README.md`, `../../docs/plans/2026-09-20-go-api-migration.md`

- [ ] **Step 1: Update `README.md`.**

Replace the paragraph that starts with `Tasks 5 through 8 currently provide` with:

```markdown
Tasks 5 through 10 currently provide the four public article reads, public category and author listings, compatible login, current-user, and logout endpoints, a pure Go port of the retained publishing policy, and the authenticated dashboard content administration (dashboard article list/get/create/update/delete, category list/create/update/delete, and site settings get/put). This is a migration slice, not a claim of production or cutover readiness; media, newsletter, and cutover verification are still pending.
```

Replace the sentence `Dashboard publication routes and policy integration remain part of a later migration task.` with:

```markdown
The dashboard article routes apply it exactly as Node does: only to articles whose next status is `published`, including the source/source-URL match.
```

Replace `audibly mounts article, category, author, and authentication routes around a caller-owned database.` with:

```markdown
audibly mounts article, category, author, authentication, newsroom, and authenticated `/api/dashboard` content and settings routes around a caller-owned database.
```

In the configuration table, replace the `INDEXNOW_ENABLED` purpose with:

```markdown
Submits article URLs to IndexNow: each publisher-published article, and dashboard publishes, unpublishes, published-slug renames, and deletions of published articles. Off by default, so local runs never ping IndexNow for an article that only exists in the dev database
```

Add this section before `## Newsroom (editorial queue)`:

```markdown
## Dashboard content administration

`internal/content` (`AdminService`, `AdminHandler`) and `internal/settings` port every non-media route of `apps/server/src/routes/dashboard.ts`. All of them sit in one chi group, `/api/dashboard`, behind `RequireAuth`; like Node, unknown dashboard paths answer `401` without a token, and any valid token may use every route (Node has no roles). `internal/jsonbody` reproduces `express.json()` and the JavaScript/better-sqlite3 value semantics the Node handlers rely on (undefined vs `null`, truthiness, `String()` coercion, numbers binding as REAL). Each mutation runs in one SQLite transaction. IndexNow notifications for dashboard changes are queued after the response, never block it, are drained on shutdown, and only run with `INDEXNOW_ENABLED=1`.

Node behaviors that are kept for parity although they look wrong are listed, with tests, in `docs/superpowers/plans/2026-09-24-go-admin-content.md` ("Known Node behaviors kept"). The most visible: saving settings with a `null` value (which the dashboard sends for blank social links) fails with `500`, duplicate category slugs fail with `500`, and `GET /api/dashboard/settings` also returns newsroom's `newsroom.*` keys from the shared `settings` table. Transport-level differences from Express (JSON instead of HTML error pages, `400 {"error":"Invalid request body"}` for malformed or oversized bodies) follow the auth slice.
```

In the Architecture list, replace the bullet

```markdown
- Future cohesive capabilities such as `newsletter`, `media`, and `settings` own their domain, repository, service, HTTP handlers, relative route mounting, and migration SQL.
```

with:

```markdown
- `settings` owns the dashboard site-settings behavior but no migration: newsroom migration 3 creates the shared `settings` table with Node's schema.
- Future cohesive capabilities such as `newsletter` and `media` own their domain, repository, service, HTTP handlers, relative route mounting, and migration SQL.
```

- [ ] **Step 2: Update `../../docs/plans/2026-09-20-go-api-migration.md`.** Under `### Task 9: Implement dashboard article CRUD` and `### Task 10: Implement category CRUD and settings`, add directly below each heading:

```markdown
**Status (2026-09-24): Complete for the preserved migration slice via `docs/superpowers/plans/2026-09-24-go-admin-content.md`; the Node dashboard contracts replay in `internal/app/dashboard_contract_test.go`. This does not establish production or cutover readiness.**
```

- [ ] **Step 3: Final verification**

Run: `gofmt -l . ; go vet -p 2 ./... && go build -p 2 ./... && go test -p 2 ./... && go test -race -p 2 ./internal/jsonbody/ ./internal/content/ ./internal/settings/ ./internal/app/ && ./scripts/sync-contracts.sh --check`
Expected: no gofmt output, and every command succeeds.

- [ ] **Step 4: Commit**

```bash
git add README.md ../../docs/plans/2026-09-20-go-api-migration.md
git commit -m "Document the Go dashboard content administration port"
```

---

## Self-review checklist (for the executing agent)

- [ ] Each of the 11 fixture operations `dashboard.articles.{list,get,create,update,delete}`, `dashboard.categories.{list,create,update,delete}`, `dashboard.settings.{get,update}` replays in canonical order, and the fixture's non-media dashboard operation set equals `dashboardContentOperations`.
- [ ] The `dashboardUnknownRoute` observation replays (401 without a token).
- [ ] Every Node error message is byte-identical: `Title, slug, and category_id are required`, `Source URL is invalid`, `Published articles require source and source_url`, `Article failed publishing policy` (+ `details`), `Category not found`, `Deals articles are not allowed`, `Article slug or source_url already exists`, `TechNews Editorial author is missing`, `Article not found`, `No fields to update`, `Name and slug are required`, `Cannot delete category with existing articles. Reassign articles first.`
- [ ] `created_at`/`updated_at` use `2006-01-02 15:04:05` (UTC); default `published_at` uses `2006-01-02T15:04:05.000Z`.
- [ ] No 4xx or 5xx response triggers IndexNow, and every notification happens after the body is written.
- [ ] No migration was added; `app.Migrations()` is unchanged.
- [ ] Commits have plain messages and no trailers.
