# Go API Migration Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Replace the Express request-serving API with a contract-compatible Go modular monolith under `apps/server-go`, without touching the production checkout or production process until an explicitly approved cutover.

**Architecture:** One Go module and one binary, composed in `internal/app`. Business capabilities are cohesive packages directly under `internal`; each owns its domain types, SQL repository, service, HTTP handlers, route registration, migrations, and tests. Cross-cutting runtime adapters live in narrowly named infrastructure packages. Capability packages do not import one another; the composition root injects the few required behaviors through consumer-owned interfaces.

**Tech Stack:** Go 1.25, `net/http`, `github.com/go-chi/chi/v5`, `database/sql`, `modernc.org/sqlite`, `golang-jwt/jwt/v5`, `golang.org/x/crypto/bcrypt`, embedded SQL migrations, standard `testing`/`httptest`.

**Safety boundary:** All development and tests run in `/Users/ozan/Projects/technews-server-go` on branch `feat/go-api-migration`. Never start the Go API on ports 4001, 3001, or 3002. Never point it at `/Users/ozan/Projects/technews/apps/server/data/technews.db`. Tests and contract capture use synthetic temporary databases and uploads directories; no production database is copied or opened. Manual runs use port 4401 with synthetic worktree-local data.

---

## Target structure

```text
apps/server-go/
├── cmd/api/main.go
├── internal/
│   ├── app/
│   │   ├── app.go
│   │   └── app_test.go
│   ├── config/
│   │   ├── config.go
│   │   └── config_test.go
│   ├── database/
│   │   ├── sqlite.go
│   │   ├── sqlite_test.go
│   │   └── migrate/
│   │       ├── migrate.go
│   │       └── migrate_test.go
│   ├── httpserver/
│   │   ├── router.go
│   │   ├── router_test.go
│   │   ├── server.go
│   │   ├── middleware/
│   │   └── response/
│   ├── health/
│   ├── editorial/       # authors, principals, login, JWT, bcrypt, migrations/*.sql
│   ├── content/         # articles, categories, publishing policy, migrations/*.sql
│   ├── media/           # media metadata, file storage, migrations/*.sql
│   ├── settings/        # settings behavior and migrations/*.sql
│   ├── newsletter/      # newsletter behavior and migrations/*.sql
│   ├── indexnow/
│   └── testutil/        # internal-only shared test infrastructure
├── Makefile
├── README.md
├── go.mod
└── go.sum
```

Every HTTP-facing capability uses the same internal shape without forced subpackages:

```text
http.go         handlers and relative route mounting
model.go        domain and response models
repository.go   SQL implementation
service.go      use cases and business rules
handler.go      HTTP parsing and response mapping
*_test.go       behavior tests colocated with code
```

A capability may omit files/layers it does not need. No `utils`, `common`, `interfaces`, global service locator, or framework-style base classes. Keep one ordinary Go package per capability until an actual import cycle or independent reuse justifies a subpackage.

## Dependency rules

1. `cmd/api` imports only `internal/app` and `internal/config`.
2. `internal/app` is the composition root and may import every module and adapter.
3. Capability packages may import `database/sql`, narrowly named transport helpers, and standard/third-party libraries, but never another capability package.
4. Interfaces are declared by consumers at the point of use and kept minimal.
5. Domain and service code do not depend on `http.Request`, `http.ResponseWriter`, Chi, or concrete SQLite types.
6. HTTP handlers translate transport values into service calls and map typed errors to the existing JSON contract.
7. All request-bound operations accept `context.Context` as the first parameter.
8. Constructors return concrete types unless a consumer requires an interface.
9. Each capability embeds and owns its migration SQL. `internal/database/migrate` owns only the migration ledger and runner; `internal/app` gathers capability migration descriptors in deterministic execution order.

---

### Task 1: Establish isolated Go module and safety guardrails

**Objective:** Create the module, developer commands, architecture documentation, and tests that reject production paths/ports.

**Files:**
- Create: `apps/server-go/go.mod`
- Create: `apps/server-go/Makefile`
- Create: `apps/server-go/README.md`
- Create: `apps/server-go/internal/config/config_test.go`
- Create: `apps/server-go/internal/config/config.go`

**TDD cycle:**
1. Write table-driven tests proving defaults are port 4401 and a worktree-local DB.
2. Write tests proving port 4001 and the production database path are rejected outside an explicit production mode.
3. Run `go test ./internal/config`; verify the package/code absence fails.
4. Implement minimal typed configuration and validation.
5. Run the package test, then `go test ./...`.
6. Commit.

### Task 2: Add the application composition root and health vertical slice

**Objective:** Produce a runnable binary with graceful shutdown and a contract-compatible `GET /api/health` endpoint.

**Files:**
- Create: `apps/server-go/internal/health/http_test.go`
- Create: `apps/server-go/internal/health/http.go`
- Create: `apps/server-go/internal/httpserver/router_test.go`
- Create: `apps/server-go/internal/httpserver/router.go`
- Create: `apps/server-go/internal/httpserver/server.go`
- Create: `apps/server-go/internal/app/app_test.go`
- Create: `apps/server-go/internal/app/app.go`
- Create: `apps/server-go/cmd/api/main.go`

**TDD cycle:**
1. Test exact status, content type, and JSON `{\"status\":\"ok\"}`.
2. Test unknown routes return a stable JSON 404.
3. Test middleware order and request ID propagation.
4. Implement route mounting using `chi.Router.Route("/api", ...)`.
5. Add `http.Server` timeouts and signal-driven graceful shutdown.
6. Run `go test ./...` and a temporary-port smoke test.
7. Commit.

### Task 3: Add SQLite adapter and migration runner

**Objective:** Open safe SQLite databases with compatible pragmas and provide a deterministic migration ledger and runner for ordered descriptors supplied by the application composition root.

**Files:**
- Create: `apps/server-go/internal/database/sqlite_test.go`
- Create: `apps/server-go/internal/database/sqlite.go`
- Create: `apps/server-go/internal/database/migrate/migrate_test.go`
- Create: `apps/server-go/internal/database/migrate/migrate.go`
- Create: `apps/server-go/internal/testutil/database_test.go`
- Create: `apps/server-go/internal/testutil/database.go`

**TDD cycle:**
1. Test WAL, foreign keys, busy timeout, connection limits, and ping.
2. Test ordered descriptor execution in a temporary database.
3. Test migration idempotency and rollback-on-failure.
4. Test duplicate/out-of-order descriptor rejection and ledger compatibility.
5. Implement the descriptor runner and migration ledger without embedding capability schema SQL. Keep it standalone: no capability descriptors exist yet, and API startup must not open or migrate a database.
6. Defer descriptor gathering in `internal/app` until Task 5 introduces the first capability-owned migration; run migrations only through an explicit deployment command, never from API startup.
7. Run `go test -race ./...`.
8. Commit.

### Task 4: Capture and automate the Node HTTP contract

**Objective:** Define executable compatibility fixtures before porting business endpoints.

**Files:**
- Create: `apps/server-go/contracts/openapi.yaml`
- Generate mirror: `apps/server-go/contracts/fixtures/node-contracts.json`
- Create: `apps/server-go/internal/contracttest/*.go`
- Create: `apps/server-go/scripts/sync-contracts.sh`
- Extend: `apps/server-go/Makefile` and `apps/server-go/README.md`
- Canonical capture source: `apps/server/contracts/node/contracts.json` and `apps/server/scripts/capture-contracts.ts`

**TDD cycle:**
1. Capture all 32 public and authenticated operations against the isolated Node contract server using a synthetic temporary SQLite database and uploads directory, never a copied production database.
2. Treat `apps/server/contracts/node/contracts.json` as canonical and validate status, JSON content type, field names, nullability, pagination, dependencies, placeholders, and error shape.
3. From the repository root, run `pnpm --filter @technews/server contract:check`; review any canonical diff for behavior, secrets, and PII before acceptance.
4. From `apps/server-go`, run `make contracts-accept` only after review to atomically update the generated Go mirror, then run `make contracts-check`. Checks must fail on drift and never silently skip.
5. Verify the OpenAPI 3.1 manifest has exact operation identity/method/path parity, observed JSON statuses, and required path parameters; run full Go tests, race tests, vet, and build before committing.

### Task 5: Implement public article reads

**Objective:** Port article list, trending, slug lookup, and ID lookup with exact response compatibility.

**Files:**
- Create: `apps/server-go/internal/editorial/migrations/*.sql` for the v1 authors table required by the article author foreign key
- Create: `apps/server-go/internal/content/migrations/*.sql`
- Create: `apps/server-go/internal/content/model.go`
- Create: `apps/server-go/internal/content/sqlite_test.go`
- Create: `apps/server-go/internal/content/sqlite.go`
- Create: `apps/server-go/internal/content/service_test.go`
- Create: `apps/server-go/internal/content/service.go`
- Create: `apps/server-go/internal/content/http_public_test.go`
- Create: `apps/server-go/internal/content/http_public.go`
- Extend: `apps/server-go/internal/app` to add the content migration descriptor in order

**TDD cycle:** Replay the four reviewed success-only article contracts in their captured database order. Separately test pagination boundaries, parseInt/clamping prefixes and overflow signs, search/category filtering (including preserved whitespace and SQL wildcard behavior), empty arrays, sort order, nested category/author JSON, missing and malformed identifiers, draft visibility, mixed date formats, CORS/content type/no-newline responses, and the pre-increment view-count response plus persisted side effect. Verify the exact fresh-database v1/v2 schema, constraints, defaults, foreign keys, and index. The API must never migrate; migration remains an explicit command. Existing unmanaged Node databases can be read but cannot be stamped by `migrate.Run`; cutover needs a future explicit full-schema verifier/adoption command.

### Task 6: Implement categories and authors public reads

**Status:** Complete at the migration-slice level. Public category and author reads have focused store/service/HTTP coverage and replay the approved success contracts; this does not imply production or cutover readiness.

**Objective:** Port category and author listing with existing ordering and response shapes.

**Files:**
- Extend `internal/content`, including capability-owned migration SQL, for category reads
- Extend `internal/editorial` with module files/tests for author reads; its foundational v1 authors migration was introduced in Task 5 because the v2 articles table references it
- Reuse the Task 5 editorial/content migration descriptor order

**TDD cycle:** Test ordering, empty results, field names, negative scenarios, and privacy behavior matching the compatibility decision. Implement, run contracts, commit. Do not infer production readiness from success-contract parity.

### Task 7: Implement authentication

**Status (2026-09-21): Complete for the preserved migration slice; this does not establish production or cutover readiness.**

**Objective:** Preserve bcrypt and JWT compatibility while failing closed on configuration.

**Files:**
- Extended `internal/editorial` with bcrypt verification, strict HS256 JWT signing/verification, login service, SQLite login lookup, and auth HTTP handlers/tests
- Extended `internal/app` with database-backed auth composition and executable replay of the three approved auth fixture operations
- Extended `internal/config` with `JWT_SECRET` loading and redacted `String`/`GoString` formatting
- Updated the Go module dependency metadata and `apps/server-go/README.md`

**TDD cycle:** Golden bcrypt and external JWT digest vectors, fixture replay, required integer claims, single-document JSON, HS256, signature and expiration boundary rejection, missing secret, malformed bearer headers, service error identity, non-leaking database failures, `/auth/me`, and stateless logout are covered. Database-backed composition fails closed without `JWT_SECRET`; health-only composition remains unaffected. The 100 KiB malformed-body response is intentional hardening. No fallback secret was added.

### Task 8: Port publishing policy

**Status (2026-09-21): Complete for the pure policy slice; dashboard route integration and production or cutover readiness remain pending.**

**Objective:** Preserve every source, normalization, AI-only, copy, HTML, and length rule.

**Files:**
- Created `internal/content/policy.go` with typed, side-effect-free feed, author, URL, rejection, and article-validation APIs
- Created `internal/content/policy_test.go` with exact retained and expanded parity vectors
- Updated `apps/server-go/README.md` with the bounded Task 8 capability statement

**TDD cycle:** Exact tables cover feed order, domains and subdomains, normalization, rejection ordering, AI signals, old-year boundaries, text helpers, HTML structure, paragraph and word boundaries, source footers, and stable validation errors. Focused RED runs were captured before correcting default-port and non-hierarchical URL normalization, JavaScript whitespace handling, sentence counting, and byte-order-mark trimming. `NEWS_PUBLISHING_POLICY.md` required no change because no policy drift was found.

### Task 9: Implement dashboard article CRUD

**Status (2026-09-24): Complete for the preserved migration slice via `docs/superpowers/plans/2026-09-24-go-admin-content.md`; the Node dashboard contracts replay in `internal/app/dashboard_contract_test.go`. This does not establish production or cutover readiness.**

**Objective:** Port authenticated article administration, policy validation, duplicate handling, editorial author assignment, and publication timestamps.

**Files:** Extend `internal/content/*` and add integration tests.

**TDD cycle:** Cover create/update/delete, partial updates, invalid categories, duplicate slug/source URL, publication rules, and transaction rollback. Commit only after contract parity.

### Task 10: Implement category CRUD and settings

**Status (2026-09-24): Complete for the preserved migration slice via `docs/superpowers/plans/2026-09-24-go-admin-content.md`; the Node dashboard contracts replay in `internal/app/dashboard_contract_test.go`. This does not establish production or cutover readiness.**

**Objective:** Port protected category CRUD and settings allowlisted upsert behavior.

**Files:** Extend `internal/content`; create `internal/settings`, capability-owned migration SQL, and tests; extend `internal/app` to add new descriptors in order.

**TDD cycle:** Test category-in-use deletion, uniqueness conflicts, no-op updates, boolean serialization, ignored unknown settings, and transactional upserts.

### Task 11: Implement media storage

**Status (2026-09-24): Complete for the preserved migration slice via `docs/superpowers/plans/2026-09-24-go-media.md`: local-disk storage in `UPLOADS_DIR` with Node parity by default (user decision), an opt-in S3 backend for new uploads (`MEDIA_STORAGE=s3`), migration 5 (Node's `media` table), static `/uploads/*` serving, and the Node media contracts replaying in `internal/app/media_contract_test.go`. This does not establish production or cutover readiness.**

**Objective:** Port listing, upload, serving, and deletion behind a storage interface.

**Files:** Create module files/tests and capability-owned migration SQL under `internal/media`; extend `internal/app` to add the media descriptor in order.

**TDD cycle:** Use temporary directories. Test maximum size, allowed content, filename generation with `crypto/rand`, path containment, cleanup on DB failure, and missing-file deletion. Preserve `/uploads/*` URLs.

### Task 12: Implement newsletter and Resend integration

**Status (2026-09-24): Complete for the preserved migration slice via `docs/superpowers/plans/2026-09-24-go-newsletter.md`: `internal/newsletter` ports signup, confirm and unsubscribe links (byte-identical HMAC tokens), the edition archive, and the daily digest through Resend (byte-identical emails and request bodies, Node's idempotency keys, retry classes, delivery states, and Europe/Istanbul edition dates), with migration 6 (Node's newsletter tables) and the eight Node newsletter contracts replaying in `internal/app/newsletter_contract_test.go`. Parity vectors were recorded from the Node implementation (`internal/newsletter/testdata/node-golden.json`). This does not establish production or cutover readiness.**

**Objective:** Port subscription, token compatibility, edition archive, digest selection, idempotent delivery, retries, and unsubscribe behavior.

**Files:** Create module files/tests and capability-owned migration SQL under `internal/newsletter`; extend `internal/app` to add the newsletter descriptor in order.

**TDD cycle:** Start with golden HMAC token vectors and existing newsletter scenarios. Use an `httptest.Server` for Resend; test retry classes, idempotency keys, Istanbul edition dates, delivery state transitions, and cancellation.

### Task 13: Implement IndexNow

**Objective:** Port canonical URL generation and non-blocking submissions with bounded lifetime.

**Files:** Create files/tests under `internal/indexnow`.

**TDD cycle:** Test deduplication, origin rejection, encoding, accepted statuses, error truncation, context cancellation, and application shutdown draining.

### Task 14: Integration, security, and cutover readiness

**Status (2026-09-24): Cutover readiness complete via `docs/superpowers/plans/2026-09-24-go-cutover.md` (branch `go-cutover`). Target changed from the plan below: Go alone on a Mac mini, Node retired (not Go on the MacBook beside Node). Delivered: production configuration with explicit absolute `DATABASE_PATH`/`UPLOADS_DIR`, any production port, no repository checkout needed, and the `.technews-production` data marker that production requires and development refuses (the hardcoded MacBook paths remain only as development guards); `migrate.Adopt` and `cmd/adopt` (read-only dry run by default with a rehearsal on a copy; `--apply` backs up with `VACUUM INTO`, then records the present migrations and runs the pending ones in one transaction; idempotent), tested against schemas recorded from Node's own `initializeDatabase`; `scripts/smoke-local.sh`; `docs/cutover.md`, `docs/rollback.md`, a launchd template, and an env template. Not done here, by design: anything on the production machines (backup, adoption, launchd, tunnel switch) and `govulncheck` (not installed locally). Steps 8 and 9 below are superseded by the runbook's freeze and verification steps. After review: stricter definition-level schema comparison, in-transaction row-count checks, a production API that never creates a database and refuses unmigrated ones, production `JWT_SECRET`/`TZ` validation, a database-backed `/api/health`, and the dashboard on the Mac mini in the runbook.**

**Objective:** Prove the Go implementation is safe and behaviorally compatible without affecting production.

**Files:**
- Create: `apps/server-go/scripts/smoke-local.sh`
- Create: `apps/server-go/docs/cutover.md`
- Create: `apps/server-go/docs/rollback.md`

**Verification:**
1. `go test ./...`
2. `go test -race ./...`
3. `go vet ./...`
4. `govulncheck ./...`
5. Build the binary.
6. Run on `127.0.0.1:4401` with a synthetic worktree-local database.
7. Diff safe GET responses against an isolated Node instance.
8. Verify production PID, port 4001 listener, database mtime, and live health were unchanged.
9. Request explicit approval before editing launchd scripts, binding port 4001, or touching production data.
