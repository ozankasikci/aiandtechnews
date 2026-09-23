# Technews Go API

This directory is an isolated Go module for the API migration. It does not share a Go module or developer commands with the existing Node server.

Tasks 5 through 12 currently provide the four public article reads, public category and author listings, compatible login, current-user, and logout endpoints, a pure Go port of the retained publishing policy, the authenticated dashboard content administration (dashboard article list/get/create/update/delete, category list/create/update/delete, and site settings get/put), the dashboard media library (media list/upload/delete plus static `/uploads/*` serving from a local directory), and the newsletter (signup, confirm and unsubscribe links, the edition archive, and the daily digest delivered through Resend). Migration task 14 adds cutover readiness: explicit production configuration guarded by a data marker, the `cmd/adopt` command that verifies and adopts the Node-created production database, a local smoke script, and the Mac mini runbooks in [`docs/cutover.md`](docs/cutover.md) and [`docs/rollback.md`](docs/rollback.md). The cutover itself is a manual, approved operation on the production machines.

## Safety defaults

Development configuration is deliberately isolated:

- `SERVER_ADDR` defaults to `127.0.0.1:4401`. In development, ports `3001`, `3002`, and `4001` are rejected; production (`APP_ENV=production`) accepts any port.
- `DATABASE_PATH` defaults to `data/technews.db` and `UPLOADS_DIR` to `data/uploads` beneath the worktree root (git-ignored). **Production has no defaults:** `APP_ENV=production` requires both, as absolute paths, and needs no repository checkout (the commands skip the worktree lookup). `UPLOADS_DIR` must be absolute and must never contain `DATABASE_PATH` (it may not be the database's directory or an ancestor of it), in every mode.
- **Production marker.** A file named `.technews-production` marks a production data root. With `APP_ENV=production`, `DATABASE_PATH` and `UPLOADS_DIR` must both lie inside a directory that contains it; in development, any database or uploads path inside such a directory is refused (`ErrProductionDatabaseAlias` / `ErrProductionUploadsAlias`). The check runs on the symlink-resolved path and walks every ancestor, so aliases and not-yet-created files below a marked directory are refused too. The marker travels with the data, so no environment has to remember a guard, and production cannot run without it being in place.
- Optional extra development guards: `PRODUCTION_DATABASE_PATH` (a `DATABASE_PATH` equivalent to it by name, symlink, hard link, or case variant is refused) and `PRODUCTION_UPLOADS_DIR` (an `UPLOADS_DIR` equal to, inside, or containing it is refused). Production ignores both.
- The Node MacBook paths `/Users/ozan/Projects/technews/apps/server/data/technews.db` and `/Users/ozan/Projects/technews/apps/server/uploads` (the rollback copy after cutover) stay refused in development, including aliases and overlapping directories.
- **Production runtime:** `JWT_SECRET` must be at least 32 bytes and must not be Node's public fallback (`technews-dev-secret-change-in-production`); `TZ` must be set to a loadable zone (production uses `Europe/Istanbul`, the Node host's zone). Development keeps accepting short local secrets and no `TZ`.
- **Production startup:** `cmd/api` opens the database with `database.OpenForServing` (Open's WAL/foreign-key/busy-timeout settings, but it never creates the file or its directory) and refuses to serve unless the database has the migration ledger with every migration applied and unchanged (`app.CheckSchema`, built on the read-only `migrate.Status`). A wrong `DATABASE_PATH`, a Node database that was not adopted, or a forgotten `cmd/migrate` stop the process instead of serving.
- Development and tests must never use the production checkout or database. Tests use temporary databases.

`APP_ENV=production` is for the production host (see `docs/cutover.md`), not a development convenience.

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
| `SERVER_ADDR` | `127.0.0.1:4401` | HTTP listen address (any port in production) |
| `DATABASE_PATH` | `<worktree>/data/technews.db` in development; **none in production** | SQLite database path; production requires an absolute path inside a directory marked with `.technews-production` |
| `PRODUCTION_DATABASE_PATH`, `PRODUCTION_UPLOADS_DIR` | none | Optional development guards (see Safety defaults); ignored in production |
| `JWT_SECRET` | none | Required by database-backed composition for signing and verifying authentication tokens; production requires at least 32 bytes and refuses Node's fallback secret |
| `TZ` | system zone | Process time zone (offset-less `published_at` values are read in it); required in production |
| `UPLOADS_DIR` | `<worktree>/data/uploads` in development; **none in production** | Absolute path of the dashboard media library directory, served at `/uploads/*`. `cmd/api` creates it at startup (like Node's `index.ts`). Production requires it explicitly, inside a marked directory |
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
| `NEWSLETTER_SITE_URL` | `https://aiandtech.news` | Origin used in every email link. Like Node it must be `https` (plain `http` only for `localhost`); only its origin is used. An invalid value fails composition, as it stops Node from starting. Go accepts only `http`/`https` URLs with ASCII host names |
| `NEWSLETTER_TOKEN_SECRET` | none | Signs and verifies confirm and unsubscribe links (Node's HMAC-SHA256 token format, byte for byte). **Must be the Node value**, or links in emails already sent stop working. Needs at least 32 characters after trimming; otherwise confirm and unsubscribe answer `503` and the digest `503`, while signup keeps working |
| `NEWSLETTER_CRON_SECRET` | value of `CRON_SECRET` | Secret for `GET`/`POST /api/newsletter/digest` (`Authorization: Bearer <secret>`). Node reads `NEWSLETTER_CRON_SECRET \|\| CRON_SECRET`, so only an empty value falls back. Empty (or blank) refuses every digest request with `401` |
| `CRON_SECRET` | none | Used only when `NEWSLETTER_CRON_SECRET` is empty; the Vercel project's name for the same secret |
| `RESEND_API_KEY` | none | Resend API key. Without it (or without `NEWSLETTER_FROM`) nothing is sent: each digest delivery is recorded as failed with `Newsletter delivery is not configured`, like Node |
| `NEWSLETTER_FROM` | none | Sender, for example `AI & Tech News <news@aiandtech.news>` (a verified Resend domain) |
| `NEWSLETTER_REPLY_TO` | none | Optional `reply_to` for every email |
| `INDEXNOW_ENABLED` | `0` | Submits article URLs to IndexNow: each publisher-published article, and dashboard publishes, unpublishes, published-slug renames, and deletions of published articles. Off by default, so local runs never ping IndexNow for an article that only exists in the dev database |

AWS credentials always come from the default AWS SDK credential chain (for example `AWS_PROFILE`), never from a file in this repo.

Configuration is represented by `internal/config.Config` and validated before runtime resources are opened. String and Go-syntax formatting redact `JWT_SECRET`, `GEMINI_API_KEY`, `RESEND_API_KEY`, `NEWSLETTER_TOKEN_SECRET`, and the cron secret. The newsletter settings are optional at startup, like in Node. Health-only `app.New` does not require the secret, while database-backed composition fails before serving when it is absent. When `PUBLISHER_ENABLED=1`, `Validate` additionally requires `GEMINI_API_KEY`, `AWS_REGION`, `S3_FEATURE_IMAGE_BUCKET`, `S3_FEATURE_IMAGE_PUBLIC_URL` (as an `https://` URL) and a `PUBLISHER_INTERVAL` of at least `10s`. `INDEXNOW_ENABLED` uses the same strict on/off parser as the other `*_ENABLED` flags and needs no other setting.

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
go run ./cmd/adopt     # read-only: verify a Node-created database against the Go migrations
go run ./cmd/adopt --apply  # back up with VACUUM INTO, then adopt it into the ledger
scripts/smoke-local.sh <database> [uploads-dir]  # smoke-test the API on a temporary copy
```

`cmd/api` opens and closes its configured database but never migrates or seeds it (in production it also refuses to start on an unmanaged or partially migrated database). With a database, `GET /api/health` runs `SELECT 1` (2s timeout): `200 {"status":"ok"}` as before, or `503 {"status":"error"}` when the database cannot answer. Run `cmd/migrate` as an explicit deployment step first. `cmd/api` also creates `UPLOADS_DIR` if it is missing. `app.New` remains a no-I/O health-only composition; `app.NewWithDatabase` audibly mounts article, category, author, authentication, newsroom, authenticated `/api/dashboard` content, settings, and media routes, the public newsletter routes, and the static `/uploads/*` files around a caller-owned database and uploads directory.

The migration runner supports fresh databases and databases already managed by its ledger; `migrate.Run` still refuses an unmanaged database (`ErrUnmanagedDatabase`). Databases created by the Node server are brought under the ledger only by `cmd/adopt`:

- **Default: read-only dry run.** It opens the database with `mode=ro` (never creating it), compares it with a reference built by applying the Go migrations to a scratch database, runs `integrity_check` and `foreign_key_check`, prints a report, then rehearses the whole adoption on a `VACUUM INTO` copy in a temporary directory. Exit `0` when adoptable, `1` on any mismatch.
- **How compatibility is decided.** A migration is *present* when every object it creates exists, *pending* otherwise. Every Go object that exists is compared: columns as a set by name (order is ignored, since older databases got columns via `ALTER TABLE`) with declared type, NOT NULL, default, primary-key position, and hidden kind, **and each column's full definition text** from the stored `CREATE TABLE` (normalized only for whitespace, keyword case, identifier quoting, and comments), so `COLLATE`, `ON CONFLICT`, `GENERATED ALWAYS AS`, and column-level clauses must match exactly; table constraints (`PRIMARY KEY`, `UNIQUE`, `CHECK`, `FOREIGN KEY`, including `ON CONFLICT` and `DEFERRABLE`) as normalized text; CHECK constraints (token-normalized); foreign keys (including ON DELETE/UPDATE); UNIQUE/PRIMARY KEY constraints and named indexes (columns, order, collation, partial WHERE, and the normalized `CREATE INDEX` text); AUTOINCREMENT, WITHOUT ROWID, STRICT. The only accepted deviation is the known Node variant `subscribers.created_at`/`updated_at` as nullable `TEXT` without a default (Node's `ALTER TABLE`; Go always writes both columns). Extra tables, views, nullable or defaulted columns, and non-unique indexes are listed and left alone; extra NOT NULL columns without a default, extra UNIQUE indexes, CHECKs, foreign keys, and triggers on Go tables are mismatches. A failed `integrity_check` is a mismatch; foreign key violations are warnings.
- **`--apply`** writes `<DATABASE_PATH>.pre-adopt-<UTC timestamp>.db` with `VACUUM INTO` (refusing an existing file; it includes frames still in the `-wal` file) and checks it, then calls `migrate.Adopt`: in one `BEGIN IMMEDIATE` transaction it recounts every table (the counts must equal the backup's, so a concurrent writer stops it), verifies the schema again, creates the ledger, records the present migrations with their exact names and checksums, runs the pending ones (for a Node database: newsroom 3 and 4), and, before `COMMIT`, requires the complete Go schema and row counts where no existing table disappeared, lost rows, or gained rows except `settings` (+2 at most, migration 3's `newsroom.*` rows). Recording 1, 2, 5, 6 and then calling `migrate.Run` would fail with `ErrHistoryGap`, hence the single transaction. `migrate.Run` afterwards is a no-op, and the result is verified against the reference again. Nothing is dropped, rewritten, or deleted; migration 3 adds its two `newsroom.*` settings rows.
- **Idempotent:** a database with a ledger reports `already managed` and exits `0` without writing.

The Node schema fixtures come from Node itself: `internal/database/adopt/testdata/record-node-schema.ts` runs `apps/server/src/db.ts` `initializeDatabase` on in-memory databases and writes `node-schema.json` (a fresh database, and a legacy one whose `articles.source`/`source_url` and `subscribers` columns Node added with `ALTER TABLE`).

## Architecture

The service is a single binary with a `cmd` plus `internal` layout:

- `cmd/api` loads guarded configuration, opens and owns the SQLite pool, injects it into the application, and closes it after shutdown. `cmd/migrate` is the only schema deployment entry point for managed databases; `cmd/adopt` (with `internal/database/adopt`) is the one-time entry point for the Node-created production database.
- `internal/app` wires modules and infrastructure, including gathering capability-owned migration descriptors in execution order.
- `editorial` owns the foundational v1 authors schema and public author read stack, while `content` owns the v2 categories/articles schema and the public article and category read stacks. Their public handlers mount relative route manifests from the composition root.
- `settings` owns the dashboard site-settings behavior but no migration: newsroom migration 3 creates the shared `settings` table with Node's schema.
- `media` owns both article-image stores: generated feature images in S3 and the dashboard media library (migration 5, Node's `media` table), whose files live in `UPLOADS_DIR`.
- `newsletter` owns migration 6 (Node's `subscribers`, `newsletter_deliveries`, and `newsletter_editions` tables) and the public newsletter routes; it only reads `articles` and `categories`. Like every capability it owns its domain, repository, service, HTTP handlers, relative route mounting, and migration SQL.
- `internal/database/migrate` owns only the migration ledger, the runner, and `Adopt`; it does not own capability schema SQL. The runner owns migration transaction boundaries, so descriptors must not contain transaction control, `VACUUM`, `ATTACH`, `DETACH`, or `PRAGMA` statements. `ATTACH`, `DETACH`, and `PRAGMA` are rejected because their file, attachment, or connection effects can survive a rollback.
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

## Newsletter

`internal/newsletter` ports `apps/server/src/newsletter/*` and the newsletter routes of `apps/server/src/routes/public.ts`; the plan, with every kept Node behavior and every Go difference, is `docs/superpowers/plans/2026-09-24-go-newsletter.md`. The routes are public (no dashboard login): `POST /api/subscribe`, `GET /api/newsletter/confirm`, `GET` and `POST /api/newsletter/unsubscribe`, `GET /api/newsletter/editions`, `GET /api/newsletter/editions/:edition`, and `GET` and `POST /api/newsletter/digest` (cron secret). The eight Node newsletter contracts replay in `internal/app/newsletter_contract_test.go`.

Parity is pinned by vectors recorded from the Node implementation (`internal/newsletter/testdata/node-golden.json`, regenerated by `testdata/record-node.ts`, see its header), not inferred:

- **Signup** activates the address at once and sends nothing, so it works without any email settings; resubmitting is idempotent, and pending (old double opt-in) and unsubscribed rows are reactivated. Addresses are trimmed and lowercased with JavaScript semantics (Unicode whitespace, `İ`, final sigma) and validated with Node's pattern and 254-code-unit limit. Signups keep Node's `429` answer and rolling minute, but see the approved changes below for how they are counted.
- **Tokens** are byte-identical: `base64url(JSON)` + `.` + `base64url(HMAC-SHA256)`, the secret trimmed, verification as lenient as Node's (a trailing `.` is accepted, the payload is decoded like `Buffer.from(…, "base64url")`, `exp` compared in whole seconds). Links in emails Node already sent keep working as long as `NEWSLETTER_TOKEN_SECRET` is unchanged.
- **Editions** are keyed by the Europe/Istanbul calendar date (the zone database is embedded, so the host's zoneinfo does not matter). The archive passes stored article JSON through when it is an array and shows `[]` otherwise.
- **Digest selection** is Node's: the 20 published articles with the greatest `published_at` text, kept when published in the last 30 hours and not in the future (Date.parse semantics: SQLite timestamps and date-only values are UTC, date-times without an offset are read in the process time zone), first 5, grouped by category in the email.
- **Emails** (HTML and text) and the **Resend request body** are byte-identical to Node's. Idempotency keys are Node's, `newsletter-welcome-<subscriber>` and `newsletter-digest-<edition>-<subscriber>`. At most three attempts; only `429` and `5xx` are retried, after `Retry-After` seconds (at most 60s, see below) or 650ms then 1300ms; transport errors (recorded as `fetch failed`), other statuses, and a `2xx` without an id fail at once. Each attempt has a 30s timeout.
- **Deliveries** move `sending` -> `sent` (provider message id, `sent_at`) or `failed` (error truncated to 500 UTF-16 units). The next run retries `failed` and `sending` rows with the same idempotency key and skips `sent` ones. Deliveries are 550ms apart.

Go-only behavior: digest runs are serialized within the process, so overlapping requests (a cron retry and a manual `POST`) never deliver the same edition twice; like Node, a started digest keeps running when the HTTP client goes away, but shutdown stops it between deliveries (the outcome of an in-flight send is still recorded). The digest route has a 15-minute write deadline and the confirm route 2 minutes (for the welcome email); other routes keep the server's timeouts. Error bodies for malformed JSON follow the earlier slices (`400 {"error":"Invalid request body"}`).

**Approved changes from Node (user decisions, 2026-09-24):**

- **Signup throttle per client IP.** Node allowed 30 signup attempts per rolling minute *in total*, so one client could block signup for everyone. Go allows 30 per rolling minute per client IP, with a ceiling of 300 per minute across all clients; refused attempts are not counted, and the answer is Node's `429 {"error":"Too many signup attempts. Please try again shortly."}`. The client IP is the peer address, except that a loopback peer (the tunnel or reverse proxy on the same host) is replaced by the first `X-Forwarded-For` address; addresses are normalized (IPv4-mapped IPv6 as IPv4, canonical IPv6). At most 10,000 clients are tracked: idle ones are evicted first, then the least recently seen. Note that the website's `/api/subscribe` route calls the API from Vercel without forwarding the visitor's address, so website signups share Vercel's bucket until that route forwards `X-Forwarded-For`.
- **`Retry-After` is capped at 60 seconds.** Node waited as long as Resend asked (a `Retry-After: 3600` stalled the whole digest for an hour, and anything beyond 2^31-1 ms fired after 1ms instead).

**Shutdown.** Digest runs and welcome emails outlive their HTTP request (as in Node) but not the process: shutdown cancels them, a delivery interrupted mid-send is recorded `failed` (`context canceled`) and retried by the next run with the same idempotency key, a digest waiting for its turn gives up, and `App.Run` waits up to 15s after the HTTP server stops for their outcomes to be recorded. Transport failures are stored as Node's `fetch failed`; the log line carries the underlying cause (never a recipient address).

**Same-day re-runs.** A second digest run for an edition (a manual `POST` after the cron) re-selects the articles, replaces the archived edition's subject and articles (keeping its `created_at`), skips subscribers already `sent`, and retries the rest with the same idempotency keys. Within 24 hours Resend treats a repeated key with an identical request as the original send and one with a different request (for example, a new lead story changed the subject) as a conflict (`409`), which is recorded as `failed` without sending: nobody gets two copies of one edition.

**Supported databases.** Go expects a database created by its own migrations or last opened by the current Node server (which adds any missing `subscribers` columns at startup). An older `subscribers` table without `status` is reported by `cmd/adopt` as a missing column (and would make migration 6's SQL fail on `idx_subscribers_status`) instead of being patched silently; start the current Node server once (or add the columns) before adopting such a database.

**Node and Go on one database.** Only one of them should serve the digest: the Vercel cron calls whatever `API_URL` points at, so switching `API_URL` switches the digest. If both ever ran a digest for the same edition, the per-row `sent` check is not a lock between processes; what prevents a second email is Resend's idempotency key, which both use in the same format with byte-identical payloads: for 24 hours Resend answers a repeated key with the original send instead of sending again (a different payload under the same key is refused with `409` and recorded as failed). No test or code path in this repository calls the real Resend API: tests use `httptest` servers or fakes.

At cutover, copy `NEWSLETTER_TOKEN_SECRET`, `RESEND_API_KEY`, `NEWSLETTER_FROM`, `NEWSLETTER_REPLY_TO`, `NEWSLETTER_SITE_URL`, and `NEWSLETTER_CRON_SECRET` (equal to the Vercel project's `CRON_SECRET`) from the Node environment, and run the Go process with the same `TZ` as the Node host (offset-less `published_at` values from the dashboard's date picker are read in that zone).

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
