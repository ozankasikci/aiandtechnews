# Mac mini: access and hard-won details

Production (the Go API and the dashboard) runs on the Mac mini. This page
records how to reach it and the non-obvious things that cost time to discover.
For deploy, cutover and rollback steps, see [cutover.md](cutover.md) and
[rollback.md](rollback.md).

## Reaching it

| What | Value |
|---|---|
| Host | `kasikcis-mac-mini.local` (LAN / Bonjour; IPv4 was `192.168.1.193`) |
| User | `ozankasikci` (**not** `ozan`, which is the MacBook's user) |
| Auth | SSH key: the MacBook's `~/.ssh/id_ed25519.pub` is in the mini's `~/.ssh/authorized_keys` |
| Hardware | Apple silicon (arm64), macOS 26.x, 24 GB RAM |

```sh
ssh ozankasikci@kasikcis-mac-mini.local
```

- **Remote Login:** System Settings → General → Sharing → Remote Login must be
  on. If a password prompt closes the connection immediately, check that the
  account is allowed there, or add a key from the mini's own Terminal.
- **Full Disk Access for SSH:** without it, SSH sessions get "Operation not
  permitted" on the external SSD and on `~/Documents` / `~/Desktop`. Add
  **`/usr/libexec/sshd-keygen-wrapper`** under Privacy & Security → Full Disk
  Access, turn on "Allow full disk access for remote users" in Remote Login (ⓘ),
  then switch Remote Login off and on. Launchd starts `sshd-keygen-wrapper` for
  every connection, so that is the binary macOS checks.
- **Commands typed as `!` in Claude Code** run on the MacBook, not the mini.
  Anything meant for the mini needs `ssh …` in front.
- **Auto-login is on, FileVault is off, and it restarts after a power
  failure.** This is required: the services are per-user LaunchAgents and the
  external drive mounts at login.

## Two locations

| Path | Purpose |
|---|---|
| `/Volumes/Samsung990PRO/AIAndTechNews` (external SSD) | Build workspace: `tools/go` (1.25.4), `tools/node` (22.16.0, pnpm via corepack), `tools/subtunnel`, `cache/`, `repo/`, `bin/`, `env.sh` |
| `~/aiandtechnews` (internal disk) | **Runtime**: `bin/`, `technews.env`, `subtunnel.toml`, `data/technews.db`, `data/uploads/`, `backups/`, `logs/`, `dashboard/`, `node/`, helper scripts |

**Why the runtime isn't on the SSD:** macOS privacy protection (TCC) blocks
launchd jobs from external volumes. The job exits with code 78 and "Operation
not permitted". Keep anything a LaunchAgent reads on the internal disk.

Run `. /Volumes/Samsung990PRO/AIAndTechNews/env.sh` in every build shell. It
puts Go and Node on `PATH` and keeps every cache on the SSD (`GOTOOLCHAIN=local`).

## Services (LaunchAgents, no sudo)

`~/Library/LaunchAgents/news.aiandtech.{api,tunnel,dashboard}.plist`

```sh
launchctl list | grep aiandtech                                   # PID, last exit code
launchctl kickstart -k gui/$(id -u)/news.aiandtech.api            # restart
launchctl bootout gui/$(id -u)/news.aiandtech.api                 # stop
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/news.aiandtech.api.plist  # start
tail -f ~/aiandtechnews/logs/api.log                              # live requests
```

- **API:** `127.0.0.1:4001`. It loads `technews.env` through `/bin/sh`, so the
  file must be valid POSIX sh under `set -u`: single-quote values, and no bare
  `$` (a stray bcrypt hash once broke startup). Check it without printing
  values:
  `(set -eu; set -a; . ~/aiandtechnews/technews.env; set +a; echo ok)`.
- **Tunnel:** SubTunnel 0.3.1 with `~/aiandtechnews/subtunnel.toml`: server
  `tunnel.subtunnel.dev:7835`, tunnel `technews` → port 4001. Only one client
  can hold a subdomain, so starting the old MacBook's client again is the
  rollback.
- **Dashboard:** a Next standalone build on `127.0.0.1:3001`, not public. Reach
  it with `ssh -N -L 3001:127.0.0.1:3001 ozankasikci@kasikcis-mac-mini.local`,
  then open http://127.0.0.1:3001.

## How traffic flows

- `www.aiandtech.news` is served by **Vercel**, which calls the API at
  `https://technews.subtunnel.dev/api`. Vercel caches fetches for about 60s, so
  site changes appear within 1–2 minutes.
- The API goes out through SubTunnel (nginx on the SubTunnel VPS) and reaches
  the Go API on the mini.
- Omni Control's release build talks to `technews.subtunnel.dev/api`. The debug
  build uses `127.0.0.1:4401`; for a test instance, forward it with `ssh -L`.
- To prove the mini is serving, request a made-up path such as
  `/api/articles/proof-xyz` and find it in `logs/api.log`.

## Deploying

1. From the MacBook, push to the mini (not GitHub):
   `git push ssh://ozankasikci@kasikcis-mac-mini.local/Volumes/Samsung990PRO/AIAndTechNews/repo main`.
   The mini's repo uses `receive.denyCurrentBranch updateInstead`, so a push
   fails if files there were edited by hand.
2. On the mini, with `env.sh` loaded: `go build -p 2 -trimpath -o $W/bin/api ./cmd/api`.
   Always use `-p 2`.
3. Back up the database:
   `sqlite3 ~/aiandtechnews/data/technews.db ".backup ~/aiandtechnews/backups/<name>.db"`.
4. Copy the binary to `~/aiandtechnews/bin/api` (write `api.new`, then `mv`).
5. Run `~/aiandtechnews/bin/migrate` with the env loaded, then kickstart the API.

- **Never run `migrate` on a database that hasn't been adopted.** Production
  refuses it; use `bin/adopt` instead.
- **Dashboard updates:** `pnpm --filter @technews/dashboard build`, then copy
  `.next/standalone` to `~/aiandtechnews/dashboard` and `.next/static` to
  `dashboard/apps/dashboard/.next/static`.

## Configuration notes (`~/aiandtechnews/technews.env`)

- **Runtime and newsroom:** `APP_ENV=production`, `SERVER_ADDR=127.0.0.1:4001`,
  `TZ=Europe/Istanbul`. The collector, publisher and IndexNow are all on.
- **JWT secret:** `JWT_SECRET` was newly generated at cutover, so editors
  signed in again.
- **Illustrations:** `FEATURED_IMAGE_SOURCE=source` copies the source article's
  image to S3. Set it to `generate` for Gemini illustrations
  (`GEMINI_IMAGE_MODEL=gemini-3.1-flash-lite-image`, `GEMINI_IMAGE_SIZE=1K`;
  the lite model rejects 2K). Generated images often failed the "no text"
  compliance check.
- **AWS:** profile `aiandtech` in the mini's `~/.aws/credentials`, region
  `eu-west-1`, bucket `aiandtech-feature-images-106111531869`, prefix
  `features`. The IAM user can only write under `features/`.
- **Newsletter:** a Vercel cron triggers the digest (`/api/newsletter/digest`,
  05:00 UTC) using `NEWSLETTER_CRON_SECRET`.

## Handy scripts (in `~/aiandtechnews`)

- `set-password.sh <email>`: reset an editor's password. It prompts without
  echoing and hashes with `htpasswd -niBC 12`; Go accepts `$2y$`. Run it with
  `ssh -t`.
- `test-article.sh publish|delete`: add or remove a clearly marked test
  article.
- `remove-fake-authors.sh`: already run. It deleted the seeded persona authors
  after a backup.

## Other gotchas

- The mini's `sqlite3` (3.51) can't open a cleanly closed WAL database with
  `-readonly`. Use `-cmd ".dbconfig no_ckpt_on_close on" -cmd "PRAGMA query_only=1"`,
  or `file:…?immutable=1` for a copy nobody is writing.
- Publishing spacing: the publisher won't start an item until the minimum delay
  (30 min) has passed since the last publish, even if the item is due.
  **Publish now** in Omni Control skips this.
- The production database arrived as a Node export whose `subscribers` table
  has a legacy schema. Adopt accepts it as a known variant; see
  `internal/database/adopt`.
- **iOS installs (Omni Control):** Xcode's Apple ID sign-in had expired, so
  builds are signed with the App Store Connect API key `M77GXJ4KN9`
  (`~/.appstoreconnect/private_keys/`) on team `FQ7DT3Q254`, using
  `-authenticationKeyPath/-authenticationKeyID/-authenticationKeyIssuerID`.
  Install with `xcrun devicectl device install app`.
