# Rollback: Go on the Mac mini → Node on the MacBook

Use this when the Go API on the Mac mini misbehaves in a way that cannot be
fixed forward quickly (wrong responses on the website, failing digests,
dashboard writes failing). It returns `https://technews.subtunnel.dev` to the
Node API on the MacBook, which [cutover](cutover.md) left stopped and intact:
`$NODE/apps/server/data/technews.db` and `uploads/` as they were at the
freeze, plus `data/technews-final-<STAMP>.db`.

The website needs no change in either direction: `API_URL` stays
`https://technews.subtunnel.dev`.

## Decide first: which database does Node get back?

Everything written through Go after the cutover lives only in the Mac mini's
database and uploads directory:

| Written on Go | Where |
| --- | --- |
| Newsletter signups, reactivations, unsubscribes | `subscribers` |
| Digest runs | `newsletter_editions`, `newsletter_deliveries` |
| Dashboard article creates, edits, deletes; category and settings changes | `articles`, `categories`, `settings` |
| Dashboard media uploads | `media` rows + files in `$ROOT/uploads` |
| Article view counts | `articles.view_count` |
| Articles published by the Go publisher (if enabled) | `articles` (images are in S3) |
| Newsroom queue | `candidates` (Go only; Node ignores it) |

**A. Carry the Go database back (preferred; loses nothing).** The adopted
database is still a valid Node database: adoption only added the
`schema_migrations` and `candidates` tables, their indexes, and two
`newsroom.*` rows in `settings`, which Node ignores (its `initializeDatabase`
uses `CREATE TABLE IF NOT EXISTS` and only adds missing columns). Use this
unless the Go database itself is suspected to be damaged.

**B. Return to the freeze-time database.** Node restarts on the untouched
MacBook database; every Go-era write in the table above is lost unless you
copy it back by hand (see the end). Use this only when the Mac mini database
is not trustworthy.

## Steps

**On the Mac mini**

1. Stop the Go API and keep it stopped:
   `sudo launchctl bootout system/news.aiandtech.api`
   (`lsof -iTCP:4001 -sTCP:LISTEN` prints nothing). If the publisher or
   collector were enabled, they stop with it.
2. Stop the Mac mini's tunnel job (`sudo launchctl bootout system/<its label>`)
   so the `technews` subdomain is free.
3. For option A: take a consistent copy and note what it holds.

   ```sh
   STAMP=$(date -u +%Y%m%dT%H%M%SZ)
   sqlite3 "$ROOT/data/technews.db" ".backup '$ROOT/data/technews-rollback-$STAMP.db'"
   sqlite3 "$ROOT/data/technews-rollback-$STAMP.db" 'PRAGMA integrity_check'   # ok
   shasum -a 256 "$ROOT/data/technews-rollback-$STAMP.db"
   ```

   Copy it to the MacBook, and copy new uploads without deleting anything:
   `rsync -a "$ROOT/uploads/" macbook:"$NODE/apps/server/uploads/"`.

**On the MacBook**

4. Make sure Node is not running (`lsof -iTCP:4001 -sTCP:LISTEN`).
5. For option A: keep the freeze-time database aside, then put the Go
   database in place (remove stale `-wal`/`-shm` files of the old one only
   while Node is stopped):

   ```sh
   cd "$NODE/apps/server/data"
   mv technews.db technews-before-rollback-$STAMP.db
   rm -f technews.db-wal technews.db-shm
   cp /path/to/technews-rollback-$STAMP.db technews.db
   ```

   For option B: change nothing; `technews.db` is the freeze-time database.
6. Start the Node API the way it was supervised before the cutover, check
   `curl -fsS http://127.0.0.1:4001/api/health`.
7. Start the MacBook's `technews` tunnel client again; check
   `curl -fsS https://technews.subtunnel.dev/api/health`.
8. Restart the `news:daily` scheduler (only after making sure the Go publisher
   is stopped: step 1).
9. Verify as in cutover step 5.10-5.13: homepage, article pages, sitemap,
   newsletter archive, `npm run smoke:production`, the GitHub smoke workflow,
   a dashboard sign-in (Node's `JWT_SECRET` differs from Go's if you changed
   it, so editors sign in again).

## Notes

- **Newsletter digest.** If Go already sent today's digest and Node runs one
  for the same edition within 24 hours, both use the same Resend idempotency
  keys and byte-identical payloads, so Resend does not send it again. With
  option B, Node has no record of Go's deliveries; avoid triggering a manual
  digest for an edition Go already sent.
- **Unsubscribe and confirm links** keep working in both directions as long as
  both used the same `NEWSLETTER_TOKEN_SECRET`.
- **Option B, carrying data back by hand.** With both files at hand
  (`go.db` = the Mac mini copy, `node.db` = the MacBook database, Node stopped),
  the Go-era rows can be listed with `sqlite3`, using the UTC cutover time
  `T`. Timestamps are stored as text, so write `T` in the format the column
  holds (look first: `SELECT updated_at FROM go.subscribers ORDER BY id DESC
  LIMIT 3`; SQLite's `datetime('now')` format is `2026-09-30 10:00:00`, ISO
  values look like `2026-09-30T10:00:00.000Z`):

  ```sql
  ATTACH 'go.db' AS go;
  -- subscribers created or changed on Go
  SELECT * FROM go.subscribers WHERE updated_at >= 'T' OR created_at >= 'T';
  -- articles created or edited on Go
  SELECT id, slug, status, updated_at FROM go.articles WHERE updated_at >= 'T' OR created_at >= 'T';
  -- media uploaded on Go
  SELECT * FROM go.media WHERE uploaded_at >= 'T';
  -- deleted on Go: in node.db but not in go.db
  SELECT id, slug FROM main.articles WHERE id NOT IN (SELECT id FROM go.articles);
  ```

  Re-apply them through the Node dashboard, or with `INSERT OR REPLACE`
  statements reviewed row by row. Never delete either file; keep both until
  the data is reconciled.
- **Going forward again later.** The carried-back database is still adopted.
  After Node has run on it, `bin/adopt` reports `already managed`; run the
  rehearsal from cutover step 4 on a fresh copy before the next cutover (Node
  does not change the schema, but the rehearsal proves it).
