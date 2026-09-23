# Cutover: Node on the MacBook → Go on the Mac mini

This runbook moves production from the Node API on the MacBook to the Go API
alone on a Mac mini. After it, Node is retired (kept stopped and intact for
[rollback](rollback.md)). Every command runs on the machine named in its step;
nothing here is run from a development checkout against production.

**Today:** Node API (`apps/server`, port 4001) + SQLite
`apps/server/data/technews.db` + `apps/server/uploads/` + an external
`news:daily` scheduler (`pnpm --filter @technews/server news:daily`), all on the
MacBook, published by a `subtunnel local` client as
`https://technews.subtunnel.dev`. The website (Vercel) reads
`API_URL=https://technews.subtunnel.dev`; its daily crons call
`/api/newsletter/digest` (05:00 UTC) and `/api/indexnow` (06:00 UTC) through
that URL.

**After:** Go API (`bin/api`) on the Mac mini, port 4001 on `127.0.0.1`, same
database (adopted into Go's migration ledger), same uploads, same tunnel
hostname. The website and its crons need **no change**: the hostname stays the
same. The Node scheduler is gone; the Go collector and publisher replace it
and are enabled last.

Placeholders: `$ROOT` is the production data root on the Mac mini (for
example `/Users/<you>/technews`), `$NODE` is the Node checkout on the MacBook
(`/Users/ozan/Projects/technews`), `$REPO` is a checkout of this repository.

---

## 0. What you need

| Item | Where from |
| --- | --- |
| Go binaries `api`, `adopt`, `migrate` for the Mac mini | built from `$REPO/apps/server-go` (below) |
| The production database | the MacBook, `$NODE/apps/server/data/technews.db` (copied with `sqlite3 .backup`, never `cp` of a live WAL database) |
| The uploads directory | the MacBook, `$NODE/apps/server/uploads/` |
| Secrets and settings | the Node process environment on the MacBook (table in step 2) |
| The `subtunnel local` command and its token | however the MacBook runs it today (step 3) |
| `CRON_SECRET` | the Vercel project's environment (`apps/web`) |

## 1. Mac mini preparation (no downtime)

1. **Keep it awake and on.** The August outage
   (`.planning/debug/production-backend-outage.md`) was the origin going
   offline. System Settings → Energy: prevent automatic sleeping, start up
   after a power failure (or `sudo pmset -a sleep 0 disksleep 0 autorestart 1`).
   Check `uname -m` (expect `arm64`).
2. **Binaries.** Either install Go (1.25+, <https://go.dev/dl/>, plus Xcode
   command line tools for cgo) on the Mac mini and build there, or build on
   another Mac of the **same architecture** and copy them (the WebP encoder
   uses cgo, so do not cross-compile between Intel and Apple silicon):

   ```sh
   cd "$REPO/apps/server-go"
   go test -p 2 ./...
   mkdir -p dist
   go build -p 2 -trimpath -o dist/api ./cmd/api
   go build -p 2 -trimpath -o dist/adopt ./cmd/adopt
   go build -p 2 -trimpath -o dist/migrate ./cmd/migrate
   ```

   Copy `dist/*` to `$ROOT/bin/` with `scp`/`rsync` (files that arrive through
   AirDrop or a browser get a quarantine flag: `xattr -d com.apple.quarantine
   "$ROOT"/bin/*`). With `APP_ENV=production` the binaries need no repository
   checkout.
3. **Directories and the production marker.** Put `$ROOT` under the home
   directory but **not** under `~/Documents`, `~/Desktop`, or `~/Downloads`
   (macOS privacy protection blocks launchd jobs there).

   ```sh
   mkdir -p "$ROOT/bin" "$ROOT/data" "$ROOT/uploads" "$ROOT/logs"
   touch "$ROOT/.technews-production"
   chmod 700 "$ROOT"
   ```

   The marker is what makes this a production data root: with
   `APP_ENV=production`, `DATABASE_PATH` and `UPLOADS_DIR` **must** be inside
   a directory containing `.technews-production`, and every development run
   (no `APP_ENV`, or `APP_ENV=development`) **refuses** any database or uploads
   path inside one, on any machine. Do not delete it. For extra safety in a
   development shell on this machine you can also export
   `PRODUCTION_DATABASE_PATH` and `PRODUCTION_UPLOADS_DIR` (development runs
   refuse those paths too; production ignores them).

## 2. Environment file

Copy `docs/technews.env.example` to `$ROOT/technews.env`, `chmod 600`, and fill
it in. To read the Node process's environment on the MacBook (only your own
processes): `ps eww -o command= -p "$(lsof -t -iTCP:4001 -sTCP:LISTEN)"`; also
check `$NODE/apps/server/.env` and however Node is launched (launchd plist,
`pm2`, a shell script). The scheduler's own environment (Gemini) is wherever
`news:daily` is started from.

| Variable | Value |
| --- | --- |
| `APP_ENV` | `production` |
| `SERVER_ADDR` | `127.0.0.1:4001` (loopback only; the tunnel client connects locally). Production accepts any port |
| `DATABASE_PATH` | `$ROOT/data/technews.db` (absolute) |
| `UPLOADS_DIR` | `$ROOT/uploads` (absolute; must not contain the database) |
| `TZ` | the MacBook's zone, expected `Europe/Istanbul` (check `date +%Z` / `sudo systemsetup -gettimezone` on the MacBook). Offset-less `published_at` values from the dashboard are read in it |
| `JWT_SECRET` | Node reads `JWT_SECRET` and falls back to the public string `technews-dev-secret-change-in-production`. If the Node environment has a real one, copying it keeps dashboard sessions; otherwise (or to be safe) use a new `openssl rand -hex 32` and sign in again. **Never** use the fallback |
| `NEWSLETTER_TOKEN_SECRET` | Node environment, **unchanged** (every confirm/unsubscribe link already sent depends on it) |
| `RESEND_API_KEY`, `NEWSLETTER_FROM`, `NEWSLETTER_REPLY_TO`, `NEWSLETTER_SITE_URL` | Node environment (`apps/server/.env`), identical values |
| `NEWSLETTER_CRON_SECRET` | Node environment; must equal Vercel's `CRON_SECRET` (the website forwards the cron's `Authorization` header). `CRON_SECRET` is only a fallback when this is empty |
| `MEDIA_STORAGE` | `local` at cutover |
| `COLLECTOR_ENABLED`, `PUBLISHER_ENABLED`, `INDEXNOW_ENABLED` | `0` at cutover (step 6) |
| `GEMINI_API_KEY`, `GEMINI_TEXT_MODEL`, `GEMINI_IMAGE_MODEL` | the `news:daily` environment (Node defaults: `gemini-3.5-flash-lite`, `gemini-3.1-flash-image-preview`); `GEMINI_VISION_MODEL` optional |
| `AWS_REGION`, `AWS_PROFILE`, `S3_FEATURE_IMAGE_BUCKET`, `S3_FEATURE_IMAGE_PREFIX`, `S3_FEATURE_IMAGE_PUBLIC_URL` | the importer IAM user's settings (the IAM user can write `features/` only). Put its credentials in `~/.aws/credentials` of the account the job runs as, never in this repository |

The first `bin/adopt` run (step 4) validates the file: configuration errors
are printed before anything is opened.

## 3. launchd jobs (installed, not started)

**API.** Fill in `docs/news.aiandtech.api.plist` (`__USER__`, `__ROOT__`) and
copy it to `/Library/LaunchDaemons/` as described in its header comment, **but
do not bootstrap it yet**. It loads `technews.env`, runs `bin/api` with
`KeepAlive`, restarts it at most every 10s, gives it 60s to shut down, and
logs to `$ROOT/logs/api.log` (JSON) and `api.err.log`. To keep logs bounded,
add a newsyslog rule, for example `/etc/newsyslog.d/technews.conf`:
`$ROOT/logs/api*.log  <you>:staff  640  7  10240  *  NJ`.

**Tunnel.** Find the exact command the MacBook uses for the technews tunnel
(`ps -axo pid,command | grep '[s]ubtunnel local'`, or its launchd plist /
`pm2` entry). The Mac mini runs the **same** command with the same subdomain
(`technews`) and token, changing only the local target to `127.0.0.1:4001`.
Run it under launchd as well (a second LaunchDaemon with `KeepAlive`, its
`ProgramArguments` being that command; keep the token out of the repository),
so a dropped connection is retried instead of leaving the site down as in
August. Do **not** start it before step 5.9: only one client can hold
`technews`.

## 4. Rehearsal (no downtime; repeat until clean)

On the MacBook, take a copy while Node runs (`.backup` is consistent with a
live WAL database; it only reads the source):

```sh
sqlite3 "$NODE/apps/server/data/technews.db" ".backup '/tmp/technews-rehearsal.db'"
```

Copy it and the uploads directory to the Mac mini into a throwaway
production root **outside** `$ROOT`, and run the real binaries against it with
the production environment (only the two paths differ):

```sh
R="$HOME/rehearsal"
mkdir -p "$R/data" "$R/uploads" && touch "$R/.technews-production"
# copy technews-rehearsal.db to $R/data/technews.db and the uploads to $R/uploads/
run() { (set -a; . "$ROOT/technews.env"; DATABASE_PATH="$R/data/technews.db"; UPLOADS_DIR="$R/uploads"; set +a; "$@"); }
run "$ROOT/bin/adopt"            # read-only report, then a full rehearsal on a scratch copy
run "$ROOT/bin/adopt" --apply    # optional: exercise the backup and the real adoption on this copy
run "$ROOT/bin/adopt"            # prints "already managed"
```

Then run the smoke test, which works on its own temporary copy, on a random
`127.0.0.1` port, with an empty environment (no emails, S3, or IndexNow):

```sh
cd "$REPO/apps/server-go"
TZ=Europe/Istanbul SMOKE_EMAIL=<dashboard email> SMOKE_PASSWORD=<password> \
  scripts/smoke-local.sh "$R/data/technews.db" "$R/uploads"
```

Without a checkout, `SMOKE_API_BIN=$ROOT/bin/api SMOKE_ADOPT_BIN=$ROOT/bin/adopt`
skips the build. Expected: `adopt` prints migrations 1, 2, 5, 6 **present**,
3 and 4 **pending** (the newsroom `candidates` table), any known Node variant
(`subscribers.created_at`/`updated_at` from Node's `ALTER TABLE`), tolerated
extras, `Rehearsal on a copy: passed`, `settings: N -> N+2 rows`, and
`Result: COMPATIBLE`; the smoke script ends with `smoke: PASS`.

Also, per the media plan, list uploads that Go serves as downloads rather than
images: `find "$R/uploads" -type f ! -iname '*.jpg' ! -iname '*.jpeg'
! -iname '*.png' ! -iname '*.webp' ! -iname '*.gif'`.

Anything but `COMPATIBLE` stops the cutover: the report names each mismatch
(missing column, type, NOT NULL, default, CHECK, foreign key, UNIQUE, index,
trigger). Fix the cause (usually by starting the current Node server once so
its `initializeDatabase` upgrades the schema) and rehearse again.

## 5. Freeze and cutover (downtime: a few minutes)

Pick a time away from 05:00 and 06:00 UTC (the Vercel crons). Tell editors
not to use the dashboard until the end.

**On the MacBook**

1. Stop the `news:daily` scheduler wherever it lives (`crontab -l`,
   `launchctl list | grep -i news`, a `pm2` cron, Calendar/Shortcuts), then
   make sure no import is running: `pgrep -fl 'scrape-news|news-importer'`
   (the importer's lock file is `$TMPDIR/technews-news-import.lock`).
2. Stop the Node API the way it is supervised (`launchctl bootout …`,
   `pm2 stop …`), so it is not restarted. Check nothing listens:
   `lsof -iTCP:4001 -sTCP:LISTEN` prints nothing. From here the site answers
   `502` through the tunnel; website pages keep their cached (ISR) versions.
3. Final copy, integrity check, and record of what was copied:

   ```sh
   cd "$NODE/apps/server"
   STAMP=$(date -u +%Y%m%dT%H%M%SZ)
   sqlite3 data/technews.db ".backup 'data/technews-final-$STAMP.db'"
   sqlite3 "data/technews-final-$STAMP.db" 'PRAGMA integrity_check'   # ok
   for t in articles authors categories media settings subscribers newsletter_deliveries newsletter_editions; do
     printf '%s ' "$t"; sqlite3 "data/technews-final-$STAMP.db" "SELECT count(*) FROM $t"; done
   shasum -a 256 "data/technews-final-$STAMP.db"
   find uploads -type f | wc -l
   ```

   Leave `data/technews.db` and `uploads/` exactly as they are: they are the
   rollback copy (Go's development runs refuse them by path).
4. Copy to the Mac mini (network or disk), for example
   `rsync -a "data/technews-final-$STAMP.db" macmini:"$ROOT/data/technews.db"`
   and `rsync -a uploads/ macmini:"$ROOT/uploads/"`.

**On the Mac mini**

5. Verify the copy: `shasum -a 256 "$ROOT/data/technews.db"` equals the
   MacBook's; `find "$ROOT/uploads" -type f | wc -l` matches.
6. Dry run, then adopt (with the production environment):

   ```sh
   cd "$ROOT"
   (set -a; . ./technews.env; set +a; ./bin/adopt)          # read-only report + rehearsal
   (set -a; . ./technews.env; set +a; ./bin/adopt --apply)  # backup, then adopt
   ```

   `--apply` first writes `$ROOT/data/technews.db.pre-adopt-<UTC>.db` with
   `VACUUM INTO` (it refuses to overwrite an existing file) and checks it,
   then records migrations 1, 2, 5, 6 and runs 3 and 4 in **one** transaction,
   then confirms `migrate.Run` has nothing left and the schema matches. It
   never drops, rewrites, or deletes data; the only data it adds are the two
   `newsroom.publish_delay_*` settings rows. Running it again prints
   `already managed` and changes nothing. Keep the `.pre-adopt-` file.
7. Start Go: `sudo launchctl bootstrap system /Library/LaunchDaemons/news.aiandtech.api.plist`.
   Check `tail "$ROOT/logs/api.log" "$ROOT/logs/api.err.log"` (a
   `starting HTTP server` line with `"mode":"production"`) and
   `curl -fsS http://127.0.0.1:4001/api/health`.
8. Local checks against the live process:

   ```sh
   curl -fsS 'http://127.0.0.1:4001/api/articles?limit=1'
   curl -fsS 'http://127.0.0.1:4001/api/newsletter/editions?limit=1'
   curl -fsS -o /dev/null -w '%{http_code}\n' "http://127.0.0.1:4001$(sqlite3 "$ROOT/data/technews.db" "SELECT url FROM media ORDER BY id DESC LIMIT 1")"
   ```

   and, from a checkout, `scripts/smoke-local.sh "$ROOT/data/technews.db"
   "$ROOT/uploads"` (it tests a temporary copy on another port).
9. **Switch the tunnel.** Stop the MacBook's `technews` tunnel client first
   (so the subdomain is free), then bootstrap the Mac mini's tunnel job. Check
   from anywhere: `curl -fsS https://technews.subtunnel.dev/api/health`.
   An nginx `404` with body `Not Found` means no client holds the subdomain;
   `502` means the client is connected but cannot reach `127.0.0.1:4001`.

**Verify the public site** (allow a minute for ISR revalidation)

10. `https://www.aiandtech.news/` lists current articles; open two article
    pages (one with an `/uploads/...` image, one with an S3 image); open
    `/sitemap.xml` (article URLs present); open the newsletter archive and one
    edition.
11. Run the production smoke check: `npm run smoke:production` in `$REPO`, and
    trigger the GitHub workflow (`gh workflow run production-smoke.yml`, then
    `gh run watch`).
12. Dashboard: sign in (a new `JWT_SECRET` means everyone signs in again),
    open articles, categories, media, settings; save one harmless edit; upload
    a small image and check it loads from `/uploads/`.
13. Newsletter: signing up with a test address on the website works. After
    the next 05:00 UTC cron, check the digest: `sqlite3 "$ROOT/data/technews.db"
    "SELECT status, count(*) FROM newsletter_deliveries WHERE edition_key = date('now') GROUP BY status"`
    (no `failed` rows) and `grep 'newsletter digest' "$ROOT/logs/api.log"`.

## 6. Enable the newsroom (last, one flag at a time)

Only after the site has run cleanly on Go (for example a day, including one
digest). Edit `technews.env`, then `sudo launchctl kickstart -k system/news.aiandtech.api`.

1. `COLLECTOR_ENABLED=1`: candidates arrive (Omni Control app, or
   `GET /api/newsroom/overview`). Nothing is published.
2. `INDEXNOW_ENABLED=1` together with, or before, the publisher (dashboard
   publishes then ping IndexNow as Node did).
3. `PUBLISHER_ENABLED=1` with the Gemini and S3 settings: at startup it checks
   AWS credentials and the bucket and fails fast if they are wrong. Queue one
   candidate and watch it publish (article page, sitemap, IndexNow log line).

The Node scheduler must stay off for good: running both would publish twice.

## 7. After cutover

- Keep the MacBook's Node checkout, `data/technews.db`, `uploads/`, and the
  `technews-final-*.db` copy untouched for the rollback window (at least two
  weeks). Do not start Node or its scheduler there.
- Back up the Mac mini database regularly with `sqlite3 "$ROOT/data/technews.db"
  ".backup '<backup dir>/technews-$(date -u +%Y%m%dT%H%M%SZ).db'"` (safe while
  Go runs) and the uploads directory with `rsync`.
- Future releases: replace the binaries, `sudo launchctl bootout
  system/news.aiandtech.api`, back up, run `bin/migrate` with the env file
  loaded (it applies only new migrations), bootstrap the job again.
- Delete `$HOME/rehearsal` when you no longer need it.
