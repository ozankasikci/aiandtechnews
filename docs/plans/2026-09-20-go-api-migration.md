# Go API Migration Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Replace the Express request-serving API with a contract-compatible Go modular monolith under `apps/server-go`, without touching the production checkout or production process until an explicitly approved cutover.

**Architecture:** One Go module and one binary, composed in `internal/app`. Business capabilities are cohesive packages directly under `internal`; each owns its domain types, SQL repository, service, HTTP handlers, route registration, migrations, and tests. Cross-cutting runtime adapters live in narrowly named infrastructure packages. Capability packages do not import one another; the composition root injects the few required behaviors through consumer-owned interfaces.

**Tech Stack:** Go 1.25, `net/http`, `github.com/go-chi/chi/v5`, `database/sql`, `modernc.org/sqlite`, `golang-jwt/jwt/v5`, `golang.org/x/crypto/bcrypt`, embedded SQL migrations, standard `testing`/`httptest`.

**Safety boundary:** All development and tests run in `/Users/ozan/Projects/technews-server-go` on branch `feat/go-api-migration`. Never start the Go API on ports 4001, 3001, or 3002. Never point it at `/Users/ozan/Projects/technews/apps/server/data/technews.db`. Tests use temporary databases; manual runs use port 4401 and a copied database under the worktree.

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
│   │   ├── migrate.go
│   │   ├── migrate_test.go
│   │   └── migrations/*.sql
│   ├── httpserver/
│   │   ├── router.go
│   │   ├── router_test.go
│   │   ├── server.go
│   │   ├── middleware/
│   │   └── response/
│   ├── health/
│   ├── editorial/       # authors, principals, login, JWT and bcrypt
│   ├── content/         # articles, categories and publishing policy
│   ├── media/           # media metadata and file storage
│   ├── settings/
│   ├── newsletter/
│   └── indexnow/
├── testutil/
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

### Task 3: Add SQLite adapter and versioned baseline migrations

**Objective:** Open safe SQLite databases with compatible pragmas and a deterministic migration ledger.

**Files:**
- Create: `apps/server-go/internal/database/sqlite_test.go`
- Create: `apps/server-go/internal/database/sqlite.go`
- Create: `apps/server-go/internal/database/migrate_test.go`
- Create: `apps/server-go/internal/database/migrate.go`
- Create: `apps/server-go/internal/database/migrations/0001_baseline.sql`
- Create: `apps/server-go/testutil/database.go`

**TDD cycle:**
1. Test WAL, foreign keys, busy timeout, connection limits, and ping.
2. Test clean schema creation in a temporary database.
3. Test migration idempotency and rollback-on-failure.
4. Test the schema against a copied/sanitized legacy database fixture.
5. Implement embedded ordered migrations and a migration ledger.
6. Run `go test -race ./...`.
7. Commit.

### Task 4: Capture and automate the Node HTTP contract

**Objective:** Define executable compatibility fixtures before porting business endpoints.

**Files:**
- Create: `apps/server-go/contracts/openapi.yaml`
- Create: `apps/server-go/contracts/fixtures/*.json`
- Create: `apps/server-go/internal/contracttest/contract_test.go`
- Create: `apps/server-go/scripts/capture-contract.sh`

**TDD cycle:**
1. Add contract cases for all public and authenticated routes.
2. Assert status code, content type, field names, nullability, pagination, and error shape.
3. Run against an isolated Node server and copied DB, never production.
4. Record fixtures only after manual review for secrets/PII.
5. Commit.

### Task 5: Implement public article reads

**Objective:** Port article list, trending, slug lookup, and ID lookup with exact response compatibility.

**Files:**
- Create: `apps/server-go/internal/content/model.go`
- Create: `apps/server-go/internal/content/sqlite_test.go`
- Create: `apps/server-go/internal/content/sqlite.go`
- Create: `apps/server-go/internal/content/service_test.go`
- Create: `apps/server-go/internal/content/service.go`
- Create: `apps/server-go/internal/content/http_public_test.go`
- Create: `apps/server-go/internal/content/http_public.go`

**TDD cycle:** Test pagination boundaries, search/category filtering, sort order, nested category/author JSON, missing articles, mixed date formats, and the view-count side effect before implementation. Run contract tests after every endpoint.

### Task 6: Implement categories and authors public reads

**Objective:** Port category and author listing with existing ordering and response shapes.

**Files:**
- Extend `internal/content` for category reads
- Create module files/tests under `internal/editorial` for author reads

**TDD cycle:** Test ordering, empty results, field names, and privacy behavior matching the compatibility decision. Implement, run contracts, commit.

### Task 7: Implement authentication

**Objective:** Preserve bcrypt and JWT compatibility while failing closed on configuration.

**Files:**
- Extend module files/tests under `internal/editorial`
- Create auth middleware/tests under `internal/httpserver/middleware`

**TDD cycle:** Add golden tests for existing bcrypt hashes and JWT claim/signature compatibility. Test missing secret, malformed bearer header, expiration, login failure, `/auth/me`, and logout. Never add a fallback production secret.

### Task 8: Port publishing policy

**Objective:** Preserve every source, normalization, AI-only, copy, HTML, and length rule.

**Files:**
- Create: `internal/content/policy.go`
- Create: `internal/content/policy_test.go`

**TDD cycle:** Translate the existing TypeScript vectors first, verify failures, then implement policy behavior. Keep `NEWS_PUBLISHING_POLICY.md` and tests synchronized.

### Task 9: Implement dashboard article CRUD

**Objective:** Port authenticated article administration, policy validation, duplicate handling, editorial author assignment, and publication timestamps.

**Files:** Extend `internal/content/*` and add integration tests.

**TDD cycle:** Cover create/update/delete, partial updates, invalid categories, duplicate slug/source URL, publication rules, and transaction rollback. Commit only after contract parity.

### Task 10: Implement category CRUD and settings

**Objective:** Port protected category CRUD and settings allowlisted upsert behavior.

**Files:** Extend `internal/content`; create `internal/settings` and tests.

**TDD cycle:** Test category-in-use deletion, uniqueness conflicts, no-op updates, boolean serialization, ignored unknown settings, and transactional upserts.

### Task 11: Implement media storage

**Objective:** Port listing, upload, serving, and deletion behind a storage interface.

**Files:** Create module files/tests under `internal/media`.

**TDD cycle:** Use temporary directories. Test maximum size, allowed content, filename generation with `crypto/rand`, path containment, cleanup on DB failure, and missing-file deletion. Preserve `/uploads/*` URLs.

### Task 12: Implement newsletter and Resend integration

**Objective:** Port subscription, token compatibility, edition archive, digest selection, idempotent delivery, retries, and unsubscribe behavior.

**Files:** Create module files/tests under `internal/newsletter`.

**TDD cycle:** Start with golden HMAC token vectors and existing newsletter scenarios. Use an `httptest.Server` for Resend; test retry classes, idempotency keys, Istanbul edition dates, delivery state transitions, and cancellation.

### Task 13: Implement IndexNow

**Objective:** Port canonical URL generation and non-blocking submissions with bounded lifetime.

**Files:** Create files/tests under `internal/indexnow`.

**TDD cycle:** Test deduplication, origin rejection, encoding, accepted statuses, error truncation, context cancellation, and application shutdown draining.

### Task 14: Integration, security, and cutover readiness

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
6. Run on `127.0.0.1:4401` with a copied database.
7. Diff safe GET responses against an isolated Node instance.
8. Verify production PID, port 4001 listener, database mtime, and live health were unchanged.
9. Request explicit approval before editing launchd scripts, binding port 4001, or touching production data.
