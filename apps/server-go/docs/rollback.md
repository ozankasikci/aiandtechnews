# Rollback: Go on the Mac mini → Node on the MacBook

Use this when the Go API misbehaves in a way that cannot be fixed forward
quickly (wrong responses on the website, failing digests, dashboard writes
failing). It hands `https://technews.subtunnel.dev` back to the Node API on the
old MacBook. The website needs no change: it reads that hostname either way.

The MacBook's Node database was never modified by the [cutover](cutover.md),
but it has nothing written on the mini since 2026-09-24.

## Decide first: which database does Node get back?

Written only on the mini since the cutover:

| Written on Go | Where |
| --- | --- |
| Newsletter signups, reactivations, unsubscribes | `subscribers` |
| Digest runs | `newsletter_editions`, `newsletter_deliveries` |
| Dashboard article, category, settings changes | `articles`, `categories`, `settings` |
| Dashboard media uploads | `media` rows + files in `~/aiandtechnews/data/uploads` |
| Article view counts | `articles.view_count` |
| Collector (and publisher, if enabled) | `candidates` (Go only; Node ignores it), `articles` |

**A. Carry the Go database back (preferred; loses nothing).** The adopted
database is still a valid Node database: adoption only added
`schema_migrations`, `candidates`, their indexes, and two `newsroom.*` rows in
`settings`, which Node ignores.

**B. Keep the MacBook's database as it is.** Every write in the table above is
lost unless copied back by hand (see Notes). Use it only when the mini's
database is not trustworthy.

## Steps

**On the Mac mini**

```sh
RT=~/aiandtechnews
STAMP=$(date -u +%Y%m%dT%H%M%SZ); echo "$STAMP"
```

1. Free the subdomain and stop Go (the collector and publisher stop with it):

   ```sh
   launchctl bootout gui/$(id -u)/news.aiandtech.tunnel
   launchctl bootout gui/$(id -u)/news.aiandtech.api
   lsof -nP -iTCP:4001 -sTCP:LISTEN                  # prints nothing
   ```

   `bootout` lasts until the next login, when agents in
   `~/Library/LaunchAgents/` load again (auto-login after a reboot); to keep
   them off, move the two plists out of that directory.
2. Option A only: a consistent copy, checked and fingerprinted, sent to the
   MacBook under a temporary name, plus the uploads (nothing on the MacBook is
   deleted):

   ```sh
   sqlite3 "$RT/data/technews.db" ".backup '$RT/data/technews-rollback-$STAMP.db'"
   sqlite3 "$RT/data/technews-rollback-$STAMP.db" 'PRAGMA integrity_check'   # ok
   shasum -a 256 "$RT/data/technews-rollback-$STAMP.db"
   rsync -a "$RT/data/technews-rollback-$STAMP.db" "<macbook>:Projects/technews/apps/server/data/incoming-rollback-$STAMP.db"
   rsync -a "$RT/data/uploads/" "<macbook>:Projects/technews/apps/server/uploads/"
   ```

**On the MacBook**

```sh
NODE=/Users/ozan/Projects/technews
STAMP=<the value from the mini>
```

3. Node is not running: `lsof -nP -iTCP:4001 -sTCP:LISTEN` prints nothing.
4. Option A: check the copy, move the old database aside, put the Go one in
   place, never overwriting:

   ```sh
   cd "$NODE/apps/server/data"
   shasum -a 256 "incoming-rollback-$STAMP.db"          # equals the mini's
   mv -n technews.db "technews-before-rollback-$STAMP.db"
   for f in technews.db-wal technews.db-shm; do [ -e "$f" ] && mv -n "$f" "technews-before-rollback-$STAMP.db${f#technews.db}"; done
   ls technews.db 2>/dev/null && echo "STOP: technews.db still in place"
   mv -n "incoming-rollback-$STAMP.db" technews.db
   ```

   Option B: change nothing.
5. Start the Node API the way it ran before, then
   `curl -fsS http://127.0.0.1:4001/api/health`.
6. Start the MacBook's `technews` subtunnel client; check
   `curl -fsS https://technews.subtunnel.dev/api/health`.
7. Restart the `news:daily` scheduler only now (the Go collector/publisher is
   stopped, so nothing publishes twice).
8. Verify: `https://www.aiandtech.news` shows current articles within 1-2
   minutes, article pages, `/sitemap.xml`, the newsletter archive,
   `npm run smoke:production`, and a dashboard sign-in (sessions stay valid if
   both used the same `JWT_SECRET`).

## Notes

- **Newsletter digest.** If Go already sent today's digest and Node runs one
  for the same edition within 24 hours, both use the same Resend idempotency
  keys and payloads, so Resend does not send it again. With option B, Node has
  no record of Go's deliveries; do not trigger a manual digest for an edition
  Go already sent.
- **Unsubscribe and confirm links** keep working as long as both used the same
  `NEWSLETTER_TOKEN_SECRET`.
- **Option B, carrying data back by hand.** With `go.db` (a mini copy) and
  `node.db` (the MacBook database, Node stopped), list Go-era rows by the UTC
  cutover time `T`, written in the format the column holds (check first, for
  example `SELECT updated_at FROM go.subscribers ORDER BY id DESC LIMIT 3`):

  ```sql
  ATTACH 'go.db' AS go;
  SELECT * FROM go.subscribers WHERE updated_at >= 'T' OR created_at >= 'T';
  SELECT id, slug, status, updated_at FROM go.articles WHERE updated_at >= 'T' OR created_at >= 'T';
  SELECT * FROM go.media WHERE uploaded_at >= 'T';
  SELECT id, slug FROM main.articles WHERE id NOT IN (SELECT id FROM go.articles);  -- deleted on Go
  ```

  Re-apply them through the Node dashboard or reviewed `INSERT OR REPLACE`
  statements. Never delete either file until the data is reconciled.
- **Going forward again.** Bootstrap the mini's API agent, stop the MacBook's
  client, bootstrap the tunnel agent. If Node wrote to a carried-back database,
  copy it back and run `bin/adopt` (read-only) first: it should report
  `already managed`.
