# Cutover: Node on the MacBook → Go on the Mac mini

This runbook moves production from the Node API on the MacBook to the Go API
alone on a Mac mini (Apple silicon), with the dashboard next to it. Node is
retired afterwards: kept stopped and intact for [rollback](rollback.md). Every
command runs on the machine its section names; each section starts by setting
the shell variables it uses, so paste that block first in every new terminal.

**Today:** Node API (`apps/server`, port 4001), SQLite
`apps/server/data/technews.db`, `apps/server/uploads/`, and an external
`news:daily` scheduler, all on the MacBook, published by a tunnel client as
`https://technews.subtunnel.dev`. The website (Vercel) reads
`API_URL=https://technews.subtunnel.dev`; its crons call
`/api/newsletter/digest` (05:00 UTC) and `/api/indexnow` (06:00 UTC) through
it.

**After:** Go API (`bin/api`) on the Mac mini at `127.0.0.1:4001`, the same
database (adopted into Go's migration ledger), the same uploads, the same
tunnel hostname; the dashboard (`apps/dashboard`) on the Mac mini at
`127.0.0.1:3001`, proxying `/api` to the Go API. The website and its crons
need **no change**. The Go collector and publisher replace the Node scheduler
and are enabled last.

Safety nets built into the Go binaries:

- With `APP_ENV=production`, `DATABASE_PATH` and `UPLOADS_DIR` must be
  absolute and inside a directory holding `.technews-production`; development
  runs refuse any path inside such a directory. `JWT_SECRET` must be at least
  32 bytes and not Node's public fallback; `TZ` must be set.
- The production API never creates a database, and refuses to serve one that
  is not adopted or not fully migrated. Starting it too early fails; it does
  not serve an empty database.
- `bin/adopt` is read-only unless given `--apply`, and never drops, rewrites,
  or deletes data.

---

## 1. MacBook: discovery (no downtime)

```sh
NODE=/Users/ozan/Projects/technews   # the Node checkout that serves production
```

1. **How is everything started?** Record, for the Node API, the `news:daily`
   scheduler, and every tunnel client, what starts it and how to stop it:

   ```sh
   lsof -nP -iTCP:4001 -sTCP:LISTEN                      # the Node API process
   ps -axo pid,ppid,command | grep -E '[n]ode|[t]sx|[p]m2|[s]ubtunnel|[c]loudflared'
   launchctl list | grep -iE 'technews|news|node|subtunnel|cloudflared'
   ls ~/Library/LaunchAgents /Library/LaunchAgents /Library/LaunchDaemons | grep -iE 'technews|news|subtunnel|cloudflared'
   crontab -l
   command -v pm2 >/dev/null && pm2 list
   ```

2. **The `technews` tunnel.** Record the exact client command and its
   configuration for `technews.subtunnel.dev` (for a `subtunnel local` client
   the `ps` line above; for cloudflared the ingress rule, see below). The Mac
   mini will run the same thing with the local target `127.0.0.1:4001`.

3. **The `technewsweb` tunnel: find out what it serves; do not assume.**
   It disconnected together with `technews` in August
   (`.planning/debug/production-backend-outage.md`). Inspect it without
   printing credentials:

   ```sh
   ls -la ~/.cloudflared /etc/cloudflared 2>/dev/null
   sed -n '1,200p' ~/.cloudflared/config.yml 2>/dev/null   # ingress: hostname -> service; the credentials JSON it names is secret, do not print it
   cloudflared tunnel list 2>/dev/null
   cloudflared tunnel info technewsweb 2>/dev/null
   ps -axo pid,command | grep -E '[s]ubtunnel.*technewsweb|[c]loudflared'
   ```

   Write down, for `technewsweb`: its public hostname(s), the local service
   each routes to (`http://localhost:<port>` or a path), what listens there
   (`lsof -nP -iTCP:<port> -sTCP:LISTEN`), and how the client is started.

   **Decision point.** If it routes to something that moves to the Mac mini
   (for example the dashboard on port 3001, or the Node API), move it in step
   5.10, pointing at the Mac mini's equivalent (`127.0.0.1:3001` or
   `127.0.0.1:4001`). If it routes to something that stays on the MacBook, or
   to nothing that is running, leave it where it is. If you cannot tell, stop
   and decide before the cutover; nothing below depends on it otherwise.

4. **Time zone.** `date +%Z; sudo systemsetup -gettimezone` (expected
   `Europe/Istanbul`, the value pre-filled in the env template).

5. **Secrets, without printing them.** Find where the Node process gets its
   environment (the plist `EnvironmentVariables`, a `pm2` ecosystem file, a
   shell script, or `$NODE/apps/server/.env`). Collect the values into a
   private file and copy the file, never the text:

   ```sh
   umask 077
   SECRETS="$HOME/technews-secrets.env"
   : > "$SECRETS"
   NAMES='JWT_SECRET|NEWSLETTER_TOKEN_SECRET|NEWSLETTER_CRON_SECRET|CRON_SECRET|RESEND_API_KEY|NEWSLETTER_FROM|NEWSLETTER_REPLY_TO|NEWSLETTER_SITE_URL|GEMINI_API_KEY|GEMINI_TEXT_MODEL|GEMINI_IMAGE_MODEL'
   # from an env file Node (or the scheduler) loads:
   grep -E "^($NAMES)=" "$NODE/apps/server/.env" >> "$SECRETS"
   # values that only live in the running process (only for values without spaces):
   ps eww -o command= -p "$(lsof -t -iTCP:4001 -sTCP:LISTEN)" | tr ' ' '\n' | grep -E "^($NAMES)=" >> "$SECRETS"
   cut -d= -f1 "$SECRETS" | sort | uniq -c          # names only; each should appear once
   awk -F= '$1=="JWT_SECRET"{v=substr($0,12); gsub(/^"|"$/,"",v); print "JWT_SECRET length:", length(v)}' "$SECRETS"
   ```

   Production Node has a real `JWT_SECRET`; reusing it keeps editors' current
   dashboard sessions valid. If its length is below 32, or it is
   `technews-dev-secret-change-in-production`, Go refuses it: replace the line
   on the Mac mini with a new `openssl rand -hex 32` value, and editors sign
   in again. `CRON_SECRET` comes from the Vercel project if it is not in the
   file. Copy the file with `scp "$SECRETS" <macmini>:` and then
   `rm "$SECRETS"`.

## 2. Mac mini: prerequisites (no downtime)

```sh
ROOT="$HOME/technews"            # production data root; not under ~/Documents, ~/Desktop, ~/Downloads
REPO="$HOME/src/aiandtechnews"   # repository checkout used for building
```

1. **Keep it on.** System Settings → Energy: prevent automatic sleeping and
   start up after a power failure (or `sudo pmset -a sleep 0 disksleep 0
   autorestart 1`). `uname -m` prints `arm64`.
2. **Toolchains, user-local.** Xcode command line tools (cgo for the WebP
   encoder, `github.com/chai2010/webp`): `xcode-select --install`. Homebrew
   (it lives in `/opt/homebrew`, owned by your user; <https://brew.sh>), then
   `brew install go node pnpm`. Check `go version` (1.25+), `node --version`
   (22), `pnpm --version` (10), and that `/usr/bin/sqlite3` exists. Instead of
   Homebrew, Go can be a go.dev tarball unpacked under `~/` (for example
   `~/sdk/go`, with `~/sdk/go/bin` on `PATH`).
3. **Repository.** `git clone <repo url> "$REPO"` (or `git -C "$REPO" pull`
   on the branch being deployed).
4. **Build and test the Go binaries**:

   ```sh
   cd "$REPO/apps/server-go"
   go test -p 2 ./...
   mkdir -p "$ROOT/bin"
   go build -p 2 -trimpath -o "$ROOT/bin/api" ./cmd/api
   go build -p 2 -trimpath -o "$ROOT/bin/adopt" ./cmd/adopt
   go build -p 2 -trimpath -o "$ROOT/bin/migrate" ./cmd/migrate
   ```

5. **Build the dashboard**:

   ```sh
   cd "$REPO"
   pnpm install --frozen-lockfile
   pnpm --filter @technews/dashboard build
   ```

## 3. Mac mini: data root and environment (no downtime)

```sh
ROOT="$HOME/technews"
REPO="$HOME/src/aiandtechnews"
```

1. Directories and the production marker:

   ```sh
   mkdir -p "$ROOT/bin" "$ROOT/data" "$ROOT/uploads" "$ROOT/logs"
   touch "$ROOT/.technews-production"
   chmod 700 "$ROOT"
   ```

   The marker makes `$ROOT` a production data root: production requires
   `DATABASE_PATH` and `UPLOADS_DIR` inside it, and every development run
   (no `APP_ENV`, or `development`) refuses any path inside it, on any
   machine, including through symlinks. **A hard link to the database file
   placed outside `$ROOT` is not detected** (a hard link has no parent
   directory to check): do not create one. For an extra guard in development
   shells on this machine, export `PRODUCTION_DATABASE_PATH` (refused by name,
   symlink, hard link, and case variant) and `PRODUCTION_UPLOADS_DIR`.
2. Environment file, then the secrets copied in step 1.5 (later lines win when
   the file is sourced, so appending replaces the empty template values):

   ```sh
   umask 077
   cp "$REPO/apps/server-go/docs/technews.env.example" "$ROOT/technews.env"
   cat "$HOME/technews-secrets.env" >> "$ROOT/technews.env" && rm "$HOME/technews-secrets.env"
   chmod 600 "$ROOT/technews.env"
   ```

   Then edit it (`open -e "$ROOT/technews.env"`) for the non-secret values:

   | Variable | Value |
   | --- | --- |
   | `APP_ENV`, `SERVER_ADDR`, `TZ` | pre-filled: `production`; `127.0.0.1:4001` (the dashboard proxies to it; loopback only, the tunnel client connects locally); `Europe/Istanbul` (change it if step 1.4 said otherwise) |
   | `DATABASE_PATH` | `$ROOT/data/technews.db`, written out as an absolute path |
   | `UPLOADS_DIR` | `$ROOT/uploads`, written out as an absolute path |
   | `JWT_SECRET`, `NEWSLETTER_*`, `RESEND_API_KEY`, `CRON_SECRET` | from the secrets file. `NEWSLETTER_TOKEN_SECRET` must be unchanged (links in sent emails depend on it); `NEWSLETTER_CRON_SECRET` must equal Vercel's `CRON_SECRET` |
   | `MEDIA_STORAGE` | `local` |
   | `COLLECTOR_ENABLED`, `PUBLISHER_ENABLED`, `INDEXNOW_ENABLED` | `0` until step 6 |
   | `GEMINI_*`, `AWS_REGION`, `AWS_PROFILE`, `S3_FEATURE_IMAGE_*` | only needed in step 6 (the importer IAM user; its credentials go in `~/.aws/credentials`, never in the repository) |

   Check names without printing values:
   `grep -v '^#' "$ROOT/technews.env" | cut -d= -f1 | sort | uniq -c`.
3. **launchd files, prepared but not installed.** Fill in
   `docs/news.aiandtech.api.plist` (`__USER__`, `__HOME__`, `__ROOT__`) and
   `docs/news.aiandtech.dashboard.plist` (`__USER__`, `__HOME__`, `__REPO__`,
   `__LOGS__` = `$ROOT/logs`, `__NODE_BIN__` = `dirname "$(command -v node)"`),
   save them in `$ROOT`, and `plutil -lint` them. **Do not copy them into
   `/Library/LaunchDaemons` yet:** a LaunchDaemon starts when it is
   bootstrapped and at every boot. They are installed in steps 5.7 and 5.8.
   To bound the logs, add `/etc/newsyslog.d/technews.conf` with
   `<absolute $ROOT>/logs/*.log  <you>:staff  640  7  10240  *  NJ`.
4. **Tunnel client**, prepared but not started: the same command as the
   MacBook's `technews` client (step 1.2), with the local target
   `127.0.0.1:4001`, as its own LaunchDaemon with `KeepAlive` (so a dropped
   connection is retried, unlike in August). Keep its token out of the
   repository. Only one client can hold `technews`: it starts in step 5.10.
5. **How the dashboard is reached.** It listens on `127.0.0.1:3001` only.
   Editors open it through an SSH tunnel (`ssh -L 3001:127.0.0.1:3001
   <macmini>`, then `http://localhost:3001`), or, if step 1.3 showed that
   `technewsweb` publishes the dashboard today, through that tunnel moved to
   point at `127.0.0.1:3001`. Do not bind it to `0.0.0.0`. (Pre-existing:
   the dashboard only proxies `/api`, so `/uploads/...` previews in its media
   page do not resolve there; articles on the website are unaffected.)

## 4. Rehearsal on a copy (no downtime; repeat until clean)

On the MacBook (`.backup` is consistent with a live WAL database and only
reads the source):

```sh
NODE=/Users/ozan/Projects/technews
umask 077
REHEARSAL=$(mktemp -d)
sqlite3 "$NODE/apps/server/data/technews.db" ".backup '$REHEARSAL/technews.db'"
echo "$REHEARSAL"
```

Copy that `technews.db` and `$NODE/apps/server/uploads/` to the Mac mini, then
`rm -rf "$REHEARSAL"` on the MacBook. On the Mac mini:

```sh
ROOT="$HOME/technews"
REPO="$HOME/src/aiandtechnews"
umask 077
R=$(mktemp -d)                                  # a throwaway production root
mkdir -p "$R/data" "$R/uploads" && touch "$R/.technews-production"
# move the copied technews.db to "$R/data/technews.db" and the uploads into "$R/uploads/"
run() { (set -a; . "$ROOT/technews.env"; DATABASE_PATH="$R/data/technews.db"; UPLOADS_DIR="$R/uploads"; set +a; "$@"); }
run "$ROOT/bin/adopt"            # read-only report, then a full rehearsal on a scratch copy
```

Expected: migrations 1, 2, 5, 6 **present**; 3 and 4 **pending** (the newsroom
`candidates` table; 3 is "partially present" because Node already has
`settings`); possibly the known Node variant `subscribers.created_at` /
`updated_at` (Node's `ALTER TABLE`); tolerated extras, if any; `Rehearsal on a
copy: passed`; `settings: N -> N+2 rows`; `Result: COMPATIBLE`. Anything else
stops the cutover: the report names each mismatch. Fix the cause (usually by
starting the current Node server once, so its `initializeDatabase` upgrades
the schema) and rehearse again.

Then exercise the real adoption and the smoke test on this copy:

```sh
run "$ROOT/bin/adopt" --apply    # backup + adoption of the rehearsal copy
run "$ROOT/bin/adopt"            # prints "already managed"
cd "$REPO/apps/server-go"
read -r SMOKE_EMAIL; export SMOKE_EMAIL
read -rs SMOKE_PASSWORD; export SMOKE_PASSWORD   # not echoed, not in shell history
TZ=Europe/Istanbul scripts/smoke-local.sh "$R/data/technews.db" "$R/uploads"
unset SMOKE_PASSWORD
```

The smoke script works on its own temporary copy, on a random `127.0.0.1`
port, with an empty environment (no emails, S3, or IndexNow) and ends with
`smoke: PASS`. Also list uploads that Go serves as downloads rather than
images: `find "$R/uploads" -type f ! -iname '*.jpg' ! -iname '*.jpeg' ! -iname
'*.png' ! -iname '*.webp' ! -iname '*.gif'`. Finally `rm -rf "$R"`.

## 5. Freeze and cutover (downtime: a few minutes)

Pick a time away from 05:00 and 06:00 UTC. Tell editors not to use the
dashboard until the end.

**On the MacBook**

```sh
NODE=/Users/ozan/Projects/technews
STAMP=$(date -u +%Y%m%dT%H%M%SZ); echo "$STAMP"   # note it: step 5.5 and the rollback use it
```

1. Stop the `news:daily` scheduler the way step 1.1 found it, and check that
   no import runs: `pgrep -fl 'scrape-news|news-importer'` prints nothing.
2. Stop the Node API so it is not restarted (`launchctl bootout …`,
   `pm2 stop …`); `lsof -nP -iTCP:4001 -sTCP:LISTEN` prints nothing. The site
   now answers `502` through the tunnel; website pages keep their ISR copies.
3. Final copy and its fingerprint:

   ```sh
   cd "$NODE/apps/server"
   sqlite3 data/technews.db ".backup 'data/technews-final-$STAMP.db'"
   sqlite3 "data/technews-final-$STAMP.db" 'PRAGMA integrity_check'          # ok
   for t in articles authors categories media settings subscribers newsletter_deliveries newsletter_editions; do
     printf '%s ' "$t"; sqlite3 "data/technews-final-$STAMP.db" "SELECT count(*) FROM $t"; done
   shasum -a 256 "data/technews-final-$STAMP.db"
   find uploads -type f | wc -l
   ```

   Leave `data/technews.db`, `uploads/`, and the final copy untouched: they are
   the rollback copy.
4. Copy the final database to a temporary name on the Mac mini, and the
   uploads (`technews/...` is relative to the Mac mini user's home, that is
   `$ROOT`):

   ```sh
   rsync -a "data/technews-final-$STAMP.db" "<macmini>:technews/data/incoming-$STAMP.db"
   rsync -a uploads/ "<macmini>:technews/uploads/"
   ```

**On the Mac mini**

```sh
ROOT="$HOME/technews"
STAMP=<the value noted on the MacBook>
```

5. Verify the copy and move it into place without overwriting anything:

   ```sh
   ls "$ROOT"/data/technews.db* 2>/dev/null && echo "STOP: a database is already in place"
   shasum -a 256 "$ROOT/data/incoming-$STAMP.db"                  # equals the MacBook's
   find "$ROOT/uploads" -type f | wc -l                           # equals the MacBook's
   mv -n "$ROOT/data/incoming-$STAMP.db" "$ROOT/data/technews.db"
   ls "$ROOT/data/incoming-$STAMP.db" 2>/dev/null && echo "STOP: not moved"
   ```

6. Dry run, then adopt:

   ```sh
   cd "$ROOT"
   (set -a; . ./technews.env; set +a; ./bin/adopt)          # read-only report + rehearsal
   (set -a; . ./technews.env; set +a; ./bin/adopt --apply)  # backup, then adopt
   ```

   `--apply` writes `data/technews.db.pre-adopt-<UTC>.db` with `VACUUM INTO`
   (refusing an existing file) and checks it; then, in one write transaction,
   it recounts every table (it must equal the backup; another writer makes it
   stop), verifies the schema again, records migrations 1, 2, 5, 6, runs 3 and
   4, verifies the full Go schema, and checks the row counts (no table lost or
   gained rows, except the two `newsroom.*` settings rows) before committing.
   Any failure rolls back. Running it again prints `already managed`. Keep the
   `.pre-adopt-` file.
7. Install and start the API:

   ```sh
   sudo cp "$ROOT/news.aiandtech.api.plist" /Library/LaunchDaemons/
   sudo chown root:wheel /Library/LaunchDaemons/news.aiandtech.api.plist
   sudo chmod 644 /Library/LaunchDaemons/news.aiandtech.api.plist
   sudo launchctl bootstrap system /Library/LaunchDaemons/news.aiandtech.api.plist
   tail -n 20 "$ROOT/logs/api.log" "$ROOT/logs/api.err.log"   # "starting HTTP server", "mode":"production"
   curl -fsS http://127.0.0.1:4001/api/health                  # {"status":"ok"}; 503 {"status":"error"} if the database fails
   ```

   If `api.err.log` says `refusing to serve`, the database is not adopted or
   not migrated: go back to step 6 (or run `bin/migrate` for pending
   migrations).
8. Install and start the dashboard the same way with
   `news.aiandtech.dashboard.plist`; `curl -fsS -o /dev/null -w '%{http_code}\n'
   http://127.0.0.1:3001/login` prints `200`.
9. Local checks against the live process:

   ```sh
   curl -fsS 'http://127.0.0.1:4001/api/articles?limit=1' | head -c 300; echo
   curl -fsS 'http://127.0.0.1:4001/api/newsletter/editions?limit=1' | head -c 300; echo
   curl -fsS -o /dev/null -w '%{http_code}\n' "http://127.0.0.1:4001$(sqlite3 "$ROOT/data/technews.db" "SELECT url FROM media WHERE url LIKE '/uploads/%' ORDER BY id DESC LIMIT 1")"
   ```

   and, from `$REPO/apps/server-go`, the smoke script against a copy of the
   live database (credentials read as in step 4):
   `scripts/smoke-local.sh "$ROOT/data/technews.db" "$ROOT/uploads"`.
10. **Switch the tunnel.** Stop the MacBook's `technews` client first (the
    subdomain must be free), then bootstrap the Mac mini's tunnel job. Move
    `technewsweb` now only if step 1.3 decided so. From anywhere:
    `curl -fsS https://technews.subtunnel.dev/api/health`. An nginx `404`
    `Not Found` means no client holds the subdomain; `502` means the client
    cannot reach `127.0.0.1:4001`.

**Verify the public site** (allow a minute for ISR revalidation)

11. `https://www.aiandtech.news/` lists current articles; open two article
    pages (one with an `/uploads/...` image, one with an S3 image); open
    `/sitemap.xml` (article URLs present); open the newsletter archive and one
    edition.
12. `npm run smoke:production` in `$REPO`, and the GitHub workflow:
    `gh workflow run production-smoke.yml`, then `gh run watch`.
13. Dashboard: open it as in step 3.5; an existing session stays valid because
    `JWT_SECRET` was reused (editors sign in again only if it was rotated).
    Open articles, categories, media, settings; save a harmless edit; upload a
    small image.
14. Newsletter: sign up with a test address on the website. After the next
    05:00 UTC cron, `sqlite3 "$ROOT/data/technews.db" "SELECT status,
    count(*) FROM newsletter_deliveries GROUP BY edition_key, status ORDER BY
    edition_key DESC LIMIT 5"` shows no `failed` rows for the new edition, and
    `grep 'newsletter digest' "$ROOT/logs/api.log"`.

## 6. Enable the newsroom (last, one flag at a time)

Only after the site has run cleanly on Go (for example a day, including one
digest). Edit `technews.env`, then `sudo launchctl kickstart -k
system/news.aiandtech.api`.

1. `COLLECTOR_ENABLED=1`: candidates arrive (Omni Control app, or
   `GET /api/newsroom/overview`). Nothing is published.
2. `INDEXNOW_ENABLED=1`, with or before the publisher.
3. `PUBLISHER_ENABLED=1` with the Gemini and S3 settings: at startup it checks
   AWS credentials and the bucket and stops if they are wrong. Queue one
   candidate and watch it publish (article page, sitemap, IndexNow log line).

The Node scheduler stays off for good: running both would publish twice.

## 7. After cutover

- Keep the MacBook's `$NODE` checkout, `data/technews.db`, `uploads/`, and
  `data/technews-final-$STAMP.db` untouched for the rollback window (at least
  two weeks). Do not start Node or its scheduler there.
- Back up the Mac mini database regularly, safely while Go runs:
  `sqlite3 "$ROOT/data/technews.db" ".backup '<backup dir>/technews-$(date -u +%Y%m%dT%H%M%SZ).db'"`,
  and the uploads with `rsync`.
- Releases: `git -C "$REPO" pull`, build into a temporary directory, `sudo
  launchctl bootout system/news.aiandtech.api`, back up, replace the
  binaries, run `bin/migrate` with the env file loaded (it applies only new
  migrations), `sudo launchctl bootstrap system
  /Library/LaunchDaemons/news.aiandtech.api.plist`. The API refuses to start
  if a migration was forgotten. Dashboard: `pnpm install --frozen-lockfile`,
  `pnpm --filter @technews/dashboard build`, then `sudo launchctl kickstart -k
  system/news.aiandtech.dashboard`.
