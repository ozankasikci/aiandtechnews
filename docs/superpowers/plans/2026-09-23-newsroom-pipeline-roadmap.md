# Newsroom Pipeline Roadmap: Go Collector, Go Publisher, Side-by-Side Production

> Roadmap for phases 3–5 of `docs/superpowers/specs/2026-09-23-ai-tech-news-newsroom-design.md`.
> Each phase below gets its own detailed, test-first implementation plan before work starts.

**Goal:** The Omni Control app drives what gets published on www.aiandtech.news
through the Go API, while the website, newsletter and dashboard keep working
without interruption.

## Decisions (2026-09-23)

- **Side by side** in production (not a full cutover).
- **S3 illustrations** for the Go publisher (port of `s3-feature-images`).
- Production Node currently runs **`main`** (og:image hotlink, or a generated
  image written to `apps/web/public/images/generated` on the home machine,
  which never reaches Vercel). The Go publisher replaces that path.

**Strategy: run Go beside Node, not instead of it.**

- Node (`apps/server`, `technews.subtunnel.dev`) keeps serving the website,
  newsletter, dashboard and media exactly as today. Porting those routes
  (migration tasks 9–14) is not required for the newsroom and stays out of scope.
- Go (`apps/server-go`) runs on the same machine against the **same SQLite
  database**, on its own port and its own tunnel hostname. It owns only the
  newsroom: `/api/newsroom/*`, the collector and the publisher.
- The website reads articles from Node. Articles the Go publisher inserts show up
  on the site immediately because both read the same `articles` table.
- The only production switch is: turn off the Node importer's external scheduler
  and turn on the Go publisher — reversible in one step.

```
iOS app ──► technews-go.subtunnel.dev ──► Go API :4402 ─┐
                                                        ├── apps/server/data/technews.db (WAL)
website ──► technews.subtunnel.dev    ──► Node API :4001 ┘
dashboard ─► localhost:4001 (Node)
```

---

## What exists vs what's missing

| Step | Node (`news-importer.ts`) | Go today | Work |
|---|---|---|---|
| Feed allowlist, URL normalization, policy gates, rewrite validation, slugify | ✓ | ✓ `content/policy.go` (full parity tests) | none |
| Feed fetch + RSS/Atom parse (regex, 30 items/feed, dedupe, sort) | ✓ | ✗ | port (phase 3) |
| Feed summary + image extraction | ✗ | ✗ | new (phase 3) |
| Store candidates, skip known URLs/slugs | ✗ (publishes directly) | store ✓, collector ✗ | phase 3 |
| Approved-redirect source fetch, canonical URL, JSON-LD/paragraph text extraction | ✓ | ✗ | port (phase 4) |
| Gemini rewrite (prompt, 2 attempts, JSON parse, validation) | ✓ | ✗ | port (phase 4) |
| Category keywords, editorial author | ✓ | ✗ | port (phase 4) |
| Illustration: reference image, generation, vision check, WebP, S3 upload + verify | branch `s3-feature-images` only | ✗ | port from the branch (phase 4) |
| Insert article + readback in one transaction | ✓ | ✗ | port (phase 4) |
| IndexNow ping | ✓ (hardcoded public key) | ✗ | port (phase 4) |
| Lock against overlapping runs | lock file | single process + guarded updates | covered |
| Adopt the Node-created production DB (ledger) | n/a | ✗ (`ErrUnmanagedDatabase`) | new (phase 5) |

---

## Phase 3 — Collector (fetching, separated from publishing)

Outcome: "Collect now" and a timer fill `candidates` with real, policy-passing
AI stories. Nothing is published. Testable fully on a dev database.

Tasks (package `internal/collector`, used by `internal/newsroom`):

1. **HTTP fetcher** — `FetchText(ctx, url, expectedSource)`: 20s timeout, UA
   `TechNews-Editorial-Importer/2.0`, manual redirects (max 5) with
   approved-source enforcement per hop (port `fetchText` +
   `resolveApprovedArticleRedirect`). Shared by the publisher.
2. **Feed parser** — port `parseFeed`/`extractXmlValue`/`decodeHtmlEntities`
   (regex-based, tolerant of malformed feeds, first 30 items). Add summary
   (`<description>`/`<summary>`/`<content:encoded>` stripped, capped ~400
   chars) and image (`<media:content>`, `<media:thumbnail>`, `<enclosure
   type="image/*">`, first `<img>` in description). Fixture-driven tests from
   real saved feeds.
3. **Collect run** — all approved feeds in parallel,
   `AutomaticItemRejectionReason`, in-batch dedupe (normalized URL, slug), skip
   URLs/slugs already in `candidates` or `articles` (inside the store insert
   with `NOT EXISTS`), insert, set `newsroom.last_collected_at`. Returns a
   summary (fetched / rejected by reason / inserted / known).
4. **Wiring** — implement `newsroom.Collector` (`Start` runs on its own context,
   mutex → `ErrCollectInProgress`), background loop every `COLLECTOR_INTERVAL`
   (default 30m) behind `COLLECTOR_ENABLED`, structured logs per run.
5. **Dev check** — `make dev-api` with `COLLECTOR_ENABLED=1` against the dev DB;
   verify in the app that real stories arrive.

## Phase 4 — Publisher (publishing, separated from fetching)

Outcome: queued candidates become real articles at their scheduled time, with
S3-hosted illustrations and IndexNow pings. Testable on a dev DB with a test
S3 prefix before production.

Tasks (packages `internal/gemini`, `internal/media`, `internal/indexnow`,
`internal/publisher`):

1. **Store seams** (from the spec): atomic claim of the earliest due item with
   the **minimum-gap rule** (`now - latest published_at ≥ min delay`),
   `MarkPublished(id, articleID)`, `MarkFailed(id, reason)`,
   `RescheduleTransient(id)` through `Service` (keeps spacing),
   `ResetProcessing` on startup.
2. **Source extraction** — canonical URL, og:image, JSON-LD `articleBody`,
   paragraph extraction (port `extractSourceText` rules exactly; test against
   the Node test fixtures), 800-char minimum.
3. **Gemini client** — text (`GEMINI_TEXT_MODEL`), image
   (`GEMINI_IMAGE_MODEL`), vision (`GEMINI_VISION_MODEL`); timeouts (the Node
   text call has none — add 90s); 429/5xx classified as transient.
4. **Rewrite** — same prompt text as `news-importer.ts:577-606`, 2 attempts with
   the same correction messages, `ValidateRewrittenArticle`.
5. **Illustration** — port the `s3-feature-images` branch:
   `buildIllustrationPrompt` + style rules, reference image (15s, 8 MB, no SVG,
   normalize to ≤1024 JPEG), up to 3 generations, vision check fails closed,
   correction lines.
6. **Media** — WebP encoding at quality 82 with a pure-Go encoder
   (`github.com/gen2brain/webp`, WASM via wazero, no cgo) — verify output
   quality against the Node/sharp result; S3 upload (AWS SDK v2) with key
   `features/YYYY/MM/<slug>-<sha16>.webp`, immutable cache headers, checksum,
   public GET verification, delete on failure.
7. **Article insert** — category keywords + `ensureCategories`, editorial author,
   insert + readback in one transaction (`published_at` set), candidate →
   `published` with `article_id`.
8. **IndexNow** — same endpoint, host, key (public; keep as a constant) and
   payload; non-blocking, errors logged.
9. **Publisher loop** — every minute behind `PUBLISHER_ENABLED`, one item per
   tick, transient failures → retry in 5 min (max 3 attempts), validation
   failures → `failed` with a readable `last_error`.
10. **Dev end-to-end** — dev DB + `S3_FEATURE_IMAGE_PREFIX=dev-features`,
    queue one candidate with a 1-minute delay, watch it publish; point the local
    web app (`API_URL=http://127.0.0.1:4001`, Node on a copy of the dev DB) or
    query the Go public `/api/articles` to see it.
11. **Policy doc** — update `NEWS_PUBLISHING_POLICY.md` (editor selection +
    queue spacing replace "1 per run / 4 slots a day"; image rules from the
    branch).

## Phase 5 — Production, side by side

Outcome: the app controls the live site; website/newsletter/dashboard untouched.

1. **Production config** — replace the hardcoded
   `ProductionDatabasePath` (`/Users/ozan/Projects/technews/...`, a path on
   this Mac) with an explicit `DATABASE_PATH` required when `APP_ENV=production`,
   and allow a production port other than 4001 (Go must not take Node's port).
2. **Adoption command** (`cmd/adopt`) — read-only verification first: compare
   the Node DB's `authors`, `categories`, `articles`, `settings` against
   migrations 1–2 by column set, types, NOT NULL, defaults, CHECKs, FKs and
   indexes (tolerant of column order from old `ALTER`s and of Node-only tables
   `media`, `subscribers`, `newsletter_*`). `--apply` then, in one transaction,
   records versions 1–2 in `schema_migrations` and applies migration 3. Never
   drops or rewrites existing data. Tested against a fresh Node-created DB and
   against a **copy** of the production DB.
3. **Backup + adopt on the home machine** — `sqlite3 .backup` copy first, dry
   run, then `--apply` (explicit approval at that moment).
4. **Deploy Go** — build the binary, run as a launchd service on
   `127.0.0.1:4402` with `APP_ENV=production`, the same `JWT_SECRET` as Node
   (so existing editor accounts sign in), Gemini/AWS env; expose it as a new
   subtunnel hostname (e.g. `technews-go.subtunnel.dev`) using the
   subtunnel-deploy skill. Collector **on**, publisher **off** (shadow mode:
   candidates accumulate while Node still publishes).
5. **iOS release config** — point `control-board.json` at the new hostname.
6. **Switch** — disable the Node importer's external scheduler, enable
   `PUBLISHER_ENABLED`. Watch the first scheduled publish end to end (article
   page, sitemap, IndexNow, smoke check).
7. **Monitoring + rollback** — add Go `/api/health` (and a DB ping) to
   `scripts/production-smoke.mjs`; `docs/newsroom-rollback.md`: set
   `PUBLISHER_ENABLED=0`, re-enable the Node scheduler.

---

## Related fixes folded in

- `s3-feature-images` branch is 50 commits behind `main`; its behavior is ported
  to Go in phase 4 instead of rebasing the Node branch. Decide whether production
  Node should also get it (separate task).
- Production must set `JWT_SECRET` (Node falls back to a hardcoded dev secret)
  and the seeded admin password must be changed — verify during phase 5.
- `.planning/debug/production-backend-outage.md` is still "fixing"; confirm the
  home machine and tunnel are healthy before phase 5.
- Go `/api/health` does not check the database; add a DB ping in phase 5.

## Order and checkpoints

1. Phase 3 plan → implement → you try "Collect now" in the app (dev DB).
2. Phase 4 plan → implement → you watch one queued story publish locally (test S3 prefix).
3. Phase 5 plan → steps 1–2 locally → production steps 3–6 one at a time, each with your approval.
