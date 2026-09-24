# Production on the Mac mini: cutover record and operations

The Go API replaced the Node API on **2026-09-24**. This file records what is
deployed, how the cutover ran, and how to operate it. Going back is in
[rollback](rollback.md).

## What runs where

| Piece | Where |
| --- | --- |
| Go API | Mac mini `kasikcis-mac-mini.local` (user `ozankasikci`, Apple silicon), `127.0.0.1:4001` |
| Public API | `https://technews.subtunnel.dev`, published by the `subtunnel` 0.3.1 client on the mini |
| Website | `https://www.aiandtech.news` on Vercel. It reads the API with 60 s Next.js fetch revalidation, so changes show within about 1-2 minutes. Not hosted on the mini (nor is the `technewsweb` tunnel) |
| Dashboard | Mac mini, `127.0.0.1:3001`, proxying `/api` to the Go API; reached only through an SSH tunnel |
| Old Node API | MacBook, stopped, kept intact for rollback |

The mini has auto-login on, FileVault off, and restart after power failure on
(`pmset -g` shows `autorestart 1`, `sleep 0`).

### Two locations on the mini

**Build workspace, external SSD: `/Volumes/Samsung990PRO/AIAndTechNews`**
(`$WS` below). Build only; nothing in production reads it.

- `env.sh`: `PATH` for the tools below; `GOPATH`, `GOCACHE`, `GOMODCACHE`,
  `COREPACK_HOME`, and the pnpm store under `cache/`; `GOTOOLCHAIN=local`.
- `tools/go`: Go 1.25.4 (go.dev tarball, SHA-256 checked).
- `tools/node`: Node 22.16.0 (nodejs.org tarball); pnpm 10.29.3 via
  `corepack enable --install-directory tools/node/bin pnpm`.
- `tools/subtunnel`.
- `repo/`: plain git repository with `receive.denyCurrentBranch
  updateInstead`. Deploy by pushing to it from a dev machine.
- `bin/`: build output.
- SSH access to the drive needs Full Disk Access for
  `/usr/libexec/sshd-keygen-wrapper` (Privacy & Security) and "Allow full
  disk access for remote users" under Remote Login.

**Runtime, internal disk: `~/aiandtechnews`** (`$RT` below). launchd jobs
cannot read external volumes (macOS TCC: "Operation not permitted", launchd
exit 78), so everything a job touches lives here:

```
.technews-production        production marker
bin/{api,adopt,migrate,subtunnel}
technews.env                600, secrets; see technews.env.example
subtunnel.toml              600, tunnel token
data/technews.db            600
data/uploads/
logs/                       api.log, api.err.log, tunnel*.log, dashboard*.log
node/                       Node runtime for the dashboard
dashboard/                  Next standalone build of apps/dashboard
set-password.sh <email>     reset an author's password (prompts without echo,
                            hashes with `htpasswd -niBC 12`; Go bcrypt accepts $2y$)
test-article.sh publish|delete   publish or remove the marked test article
```

`subtunnel.toml`:

```toml
server = "tunnel.subtunnel.dev:7835"
token = "..."            # secret, never print it

[tunnels.technews]
local_port = 4001
subdomain = "technews"
```

Only one client can hold a subdomain: starting the MacBook's client again is
the rollback.

### launchd

Per-user **LaunchAgents** (no sudo on the mini), templates in this directory:

| Label | Runs |
| --- | --- |
| `news.aiandtech.api` | `/bin/sh -c 'set -a; . ~/aiandtechnews/technews.env; set +a; exec ~/aiandtechnews/bin/api'` |
| `news.aiandtech.tunnel` | `bin/subtunnel run --config subtunnel.toml technews` |
| `news.aiandtech.dashboard` | `node/bin/node dashboard/apps/dashboard/server.js` (`HOSTNAME=127.0.0.1`, `PORT=3001`) |

Fill `__RUNTIME__` with the absolute `$RT` (`/Users/ozankasikci/aiandtechnews`),
`plutil -lint`, copy to `~/Library/LaunchAgents/`, then:

```sh
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/news.aiandtech.api.plist   # load + start
launchctl kickstart -k gui/$(id -u)/news.aiandtech.api                            # restart
launchctl bootout gui/$(id -u)/news.aiandtech.api                                 # stop, stays stopped
launchctl print gui/$(id -u)/news.aiandtech.api | grep -E 'state|last exit'       # status
```

`KeepAlive` uses `PathState` on a runtime file (`technews.env`,
`subtunnel.toml`, `server.js`): the job restarts on exit while that file exists.
Agents start at login, which auto-login provides after a reboot.

## Safety rules (built into the Go binaries; do not work around them)

- With `APP_ENV=production`, `DATABASE_PATH` and `UPLOADS_DIR` must be absolute
  and inside a directory holding `.technews-production`; development runs
  refuse any path inside such a directory. A hard link to the database outside
  it is not detected: do not create one. `JWT_SECRET` must be at least 32 bytes
  and not Node's public fallback; `TZ` must be set.
- The production API never creates a database and refuses one that is not
  adopted or not fully migrated.
- `bin/adopt` is read-only unless given `--apply`. `--apply` first writes
  `data/technews.db.pre-adopt-<UTC>.db` (`VACUUM INTO`, refuses an existing
  file) and checks it; then, in one transaction, recounts every table against
  the backup, verifies the schema, records migrations 1, 2, 5, 6, runs 3 and 4
  (the newsroom `candidates` table), verifies the full Go schema, and checks
  that no table lost or gained rows except the two `newsroom.*` settings rows.
  Any failure rolls back. Run again, it prints `already managed`.
- **Never run `bin/migrate` on an unadopted database.** In production it
  refuses one without the migration ledger; if the API logs `refusing to
  serve` / `no migration ledger`, stop it and go through `adopt`, not migrate.
- Never print secrets. Check `technews.env` by names only (below). Never `cat`
  it, `subtunnel.toml`, or the process environment.
- Build and test with `-p 2`.

### Checking technews.env

It is sourced by `/bin/sh` under `set -a`, so it must be valid POSIX sh and
survive `set -u`. An unquoted value containing `$` broke loading once.
Single-quote every value (`NAME='value'`, a `'` inside written as `'\''`), and
after every edit run:

```sh
sh -n ~/aiandtechnews/technews.env && echo "syntax ok"
( set -eu; set -a; . ~/aiandtechnews/technews.env > /dev/null 2>&1; set +a
  for n in APP_ENV SERVER_ADDR DATABASE_PATH UPLOADS_DIR TZ JWT_SECRET NEWSLETTER_SITE_URL \
           NEWSLETTER_TOKEN_SECRET NEWSLETTER_CRON_SECRET RESEND_API_KEY NEWSLETTER_FROM \
           COLLECTOR_ENABLED PUBLISHER_ENABLED INDEXNOW_ENABLED; do
    eval "v=\${$n-}"; if [ -n "$v" ]; then echo "set    $n"; else echo "EMPTY  $n"; fi
  done ) || echo "STOP: technews.env does not load under set -eu"
```

It prints names only, never values.

Newsroom: the collector is on (`COLLECTOR_ENABLED=1`). Publisher and IndexNow
stay `0` until the Gemini, AWS, and S3 values are filled in; then enable
IndexNow with or before the publisher, one flag at a time, and restart the API.
The Node `news:daily` scheduler stays off for good (both would publish).

## How the cutover ran (2026-09-24)

1. **Export.** On the MacBook, the Node export zip: an SQLite online-backup
   copy of the database with a `MANIFEST` (SHA-256), and an uploads
   `tar.gz`. Node stayed stopped from then on; its database was never modified.
2. **Rehearsal** on a throwaway production root (its own
   `.technews-production`, a copy of the export): `adopt`, `adopt --apply`,
   `adopt` again (`already managed`), then `scripts/smoke-local.sh <db>
   <uploads>` (`smoke: PASS`, 17 checks), and a test API instance tunnelled
   to the Omni Control debug build on `127.0.0.1:4401`. Removed afterwards.
3. **Place.** Checked the database against the manifest SHA-256
   (`shasum -a 256`), placed it at `data/technews.db` and the uploads under
   `data/uploads/` without overwriting anything, `chmod 600` the database.
4. **Adopt**, from `~/aiandtechnews` with the env loaded:
   `./bin/adopt` (dry run: migrations 1, 2, 5, 6 present, 3 and 4 pending,
   `Rehearsal on a copy: passed`, `Result: COMPATIBLE`), then
   `./bin/adopt --apply`. The report accepted three known Node variants of
   the legacy `subscribers` table (column types from Node's `ALTER TABLE` and
   the original table definition); anything else would have stopped it. The
   `.pre-adopt-` backup is kept.
5. **Start**: bootstrapped the API agent, then (after stopping the MacBook's
   client) the tunnel agent.
6. **Verify**: `curl -fsS https://technews.subtunnel.dev/api/health`; a
   request to a unique path through the public URL appears in
   `~/aiandtechnews/logs/api.log`; the website shows current articles.

## Operations

### Deploy a new API build

```sh
# dev machine
git push ssh://ozankasikci@kasikcis-mac-mini.local/Volumes/Samsung990PRO/AIAndTechNews/repo main

# mini
WS=/Volumes/Samsung990PRO/AIAndTechNews; RT=~/aiandtechnews
. "$WS/env.sh"
cd "$WS/repo/apps/server-go"
go test -p 2 ./...
for c in api adopt migrate; do go build -p 2 -trimpath -o "$WS/bin/$c" ./cmd/$c; done
sqlite3 "$RT/data/technews.db" ".backup '$RT/data/technews-predeploy-$(date -u +%Y%m%dT%H%M%SZ).db'"
cp "$WS/bin/api" "$WS/bin/adopt" "$WS/bin/migrate" "$RT/bin/"
( cd "$RT"; set -a; . ./technews.env; set +a; ./bin/migrate )   # new migrations only; refuses an unadopted DB
launchctl kickstart -k gui/$(id -u)/news.aiandtech.api
curl -fsS http://127.0.0.1:4001/api/health
```

The API refuses to start if a migration was skipped.

### Deploy the dashboard

Build in the workspace (`pnpm install --frozen-lockfile`,
`pnpm --filter @technews/dashboard build`, standalone output), then copy
`apps/dashboard/.next/standalone/` to `$RT/dashboard/` and
`apps/dashboard/.next/static/` to `$RT/dashboard/apps/dashboard/.next/static/`.
`$RT/node` is a copy of `$WS/tools/node`. Restart with
`launchctl kickstart -k gui/$(id -u)/news.aiandtech.dashboard`.

Open it from a dev machine:

```sh
ssh -L 3001:127.0.0.1:3001 ozankasikci@kasikcis-mac-mini.local   # then http://localhost:3001
```

Do not bind it to `0.0.0.0`.

### Watch and check

```sh
tail -f ~/aiandtechnews/logs/api.log          # api.err.log for startup refusals
curl -fsS http://127.0.0.1:4001/api/health    # {"status":"ok"}
curl -fsS https://technews.subtunnel.dev/api/health
```

Through the tunnel, nginx `404 Not Found` means no client holds the subdomain;
`502` means the client cannot reach `127.0.0.1:4001`.

### Back up

Safe while the API runs:
`sqlite3 ~/aiandtechnews/data/technews.db ".backup '<dir>/technews-$(date -u +%Y%m%dT%H%M%SZ).db'"`,
plus `rsync -a ~/aiandtechnews/data/uploads/ <dir>/uploads/`.
