# Technews Go API

This directory is an isolated Go module for the API migration. It does not share a Go module or developer commands with the existing Node server.

Tasks 5 through 11 currently provide the four public article reads, public category and author listings, compatible login, current-user, and logout endpoints, a pure Go port of the retained publishing policy, the authenticated dashboard content administration (dashboard article list/get/create/update/delete, category list/create/update/delete, and site settings get/put), and the dashboard media library (media list/upload/delete plus static `/uploads/*` serving from a local directory). This is a migration slice, not a claim of production or cutover readiness; newsletter and cutover verification are still pending.

## Safety defaults

Development configuration is deliberately isolated:

- `SERVER_ADDR` defaults to `127.0.0.1:4401`.
- `DATABASE_PATH` defaults to `data/technews.db` beneath the worktree root supplied by the composition root.
- `UPLOADS_DIR` defaults to `data/uploads` beneath the worktree root (git-ignored). Production has no default: `cmd/api` refuses to start with `APP_ENV=production` unless `UPLOADS_DIR` is set explicitly. The Node MacBook uploads directory `/Users/ozan/Projects/technews/apps/server/uploads` (any alias of it, and any directory above or below it) is rejected unless `APP_ENV=production` is set. `UPLOADS_DIR` must be absolute and must never contain `DATABASE_PATH` (it may not be the database's directory or an ancestor of it), in every mode.
- Ports `3001` and `3002` are always rejected. Port `4001` and `/Users/ozan/Projects/technews/apps/server/data/technews.db` are rejected unless `APP_ENV=production` is explicitly set.
- Development and tests must never use the production checkout or database. Tests should use temporary databases.

The production override is a cutover guard, not a development convenience. Do not set `APP_ENV=production` without explicit cutover approval.

## HTTP compatibility

`OPTIONS` responses match Express in status (`204`), CORS headers and `Vary`, and empty-body semantics. They deliberately omit Express's wire-level `Content-Length: 0`: [RFC 9110 section 8.6](https://www.rfc-editor.org/rfc/rfc9110#section-8.6) forbids servers from sending `Content-Length` on a `204` response, and Go's `net/http` strips it accordingly.

The reviewed compatibility authority is the synthetic Node fixture at `apps/server/contracts/node/contracts.json`. Its 32 canonical operations capture reviewed primary success responses. The implemented public-read slice additionally tests unknown and malformed identifiers, draft visibility, query clamping/filtering/pagination, pre-increment view responses, persistence, ordering, nullability, empty collections, cancellation, and database failures. Negative scenarios for capabilities not yet ported remain deferred. `contracts/fixtures/node-contracts.json` is a generated Go-side mirror, not a separately editable fixture. Capture uses an isolated temporary SQLite database and uploads directory; it never copies or opens a production database.

The public author contract includes email addresses for compatibility with the current Node endpoint. This is recorded privacy debt, not an endorsement of public email exposure. The Go query uses an explicit six-column allowlist and never selects or serializes `password_hash`; changing email visibility requires a separately reviewed contract change.

Review contract changes in this order:

1. From the repository root, run `pnpm --filter @technews/server contract:check` to reproduce and compare the canonical Node fixture.
2. Review the canonical fixture diff for behavior, secrets, and PII.
3. From `apps/server-go`, run `make contracts-accept` only after that review to atomically update the Go mirror.
4. Run `make contracts-check`. Normal checks never rewrite either fixture.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `APP_ENV` | `development` | Typed runtime mode: `development` or `production` |
| `SERVER_ADDR` | `127.0.0.1:4401` | HTTP listen address |
| `DATABASE_PATH` | `<worktree>/data/technews.db` | SQLite database path |
| `JWT_SECRET` | none | Required by database-backed composition for signing and verifying authentication tokens |
| `UPLOADS_DIR` | `<worktree>/data/uploads` in development; **none in production** | Absolute path of the dashboard media library directory, served at `/uploads/*`. `cmd/api` creates it at startup (like Node's `index.ts`) and requires it explicitly when `APP_ENV=production` |
| `MEDIA_STORAGE` | `local` | Where new dashboard media uploads go: `local` (`UPLOADS_DIR`, URLs `/uploads/<file>`) or `s3` (the feature-image bucket, absolute public URLs). Strictly parsed; anything else is a configuration error. `/uploads/*` keeps serving `UPLOADS_DIR` either way |
| `MEDIA_S3_PREFIX` | `uploads` | S3 key prefix of media uploads when `MEDIA_STORAGE=s3`; must not overlap `S3_FEATURE_IMAGE_PREFIX` |
| `COLLECTOR_ENABLED` | `0` | Enables the feed collector loop and `POST /api/newsroom/collect` |
| `COLLECTOR_INTERVAL` | `30m` | How often the collector loop runs when enabled |
| `PUBLISHER_ENABLED` | `0` | Enables the publisher loop (queued candidate -> published article with a generated illustration) |
| `PUBLISHER_INTERVAL` | `1m` | How often the publisher loop runs when enabled (minimum `10s`) |
| `GEMINI_API_KEY` | none | Required when `PUBLISHER_ENABLED=1`; Gemini API key used for rewriting and illustration |
| `GEMINI_TEXT_MODEL`, `GEMINI_IMAGE_MODEL`, `GEMINI_VISION_MODEL` | client defaults | Optional Gemini model overrides |
| `AWS_REGION` | none | Required when `PUBLISHER_ENABLED=1`; region for the S3 feature-image bucket. Credentials come from the default AWS chain, never from a file in this repo |
| `S3_FEATURE_IMAGE_BUCKET` | none | Required when `PUBLISHER_ENABLED=1`; bucket that stores generated feature images |
| `S3_FEATURE_IMAGE_PREFIX` | `features` | Key prefix under which feature images are stored |
| `S3_FEATURE_IMAGE_PUBLIC_URL` | none | Required when `PUBLISHER_ENABLED=1`; public `https://` base URL feature images are served from |
| `INDEXNOW_ENABLED` | `0` | Submits article URLs to IndexNow: each publisher-published article, and dashboard publishes, unpublishes, published-slug renames, and deletions of published articles. Off by default, so local runs never ping IndexNow for an article that only exists in the dev database |

AWS credentials always come from the default AWS SDK credential chain (for example `AWS_PROFILE`), never from a file in this repo.

Configuration is represented by `internal/config.Config` and validated before runtime resources are opened. String and Go-syntax formatting redact `JWT_SECRET` and `GEMINI_API_KEY`. Health-only `app.New` does not require the secret, while database-backed composition fails before serving when it is absent. When `PUBLISHER_ENABLED=1`, `Validate` additionally requires `GEMINI_API_KEY`, `AWS_REGION`, `S3_FEATURE_IMAGE_BUCKET`, `S3_FEATURE_IMAGE_PUBLIC_URL` (as an `https://` URL) and a `PUBLISHER_INTERVAL` of at least `10s`. `INDEXNOW_ENABLED` uses the same strict on/off parser as the other `*_ENABLED` flags and needs no other setting.

At startup, an enabled publisher also loads AWS credentials and calls `HeadBucket` on `S3_FEATURE_IMAGE_BUCKET`, failing composition fast if credentials are missing/rejected or the bucket cannot be reached. A `403` from `HeadBucket` is tolerated: least-privilege importer IAM users that can only put/delete objects under their prefix (no `s3:ListBucket`) do not have permission to call it, and a denied write is still caught as a system fault at publish time.

Authentication preserves the reviewed Node bcrypt hashes, HS256 JWT shape, seven-day lifetime, and stateless logout behavior. JWT verification requires all identity and timestamp claims, exact integer numeric claims, a single JSON document in each segment, an HS256 header, a valid signature, and `exp` strictly after the current time. Login parsing intentionally caps request bodies at 100 KiB and returns the same stable JSON error for oversized and otherwise malformed bodies. Secrets, passwords, and raw JWTs are not included in client errors or compatibility-test failure output.

The pure `internal/content` publishing policy mirrors the retained TypeScript source allowlist, URL normalization, item rejection, AI-only gate, and rewritten-article validation. It performs no network, database, HTTP, or listener work. The dashboard article routes apply it exactly as Node does: only to articles whose next status is `published`, including the source/source-URL match.

## Developer commands

Run commands from `apps/server-go`:

```sh
make test       # all module tests
make test-race  # all tests with the race detector
make vet        # static checks
make check      # tests and static checks
make fmt        # format Go sources
make contracts-check   # verify mirror drift and focused executable contract tests
make contracts-accept  # explicitly accept the reviewed canonical Node fixture
go run ./cmd/migrate   # explicitly migrate the guarded configured database
```

`cmd/api` opens and closes its configured database but never migrates or seeds it. Run `cmd/migrate` as an explicit deployment step first. `cmd/api` also creates `UPLOADS_DIR` if it is missing. `app.New` remains a no-I/O health-only composition; `app.NewWithDatabase` audibly mounts article, category, author, authentication, newsroom, authenticated `/api/dashboard` content, settings, and media routes, and the static `/uploads/*` files around a caller-owned database and uploads directory.

The migration runner supports fresh databases and databases already managed by its ledger. It intentionally cannot stamp or adopt an existing unmanaged database initialized by the Node server, although `cmd/api` can read that compatible schema. Cutover requires a future explicit full-schema verifier/adoption command; do not weaken `migrate.Run` or partially stamp an unmanaged database.

## Architecture

The service is a single binary with a `cmd` plus `internal` layout:

- `cmd/api` loads guarded configuration, opens and owns the SQLite pool, injects it into the application, and closes it after shutdown. `cmd/migrate` is the only schema deployment entry point.
- `internal/app` wires modules and infrastructure, including gathering capability-owned migration descriptors in execution order.
- `editorial` owns the foundational v1 authors schema and public author read stack, while `content` owns the v2 categories/articles schema and the public article and category read stacks. Their public handlers mount relative route manifests from the composition root.
- `settings` owns the dashboard site-settings behavior but no migration: newsroom migration 3 creates the shared `settings` table with Node's schema.
- `media` owns both article-image stores: generated feature images in S3 and the dashboard media library (migration 5, Node's `media` table), whose files live in `UPLOADS_DIR`.
- Future cohesive capabilities such as `newsletter` own their domain, repository, service, HTTP handlers, relative route mounting, and migration SQL.
- `internal/database/migrate` owns only the migration ledger and runner; it does not own capability schema SQL. The runner owns migration transaction boundaries, so descriptors must not contain transaction control, `VACUUM`, `ATTACH`, `DETACH`, or `PRAGMA` statements. `ATTACH`, `DETACH`, and `PRAGMA` are rejected because their file, attachment, or connection effects can survive a rollback.
- Narrow infrastructure packages live under `internal` and are named for their purpose.
- Interfaces are declared by the consuming package at the point of use. Constructors return concrete types unless a consumer needs an interface.

Capability packages do not import one another. Cross-capability behavior is injected through narrow interfaces owned by the consumer. Domain and service code remain independent of HTTP and concrete SQLite types. Do not create generic `utils`, `common`, or global service-locator packages.

## Dashboard content administration

`internal/content` (`AdminService`, `AdminHandler`) and `internal/settings` port every non-media route of `apps/server/src/routes/dashboard.ts`. All of them sit in one chi group, `/api/dashboard`, behind `RequireAuth`; like Node, unknown *dashboard* paths (`/api/dashboard/*`) answer `401` without a token, and any valid token may use every route (Node has no roles). This is not general parity for every unknown path: an unknown **non-dashboard** `/api/*` path answers `404 {"error":"Not found"}` in Go, where Node's `router.use(requireAuth)` on the dashboard router incidentally catches it too and answers `401`; this gap is pre-existing and out of scope for this port (see the plan's "Go transport differences"). `internal/jsonbody` reproduces `express.json()` and the JavaScript/better-sqlite3 value semantics the Node handlers rely on (undefined vs `null`, truthiness, `String()` coercion, numbers binding as REAL). Each mutation runs in one SQLite transaction. IndexNow notifications for dashboard changes are queued after the response, never block it, are drained (with a bounded deadline) on shutdown, and only run with `INDEXNOW_ENABLED=1`.

Node behaviors that are kept for parity although they look wrong are listed, with tests, in `docs/superpowers/plans/2026-09-24-go-admin-content.md` ("Known Node behaviors kept"). The most visible: duplicate category slugs fail with `500`. Two reviewed contract changes intentionally diverge from Node: **changed: blank settings are stored as empty strings** -- Node upserts a blank social/webhook field as SQL `NULL`, which violates `settings.value NOT NULL` and rolls the whole update back with a `500` (the dashboard sends `null` for every blank field on every save, so Node cannot save settings while any of them is blank); Go stores an empty string instead. And `GET /api/dashboard/settings` hides keys with the `newsroom.` prefix, and `PUT` can never write them, instead of exposing newsroom's own state on the dashboard settings surface as Node does. Transport-level differences from Express (JSON instead of HTML error pages, `400 {"error":"Invalid request body"}` for malformed or oversized bodies, where Express answers `413`) follow the auth slice. One more: updating an article whose stored `author_id` no longer matches any `authors` row commits in Node (better-sqlite3 does not enforce the foreign key) but answers `500` and rolls back in Go, because `database.Open` runs with `PRAGMA foreign_keys = ON` and the post-write readback `INNER JOIN`s `authors` (see the plan's "Go transport differences" table).

## Dashboard media library

`internal/media` (`Uploads`, `Library`, `Handler`) ports `apps/server/src/upload.ts`, the media routes of `apps/server/src/routes/dashboard.ts`, and `app.use("/uploads", express.static(uploadRoot))`. `GET /api/dashboard/media`, `POST /api/dashboard/media/upload`, and `DELETE /api/dashboard/media/:id` sit in the authenticated `/api/dashboard` group; `GET`/`HEAD /uploads/*` is public. Parity with Node (recorded from the real multer/busboy/serve-static stack, see the plan below): one file in the multipart field `file`; client-declared MIME type `image/jpeg`, `image/png`, `image/webp`, or `image/gif` (lowercased, parameters dropped, content not sniffed); files must be smaller than 5 MiB (a file of exactly 5 MiB is rejected, as in busboy); the stored name is 16 `crypto/rand` bytes in hex plus Node's `path.extname` of the original name (case kept); `url` is `/uploads/<name>`; `filename` is the original name after busboy's basename and latin1 decoding (so a UTF-8 name is stored with Node's mojibake); `uploaded_at` is `YYYY-MM-DD HH:MM:SS` UTC; the list is `ORDER BY uploaded_at DESC`; delete answers `404 {"error":"Media not found"}` for an unknown id, removes `path.basename(url)` from the uploads directory (a missing file is fine) and then the row. Static files carry send's `Cache-Control: public, max-age=0`, weak `ETag`, `Last-Modified`, byte ranges and conditional `304`s, with Content-Type chosen by extension.

Two reviewed changes intentionally diverge from Node (user decision, 2026-09-24): **changed: the upload filter also requires a matching image extension** -- the original name's extension, compared case-insensitively, must be `.jpg`/`.jpeg` for `image/jpeg`, `.png` for `image/png`, `.webp` for `image/webp`, or `.gif` for `image/gif`; anything else (for example `evil.html` declared as `image/png`, a name without an extension, `.png`, or `p.png` declared as `image/jpeg`) is rejected exactly like a bad type, `500 {"error":"Only .jpg, .png, .webp, and .gif files are allowed"}`. Node checked the declared type only, so it stored and then served such files as HTML from the API origin. And **changed: every `/uploads/*` response carries `X-Content-Type-Options: nosniff`**, including legacy files Node accepted with other extensions, which stay served with their extension's Content-Type. Any file that is not JPEG, PNG, WebP, or GIF (for example a legacy `.svg`, `.html`, `.xml`, or extension-less file) is additionally served with `Content-Security-Policy: sandbox; default-src 'none'` and `Content-Disposition: attachment`, so it is downloaded rather than rendered on the API origin.

The upload route alone extends its connection deadlines before reading the multipart body (`http.ResponseController`): 2 minutes to read the body (`media.UploadReadTimeout`) and 2.5 minutes to write the response, so a 5 MiB upload over a slow link is not cut off by the server-wide `ReadTimeout` of 15s (which would need roughly 350 KB/s). Files served from `/uploads/*` likewise get a 2-minute write deadline (`media.DownloadWriteTimeout`), set only once a file is about to be sent, so a large legacy file on a slow link is not cut off at 30s. Every other route keeps the server's 15s read and 30s write timeouts.

Every file operation goes through an `os.Root` opened on `UPLOADS_DIR`, so neither serving nor deleting can reach outside it (including through `..`, encoded separators, absolute paths, or symlinks pointing elsewhere); dotfiles and directories answer `404`, and deletes never remove a directory. Multipart bodies are streamed straight to disk, never buffered in memory, and capped at 6 MiB in total. Transport-level differences from Express follow the earlier slices: multer rejections answer `500` with the multer message as JSON (`Only .jpg, .png, .webp, and .gif files are allowed`, `File too large`, `Unexpected field`) instead of an HTML error page; other malformed multipart bodies answer `500 {"error":"Internal server error"}`; a failed or rejected upload never leaves a file behind (Node leaves one after a truncated body, an oversized text field, or a failed insert); unknown `/uploads/*` paths answer `404 {"error":"Not found"}` instead of `Cannot GET`; `/uploads` and subdirectories are not redirected; and a symlink out of the uploads directory is not followed. The full list is in `docs/superpowers/plans/2026-09-24-go-media.md`.

**Storage backends.** The handlers and the `media` table only see a `media.Storage` (`Save`, `Remove`) and the URL it returns. `MEDIA_STORAGE=local` (the default) is the local directory above. `MEDIA_STORAGE=s3` stores new uploads in `S3_FEATURE_IMAGE_BUCKET` under `MEDIA_S3_PREFIX` (default `uploads/`) with the declared content type, `Cache-Control: public, max-age=31536000, immutable`, and a SHA-256 checksum, verifies the object through `S3_FEATURE_IMAGE_PUBLIC_URL` (deleting it again if that fails), and records `<S3_FEATURE_IMAGE_PUBLIC_URL>/<key>` as the URL; it reuses `AWS_REGION`, the default AWS credential chain, and the same startup credential/`HeadBucket` check as the publisher (one shared client), and requires the three S3 settings at configuration time. With S3, `/uploads/*` still serves the legacy files in `UPLOADS_DIR`, and deletes are routed by URL shape: a URL under `<public URL>/<MEDIA_S3_PREFIX>/` deletes that object (only single-segment names under the prefix), anything else deletes from `UPLOADS_DIR` with Node's basename rule. Switching back to `local` keeps S3 rows readable (their URLs are absolute), but deleting one then only removes the row, not the object. **The current importer IAM user can only write `features/`**, so enabling `MEDIA_STORAGE=s3` first needs an IAM policy update granting `s3:PutObject` and `s3:DeleteObject` on `<bucket>/uploads/*` (or the chosen prefix).

At cutover the Node uploads directory is copied with the database; point `UPLOADS_DIR` at the copy so every existing `/uploads/...` URL in `articles.featured_image`, `authors.avatar`, and `media.url` keeps resolving.

## Newsroom (editorial queue)

`internal/newsroom` owns the `candidates` table (migration 3) and the
authenticated `/api/newsroom/*` routes described in
`contracts/newsroom.openapi.yaml`. Candidates are policy-passing feed items
awaiting an editor's decision. Publishing queues them one after another,
each `random(min..max)` minutes after the previous queue entry (defaults 30–40,
stored in `settings`). Rejected candidates are kept so their URLs are never
re-collected.

The collector (`internal/collector`) fetches the approved feeds, applies the
publishing policy and stores new items as pending candidates; it never
publishes. It runs when `COLLECTOR_ENABLED=1` (every `COLLECTOR_INTERVAL`,
default `30m`, and on `POST /api/newsroom/collect`); otherwise that endpoint
answers `503`. `make dev-api` enables it.

The publisher (`internal/publisher`) runs when `PUBLISHER_ENABLED=1` (every
`PUBLISHER_INTERVAL`, default `1m`) and requires `GEMINI_API_KEY` plus the
`AWS_REGION`/`S3_FEATURE_IMAGE_*` settings above. Each tick claims at most one
due candidate — respecting the configured minimum gap since the last publish —
and only one candidate is ever in flight at a time. A claimed candidate flows
through:

1. source fetch and canonical-URL extraction (`internal/collector`,
   `internal/publisher`'s source helpers);
2. Gemini rewrite into two drafts (`internal/publisher/rewrite.go`);
3. illustration: up to 3 Gemini image generations, each checked by a Gemini
   vision compliance review (no text/logos/unsupported injury), with the
   source's `og:image` (or the candidate's stored source image) used only as
   an optional in-memory reference — never stored or hotlinked
   (`internal/illustration`);
4. WebP encoding (`internal/imaging`) and S3 upload with a public-URL
   verification fetch (`internal/media`);
5. article insert and marking the candidate published, in one database
   transaction (`internal/publisher/articles.go`);
6. an IndexNow submission for the new URL, when `INDEXNOW_ENABLED=1`.

Failures are classified so retries make sense:

- **permanent** (policy rejection, duplicate, unusable source, empty slug, a
  compliance-rejected illustration after 3 attempts) marks the candidate
  `failed`, no retry;
- **transient** (rate limits, 5xx, network) requeues in `RetryDelay` (5
  minutes), consuming one of `MaxAttempts` (3) attempts, then fails
  permanently;
- **system faults** (bad Gemini key/model, denied AWS credentials, missing S3
  bucket) requeue after the same delay but without consuming an attempt, since
  no candidate is at fault;
- a **shutdown** mid-publish requeues the candidate immediately, attempt
  intact.

AWS credentials always come from the default credential chain (for example
`AWS_PROFILE`), never from a file in this repo. `make dev-publish` enables the
publisher with `S3_FEATURE_IMAGE_PREFIX=dev-features` by default so local runs
never mix with production images — but the importer's IAM user can currently
only write under `features/`, so a real local publish needs either
`S3_FEATURE_IMAGE_PREFIX=features` or an IAM policy change granting write
access under `dev-features/`. Design:
`docs/superpowers/specs/2026-09-23-ai-tech-news-newsroom-design.md`.

### Local development with the Omni Control app

```bash
make dev-seed   # migrate data/technews.db, create dev@example.invalid / dev-password, add 12 candidates
make dev-api    # serve on 127.0.0.1:4401 with a local JWT secret
```

`dev-seed` is idempotent (it re-sets the dev password and skips existing candidates) and refuses to run with `APP_ENV=production`. A Debug build of the iOS app points at `http://127.0.0.1:4401/api`.
