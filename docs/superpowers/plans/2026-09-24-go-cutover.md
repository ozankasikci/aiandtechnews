# Go Cutover Readiness (migration task 14, "5d") Implementation Plan

> Implement test-first on branch `go-cutover` in a temporary worktree
> (`git worktree add ../aiandtechnews-cutover -b go-cutover main`). The main
> checkout stays on `main`. Build and test only with `-p 2`.

**Goal:** Make the Go API ready to replace Node in production: Go alone on a Mac mini
(macOS), Node retired. Production today is the Node API, a SQLite database, the uploads
directory, and an external `news:daily` scheduler on a MacBook, exposed via a
`subtunnel local` client as `https://technews.subtunnel.dev`.

**Boundary:** No access to either machine. Nothing here opens a real database file
(including `data/technews.db` in the repository root), starts a long-running server,
or touches a real network. The user provides the production database copy and runs
every command on the machines.

---

## 1. Production configuration (`internal/config`)

Problems today:

- `ProductionDatabasePath` / `ProductionUploadsDir` are hardcoded MacBook paths
  (`/Users/ozan/Projects/technews/apps/server/...`). They mean nothing on the Mac mini.
- Ports 3001/3002 are rejected even in production.
- Every command locates a repository checkout (`pnpm-workspace.yaml`) before loading
  configuration, so a prebuilt binary cannot start on a machine without the repo.

Design:

| Rule | Development | Production (`APP_ENV=production`) |
| --- | --- | --- |
| `DATABASE_PATH` | default `<worktree>/data/technews.db` | **required, absolute** |
| `UPLOADS_DIR` | default `<worktree>/data/uploads` | **required, absolute** (was: only `cmd/api`) |
| `SERVER_ADDR` | default `127.0.0.1:4401`; 3001, 3002, 4001 rejected | default `127.0.0.1:4401`; **any port** |
| Worktree root | required (for defaults) | not needed; commands skip the lookup |
| Production marker | `DATABASE_PATH`/`UPLOADS_DIR` under a marked directory is **refused** | `DATABASE_PATH` and `UPLOADS_DIR` **must** be under a marked directory |
| `PRODUCTION_DATABASE_PATH` (optional) | `DATABASE_PATH` equivalent to it (canonical name, symlink, hard link, case variant) is refused | ignored |
| `PRODUCTION_UPLOADS_DIR` (optional) | `UPLOADS_DIR` equal to, inside, or containing it is refused | ignored |
| Legacy Node MacBook paths | still refused (they become the rollback copy) | not referenced |

**The marker.** A file named `.technews-production` in the production data root
(for example `~/technews/.technews-production` with `~/technews/data/technews.db` and
`~/technews/uploads`). A path is "marked" when the marker exists in its directory or any
ancestor directory (checked on the symlink-resolved canonical path, so aliases do not
escape it). Why this is robust:

- It travels with the data, not with the code or the shell environment: a dev run on
  any machine refuses the production tree without anyone remembering to export a guard.
- Production *requires* it, so the protection cannot silently be missing on the
  production host: forgetting to create it stops the API, it does not leave dev runs
  unprotected.
- It is a single `touch`; removing it is a deliberate act.

`PRODUCTION_DATABASE_PATH` / `PRODUCTION_UPLOADS_DIR` are an extra, optional guard for a
shell profile on a machine that holds production data somewhere the marker cannot be
placed. The two legacy Node constants stay (renamed `LegacyNodeDatabasePath`,
`LegacyNodeUploadsDir`) as a development-only denylist, because after cutover they hold
the rollback copy on the MacBook.

Tests (config package): production requires explicit absolute `DATABASE_PATH` and
`UPLOADS_DIR`; production requires the marker (missing marker, marker only above the
uploads directory, marker in a sibling); production accepts ports 3001/4001/8080;
development keeps rejecting 3001/3002/4001; development refuses paths under a marked
directory (same directory, nested, symlinked alias, nonexistent path below a marked
directory); `PRODUCTION_DATABASE_PATH` refuses equal, relative, symlink, and hard-link
aliases; `PRODUCTION_UPLOADS_DIR` refuses equal, nested, and ancestor directories;
production `Load` works with an empty worktree root, development does not. `cmd/api`,
`cmd/migrate`, `cmd/devseed` skip the worktree lookup when `APP_ENV=production`.

## 2. Adoption command (`cmd/adopt`, `internal/database/adopt`, `migrate.Adopt`)

### Why not "stamp, then `migrate.Run`"

A Node database already has the objects of Go migrations 1, 2, 5, 6 (authors;
categories/articles; media; newsletter) but not 3 and 4 (candidates). Stamping 1, 2, 5, 6
and then calling `migrate.Run` fails by design: `Run` refuses a ledger with unapplied
versions below the highest applied one (`ErrHistoryGap`). So adoption is one new runner
entry point in `internal/database/migrate`:

`Adopt(ctx, db, descriptors, verify)`: in one `BEGIN IMMEDIATE` transaction it
refuses a database that already has a ledger (`ErrAlreadyManaged`) or is empty
(`ErrNothingToAdopt`), calls `verify` on the same connection (so the check and the
write see the same schema), creates `schema_migrations` with the exact DDL `Run` uses,
records the versions `verify` reported present with their exact names and checksums,
applies every other descriptor in version order, and commits. Any error rolls back
everything. `Run` is unchanged. After `Adopt`, `cmd/adopt` calls
`migrate.Run(app.Migrations())`, which must be a no-op.

### How compatibility is decided (`internal/database/adopt`)

1. **Reference schema.** Apply the Go descriptors one by one to a scratch database in a
   temporary directory and record, per migration, the objects it creates (tables,
   indexes, triggers, views; `sqlite_%` names excluded). A migration that alters an
   object an earlier migration created is rejected as unsupported (none does today).
2. **Inspection** of a database, through the given connection only: `sqlite_schema`,
   `PRAGMA table_list` (WITHOUT ROWID, STRICT), `table_xinfo` (name, declared type,
   NOT NULL, default, primary-key position, hidden), `foreign_key_list`, `index_list` +
   `index_xinfo` (unique, origin, partial, key columns with order and collation, the
   WHERE clause), CHECK constraints parsed from the stored `CREATE TABLE` text, and
   AUTOINCREMENT.
3. **Per migration:** *present* when every object it creates exists in the target,
   *pending* otherwise (a partially present migration is reported as such). Every
   reference object that exists in the target is compared, whether its migration is
   present or pending:
   - columns compared as a **set by name** (column order is ignored: older databases got
     `articles.source`/`source_url` and the `subscribers` columns via `ALTER TABLE`);
     declared type (case/whitespace-normalized), NOT NULL, default (normalized: outer
     parentheses, keyword case, whitespace), primary-key position, hidden;
   - CHECK constraints as a multiset of token-normalized expressions (keyword and
     identifier case, identifier quoting, whitespace; string literals kept verbatim);
   - foreign keys as a multiset of (referenced table, from columns, to columns,
     ON UPDATE, ON DELETE, MATCH);
   - indexes: named indexes by name and definition, automatic UNIQUE/PRIMARY KEY indexes
     by definition (their generated names depend on column order);
   - AUTOINCREMENT, WITHOUT ROWID, STRICT.
4. **Known Node legacy variant** (the only relaxation of an exact match):
   `subscribers.created_at` and `subscribers.updated_at` may be `TEXT` nullable without a
   default, exactly what `apps/server/src/db.ts` adds with `ALTER TABLE` to an older
   `subscribers` table. Safe because Go always writes both columns explicitly
   (`internal/newsletter/store.go`), as Node does.
5. **Tolerated extras (listed in the report, no failure):** tables and views the
   reference does not know; extra columns that are nullable or have a default (Go never
   names them in an INSERT); extra non-unique indexes. **Not tolerated:** extra NOT NULL
   columns without a default, extra UNIQUE indexes, extra CHECKs or foreign keys (they
   can reject Go's writes), and triggers on reference tables (they change write
   behavior).
6. **Health checks:** `PRAGMA integrity_check` must be `ok`; `PRAGMA foreign_key_check`
   violations are warnings (Go enforces foreign keys, so updating such rows can fail).

### Command behavior

- Configuration comes from `internal/config` (same env file as the API), so the
  development guards apply: a dev run refuses a production-marked database.
- The database must already exist; it is never created.
- **Default: read-only dry run.** Opens the database read-only (`mode=ro`), prints the
  report, then rehearses the full adoption on a scratch copy (`VACUUM INTO` a temporary
  directory, `Adopt`, `migrate.Run`, full re-verification) and prints the outcome. Exit
  `0` when adoptable, `1` on any mismatch or failure.
- **`--apply`:** verify (stop on mismatch, nothing written), rehearse, then
  `VACUUM INTO '<DATABASE_PATH>.pre-adopt-<UTC timestamp>.db'` (refuses when the target
  exists; the backup is checked with `integrity_check` and row counts), then `Adopt`,
  then `migrate.Run`, then a final verification and a before/after row-count report
  (migration 3 adds the two `newsroom.*` settings rows; nothing else changes).
- **Idempotent:** a database with a ledger reports "already managed" with its versions
  and exits `0` without writing (pending versions are named; `cmd/migrate` applies them).
- Never drops, rewrites, or deletes: the only writes are the ledger, the pending
  migrations' SQL, and the backup file.

### Fixtures

`internal/database/adopt/testdata/record-node-schema.ts` runs Node's real
`initializeDatabase` (better-sqlite3, in memory, no seed) and writes
`node-schema.json`:

- `fresh`: a database created by the current Node server;
- `legacy`: the first recorded Node schema (`e08f508`: `articles` without
  `source`/`source_url`) plus a minimal `subscribers (id, email)` table, then
  `initializeDatabase`, so Node's own `ALTER TABLE` path appends the columns.

Tests build fixtures from those recorded statements and cover: fresh adopts; legacy
(ALTER-evolved) adopts; a missing column fails with a precise report and no change
(no ledger, no backup); an extra unknown table is reported and tolerated; a changed
CHECK, an extra NOT NULL column, an extra unique index, a trigger, and a changed default
fail; data and row counts are unchanged after `--apply` (except the two newsroom settings
rows), the backup exists and holds the original data, an existing backup name is refused,
a second run is a no-op ("already managed"), and the result passes
`migrate.Run(app.Migrations())` and matches a fresh Go database's schema. `Adopt` gets
unit tests in `migrate` (atomic rollback, ledger refusal, recorded checksums).

## 3. Local smoke script (`apps/server-go/scripts/smoke-local.sh`)

`smoke-local.sh <database> [uploads-dir]`: copies the database (with `sqlite3 .backup`
when available, else `cp` of the file and any `-wal`) into a `mktemp -d` directory,
builds `cmd/api` and `cmd/adopt` with `-p 2`, adopts the copy if it has no ledger,
starts the API on `127.0.0.1:<random free port>` in development mode with a throwaway
`JWT_SECRET` (collector, publisher, IndexNow off), and checks every GET the website
uses: `/api/health`, `/api/articles` (plain, `page`/`limit`, `category`, `search`),
`/api/articles/trending?limit=5`, `/api/articles/<first slug>`, `/api/articles/id/<id>`,
`/api/categories`, `/api/authors`, `/api/newsletter/editions?limit=30`,
`/api/newsletter/editions/<first key>` when one exists, and one `/uploads/...` URL when
an uploads directory is given. With `SMOKE_EMAIL`/`SMOKE_PASSWORD` it logs in and checks
`/api/auth/me`, `/api/dashboard/{articles,categories,media,settings}`,
`/api/newsroom/{overview,candidates,settings}`. Exits non-zero on the first failure; a
trap kills the server and removes the temporary directory. The address is always
`127.0.0.1`; the script accepts no host argument.

A Go test runs the script against a seeded temporary database (skipped with `-short`
and when `bash`/`curl` are missing) to prove it passes and cleans up.

## 4. Documentation

- `apps/server-go/docs/cutover.md`: Mac mini prerequisites (Go toolchain or a prebuilt
  `darwin/arm64` binary), directory layout and the marker, the env file (every variable
  and where its value comes from on the MacBook), launchd service
  (`docs/news.aiandtech.api.plist` template, KeepAlive, log paths), time zone,
  the subtunnel client pointed at the Go port; the freeze sequence (stop Node scheduler
  and Node API, final `sqlite3 .backup`, copy DB + uploads, `cmd/adopt` dry run then
  `--apply`, start Go, smoke, switch the tunnel, verify website, article pages, sitemap,
  newsletter archive, production-smoke workflow, enable collector/publisher last).
- `apps/server-go/docs/rollback.md`: switch the tunnel back to the MacBook, restart Node
  and its scheduler, what Go-era writes would be lost and how to carry them back.
- `apps/server-go/docs/technews.env.example` (names only) and `apps/server-go/docs/news.aiandtech.api.plist`.
- README and migration plan task 14 status.

## Verification

`gofmt -l . && go vet ./... && go test -p 2 ./... && make contracts-check`, plus
`go test -race -p 2` on `internal/config`, `internal/database/...`, and `cmd/adopt`.
`govulncheck` only if already installed (it is not; installing it globally is not allowed).

## Review changes (2026-09-24)

Applied after code review, test-first:

- **Verifier strictness (false accepts fixed).** Besides the PRAGMA-level
  comparison, each column's full definition text, the table constraints, and
  the `CREATE INDEX` text are compared after normalizing only whitespace,
  keyword case, identifier quoting, and comments. `UNIQUE ON CONFLICT
  REPLACE`, `NOT NULL ON CONFLICT IGNORE`, `COLLATE NOCASE`, generated
  columns, `DEFERRABLE` foreign keys, table-level `ON CONFLICT`, a CHECK
  moved from a column to the table, and a partial index are rejected. False
  rejects are accepted as the price of no false accepts; the known Node
  variant is matched on its exact definition (`created_at text`).
- **Adoption transaction.** `migrate.Adopt` takes `AdoptChecks{Verify,
  BeforeCommit}`. Verify recounts every table and requires the backup's
  counts; BeforeCommit requires the complete schema and that no existing table
  vanished, lost rows, or gained rows beyond `settings` +2. A WAL-mode source
  with uncheckpointed frames is fully captured by the backup (regression
  test). The optional refusal of a non-empty `-wal` was not added: a `-wal`
  after an unclean stop is normal, SQLite reads it, and the in-transaction
  recount already stops concurrent writers.
- **Production startup.** `cmd/api` in production opens with
  `database.OpenForServing` (no create; the serving pragmas, unlike the
  read-only-oriented `OpenExisting`) and refuses to serve unless
  `migrate.Status` reports a valid ledger with nothing pending.
- **Production config.** `JWT_SECRET` at least 32 bytes and not Node's
  fallback; `TZ` required and loadable.
- **Health.** With a database, `/api/health` runs `SELECT 1` (2s timeout):
  unchanged `200 {"status":"ok"}`, or `503 {"status":"error"}`.
- **Runbooks.** Per-machine variable blocks; launchd plists installed only at
  the start steps (with `HOME` set); copy into a temporary name and `mv -n`;
  `umask 077` + `mktemp -d` rehearsal; `read -s` for the smoke password;
  secrets copied as files, never printed; user answers folded in: dashboard on
  the Mac mini (build, launchd template, reached on `127.0.0.1:3001` via SSH
  tunnel or the moved tunnel), `technewsweb` discovery and decision point, the
  existing `JWT_SECRET` reused, Apple silicon build on the Mac mini with
  user-local Go/Node/pnpm (Homebrew or a tarball in `~/`) and Xcode CLT.
- **Smoke script** requires `sqlite3` (no `cp` fallback).

## Re-review follow-ups (2026-09-24)

- `cmd/migrate` in production opens without creating (`OpenForServing`),
  after checking the ledger on a read-only connection; a missing database or
  one without the ledger is refused with a pointer to `bin/adopt`. Tests:
  missing database (nothing created), unadopted Node database (byte-identical
  afterwards, including its journal mode), empty file, adopted database with a
  pending migration (migrated), development unchanged.
- Runbook: a `refusing to serve` / `no migration ledger` start sends the
  operator back to the adopt step, never to `bin/migrate`; the release
  section says `bin/migrate` is only for the adopted database. The secrets
  file is rewritten as `NAME='value'` (single quotes escaped) when it is
  built, and `technews.env` is checked with `sh -n` plus a names-only
  set/empty list before adoption.
