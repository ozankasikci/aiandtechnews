# Go Newsletter Port Implementation Plan (Plan 5c)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port the Node newsletter to the Go API with exact parity for everything the website relies on: `POST /api/subscribe`, `GET /api/newsletter/confirm`, `GET`/`POST /api/newsletter/unsubscribe`, `GET /api/newsletter/editions`, `GET /api/newsletter/editions/:edition`, and `GET`/`POST /api/newsletter/digest` (the Vercel cron target). Request and response shapes, status codes, and error messages stay the same; confirm and unsubscribe tokens stay byte-identical so links in emails Node already sent keep working; edition keys stay Europe/Istanbul calendar dates; the digest selects the same articles, renders byte-identical HTML and text, posts a byte-identical Resend request body with Node's idempotency keys and retry classes, and walks the same delivery states. This is migration task 12 in `docs/plans/2026-09-20-go-api-migration.md`.

**Architecture:** A new capability package, `internal/newsletter`, owns everything; it imports only `database/sql`, `internal/jsonbody`, chi, and the standard library, and reads the `articles` and `categories` tables with Node's SQL.
- `jsvalues.go` + `jscase.go` port the JavaScript semantics the Node code relies on without saying so: `trim`/`\s` whitespace, UTF-16 `length` and `slice` (with better-sqlite3's WTF-8 lone surrogates), `toLowerCase`/`toUpperCase` (SpecialCasing, final sigma), `encodeURIComponent`, `JSON.stringify` for strings, `parseInt(v, 10)`, `Math.round`, `toISOString`, the part of V8's `Date.parse` that article timestamps use, and Intl's Europe/Istanbul date (zone data embedded with `time/tzdata`).
- `tokens.go` signs and verifies Node's `base64url(JSON).base64url(HMAC-SHA256)` tokens, including Node's lenient verification.
- `reading.go` and `email.go` port `readingMinutes` and the email templates verbatim.
- `resend.go` is Node's `createResendSender`: byte-identical JSON body, `Idempotency-Key`, three attempts, `429`/`5xx` retried after `Retry-After` or 650/1300ms, Node's error messages, a 30s timeout per attempt.
- `store.go` + `digest.go` hold Node's SQL (check-then-write pairs in one transaction, the delivery claim as one conditional upsert); `service.go` + `digest.go` are `NewsletterService`; `http.go` is the eight routes with Node's statuses and bodies, the global signup throttle, and the cron check.
- Migration 6 (`migrations/006_newsletter.sql`) is Node's final newsletter schema with `CREATE ... IF NOT EXISTS`, since every production database already has it.
- `internal/config` gains the Node env vars (`NEWSLETTER_*`, `RESEND_API_KEY`, `CRON_SECRET` fallback), redacted in `String`/`GoString`; `internal/app` composes the service, mounts the routes, binds digest runs to shutdown, and replays the eight Node newsletter contracts.

**Tech Stack:** Go 1.25, `net/http` (+ `http.ResponseController` deadlines), `crypto/hmac`, `crypto/subtle`, `time/tzdata`, `github.com/go-chi/chi/v5`, `database/sql` + `modernc.org/sqlite`, and the existing `contracttest`, `migrate`, `testutil`, and `jsonbody` packages. Node 22 with `tsx` (from `apps/server`) only to record golden vectors.

**Working directory:** `apps/server-go` (repo `/Users/ozan/Projects/aiandtechnews`, branch `go-newsletter` created from `main` in a temporary worktree, `git worktree add ../aiandtechnews-newsletter -b go-newsletter main`). Every `go` command below runs from `apps/server-go`.

**Build and test commands:** always pass `-p 2`, because a heavy parallel compile once crashed this machine: `go build -p 2 ./...`, `go vet -p 2 ./...`, `go test -p 2 ./...`. Run the race detector only on the touched packages: `go test -p 2 -race ./internal/newsletter ./internal/app`. If a compile is killed (`signal: killed`), stop and report instead of retrying.

**Commits:** plain sentences, no `feat:`-style prefixes, no co-author trailers.

**Safety:** no test sends email, calls the real Resend API, or uses a real API key. Every sender in a test is a fake (`recordingSender`) or points at an `httptest` stand-in (`scriptedResend`, `fakeResend`); `internal/app` tests override the endpoint through `app.StubNewsletterDeliveryForTest`. Tests use `testutil.OpenDatabase` (a temporary file) for every database, never a real one, and start no listener other than `httptest`'s loopback stand-ins. The recorder (`testdata/record-node.ts`) runs the Node modules on in-memory better-sqlite3 databases with `fetch` and `setTimeout` stubbed. Never start the API on ports 3001, 3002, or 4001.

**Verified:** every task's code was built and tested in the worktree before this plan was written, and each task's new tests were run against the previous task's tree first (compile failure or wrong result). After Task 12: `gofmt -l .` is empty, `go vet -p 2 ./...` is clean, `go test -p 2 ./...` passes (1011 tests), `go test -p 2 -race ./internal/newsletter ./internal/app` passes, and `make contracts-check` passes. The Node behaviors the tests pin were **recorded, not inferred**: `testdata/record-node.ts` drives the real `apps/server/src/newsletter/{tokens,email,reading-time,service}.ts` and `apps/server/src/db.ts` (Node 22.16.0, better-sqlite3 11, V8's `Date.parse`, ICU case mappings, `TZ=Europe/Istanbul`), and the handful of hand-written expectations that are not in the recording (the script/style scanner, `Number()`/`setTimeout` clamping) were checked in Node before being written down.

---

## Node reference

Line numbers are in `apps/server/src/newsletter/service.ts` unless noted.

| Behavior | Node source |
|---|---|
| `normalizeEmail`: `trim().toLowerCase()`, `/^[^\s@]+@[^\s@]+\.[^\s@]+$/`, at most 254 UTF-16 units | `:64-67` |
| `formatIstanbulDate`: `Intl.DateTimeFormat("en-CA", {timeZone: "Europe/Istanbul"})` → `YYYY-MM-DD` (the edition key) | `:69-78` |
| `safeSiteUrl`: `NEWSLETTER_SITE_URL?.trim() \|\| "https://aiandtech.news"`, `https` unless `hostname === "localhost"`, returns the origin; the constructor calls it, so a bad value stops Node at startup | `:80-87`, `:123` |
| `requireTokenSecret`: trimmed `NEWSLETTER_TOKEN_SECRET` of at least 32 characters, else `NewsletterConfigurationError` | `:89-95` |
| `toPublicArticle` / `readingMinutes(row.content)` | `:97-105`; `reading-time.ts:1-17` |
| `parseArticleTimestamp`: `YYYY-MM-DD HH:MM:SS` → UTC, anything else → `Date.parse` | `:107-113` |
| `requestSubscription`: activates at once, sends nothing; active → `already_active`; any other row reactivated (`confirmed_at = COALESCE(confirmed_at, now)`, `unsubscribed_at = NULL`); new row inserted active; `placement.slice(0, 80)` | `:130-166` |
| `confirmSubscription`: verify `confirm` token; missing or unsubscribed → `invalid`; active → `already_confirmed`; otherwise activate and send the welcome email with key `newsletter-welcome-<id>`, `welcomeSent` false when the send fails | `:168-198` |
| `unsubscribe`: verify `unsubscribe` token; missing → `invalid`; unsubscribed → `already_unsubscribed`; otherwise unsubscribe (pending rows too) | `:200-219` |
| `listEditions` (`ORDER BY edition_key DESC LIMIT min(max(limit,1),100)`), `getEdition`, `toEdition` (stored JSON passed through when it is an array, `[]` otherwise) | `:221-262` |
| `sendDailyDigest`: token secret first; edition key; `LIMIT 20` published articles `ORDER BY published_at DESC`; keep `now-30h <= published <= now`; first 5; no articles → counts only; upsert the edition (keeps `created_at`); active subscribers by id; skip `sent`; upsert `sending` (clears `error`); send with `newsletter-digest-<edition>-<id>`; `sent` (+ provider id, `sent_at = now`) or `failed` (`message.slice(0, 500)`); 550ms between subscribers after every attempted one but the last | `:264-354` |
| `createNewsletterToken` / `verifyNewsletterToken` | `tokens.ts:12-62` |
| `createResendSender`: `RESEND_API_KEY`/`NEWSLETTER_FROM` trimmed per call or `NewsletterConfigurationError`; body `{from,to:[to],subject,html,text,reply_to?,headers?,tags?}`; three attempts; retry `429`/`>=500` after `Number(retry-after)` seconds or `650*(attempt+1)` ms; error `body.message \|\| body.error \|\| "Email provider returned <status>"` | `email.ts:18-66` |
| `escapeHtml`, `emailLayout`, `button`, `welcomeEmail`, `digestSubject`, `groupByCategory`, `digestEmail` | `email.ts:68-195` |
| Routes: `secretsMatch` (constant time), subscribe (throttle 30/60s global, `TypeError` → 400, config → 503, else 502), confirm (config → 503 `unavailable`, else 500 `invalid`), unsubscribe GET/POST (query token, else body token), editions (`parseInt(String(limit \|\| "30"), 10)`), edition (404 `Edition not found`), digest GET/POST (`Bearer` stripped with `/^Bearer\s+/i`, `{success: failed === 0, ...result}`, config → 503, else 500) | `routes/public.ts:188-303` |
| `express.json()` before every route (malformed JSON never reaches a handler) | `app.ts:29` |
| Cron secret: `NEWSLETTER_CRON_SECRET \|\| CRON_SECRET \|\| ""` | `index.ts:29` |
| Tables `subscribers`, `newsletter_deliveries` (`UNIQUE(subscriber_id, edition_key)`, `ON DELETE CASCADE`), `newsletter_editions` (`edition_key UNIQUE`) | `db.ts:72-103` |
| Legacy `subscribers` columns added with `ALTER TABLE` (`status NOT NULL DEFAULT 'pending'`, the rest nullable), the normalizing `UPDATE`, and the two indexes | `db.ts:116-139` |
| Contract capture: secrets, recorder sender (`synthetic-email-<n>`), fixed clock `2026-09-20T12:00:00.000Z`, seed subscribers 501-503 and editions 601-602 | `scripts/contract-server.ts:17-19, 167-185`; `scripts/contracts/synthetic-seed.ts:64-84` |
| Contract operations 7-14 `newsletter.*`, bindings `$NEWSLETTER_*_TOKEN` (`newsletterToken` resolver with a SHA-256 vector) and `$CRON_AUTHORIZATION` (`secretRefTemplate`) | `apps/server/contracts/node/contracts.json` |

### What the website relies on

| Caller | Uses |
|---|---|
| `apps/web/app/api/subscribe/route.ts` | forwards the JSON body to `POST /api/subscribe`, returns Go's JSON and status (the forms read `success`, `state`, `message`, `error`) |
| `apps/web/app/api/newsletter/confirm/route.ts` | `GET /api/newsletter/confirm?token=`; redirects to `/newsletter/confirmed?state=<state>` when the response is OK and has `state`, else `unavailable` |
| `apps/web/app/api/newsletter/unsubscribe/route.ts` | `POST /api/newsletter/unsubscribe?token=` with body `{token}`; redirects (GET) or answers `{state}` (POST); `state` values `unsubscribed`, `already_unsubscribed`, `invalid`, `unavailable` |
| `apps/web/app/api/newsletter/digest/route.ts` + `apps/web/vercel.json` | Vercel cron `0 5 * * *` (05:00 UTC, 08:00 Istanbul) → `GET` on the website, which checks `Bearer CRON_SECRET` and forwards `POST /api/newsletter/digest` with the same `Authorization` header; returns Go's JSON and status (`maxDuration` 300s) |
| `apps/web/app/lib/api.ts:203-224`, `app/newsletter/archive/*`, `app/sitemap.ts:26-35` | `GET /api/newsletter/editions?limit=` and `/editions/<key>`: `{edition, subject, articles[{title,slug,excerpt,category,readingMinutes}], createdAt}`; keys are rendered as dates |

### Response shapes

- Subscribe: `200 {"success":true,"state":"subscribed"|"already_active","message":"You're subscribed. The next digest will arrive in your inbox."|"You're already subscribed."}`; `400 {"error":"Valid email required"}`; `429 {"error":"Too many signup attempts. Please try again shortly."}`; `503 {"error":"Newsletter signup is temporarily unavailable"}`; `502 {"error":"We could not complete your signup. Please try again."}`.
- Confirm: `200 {"state":"confirmed"|"already_confirmed"|"invalid","welcomeSent":bool}`; `503 {"state":"unavailable","error":"Newsletter confirmation is temporarily unavailable"}`; `500 {"state":"invalid","error":"Confirmation failed"}`.
- Unsubscribe: `200 {"state":"unsubscribed"|"already_unsubscribed"|"invalid"}`; `503 {"state":"unavailable","error":"Unsubscribe is temporarily unavailable"}`; `500 {"state":"invalid","error":"Unsubscribe failed"}`.
- Editions: `200 {"editions":[...]}`; edition: `200 {"edition":{...}}` or `404 {"error":"Edition not found"}`.
- Digest: `200 {"success":failed==0,"edition","articles","sent","skipped","failed"}` (that key order); `401 {"error":"Unauthorized"}`; `503 {"error":"Newsletter delivery is not configured"}` (also for a missing token secret); `500 {"error":"Newsletter digest failed"}`.

## Known Node behaviors kept (parity, deliberately not fixed)

Each is pinned by a test (named in brackets). Changing any of them needs a separately reviewed contract change, ideally after cutover.

1. **Signup reactivates anyone, immediately, without email confirmation** (commit `1a244d8`), including an address that unsubscribed; `confirmed_at` keeps its first value. [`TestSubscribeReplaysNodesSignupScenario`]
2. **changed (approved, see "Approved changes"): the signup throttle is per client IP.** Node's 30 attempts per rolling minute were global, so one client could make signup answer `429` for everybody. Kept: the window, the limit of 30, counting invalid addresses, not counting refused attempts or malformed JSON bodies, and the `429` body. [`TestSignupThrottleIsNodesRollingMinute`, `TestSignupLimitIsPerClientIP`]
3. **The confirm route stays live** for links from the old double opt-in emails, although nothing mints confirm tokens any more; confirming sends the welcome email. [`TestConfirmAndUnsubscribeReplayNodeWithNodeMintedTokens`, contract replay]
4. **Unsubscribe tokens never expire**, and verification accepts a token followed by `.` or `..anything` (the third `.`-segment only has to be empty), decodes the payload as leniently as `Buffer.from(…, "base64url")` (both alphabets, invalid characters skipped, stops at `=`), accepts `v: 1.0` and `id: 1e2`, and compares `exp` in whole seconds. [`TestVerifyTokenMatchesNode`, `TestNodeBase64DecodingIsLenientLikeBuffer`]
5. **A valid unsubscribe token unsubscribes pending subscribers too.** [`TestConfirmAndUnsubscribeReplayNodeWithNodeMintedTokens`]
6. **Only the 20 largest `published_at` texts are considered**, sorted as text: ISO values (`…T…`) sort above SQLite values (`… …`) of the same day, and twenty far-future timestamps crowd out every real article (recorded: `articles: 0`). [`TestDigestOnlyConsidersTheTwentyNewestPublishedAtTextsLikeNode`, `TestDigestTakesTheFiveNewestAndRendersThemLikeNode`]
7. **Offset-less date-times are read in the process time zone** (V8), which is what the dashboard's date picker stores. The dashboard's edit page also round-trips `new Date(published_at).toISOString().slice(0, 16)` (UTC digits) into that picker, so re-saving an article shifts its `published_at` by the host's UTC offset (see Open questions). [`TestParseArticleTimestampMatchesNodesDateParse`, `TestDigestSelectsArticlesLikeNode`]
8. **A rerun of the same edition rewrites its archived subject and articles** (keeping `created_at`), so the archive shows the latest selection even when earlier recipients got a different one. [`TestDigestDeliveryStatesAndRetriesMatchNode`]
9. **`sent_at` is the digest's start time,** not the send time; the delivery's `created_at` is kept on retries. [`TestDigestDeliveryStatesAndRetriesMatchNode`]
10. **`sending` and `failed` deliveries are retried by the next run** with the same idempotency key (crash recovery); only `sent` is skipped. Duplicate protection beyond this process is Resend's 24-hour idempotency window. [`TestDigestDeliveryStatesAndRetriesMatchNode`]
11. **Retry classes:** only `429` and `>= 500` are retried; a `2xx` without an id, any other status, and transport errors (`fetch failed`) fail at once. `Retry-After` is `Number()`-parsed (`0x2`, `1e1`, ` 3 ` work; HTTP dates do not), waits at least 1 ms like `setTimeout`, and (changed, approved) at most 60s; Node did not cap it. A JSON `null` body is retried and then fails with V8's `Cannot read properties of null (reading 'message')`. [`TestResendSenderMatchesNodeRequestsRetriesAndErrors`, `TestResendRetryDelayFollowsNodeTimers`]
12. **Unconfigured delivery is not an error for the digest:** without `RESEND_API_KEY`/`NEWSLETTER_FROM` every delivery is recorded `failed` with `Newsletter delivery is not configured` and the route answers `200 {"success":false,…}`; only a missing token secret answers `503`. [`TestUnconfiguredDeliveryFailsEachDeliveryLikeNode`]
13. **The bare cron secret is accepted** without `Bearer ` (Node only strips the prefix when present); `bearer` in any case and any whitespace run work; an empty or blank configured secret refuses everything. [`TestDigestAuthorizationMatchesNode`]
14. **`limit` parsing:** `parseInt` semantics (`1e3` → 1, `7abc` → 7, `-3` → clamped to 1, `?limit=5&limit=9` → 5, `?limit=&limit=9` → 30), clamped to 1..100. [`TestEditionsLimitIsParsedLikeNode`, `TestEditionsReplayNode`]
15. **The archive passes arbitrary stored arrays through** (extra fields, non-object elements); objects and malformed JSON become `[]`. [`TestEditionsReplayNode`]
16. **JavaScript text semantics:** emails are trimmed with Unicode whitespace and lowercased with `toLowerCase` (`İ` → `i̇`, final `ς`), lengths and truncations count UTF-16 units, a cut through a surrogate pair stores better-sqlite3's WTF-8 lone surrogate, categories are uppercased with SpecialCasing (`ß` → `SS`). The existing-row lookup is `lower(email) = ?`, which SQLite lowercases in ASCII only. [`TestNormalizeEmailMatchesNode`, `TestSliceUTF16MatchesStringSlice`, `TestJavaScriptCaseMappingMatchesNodeForEveryCodePoint`]

## Go differences (transport conventions, robustness, and documented gaps)

None of these is visible in the recorded contracts.

| Situation | Node (recorded or read from source) | Go |
|---|---|---|
| Malformed JSON body (subscribe, unsubscribe) | `express.json()` 400 HTML page | `400 {"error":"Invalid request body"}` (earlier slices' convention); bodies over 100 KiB likewise |
| JSON responses | literal `<`, `>`, `&`, U+2028/U+2029 | `encoding/json` escapes them (semantically identical, existing convention); stored edition JSON and Resend bodies use a `JSON.stringify` port and are byte-identical |
| Edition archive DB error | 500 HTML (Express default handler) | `500 {"error":"Internal server error"}` |
| Two digest requests at once (cron retry + manual `POST`) | both run interleaved; a subscriber mid-send can be sent twice, deduplicated only by Resend's idempotency key | runs are serialized in-process (`Service.digestMu`); the second sees `sent` rows and skips them |
| Digest check-then-claim | `SELECT status` then upsert | one conditional upsert (`… DO UPDATE … WHERE status <> 'sent'`), same outcomes, atomic |
| Signup, confirm, unsubscribe check-then-write | two statements on Node's single thread | one SQLite transaction (concurrent Go requests cannot interleave them) |
| Two confirms of one pending subscriber at once | cannot interleave (single thread) | the transaction makes exactly one of them send the welcome email |
| Client disconnects mid-request | Express keeps going | signup/confirm/unsubscribe writes and the digest run on contexts detached from the request, so they complete too |
| Process shutdown during a digest | the process dies mid-run | the digest stops between deliveries (the in-flight send's outcome is still recorded), so no row is left `sending` by Go itself; the next run resumes with the same keys |
| Time limits | none (undici defaults) | 30s per Resend attempt (a timeout is a transport error, recorded as `fetch failed`, not retried); the digest route gets a 15-minute write deadline and confirm 2 minutes (the server default is 30s); a response body is read up to 1 MiB |
| `NEWSLETTER_SITE_URL` | any WHATWG URL whose scheme is `https:` or whose host is `localhost` (so `ftp://localhost` passes); IDN hosts are punycoded | `http`/`https` only, ASCII host names only; anything else fails composition like an invalid URL does in Node |
| Legacy `Date.parse` formats (`Sep 19 2026`, RFC 2822, 1-digit months, unsigned 5-digit years) | parsed, in the process time zone | unparseable: such an article is left out of the digest (no code path writes them) |
| A truthy non-string Resend `id` (`{"id":5}`) | stored as a number | stored as its JavaScript string (`"5"`) |
| A lone surrogate in a slug (`encodeURIComponent`) | throws `URIError` | encoded as U+FFFD |
| Unicode case mappings newer than Go's tables (Unicode 16 in Node 22, 15.0 in Go 1.25) | mapped | unchanged (the exhaustive test skips only code points Go does not assign) |
| Shutdown | the process dies | digest runs and welcome emails are bound to the service lifecycle: shutdown cancels them (an interrupted send is recorded `failed` with `context canceled` and retried with the same key), a digest queued behind another gives up, and `App.Run` waits up to 15s after the HTTP server stops for their outcomes (`Service.Wait`) |
| Transport failure logs | `console.error` of the error | the stored text stays `fetch failed`; the log carries the cause (`fetchError.LogValue`), never a recipient address |
| `HEAD` on the GET routes | answered by Express | `405` (router-wide convention of the earlier slices) |

## Approved changes (user decisions after review, 2026-09-24)

These deliberately diverge from Node; they were applied as follow-up commits on `go-newsletter` (see "Follow-up after review").

1. **Signup throttle per client IP with a global ceiling** (`internal/newsletter/signup_limit.go`). 30 attempts per rolling minute per client IP (Node: in total) and at most 300 per minute across all clients; a refused attempt is counted in neither; the answer stays `429 {"error":"Too many signup attempts. Please try again shortly."}`. The client IP is `RemoteAddr`, except that a loopback peer (the tunnel/reverse proxy on the same host) is replaced by the first `X-Forwarded-For` address when it parses; addresses are normalized with `netip` (IPv4-mapped IPv6 unmapped, zones dropped, canonical IPv6). The table holds at most 10,000 clients: on insert into a full table, idle clients (no attempt in the window) are evicted, then the least recently seen. Caveat: the website's `/api/subscribe` route calls the API from Vercel without forwarding the visitor's address, so website signups share one bucket until that route forwards `X-Forwarded-For`. [`TestSignupLimitIsPerClientIP`, `TestForwardedForIsTrustedOnlyFromLoopback`, `TestSignupHasAGlobalCeiling`, `TestSignupLimiterBoundsTrackedClients`]
2. **`Retry-After` capped at 60s** (`maxRetryDelay` in `resend.go`). [`TestResendRetryDelayFollowsNodeTimers`]
3. **Dashboard publish time round trip** (`apps/dashboard`, not the API). The edit page filled its `datetime-local` picker with UTC digits and both editor pages sent the picked wall-clock time without an offset, which the API reads in the server's zone, so every re-save shifted `published_at`. `apps/dashboard/lib/dates.ts` now shows stored values in the browser's zone (SQLite timestamps as UTC, like the API), sends picked values as `toISOString()` (`…Z`), and the edit page sends the stored value unchanged when the picker was not touched. Node and Go parse `…Z` identically, whatever their `TZ`. [`TestDashboardISOTimestampsAreTheSameInstantInEveryZone`; the helpers were checked in Node under `Europe/Istanbul` and `America/New_York`]

Kept as Node parity by decision: unsubscribe tokens never expire, and signup reactivates unsubscribed addresses.

## Cutover checklist

- Copy `NEWSLETTER_TOKEN_SECRET` unchanged from the Node environment: it verifies every confirm and unsubscribe link already in subscribers' inboxes (and `List-Unsubscribe` headers). A new secret silently turns every existing link into `invalid`.
- Copy `RESEND_API_KEY`, `NEWSLETTER_FROM`, `NEWSLETTER_REPLY_TO`, and `NEWSLETTER_SITE_URL` (`https://aiandtech.news`), and set `NEWSLETTER_CRON_SECRET` to the Vercel project's `CRON_SECRET`. Keep the From/Reply-To values identical so a digest repeated by both servers within 24 hours is recognized by Resend as the same request.
- Run the Go process with the Node host's `TZ` (offset-less `published_at` values from the dashboard are read in that zone). The MacBook's zone is expected to be `Europe/Istanbul`; check with `date +%Z` before cutover.
- Switch the website's `API_URL` (Vercel) to the Go API; the digest follows it. Do not keep a second scheduler calling Node's digest.
- Run `cmd/migrate` only through the future adoption command: `migrate.Run` refuses Node-created databases; migration 6 is `IF NOT EXISTS` so stamping it is a no-op. The adoption verifier must check that `subscribers` has all nine columns (a database Node has started since `fe966d9` does, possibly with the `ALTER`-added ones at the end).
- After the first Go digest, check `newsletter_deliveries` for `failed` rows and the Go log for `newsletter digest delivery failed`.

## Out of scope

- A per-client signup throttle, bounded `Retry-After`, and expiring unsubscribe tokens (Node behavior kept; see Open questions).
- Updating `NEWSLETTER_SETUP.md`, which still describes the removed double opt-in flow (Node documentation).
- Bounce/complaint webhooks, list management, and an admin view of subscribers (Node has none).
- Cutover itself (migration task 14).

## Open questions for the user (Node behaviors that look like bugs)

Decided on 2026-09-24: 1 and unsubscribe-token expiry are kept (parity); 2, 3, and 5 were changed (see "Approved changes"); 4 remains open.

1. **Signup reactivates unsubscribed addresses without consent.** Anyone who knows an address can resubscribe it with one request; with double opt-in gone there is no confirmation. Keep (parity), or require the unsubscribe to be honoured unless the owner confirms?
2. **The global signup throttle is a denial-of-service lever** (30 requests a minute from one client block signup for everyone). Make it per IP (behind Vercel, the forwarded IP) or keep it?
3. **Dashboard date round-trip shifts `published_at`.** The edit page fills the date picker with UTC digits and the API reads the saved value as local time, so every re-save moves an article by the host's UTC offset (3 hours in Istanbul), which also moves it in or out of the digest's 30-hour window. This is a dashboard bug; fix it there (send an ISO string with offset)?
4. **`NEWSLETTER_SETUP.md` is stale** (describes double opt-in, pending rows, and a confirmation email). Update it as part of the cutover docs?
5. **`Retry-After` is unbounded** (a `Retry-After: 3600` from Resend stalls the digest for an hour). Cap it (for example at 60s)?

---

## File structure

| File | Responsibility |
|---|---|
| Modify `internal/config/config.go`; create `internal/config/newsletter_test.go` | Node's newsletter env vars, the `CRON_SECRET` fallback, redaction |
| Create `internal/newsletter/testdata/record-node.ts`, `node-golden.json`, `node-case-mappings.json` | The Node recorder and its golden output |
| Create `internal/newsletter/migrations.go`, `migrations/006_newsletter.sql`, `migrations_test.go`, `golden_test.go`; modify `internal/app/app.go` (`Migrations`), `internal/app/migrations_test.go` | Migration 6; the golden loader |
| Create `internal/newsletter/jsvalues.go`, `jscase.go`, `jsvalues_test.go` | JavaScript string, number, and date semantics |
| Create `internal/newsletter/tokens.go`, `tokens_test.go` | Token signing and verification |
| Create `internal/newsletter/reading.go`, `email.go`, `email_test.go` | Reading time and the two email templates |
| Create `internal/newsletter/resend.go`, `resend_test.go` | The Resend sender |
| Create `internal/newsletter/store.go`, `service.go`, `service_test.go` | Signup, confirm, unsubscribe, editions |
| Create `internal/newsletter/digest.go`, `digest_test.go` | The daily digest |
| Create `internal/newsletter/http.go`, `http_test.go` | The eight routes |
| Modify `internal/app/app.go`, `internal/app/export_test.go`; create `internal/app/newsletter_contract_test.go` | Composition, shutdown binding, contract replay |
| Modify `README.md`, `../../docs/plans/2026-09-20-go-api-migration.md` | Configuration, newsletter section, task 12 status |

---

### Task 1: Newsletter configuration

Node reads six variables (`apps/server/.env.example`) plus `CRON_SECRET` as the digest secret's fallback (`index.ts:29`: `NEWSLETTER_CRON_SECRET || CRON_SECRET || ""`). Go loads them with the same names and **raw values**: Node trims each one where it uses it, and so does `internal/newsletter`, so `internal/config` must not trim or validate them. None is required at startup (signup works unconfigured, like Node); `NEWSLETTER_SITE_URL` is validated when the application is composed (Task 11), which is when Node's constructor would stop the process. The three secrets are redacted in `String`/`GoString`.

**Files:** Modify `internal/config/config.go`; create `internal/config/newsletter_test.go`

- [ ] **Step 1: Write the failing test.** Create `internal/config/newsletter_test.go`:

```go
package config

import (
	"fmt"
	"strings"
	"testing"
)

func TestLoadReadsNewsletterSettingsLikeNode(t *testing.T) {
	cfg, err := Load(mapLookup(map[string]string{
		"NEWSLETTER_SITE_URL":     "https://aiandtech.news",
		"NEWSLETTER_TOKEN_SECRET": "  synthetic-token-secret-with-32-characters  ",
		"RESEND_API_KEY":          " re_synthetic ",
		"NEWSLETTER_FROM":         "AI & Tech News <news@example.invalid>",
		"NEWSLETTER_REPLY_TO":     "reply@example.invalid",
		"NEWSLETTER_CRON_SECRET":  "cron-secret",
		"CRON_SECRET":             "vercel-secret",
	}), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Values are kept raw: Node trims them where it uses them, and so does Go.
	if cfg.NewsletterSiteURL != "https://aiandtech.news" ||
		cfg.NewsletterTokenSecret != "  synthetic-token-secret-with-32-characters  " ||
		cfg.ResendAPIKey != " re_synthetic " ||
		cfg.NewsletterFrom != "AI & Tech News <news@example.invalid>" ||
		cfg.NewsletterReplyTo != "reply@example.invalid" ||
		cfg.NewsletterCronSecret != "cron-secret" {
		t.Fatalf("newsletter settings were not loaded: %v", cfg)
	}
}

func TestLoadNewsletterSettingsDefaultToUnset(t *testing.T) {
	cfg, err := Load(func(string) string { return "" }, t.TempDir())
	if err != nil {
		t.Fatalf("an unconfigured newsletter must not block startup: %v", err)
	}
	if cfg.NewsletterSiteURL != "" || cfg.NewsletterTokenSecret != "" || cfg.ResendAPIKey != "" ||
		cfg.NewsletterFrom != "" || cfg.NewsletterReplyTo != "" || cfg.NewsletterCronSecret != "" {
		t.Fatalf("newsletter defaults = %v", cfg)
	}
}

// Node: process.env.NEWSLETTER_CRON_SECRET || process.env.CRON_SECRET || "".
// An empty NEWSLETTER_CRON_SECRET falls through; a blank one does not.
func TestNewsletterCronSecretFallsBackToCronSecretLikeNode(t *testing.T) {
	tests := []struct {
		name       string
		newsletter string
		cron       string
		want       string
	}{
		{name: "newsletter secret wins", newsletter: "a", cron: "b", want: "a"},
		{name: "empty falls back to CRON_SECRET", newsletter: "", cron: "b", want: "b"},
		{name: "blank does not fall back", newsletter: "  ", cron: "b", want: "  "},
		{name: "neither", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(mapLookup(map[string]string{"NEWSLETTER_CRON_SECRET": tt.newsletter, "CRON_SECRET": tt.cron}), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if cfg.NewsletterCronSecret != tt.want {
				t.Fatalf("cron secret = %q, want %q", cfg.NewsletterCronSecret, tt.want)
			}
		})
	}
}

func TestConfigFormattingRedactsNewsletterSecrets(t *testing.T) {
	secrets := []string{"resend-key-that-must-never-be-formatted", "token-secret-that-must-never-be-formatted", "cron-secret-that-must-never-be-formatted"}
	cfg := Config{Mode: ModeDevelopment, Address: DefaultAddress, DatabasePath: "/tmp/synthetic.db",
		ResendAPIKey: secrets[0], NewsletterTokenSecret: secrets[1], NewsletterCronSecret: secrets[2],
		NewsletterSiteURL: "https://site.example.invalid", NewsletterFrom: "from@example.invalid", NewsletterReplyTo: "reply@example.invalid"}
	for _, formatted := range []string{fmt.Sprint(cfg), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%#v", cfg)} {
		for _, secret := range secrets {
			if strings.Contains(formatted, secret) {
				t.Fatalf("formatted config leaked a newsletter secret: %s", formatted)
			}
		}
		for _, visible := range []string{"https://site.example.invalid", "from@example.invalid", "reply@example.invalid"} {
			if !strings.Contains(formatted, visible) {
				t.Errorf("formatted config hides non-secret %q: %s", visible, formatted)
			}
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -p 2 ./internal/config/ -run Newsletter`
Expected: FAIL to compile:

```
internal/config/newsletter_test.go:23:9: cfg.NewsletterSiteURL undefined (type Config has no field or method NewsletterSiteURL)
internal/config/newsletter_test.go:24:7: cfg.NewsletterTokenSecret undefined (type Config has no field or method NewsletterTokenSecret)
internal/config/newsletter_test.go:25:7: cfg.ResendAPIKey undefined (type Config has no field or method ResendAPIKey)
```

- [ ] **Step 3: Implement.** Add the fields, load them, and extend the redacted formatting:

```diff
diff --git a/apps/server-go/internal/config/config.go b/apps/server-go/internal/config/config.go
index ca18cbb..69baa02 100644
--- a/apps/server-go/internal/config/config.go
+++ b/apps/server-go/internal/config/config.go
@@ -95,15 +95,29 @@ type Config struct {
 	// IndexNowEnabled wires the IndexNow notifier into the publisher. When
 	// false (the default), published URLs are not submitted to IndexNow.
 	IndexNowEnabled bool
+
+	// Newsletter settings keep Node's names (apps/server/.env.example) and
+	// raw values: Node trims each one where it uses it, and so does
+	// internal/newsletter. None of them is required to start: like Node,
+	// signup works unconfigured, and the other routes answer 503 until
+	// NEWSLETTER_TOKEN_SECRET is set.
+	NewsletterSiteURL     string // NEWSLETTER_SITE_URL (default https://aiandtech.news)
+	NewsletterTokenSecret string // NEWSLETTER_TOKEN_SECRET, signs confirm/unsubscribe links (secret)
+	NewsletterCronSecret  string // NEWSLETTER_CRON_SECRET || CRON_SECRET, guards the digest (secret)
+	ResendAPIKey          string // RESEND_API_KEY (secret)
+	NewsletterFrom        string // NEWSLETTER_FROM
+	NewsletterReplyTo     string // NEWSLETTER_REPLY_TO (optional)
 }
 
 func (c Config) String() string {
 	return fmt.Sprintf("Config{Mode:%q Address:%q DatabasePath:%q UploadsDir:%q MediaStorage:%q MediaS3Prefix:%q JWTSecret:[REDACTED] CollectorEnabled:%t CollectorInterval:%s "+
 		"PublisherEnabled:%t PublisherInterval:%s GeminiAPIKey:[REDACTED] GeminiTextModel:%q GeminiImageModel:%q GeminiVisionModel:%q "+
-		"AWSRegion:%q S3Bucket:%q S3Prefix:%q S3PublicURL:%q IndexNowEnabled:%t}",
+		"AWSRegion:%q S3Bucket:%q S3Prefix:%q S3PublicURL:%q IndexNowEnabled:%t "+
+		"NewsletterSiteURL:%q NewsletterTokenSecret:[REDACTED] NewsletterCronSecret:[REDACTED] ResendAPIKey:[REDACTED] NewsletterFrom:%q NewsletterReplyTo:%q}",
 		c.Mode, c.Address, c.DatabasePath, c.UploadsDir, c.MediaStorage, c.MediaS3Prefix, c.CollectorEnabled, c.CollectorInterval,
 		c.PublisherEnabled, c.PublisherInterval, c.GeminiTextModel, c.GeminiImageModel, c.GeminiVisionModel,
-		c.AWSRegion, c.S3Bucket, c.S3Prefix, c.S3PublicURL, c.IndexNowEnabled)
+		c.AWSRegion, c.S3Bucket, c.S3Prefix, c.S3PublicURL, c.IndexNowEnabled,
+		c.NewsletterSiteURL, c.NewsletterFrom, c.NewsletterReplyTo)
 }
 
 func (c Config) GoString() string { return c.String() }
@@ -201,6 +215,18 @@ func Load(lookup func(string) string, worktreeRoot string) (Config, error) {
 	}
 	cfg.IndexNowEnabled = indexNowEnabled
 
+	cfg.NewsletterSiteURL = lookup("NEWSLETTER_SITE_URL")
+	cfg.NewsletterTokenSecret = lookup("NEWSLETTER_TOKEN_SECRET")
+	// Node: process.env.NEWSLETTER_CRON_SECRET || process.env.CRON_SECRET || ""
+	// (apps/server/src/index.ts), so only an empty value falls through.
+	cfg.NewsletterCronSecret = lookup("NEWSLETTER_CRON_SECRET")
+	if cfg.NewsletterCronSecret == "" {
+		cfg.NewsletterCronSecret = lookup("CRON_SECRET")
+	}
+	cfg.ResendAPIKey = lookup("RESEND_API_KEY")
+	cfg.NewsletterFrom = lookup("NEWSLETTER_FROM")
+	cfg.NewsletterReplyTo = lookup("NEWSLETTER_REPLY_TO")
+
 	if err := cfg.Validate(); err != nil {
 		return Config{}, err
 	}
```

- [ ] **Step 4: Run the package**

Run: `go test -p 2 ./internal/config/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/newsletter_test.go
git commit -m "Load the newsletter settings with Node's names and redact their secrets"
```

---

### Task 2: Record the Node golden vectors

Every parity test compares against values produced by the Node implementation itself. `record-node.ts` loads the real `apps/server/src/newsletter/{tokens,email,reading-time,service}.ts` and `apps/server/src/db.ts` with `tsx`, runs them on in-memory better-sqlite3 databases, stubs `fetch` (scripted Resend responses) and `setTimeout` (records delays, fires at once), and writes two files:

- `node-golden.json`: Node's final newsletter schema from `sqlite_schema`; token creation and verification vectors (including signed payloads with odd `v`/`id`/`exp` values and non-canonical base64); `readingMinutes` vectors; the welcome and digest emails for fixed inputs; the Resend sender's requests, delays, results, and errors for 37 scripted response sequences; `Date.parse`, Istanbul dates, `parseInt`, `slice`, and `JSON.stringify` vectors; four digest scenarios (selection across timestamp formats, the 20-row window, the top five with rendering, a two-run delivery lifecycle with a failure, a stuck `sending` row, and a `sent` row); signup, confirm, unsubscribe, archive, site URL, and token-secret scenarios.
- `node-case-mappings.json`: every code point whose `toUpperCase`/`toLowerCase` differs from itself, plus final-sigma cases.

Nothing touches the network or a real database. The output is deterministic (running it twice gives identical files).

**Files:** Create `internal/newsletter/testdata/record-node.ts`, `internal/newsletter/testdata/node-golden.json`, `internal/newsletter/testdata/node-case-mappings.json`

- [ ] **Step 1: Create the recorder** `internal/newsletter/testdata/record-node.ts`:

```ts
// Records the Node newsletter implementation's behavior as golden vectors for
// the Go port (internal/newsletter). Nothing here is inferred: every value in
// node-golden.json and node-case-mappings.json is produced by the real Node
// modules in apps/server/src/newsletter, better-sqlite3, and V8.
//
// No network, no listener, no real database: fetch and setTimeout are stubbed,
// databases are in-memory. Run from the repository root of a checkout whose
// apps/server dependencies are installed (NODE_SERVER_ROOT overrides the
// apps/server location, for example when running from a git worktree):
//
//   TZ=Europe/Istanbul apps/server/node_modules/.bin/tsx \
//     apps/server-go/internal/newsletter/testdata/record-node.ts \
//     apps/server-go/internal/newsletter/testdata
//
// TZ matters: V8 parses offset-less date-times (the dashboard's
// datetime-local values) in the process time zone, and the Go tests replay
// these vectors with Europe/Istanbul as the local zone.
import { createHmac } from "node:crypto";
import { writeFileSync } from "node:fs";
import path from "node:path";

type Json = null | boolean | number | string | Json[] | { [key: string]: Json };

const serverRoot = process.env.NODE_SERVER_ROOT || path.resolve(__dirname, "../../../../server");
const outputDir = process.argv[2] || __dirname;

// eslint-disable-next-line @typescript-eslint/no-require-imports
const tokens = require(path.join(serverRoot, "src/newsletter/tokens.ts"));
// eslint-disable-next-line @typescript-eslint/no-require-imports
const email = require(path.join(serverRoot, "src/newsletter/email.ts"));
// eslint-disable-next-line @typescript-eslint/no-require-imports
const readingTime = require(path.join(serverRoot, "src/newsletter/reading-time.ts"));
// eslint-disable-next-line @typescript-eslint/no-require-imports
const serviceModule = require(path.join(serverRoot, "src/newsletter/service.ts"));
// eslint-disable-next-line @typescript-eslint/no-require-imports
const dbModule = require(path.join(serverRoot, "src/db.ts"));

const TEST_SECRET = "test-newsletter-token-secret-with-32-characters";
const CONTRACT_SECRET = "synthetic-contract-newsletter-secret-000000000000";
const UNICODE_SECRET = "ünïcödé-secret-🔐-with-more-than-thirty-two-characters";

function b64url(value: string): string {
  return Buffer.from(value, "utf8").toString("base64url");
}

function signed(payloadText: string, secret: string, encode: (value: string) => string = b64url): string {
  const payload = encode(payloadText);
  return `${payload}.${createHmac("sha256", secret).update(payload).digest("base64url")}`;
}

// ---------------------------------------------------------------- tokens
function recordTokens(): Json {
  const creates: Json[] = [];
  for (const secret of [TEST_SECRET, CONTRACT_SECRET, UNICODE_SECRET]) {
    const cases: [number, string, string | null][] = [
      [1, "unsubscribe", null],
      [42, "confirm", "2026-08-09T08:01:00.000Z"],
      [501, "confirm", "2026-09-21T12:00:00.000Z"],
      [502, "unsubscribe", "2026-09-21T12:00:00.000Z"],
      [504, "unsubscribe", null],
      [7, "unsubscribe", "2026-09-21T12:00:00.999Z"],
      [9007199254740991, "unsubscribe", null],
    ];
    for (const [id, purpose, expiresAt] of cases) {
      const token = tokens.createNewsletterToken(id, purpose, secret, expiresAt ? new Date(expiresAt) : undefined);
      creates.push({ secret, id, purpose, expiresAt, token });
    }
  }

  const now = Date.parse("2026-09-20T12:00:00.000Z");
  const valid = tokens.createNewsletterToken(5, "unsubscribe", TEST_SECRET);
  const [validPayload, validSignature] = valid.split(".");
  const tampered = `${validPayload}.${validSignature.slice(0, -1)}${validSignature.endsWith("A") ? "B" : "A"}`;
  const expiring = tokens.createNewsletterToken(9, "confirm", TEST_SECRET, new Date("2026-09-20T12:00:00.000Z"));
  const standardBase64 = (value: string) => Buffer.from(value, "utf8").toString("base64");
  const verifyCases: [string, string, string, number][] = [
    ["valid", valid, "unsubscribe", now],
    ["wrong purpose", valid, "confirm", now],
    ["trailing dot", `${valid}.`, "unsubscribe", now],
    ["empty third and fourth segment", `${valid}..x`, "unsubscribe", now],
    ["non-empty third segment", `${valid}.x`, "unsubscribe", now],
    ["payload only", `${validPayload}.`, "unsubscribe", now],
    ["signature only", `.${validSignature}`, "unsubscribe", now],
    ["no dot", validPayload, "unsubscribe", now],
    ["empty", "", "unsubscribe", now],
    ["tampered signature", tampered, "unsubscribe", now],
    ["signature with padding", `${valid}=`, "unsubscribe", now],
    ["other secret", tokens.createNewsletterToken(5, "unsubscribe", CONTRACT_SECRET), "unsubscribe", now],
    ["expires this second", expiring, "confirm", now],
    ["expires this second, 999ms later", expiring, "confirm", now + 999],
    ["expired one second ago", expiring, "confirm", now + 1000],
    ["v 2", signed('{"v":2,"id":5,"purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["v string", signed('{"v":"1","id":5,"purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["v 1.0", signed('{"v":1.0,"id":5,"purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["id 0", signed('{"v":1,"id":0,"purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["id -1", signed('{"v":1,"id":-1,"purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["id 1.5", signed('{"v":1,"id":1.5,"purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["id 5.0", signed('{"v":1,"id":5.0,"purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["id 1e2", signed('{"v":1,"id":1e2,"purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["id string", signed('{"v":1,"id":"5","purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["id null", signed('{"v":1,"id":null,"purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["id missing", signed('{"v":1,"purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["duplicate id keeps last", signed('{"v":1,"id":5,"id":6,"purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["extra fields", signed('{"v":1,"id":5,"purpose":"unsubscribe","x":[1,{"y":null}]}', TEST_SECRET), "unsubscribe", now],
    ["exp null", signed('{"v":1,"id":5,"purpose":"unsubscribe","exp":null}', TEST_SECRET), "unsubscribe", now],
    ["exp string", signed('{"v":1,"id":5,"purpose":"unsubscribe","exp":"9999999999"}', TEST_SECRET), "unsubscribe", now],
    ["exp 1e400", signed('{"v":1,"id":5,"purpose":"unsubscribe","exp":1e400}', TEST_SECRET), "unsubscribe", now],
    ["exp fractional future", signed('{"v":1,"id":5,"purpose":"unsubscribe","exp":1789905600.5}', TEST_SECRET), "unsubscribe", now],
    ["exp fractional past", signed('{"v":1,"id":5,"purpose":"unsubscribe","exp":1789905599.5}', TEST_SECRET), "unsubscribe", now],
    ["not json", signed("not json", TEST_SECRET), "unsubscribe", now],
    ["json array", signed("[1]", TEST_SECRET), "unsubscribe", now],
    ["json null", signed("null", TEST_SECRET), "unsubscribe", now],
    ["json whitespace", signed(' \n{"v":1,"id":5,"purpose":"unsubscribe"}\t', TEST_SECRET), "unsubscribe", now],
    ["trailing json", signed('{"v":1,"id":5,"purpose":"unsubscribe"}{}', TEST_SECRET), "unsubscribe", now],
    ["standard base64 payload", signed('{"v":1,"id":5,"purpose":"unsubscribe","pad":"??>"}', TEST_SECRET, standardBase64), "unsubscribe", now],
    ["unicode secret", tokens.createNewsletterToken(5, "unsubscribe", UNICODE_SECRET), "unsubscribe", now],
  ];
  const verifies = verifyCases.map(([name, token, purpose, at]) => ({
    name,
    token,
    purpose,
    secret: name === "unicode secret" ? UNICODE_SECRET : TEST_SECRET,
    now: new Date(at).toISOString(),
    result: tokens.verifyNewsletterToken(token, purpose, name === "unicode secret" ? UNICODE_SECRET : TEST_SECRET, new Date(at)),
  }));
  return { creates, verifies };
}

// ---------------------------------------------------------------- reading time
function words(count: number, word = "word"): string {
  return Array.from({ length: count }, () => word).join(" ");
}

function recordReadingMinutes(): Json {
  const inputs: (string | null)[] = [
    null,
    "",
    "   ",
    "<p></p>",
    "word",
    words(109),
    words(110),
    words(329),
    words(330),
    words(440),
    `<p>${words(440)}</p>`,
    words(550),
    words(549),
    words(1000),
    `<p>${words(300)}</p><script>${words(300)}</script>`,
    `<SCRIPT type="x">${words(300)}</script>${words(300)}`,
    `<script>${words(300)}</style>${words(300)}</script>${words(30)}`,
    `<script>${words(300)}`,
    `<scriptx>${words(300)}</scriptx>`,
    `<style media="a>b">${words(300)}</STYLE>${words(10)}`,
    `<style>a</style><style>${words(400)}</style>b`,
    `<script>\n${words(300)}\n</script >${words(300)}`,
    "a&nbsp;b&NBSP;c",
    "a&amp;b &#8217; c &#x2019; d &Eacute;e",
    "a b c​d　e﻿",
    "<p>a</p><p>b</p>",
    "<a\nhref='x'>link</a> text",
    "<p>" + words(219) + "</p>",
    "  ",
    "&nbsp;",
    "<br/>",
    "a <!-- comment --> b",
    "1 < 2 and 3 > 2",
  ];
  return inputs.map((input) => ({ input, minutes: readingTime.readingMinutes(input) }));
}

// ---------------------------------------------------------------- emails
const siteUrl = "https://aiandtech.news";
const unsubscribeToken = tokens.createNewsletterToken(504, "unsubscribe", TEST_SECRET);
const unsubscribeUrl = `${siteUrl}/api/newsletter/unsubscribe?token=${encodeURIComponent(unsubscribeToken)}`;

const digestCases: { name: string; articles: Json[] }[] = [
  {
    name: "single",
    articles: [{ title: "A useful AI update", slug: "useful-ai-update", excerpt: "What changed and why it matters.", category: "AI", readingMinutes: 2 }],
  },
  {
    name: "one minute",
    articles: [{ title: "Short", slug: "short", excerpt: "Brief.", category: "AI", readingMinutes: 1 }],
  },
  {
    name: "grouped",
    articles: [
      { title: "First AI story", slug: "first-ai", excerpt: "One.", category: "AI", readingMinutes: 3 },
      { title: "A Go release", slug: "go-release", excerpt: "Two.", category: "Programming", readingMinutes: 1 },
      { title: "Second AI story", slug: "second-ai", excerpt: "Three.", category: "AI", readingMinutes: 4 },
      { title: "Funding round", slug: "funding", excerpt: "Four.", category: "Startups", readingMinutes: 2 },
      { title: "Another release", slug: "another", excerpt: "Five.", category: "Programming", readingMinutes: 5 },
    ],
  },
  {
    name: "escaping",
    articles: [
      {
        title: `<b>"Quotes" & 'apostrophes'</b>`,
        slug: "çok güzel/slug?x=1&y=ü 🚀(it's)*~!",
        excerpt: "<script>alert('x')</script> & more — em dash",
        category: "R&D <Labs> \"quoted\"",
        readingMinutes: 12,
      },
    ],
  },
  {
    name: "unicode categories",
    articles: [
      { title: "Straße", slug: "strasse", excerpt: "ß", category: "straße ﬁnance", readingMinutes: 1 },
      { title: "İstanbul", slug: "istanbul", excerpt: "i", category: "yapay zekâ ıi", readingMinutes: 1 },
    ],
  },
  { name: "empty", articles: [] },
];

function recordEmails(): Json {
  return {
    welcome: [
      { to: "reader@example.com", siteUrl, unsubscribeUrl, email: email.welcomeEmail("reader@example.com", siteUrl, unsubscribeUrl) },
      {
        to: "o'brien+news@example.com",
        siteUrl: "http://localhost:3000",
        unsubscribeUrl: "http://localhost:3000/api/newsletter/unsubscribe?token=a.b&x=<y>\"'",
        email: email.welcomeEmail("o'brien+news@example.com", "http://localhost:3000", "http://localhost:3000/api/newsletter/unsubscribe?token=a.b&x=<y>\"'"),
      },
    ],
    digests: digestCases.map(({ name, articles }) => ({
      name,
      to: "reader@example.com",
      siteUrl,
      unsubscribeUrl,
      articles,
      email: email.digestEmail("reader@example.com", articles, siteUrl, unsubscribeUrl),
    })),
    escapeHtml: ["", "plain", `&<>"'`, "a&amp;b", "üñí 🚀"].map((input) => ({ input, output: email.escapeHtml(input) })),
    encodeURIComponent: ["", "abc-_.!~*'()", "a b", "ç/?#&=+", "🚀", " ", "a%20b"].map((input) => ({
      input,
      output: encodeURIComponent(input),
    })),
  };
}

// ---------------------------------------------------------------- Resend sender
interface RecordedCall {
  url: string;
  method: string;
  headers: Record<string, string>;
  body: string;
}

interface ScriptedResponse {
  status: number;
  body?: string;
  retryAfter?: string;
  throws?: string;
}

async function runSender(
  environment: Record<string, string>,
  scripted: ScriptedResponse[],
  message: Json,
  idempotencyKey: string,
): Promise<Json> {
  const calls: RecordedCall[] = [];
  const delays: number[] = [];
  const originalFetch = globalThis.fetch;
  const originalSetTimeout = globalThis.setTimeout;
  let index = 0;
  globalThis.fetch = (async (url: string, init: RequestInit) => {
    const headers: Record<string, string> = {};
    for (const [name, value] of Object.entries(init.headers as Record<string, string>)) headers[name] = value;
    calls.push({ url: String(url), method: String(init.method), headers, body: String(init.body) });
    const next = scripted[Math.min(index, scripted.length - 1)];
    index += 1;
    if (next.throws) throw new TypeError(next.throws);
    const responseHeaders: Record<string, string> = {};
    if (next.retryAfter !== undefined) responseHeaders["retry-after"] = next.retryAfter;
    return new Response(next.body ?? "", { status: next.status, headers: responseHeaders });
  }) as typeof fetch;
  globalThis.setTimeout = ((callback: () => void, delay: number) => {
    delays.push(delay);
    callback();
    return 0;
  }) as unknown as typeof setTimeout;
  try {
    const send = email.createResendSender(environment);
    const result = await send(message, idempotencyKey);
    return { calls: calls as unknown as Json, delays, result };
  } catch (error) {
    const err = error as Error;
    return {
      calls: calls as unknown as Json,
      delays,
      error: {
        name: err.constructor.name,
        configuration: err instanceof email.NewsletterConfigurationError,
        message: err.message,
      },
    };
  } finally {
    globalThis.fetch = originalFetch;
    globalThis.setTimeout = originalSetTimeout;
  }
}

async function recordSender(): Promise<Json> {
  const configured = {
    RESEND_API_KEY: " re_synthetic_key ",
    NEWSLETTER_FROM: " AI & Tech News <news@example.invalid> ",
    NEWSLETTER_REPLY_TO: " reply@example.invalid ",
  };
  const noReplyTo = { RESEND_API_KEY: "re_synthetic_key", NEWSLETTER_FROM: "News <news@example.invalid>" };
  const welcome = email.welcomeEmail("reader@example.com", siteUrl, unsubscribeUrl);
  const digest = email.digestEmail("reader@example.com", digestCases[3].articles, siteUrl, unsubscribeUrl);
  const bare = { to: "bare@example.com", subject: "Bare", html: "<p>x</p>", text: "x" };
  const ok = { status: 200, body: '{"id":"msg_1"}' };
  const cases: { name: string; env: Record<string, string>; scripted: ScriptedResponse[]; email: Json }[] = [
    { name: "success welcome", env: configured, scripted: [ok], email: welcome },
    { name: "success digest", env: configured, scripted: [ok], email: digest },
    { name: "no reply-to, no headers or tags", env: noReplyTo, scripted: [ok], email: bare },
    { name: "blank reply-to", env: { ...noReplyTo, NEWSLETTER_REPLY_TO: "   " }, scripted: [ok], email: bare },
    { name: "201 with id", env: noReplyTo, scripted: [{ status: 201, body: '{"id":"msg_2"}' }], email: bare },
    { name: "500 then success", env: noReplyTo, scripted: [{ status: 500, body: '{"message":"boom"}' }, ok], email: bare },
    {
      name: "429 retry-after then 503 then success",
      env: noReplyTo,
      scripted: [{ status: 429, retryAfter: "2", body: "{}" }, { status: 503, body: "oops" }, ok],
      email: bare,
    },
    { name: "500 three times", env: noReplyTo, scripted: [{ status: 500, body: '{"message":"boom"}' }], email: bare },
    { name: "502 three times without body", env: noReplyTo, scripted: [{ status: 502 }], email: bare },
    { name: "400 message", env: noReplyTo, scripted: [{ status: 400, body: '{"message":"Invalid from"}' }], email: bare },
    { name: "422 error field", env: noReplyTo, scripted: [{ status: 422, body: '{"error":"bad"}' }], email: bare },
    { name: "409 idempotency conflict", env: noReplyTo, scripted: [{ status: 409, body: '{"statusCode":409,"name":"invalid_idempotent_request","message":"Same idempotency key used with a different request payload."}' }], email: bare },
    { name: "401 not json", env: noReplyTo, scripted: [{ status: 401, body: "<html>no</html>" }], email: bare },
    { name: "200 without id", env: noReplyTo, scripted: [{ status: 200, body: "{}" }], email: bare },
    { name: "200 empty id", env: noReplyTo, scripted: [{ status: 200, body: '{"id":""}' }], email: bare },
    { name: "200 not json", env: noReplyTo, scripted: [{ status: 200, body: "ok" }], email: bare },
    { name: "message number", env: noReplyTo, scripted: [{ status: 400, body: '{"message":5}' }], email: bare },
    { name: "empty message falls back to error", env: noReplyTo, scripted: [{ status: 400, body: '{"message":"","error":"e"}' }], email: bare },
    { name: "error object", env: noReplyTo, scripted: [{ status: 400, body: '{"error":{"a":1}}' }], email: bare },
    { name: "json array body", env: noReplyTo, scripted: [{ status: 400, body: "[1]" }], email: bare },
    { name: "json string body", env: noReplyTo, scripted: [{ status: 400, body: '"text"' }], email: bare },
    { name: "json null body", env: noReplyTo, scripted: [{ status: 500, body: "null" }], email: bare },
    { name: "network error", env: noReplyTo, scripted: [{ status: 0, throws: "fetch failed" }], email: bare },
    { name: "missing key", env: { NEWSLETTER_FROM: "News <news@example.invalid>" }, scripted: [ok], email: bare },
    { name: "blank key", env: { ...noReplyTo, RESEND_API_KEY: "  " }, scripted: [ok], email: bare },
    { name: "missing from", env: { RESEND_API_KEY: "re_synthetic_key" }, scripted: [ok], email: bare },
  ];
  for (const retryAfter of ["0", "1.5", "abc", " 3 ", "0x2", "1e1", "-1", "Infinity", "", "Wed, 21 Oct 2026 07:28:00 GMT", "2 "]) {
    cases.push({
      name: `retry-after ${JSON.stringify(retryAfter)}`,
      env: noReplyTo,
      scripted: [{ status: 503, retryAfter, body: "{}" }, ok],
      email: bare,
    });
  }
  const results: Json[] = [];
  for (const testCase of cases) {
    results.push({
      name: testCase.name,
      env: testCase.env,
      scripted: testCase.scripted as unknown as Json,
      email: testCase.email,
      idempotencyKey: "newsletter-digest-2026-09-20-504",
      ...(await runSender(testCase.env, testCase.scripted, testCase.email, "newsletter-digest-2026-09-20-504") as Record<string, Json>),
    });
  }
  return results;
}

// ---------------------------------------------------------------- JavaScript primitives
function recordPrimitives(): Json {
  const dateInputs = [
    "2026-09-19 12:00:00",
    "2026-09-19T12:00:00.000Z",
    "2026-09-19T12:00:00Z",
    "2026-09-19T12:00:00",
    "2026-09-19T12:00",
    "2026-09-19T12:00:00.5",
    "2026-09-19T12:00:00.123456Z",
    "2026-09-19T12:00:00+03:00",
    "2026-09-19T12:00:00.000+05:30",
    "2026-09-19T12:00:00-0100",
    "2026-09-19t12:00:00z",
    "2026-09-19",
    "2026-09",
    "2026",
    "+002026-09-19T12:00:00.000Z",
    "2026-09-19 12:00",
    "2026-09-19 12:00:00.000",
    "2026-09-19 12:00:00Z",
    "2026-09-19T24:00:00Z",
    "2026-09-19T24:00:01Z",
    "2026-02-30T12:00:00Z",
    "2026-13-01T12:00:00Z",
    "2026-09-19T12:60:00Z",
    "2026-9-19",
    " 2026-09-19T12:00:00Z",
    "2026-09-19T12:00:00Z ",
    "Sat, 19 Sep 2026 12:00:00 GMT",
    "September 19, 2026",
    "19 Sep 2026 12:00",
    "not a date",
    "",
    "2026-09-00",
    "2026-00-10",
    "2026-09-19T12",
    "2026-09-19T1:00",
    "2026-09-19T12:00:00.",
    "2026-09-19T12:00+03",
    "2026-09-19T12:00:00+0300",
    "2026-09-19T12:00:00 +03:00",
    "2026-09-31T00:00:00Z",
    "-000001-01-01T00:00:00Z",
    "10000-01-01",
    "2026-09-19T12:00:00.000+25:00",
    "2026-09-19T12:00:00.000+23:59",
    "2026-09-19T12:00:00.000Z+",
    "2026-09-19T12:00:00.1234567890123Z",
    "2026-09-19T12:00:00,5Z",
    "2026-09-19 12:00:00+03:00",
    "2026-09-19T12:00:60Z",
    "2026-09-19T23:59:59.999Z",
    "2026-09-19T24:00",
    "2026-09-19T24:00:00.000Z",
    "2026-09-19T24:00:00.001Z",
    "20260919",
    "2026-09-19Z",
    "2026-09-19TZ",
    "2026-09-19T12:00:00Z0",
    "2026-09-19T12:00:00.000z",
    "2026-09-19t12:00",
  ];
  const datesParsed = dateInputs.map((input) => {
    const normalized = /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(input) ? `${input.replace(" ", "T")}Z` : input;
    const value = Date.parse(normalized);
    return { input, ms: Number.isFinite(value) ? value : null };
  });

  const instants = [
    "2026-09-19T20:59:59.999Z",
    "2026-09-19T21:00:00.000Z",
    "2026-09-20T05:00:00.000Z",
    "2026-12-31T21:00:00.000Z",
    "2015-03-29T00:30:00.000Z",
    "2015-10-25T01:30:00.000Z",
    "2016-09-07T20:59:59.000Z",
    "2016-09-07T21:00:00.000Z",
  ];
  const istanbulDates = instants.map((instant) => {
    const parts = new Intl.DateTimeFormat("en-CA", { timeZone: "Europe/Istanbul", year: "numeric", month: "2-digit", day: "2-digit" }).formatToParts(new Date(instant));
    const get = (type: string) => parts.find((part) => part.type === type)?.value || "";
    return { instant, edition: `${get("year")}-${get("month")}-${get("day")}` };
  });

  const parseIntInputs = ["2", " 7", "7abc", "-3", "0", "", "abc", "1e3", "0x10", "+5", "  +5", "99999999999999999999", "12.9", " 5", "﻿8", "- 5", "--5", "30,40", "[object Object]", "Infinity"];
  const parseInts = parseIntInputs.map((input) => {
    const value = parseInt(input, 10);
    return { input, value: Number.isFinite(value) ? value : null };
  });

  const emailInputs = [
    " Reader@Example.com ",
    "a@b.c",
    "a@b",
    "@b.c",
    "a@.c",
    "a@b.",
    "a@b..c",
    "a b@c.d",
    "a@b.c\n",
    " a@b.c ",
    "a@b.c@d.e",
    "İSTANBUL@ÖRNEK.COM",
    "ΑΣ@ΣΑΣ.GR",
    "ΣΑ.Σ@X.GR",
    "a@b c.d",
    "a@b​c.d",
    `${"a".repeat(250)}@b.c`,
    `${"a".repeat(249)}@b.c`,
    `${"🚀".repeat(125)}@b.c`,
    `${"🚀".repeat(124)}@b.c`,
    "",
    "   ",
    "STRASSE@ß.DE",
  ];

  const truncations = ["", "short", "x".repeat(81), `${"x".repeat(79)}🚀tail`, `${"x".repeat(78)}🚀tail`].map((input) => ({
    input,
    slice80: input.slice(0, 80),
  }));

  const stringifyInputs = ["", "plain", 'a"b\\c/d', "\b\f\n\r\t\v", "\u0000\u0001\u001f\u007f\u0080", "\u2028\u2029", "<>&'", "é🚀\ufeff"];
  const stringified = stringifyInputs.map((input) => ({ input, output: JSON.stringify(input) }));

  return { datesParsed, istanbulDates, parseInts, emailInputs, truncations, stringified };
}

// Every code point whose JavaScript toUpperCase/toLowerCase differs from itself.
function recordCaseMappings(): Json {
  const upper: [number, string][] = [];
  const lower: [number, string][] = [];
  for (let codePoint = 0; codePoint <= 0x10ffff; codePoint += 1) {
    if (codePoint >= 0xd800 && codePoint <= 0xdfff) continue;
    const value = String.fromCodePoint(codePoint);
    const upperValue = value.toUpperCase();
    const lowerValue = value.toLowerCase();
    if (upperValue !== value) upper.push([codePoint, upperValue]);
    if (lowerValue !== value) lower.push([codePoint, lowerValue]);
  }
  const contextual = ["ΑΣ", "Σ", "ΑΣ Α", "ΑΣΑ", "Α'Σ", "ΑΣ'", "ΑΣ.Β", "1Σ", "ΑΣ1", "ΆΣ", "ΑΣ́", "ΑΣ́Α"].map((input) => ({
    input,
    lower: input.toLowerCase(),
  }));
  return { unicodeVersion: process.versions.unicode, upper, lower, contextual };
}

// ---------------------------------------------------------------- service scenarios
function openDatabase() {
  const db = dbModule.openDatabase(":memory:");
  dbModule.initializeDatabase(db, { seedDefaults: false });
  return db;
}

function stubTimers() {
  const delays: number[] = [];
  const original = globalThis.setTimeout;
  globalThis.setTimeout = ((callback: () => void, delay: number) => {
    delays.push(delay);
    callback();
    return 0;
  }) as unknown as typeof setTimeout;
  return { delays, restore: () => { globalThis.setTimeout = original; } };
}

function rows(db: any, sql: string): Json {
  return db.prepare(sql).all() as Json;
}

function schemaOf(db: any): Json {
  return db
    .prepare("SELECT type, name, tbl_name, sql FROM sqlite_schema WHERE name IN ('subscribers','newsletter_deliveries','newsletter_editions','idx_subscribers_status','idx_newsletter_deliveries_edition') ORDER BY name")
    .all() as Json;
}

async function recordDigestSelection(): Promise<Json> {
  const db = openDatabase();
  db.prepare("INSERT INTO categories (id, name, slug) VALUES (1, 'AI', 'ai'), (2, 'Programming', 'programming')").run();
  db.prepare("INSERT INTO authors (id, name, email, password_hash) VALUES (1, 'A', 'a@example.invalid', 'x')").run();
  const now = new Date("2026-09-20T05:00:00.000Z");
  // [slug, status, published_at, category, words]
  const articles: [string, string, string | null, number, number][] = [
    ["future-iso", "published", "2026-09-20T05:00:00.001Z", 1, 10],
    ["exactly-now", "published", "2026-09-20T05:00:00.000Z", 1, 220],
    ["sqlite-recent", "published", "2026-09-20 04:00:00", 2, 330],
    ["local-datetime", "published", "2026-09-20T07:30", 1, 500],
    ["cutoff-exact", "published", "2026-09-18T23:00:00.000Z", 2, 1],
    ["cutoff-minus-1ms", "published", "2026-09-18T22:59:59.999Z", 1, 1],
    ["draft-recent", "draft", "2026-09-20T04:00:00.000Z", 1, 1],
    ["scheduled-recent", "scheduled", "2026-09-20T04:00:00.000Z", 1, 1],
    ["null-date", "published", null, 1, 1],
    ["garbage-date", "published", "zzz", 1, 1],
    ["sqlite-old", "published", "2026-09-18 01:00:00", 1, 1],
    ["offset", "published", "2026-09-20T06:00:00+03:00", 2, 900],
    ["date-only", "published", "2026-09-20", 1, 50],
  ];
  const insert = db.prepare(`INSERT INTO articles (title, slug, excerpt, content, category_id, author_id, status, published_at)
    VALUES (?, ?, ?, ?, ?, 1, ?, ?)`);
  for (const [slug, status, publishedAt, category, count] of articles) {
    insert.run(`Title ${slug}`, slug, `Excerpt ${slug}`, `<p>${words(count)}</p>`, category, status, publishedAt);
  }
  db.prepare("INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (1, 'one@example.com', 'active', 'x', 'x')").run();
  const sent: Json[] = [];
  const service = new serviceModule.NewsletterService(
    db,
    { NEWSLETTER_SITE_URL: "https://aiandtech.news", NEWSLETTER_TOKEN_SECRET: TEST_SECRET },
    async (message: any, idempotencyKey: string) => {
      sent.push({ to: message.to, subject: message.subject, idempotencyKey, html: message.html, text: message.text, headers: message.headers, tags: message.tags });
      return { id: `message-${sent.length}` };
    },
  );
  const timers = stubTimers();
  try {
    const result = await service.sendDailyDigest(now);
    return {
      now: now.toISOString(),
      articles: articles as unknown as Json,
      result,
      editions: rows(db, "SELECT id, edition_key, subject, articles, created_at FROM newsletter_editions"),
      deliveries: rows(db, "SELECT id, subscriber_id, edition_key, status, provider_message_id, error, created_at, sent_at FROM newsletter_deliveries"),
      sent,
      delays: timers.delays,
    };
  } finally {
    timers.restore();
    db.close();
  }
}

async function recordDigestWindow(): Promise<Json> {
  // Twenty published articles whose published_at text sorts above the recent
  // ones push the recent ones out of the SQL LIMIT 20 window.
  const db = openDatabase();
  db.prepare("INSERT INTO categories (id, name, slug) VALUES (1, 'AI', 'ai')").run();
  db.prepare("INSERT INTO authors (id, name, email, password_hash) VALUES (1, 'A', 'a@example.invalid', 'x')").run();
  const insert = db.prepare(`INSERT INTO articles (title, slug, excerpt, content, category_id, author_id, status, published_at)
    VALUES (?, ?, ?, '', 1, 1, 'published', ?)`);
  for (let index = 0; index < 20; index += 1) insert.run(`Future ${index}`, `future-${index}`, "x", `2027-01-${String(index + 1).padStart(2, "0")}T00:00:00.000Z`);
  insert.run("Recent", "recent", "x", "2026-09-20T04:00:00.000Z");
  const service = new serviceModule.NewsletterService(db, { NEWSLETTER_SITE_URL: "https://aiandtech.news", NEWSLETTER_TOKEN_SECRET: TEST_SECRET }, async () => ({ id: "unused" }));
  try {
    const result = await service.sendDailyDigest(new Date("2026-09-20T05:00:00.000Z"));
    return { result, editions: rows(db, "SELECT edition_key FROM newsletter_editions") };
  } finally {
    db.close();
  }
}

async function recordDigestTopFive(): Promise<Json> {
  const db = openDatabase();
  db.prepare("INSERT INTO categories (id, name, slug) VALUES (1, 'AI', 'ai'), (2, 'Code', 'code')").run();
  db.prepare("INSERT INTO authors (id, name, email, password_hash) VALUES (1, 'A', 'a@example.invalid', 'x')").run();
  const insert = db.prepare(`INSERT INTO articles (title, slug, excerpt, content, category_id, author_id, status, published_at)
    VALUES (?, ?, ?, ?, ?, 1, 'published', ?)`);
  const stamps = [
    "2026-09-20 04:59:00",
    "2026-09-20T04:00:00.000Z",
    "2026-09-20 03:00:00",
    "2026-09-20T02:00:00.000Z",
    "2026-09-20 01:00:00",
    "2026-09-20T00:30:00.000Z",
    "2026-09-19 23:00:00",
  ];
  stamps.forEach((stamp, index) => insert.run(`Story ${index}`, `story-${index}`, `Excerpt ${index}`, `<p>${words(index * 150)}</p>`, (index % 2) + 1, stamp));
  const sent: Json[] = [];
  const service = new serviceModule.NewsletterService(
    db,
    { NEWSLETTER_SITE_URL: "https://aiandtech.news/some/path?q=1", NEWSLETTER_TOKEN_SECRET: TEST_SECRET },
    async (message: any, idempotencyKey: string) => {
      sent.push({ to: message.to, subject: message.subject, idempotencyKey, html: message.html, text: message.text });
      return { id: `message-${sent.length}` };
    },
  );
  db.prepare("INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (3, 'three@example.com', 'active', 'x', 'x')").run();
  const timers = stubTimers();
  try {
    const result = await service.sendDailyDigest(new Date("2026-09-20T05:00:00.000Z"));
    return { stamps, result, editions: rows(db, "SELECT edition_key, subject, articles, created_at FROM newsletter_editions"), sent };
  } finally {
    timers.restore();
    db.close();
  }
}

async function recordDeliveryLifecycle(): Promise<Json> {
  const db = openDatabase();
  db.prepare("INSERT INTO categories (id, name, slug) VALUES (1, 'AI', 'ai')").run();
  db.prepare("INSERT INTO authors (id, name, email, password_hash) VALUES (1, 'A', 'a@example.invalid', 'x')").run();
  db.prepare(`INSERT INTO articles (title, slug, excerpt, content, category_id, author_id, status, published_at)
    VALUES ('Lead story', 'lead', 'Lead excerpt', '<p>lead</p>', 1, 1, 'published', '2026-09-20 04:00:00')`).run();
  const subscribers: [number, string, string][] = [
    [1, "one@example.com", "active"],
    [2, "pending@example.com", "pending"],
    [3, "fails@example.com", "active"],
    [4, "gone@example.com", "unsubscribed"],
    [5, "stuck@example.com", "active"],
    [6, "five@example.com", "active"],
  ];
  const insertSubscriber = db.prepare("INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (?, ?, ?, 'x', 'x')");
  for (const subscriber of subscribers) insertSubscriber.run(...subscriber);
  // A delivery left 'sending' by a crashed run, and one already sent.
  db.prepare(`INSERT INTO newsletter_deliveries (subscriber_id, edition_key, status, provider_message_id, error, created_at)
    VALUES (5, '2026-09-20', 'sending', 'old-id', 'old error', '2026-09-20T04:59:00.000Z')`).run();
  db.prepare(`INSERT INTO newsletter_deliveries (subscriber_id, edition_key, status, provider_message_id, created_at, sent_at)
    VALUES (6, '2026-09-20', 'sent', 'sent-id', '2026-09-20T04:59:00.000Z', '2026-09-20T04:59:30.000Z')`).run();
  let failFor: string | null = "fails@example.com";
  const sent: Json[] = [];
  const service = new serviceModule.NewsletterService(
    db,
    { NEWSLETTER_SITE_URL: "https://aiandtech.news", NEWSLETTER_TOKEN_SECRET: TEST_SECRET },
    async (message: any, idempotencyKey: string) => {
      sent.push({ to: message.to, idempotencyKey });
      if (message.to === failFor) throw new Error(`${"é".repeat(499)}🚀 provider rejected`);
      return { id: `message-${sent.length}` };
    },
  );
  const timers = stubTimers();
  try {
    const first = await service.sendDailyDigest(new Date("2026-09-20T05:00:00.000Z"));
    const afterFirst = rows(db, "SELECT subscriber_id, edition_key, status, provider_message_id, error, created_at, sent_at FROM newsletter_deliveries ORDER BY subscriber_id");
    const firstDelays = [...timers.delays];
    timers.delays.length = 0;
    failFor = null;
    const second = await service.sendDailyDigest(new Date("2026-09-20T05:10:00.000Z"));
    const afterSecond = rows(db, "SELECT subscriber_id, edition_key, status, provider_message_id, error, created_at, sent_at FROM newsletter_deliveries ORDER BY subscriber_id");
    return {
      subscribers: subscribers as unknown as Json,
      first,
      afterFirst,
      firstDelays,
      second,
      afterSecond,
      secondDelays: timers.delays,
      sent,
      editions: rows(db, "SELECT edition_key, subject, articles, created_at FROM newsletter_editions"),
    };
  } finally {
    timers.restore();
    db.close();
  }
}

async function recordSubscriptions(): Promise<Json> {
  const db = openDatabase();
  const service = new serviceModule.NewsletterService(db, { NEWSLETTER_SITE_URL: "https://aiandtech.news" }, async () => {
    throw new Error("signup must not send");
  });
  const now = new Date("2026-08-09T08:00:00.000Z");
  const later = new Date("2026-08-09T09:00:00.000Z");
  db.prepare("INSERT INTO subscribers (id, email, status, confirmed_at, created_at, updated_at) VALUES (10, 'pending@example.com', 'pending', NULL, 'c', 'u')").run();
  db.prepare("INSERT INTO subscribers (id, email, status, source_placement, confirmed_at, unsubscribed_at, created_at, updated_at) VALUES (11, 'gone@example.com', 'unsubscribed', 'old', '2026-01-01T00:00:00.000Z', '2026-02-01T00:00:00.000Z', 'c', 'u')").run();
  db.prepare("INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (12, 'MixedCase@Example.com', 'active', 'c', 'u')").run();
  const steps: [string, string, Date][] = [
    [" Reader@Example.com ", "inline", now],
    ["reader@example.com", "header", later],
    ["PENDING@example.com", "footer", later],
    ["gone@example.com", "x".repeat(79) + "🚀tail", later],
    ["mixedcase@example.com", "archive_index", later],
    ["not-an-email", "inline", later],
  ];
  const outcomes: Json[] = [];
  for (const [address, placement, at] of steps) {
    try {
      outcomes.push({ address, placement, at: at.toISOString(), result: await service.requestSubscription(address, placement, at) });
    } catch (error) {
      outcomes.push({ address, placement, at: at.toISOString(), error: { name: (error as Error).constructor.name, message: (error as Error).message } });
    }
  }
  const normalized: Json[] = [];
  const probe = openDatabase();
  const probeService = new serviceModule.NewsletterService(probe, { NEWSLETTER_SITE_URL: "https://aiandtech.news" }, async () => ({ id: "x" }));
  for (const input of (recordPrimitives() as any).emailInputs as string[]) {
    try {
      await probeService.requestSubscription(input, "probe", now);
      const stored = probe.prepare("SELECT email FROM subscribers ORDER BY id DESC LIMIT 1").get() as { email: string };
      normalized.push({ input, email: stored.email });
      probe.prepare("DELETE FROM subscribers").run();
    } catch (error) {
      normalized.push({ input, error: (error as Error).message });
    }
  }
  probe.close();
  try {
    return {
      outcomes,
      subscribers: rows(db, "SELECT id, email, status, source_placement, confirmation_sent_at, confirmed_at, unsubscribed_at, created_at, updated_at FROM subscribers ORDER BY id"),
      normalized,
    };
  } finally {
    db.close();
  }
}

async function recordConfirmAndUnsubscribe(): Promise<Json> {
  const db = openDatabase();
  const insert = db.prepare("INSERT INTO subscribers (id, email, status, confirmed_at, unsubscribed_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'c', 'u')");
  insert.run(1, "pending@example.com", "pending", null, null);
  insert.run(2, "active@example.com", "active", "2026-01-01T00:00:00.000Z", null);
  insert.run(3, "gone@example.com", "unsubscribed", "2026-01-01T00:00:00.000Z", "2026-02-01T00:00:00.000Z");
  insert.run(4, "pending-fail@example.com", "pending", null, null);
  const sent: Json[] = [];
  const service = new serviceModule.NewsletterService(
    db,
    { NEWSLETTER_SITE_URL: "https://aiandtech.news", NEWSLETTER_TOKEN_SECRET: TEST_SECRET },
    async (message: any, idempotencyKey: string) => {
      sent.push({ ...message, idempotencyKey });
      if (message.to === "pending-fail@example.com") throw new Error("provider down");
      return { id: "welcome-id" };
    },
  );
  const now = new Date("2026-09-20T12:00:00.000Z");
  const expiry = new Date("2026-09-21T12:00:00.000Z");
  const confirm = (id: number) => tokens.createNewsletterToken(id, "confirm", TEST_SECRET, expiry);
  const unsubscribe = (id: number) => tokens.createNewsletterToken(id, "unsubscribe", TEST_SECRET);
  const originalError = console.error;
  console.error = () => {};
  try {
    const confirms: Json[] = [];
    for (const [name, token] of [
      ["pending", confirm(1)],
      ["pending again", confirm(1)],
      ["active", confirm(2)],
      ["unsubscribed", confirm(3)],
      ["unknown", confirm(99)],
      ["unsubscribe token", unsubscribe(1)],
      ["garbage", "garbage"],
      ["send fails", confirm(4)],
    ] as [string, string][]) {
      confirms.push({ name, token, result: await service.confirmSubscription(token, now) });
    }
    const unsubscribes: Json[] = [];
    for (const [name, token] of [
      ["active", unsubscribe(2)],
      ["again", unsubscribe(2)],
      ["pending", unsubscribe(4)],
      ["unknown", unsubscribe(99)],
      ["confirm token", confirm(1)],
      ["garbage", "a.b"],
    ] as [string, string][]) {
      unsubscribes.push({ name, token, result: service.unsubscribe(token, now) });
    }
    return {
      now: now.toISOString(),
      confirms,
      unsubscribes,
      sent,
      subscribers: rows(db, "SELECT id, email, status, source_placement, confirmation_sent_at, confirmed_at, unsubscribed_at, created_at, updated_at FROM subscribers ORDER BY id"),
    };
  } finally {
    console.error = originalError;
    db.close();
  }
}

function recordEditions(): Json {
  const db = openDatabase();
  const insert = db.prepare("INSERT INTO newsletter_editions (edition_key, subject, articles, created_at) VALUES (?, ?, ?, ?)");
  insert.run("2026-09-17", "Object", '{"not":"array"}', "2026-09-17T05:00:00.000Z");
  insert.run("2026-09-18", "Malformed", "{malformed", "2026-09-18T05:00:00.000Z");
  insert.run("2026-09-19", "Loose", ' [ {"title":"T","extra":[1,2],"readingMinutes":1.5}, 7, null ] ', "2026-09-19T05:00:00.000Z");
  insert.run("2026-09-20", "Typed", JSON.stringify([{ title: "<T&>", slug: "s", excerpt: "e", category: "c", readingMinutes: 2 }]), "2026-09-20T05:00:00.000Z");
  insert.run("2026-09-16T", "Odd key", "[]", "x");
  const service = new serviceModule.NewsletterService(db, { NEWSLETTER_SITE_URL: "https://aiandtech.news" }, async () => ({ id: "x" }));
  try {
    const lists: Json[] = [];
    for (const limit of [30, 2, 0, -5, 1, 1000, 100]) lists.push({ limit, editions: service.listEditions(limit) });
    const gets: Json[] = [];
    for (const key of ["2026-09-19", "2026-09-17", "2026-09-18", "missing", "2026-09-16T"]) gets.push({ key, edition: service.getEdition(key) });
    return { lists, gets, jsonText: JSON.stringify(service.getEdition("2026-09-19")) };
  } finally {
    db.close();
  }
}

function recordSiteUrls(): Json {
  const inputs = [undefined, "", "   ", "https://aiandtech.news", "https://aiandtech.news/", "https://AIANDTECH.news:443/path?q#h", "https://example.com:8443/x",
    "http://localhost:3000", "http://localhost", "http://example.com", "ftp://localhost", "not a url", " https://aiandtech.news ", "https://user:pw@example.com/"];
  return inputs.map((input) => {
    try {
      const service = new serviceModule.NewsletterService(openDatabase(), { NEWSLETTER_SITE_URL: input }, async () => ({ id: "x" }));
      return { input: input ?? null, siteUrl: (service as any).siteUrl };
    } catch (error) {
      return { input: input ?? null, error: { name: (error as Error).constructor.name, message: (error as Error).message, configuration: error instanceof email.NewsletterConfigurationError } };
    }
  });
}

function recordTokenSecrets(): Json {
  const db = openDatabase();
  const results: Json[] = [];
  for (const secret of [undefined, "", "short", "x".repeat(31), "x".repeat(32), `  ${"x".repeat(31)}  `, `  ${"x".repeat(32)}  `]) {
    const service = new serviceModule.NewsletterService(db, { NEWSLETTER_SITE_URL: "https://aiandtech.news", NEWSLETTER_TOKEN_SECRET: secret }, async () => ({ id: "x" }));
    try {
      results.push({ secret: secret ?? null, unsubscribe: service.unsubscribe("a.b") });
    } catch (error) {
      results.push({ secret: secret ?? null, error: { message: (error as Error).message, configuration: error instanceof email.NewsletterConfigurationError } });
    }
  }
  // The trimmed secret is the HMAC key.
  const padded = `  ${TEST_SECRET}  `;
  const service = new serviceModule.NewsletterService(db, { NEWSLETTER_SITE_URL: "https://aiandtech.news", NEWSLETTER_TOKEN_SECRET: padded }, async () => ({ id: "x" }));
  db.prepare("INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (1, 'a@b.c', 'active', 'c', 'u')").run();
  const trimmedKeyResult = service.unsubscribe(tokens.createNewsletterToken(1, "unsubscribe", TEST_SECRET), new Date("2026-09-20T12:00:00.000Z"));
  db.close();
  return { results, trimmedKeyResult };
}

async function main() {
  const schemaDb = openDatabase();
  const golden = {
    note: "Generated by record-node.ts from the Node implementation. Do not edit by hand.",
    node: process.version,
    timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    schema: schemaOf(schemaDb),
    tokens: recordTokens(),
    readingMinutes: recordReadingMinutes(),
    emails: recordEmails(),
    sender: await recordSender(),
    primitives: recordPrimitives(),
    digestSelection: await recordDigestSelection(),
    digestWindow: await recordDigestWindow(),
    digestTopFive: await recordDigestTopFive(),
    deliveryLifecycle: await recordDeliveryLifecycle(),
    subscriptions: await recordSubscriptions(),
    confirmAndUnsubscribe: await recordConfirmAndUnsubscribe(),
    editions: recordEditions(),
    siteUrls: recordSiteUrls(),
    tokenSecrets: recordTokenSecrets(),
  };
  schemaDb.close();
  writeFileSync(path.join(outputDir, "node-golden.json"), `${JSON.stringify(golden, null, 1)}\n`);
  writeFileSync(path.join(outputDir, "node-case-mappings.json"), `${JSON.stringify(recordCaseMappings())}\n`);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
```

- [ ] **Step 2: Record.** From the repository root of a checkout with `apps/server` dependencies installed (from a worktree, point `NODE_SERVER_ROOT` at the main checkout's `apps/server`, whose sources are identical):

```bash
TZ=Europe/Istanbul NODE_SERVER_ROOT=/Users/ozan/Projects/aiandtechnews/apps/server \
  /Users/ozan/Projects/aiandtechnews/apps/server/node_modules/.bin/tsx \
  apps/server-go/internal/newsletter/testdata/record-node.ts \
  apps/server-go/internal/newsletter/testdata
```

- [ ] **Step 3: Check the output.** With Node 22.16.0 the files are byte-identical to the ones this plan was verified with:

```bash
shasum -a 256 apps/server-go/internal/newsletter/testdata/*.json
# 55eec98991a1fb1fe67f2bdc04f02a60844a85a98ab6becb9423d7f886866ad8  node-case-mappings.json
# 15be47841f03ca38e71e0a060800da72d528273bc16f25d867ff27f5261f3ddf  node-golden.json
```

A different Node or ICU version may change `node-case-mappings.json` (newer Unicode); the Go test tolerates code points Go's tables do not assign yet. Review any other difference before continuing.

- [ ] **Step 4: Commit**

```bash
git add internal/newsletter/testdata
git commit -m "Record the Node newsletter behavior as golden vectors for the Go port"
```

---

### Task 3: Migration 6, Node's newsletter tables

Go migrations 1-5 do not define the newsletter tables. Add them as version 6 with Node's final schema, `CREATE ... IF NOT EXISTS`, because every production database already has them. The test compares the fresh schema with the `sqlite_schema` SQL Node leaves behind (recorded in Task 2), checks defaults and constraints, and runs the migration SQL over a Node-created database and over a legacy one whose `subscribers` columns were added with `ALTER TABLE` (so they sit at the end, without `NOT NULL` defaults): the schema must not change and the next id must continue. `golden_test.go` is the typed loader for `node-golden.json` that the later tasks use.

**Files:** Create `internal/newsletter/golden_test.go`, `internal/newsletter/migrations_test.go`, `internal/newsletter/migrations/006_newsletter.sql`, `internal/newsletter/migrations.go`; modify `internal/app/app.go`, `internal/app/migrations_test.go`

- [ ] **Step 1: Write the failing tests.** Create `internal/newsletter/golden_test.go`:

```go
package newsletter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// nodeGolden is testdata/node-golden.json, recorded from the real Node
// implementation by testdata/record-node.ts. Tests compare against it; they
// never infer Node's behavior.
type nodeGolden struct {
	Node     string `json:"node"`
	TimeZone string `json:"timeZone"`
	Schema   []struct {
		Type string `json:"type"`
		Name string `json:"name"`
		SQL  string `json:"sql"`
	} `json:"schema"`
	Tokens struct {
		Creates []struct {
			Secret    string  `json:"secret"`
			ID        int64   `json:"id"`
			Purpose   string  `json:"purpose"`
			ExpiresAt *string `json:"expiresAt"`
			Token     string  `json:"token"`
		} `json:"creates"`
		Verifies []struct {
			Name    string `json:"name"`
			Token   string `json:"token"`
			Purpose string `json:"purpose"`
			Secret  string `json:"secret"`
			Now     string `json:"now"`
			Result  *int64 `json:"result"`
		} `json:"verifies"`
	} `json:"tokens"`
	ReadingMinutes []struct {
		Input   *string `json:"input"`
		Minutes int     `json:"minutes"`
	} `json:"readingMinutes"`
	Emails struct {
		Welcome []struct {
			To             string    `json:"to"`
			SiteURL        string    `json:"siteUrl"`
			UnsubscribeURL string    `json:"unsubscribeUrl"`
			Email          nodeEmail `json:"email"`
		} `json:"welcome"`
		Digests []struct {
			Name           string          `json:"name"`
			To             string          `json:"to"`
			SiteURL        string          `json:"siteUrl"`
			UnsubscribeURL string          `json:"unsubscribeUrl"`
			Articles       []goldenArticle `json:"articles"`
			Email          nodeEmail       `json:"email"`
		} `json:"digests"`
		EscapeHTML         []stringPair `json:"escapeHtml"`
		EncodeURIComponent []stringPair `json:"encodeURIComponent"`
	} `json:"emails"`
	Sender     []senderCase `json:"sender"`
	Primitives struct {
		DatesParsed []struct {
			Input string `json:"input"`
			MS    *int64 `json:"ms"`
		} `json:"datesParsed"`
		IstanbulDates []struct {
			Instant string `json:"instant"`
			Edition string `json:"edition"`
		} `json:"istanbulDates"`
		ParseInts []struct {
			Input string   `json:"input"`
			Value *float64 `json:"value"`
		} `json:"parseInts"`
		Truncations []struct {
			Input   string `json:"input"`
			Slice80 string `json:"slice80"`
		} `json:"truncations"`
	} `json:"primitives"`
	DigestSelection struct {
		Now      string             `json:"now"`
		Articles []json.RawMessage  `json:"articles"`
		Result   goldenDigestResult `json:"result"`
		Editions []editionRow       `json:"editions"`
		Delivery []deliveryRow      `json:"deliveries"`
		Sent     []sentEmail        `json:"sent"`
		Delays   []int              `json:"delays"`
	} `json:"digestSelection"`
	DigestWindow struct {
		Result goldenDigestResult `json:"result"`
	} `json:"digestWindow"`
	DigestTopFive struct {
		Stamps   []string           `json:"stamps"`
		Result   goldenDigestResult `json:"result"`
		Editions []editionRow       `json:"editions"`
		Sent     []sentEmail        `json:"sent"`
	} `json:"digestTopFive"`
	DeliveryLifecycle struct {
		Subscribers  [][]json.RawMessage `json:"subscribers"`
		First        goldenDigestResult  `json:"first"`
		AfterFirst   []deliveryRow       `json:"afterFirst"`
		FirstDelays  []int               `json:"firstDelays"`
		Second       goldenDigestResult  `json:"second"`
		AfterSecond  []deliveryRow       `json:"afterSecond"`
		SecondDelays []int               `json:"secondDelays"`
		Sent         []sentEmail         `json:"sent"`
		Editions     []editionRow        `json:"editions"`
	} `json:"deliveryLifecycle"`
	Subscriptions struct {
		Outcomes []struct {
			Address   string `json:"address"`
			Placement string `json:"placement"`
			At        string `json:"at"`
			Result    *struct {
				State string `json:"state"`
			} `json:"result"`
			Error *struct {
				Name    string `json:"name"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"outcomes"`
		Subscribers []subscriberRow `json:"subscribers"`
		Normalized  []struct {
			Input string  `json:"input"`
			Email *string `json:"email"`
			Error *string `json:"error"`
		} `json:"normalized"`
	} `json:"subscriptions"`
	ConfirmAndUnsubscribe struct {
		Now      string `json:"now"`
		Confirms []struct {
			Name   string             `json:"name"`
			Token  string             `json:"token"`
			Result goldenConfirmation `json:"result"`
		} `json:"confirms"`
		Unsubscribes []struct {
			Name   string `json:"name"`
			Token  string `json:"token"`
			Result string `json:"result"`
		} `json:"unsubscribes"`
		Sent []struct {
			nodeEmail
			IdempotencyKey string `json:"idempotencyKey"`
		} `json:"sent"`
		Subscribers []subscriberRow `json:"subscribers"`
	} `json:"confirmAndUnsubscribe"`
	Editions struct {
		Lists []struct {
			Limit    int               `json:"limit"`
			Editions []json.RawMessage `json:"editions"`
		} `json:"lists"`
		Gets []struct {
			Key     string          `json:"key"`
			Edition json.RawMessage `json:"edition"`
		} `json:"gets"`
		JSONText string `json:"jsonText"`
	} `json:"editions"`
	SiteURLs []struct {
		Input   *string `json:"input"`
		SiteURL *string `json:"siteUrl"`
		Error   *struct {
			Name          string `json:"name"`
			Message       string `json:"message"`
			Configuration bool   `json:"configuration"`
		} `json:"error"`
	} `json:"siteUrls"`
	TokenSecrets struct {
		Results []struct {
			Secret      *string `json:"secret"`
			Unsubscribe *string `json:"unsubscribe"`
			Error       *struct {
				Message       string `json:"message"`
				Configuration bool   `json:"configuration"`
			} `json:"error"`
		} `json:"results"`
		TrimmedKeyResult string `json:"trimmedKeyResult"`
	} `json:"tokenSecrets"`
}

type goldenArticle struct {
	Title          string `json:"title"`
	Slug           string `json:"slug"`
	Excerpt        string `json:"excerpt"`
	Category       string `json:"category"`
	ReadingMinutes int    `json:"readingMinutes"`
}

type goldenTag struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type goldenDigestResult struct {
	Edition  string `json:"edition"`
	Articles int    `json:"articles"`
	Sent     int    `json:"sent"`
	Skipped  int    `json:"skipped"`
	Failed   int    `json:"failed"`
}

type goldenConfirmation struct {
	State       string `json:"state"`
	WelcomeSent bool   `json:"welcomeSent"`
}

type stringPair struct {
	Input  string `json:"input"`
	Output string `json:"output"`
}

type nodeEmail struct {
	To      string            `json:"to"`
	Subject string            `json:"subject"`
	HTML    string            `json:"html"`
	Text    string            `json:"text"`
	Headers map[string]string `json:"headers"`
	Tags    []goldenTag       `json:"tags"`
}

type senderCase struct {
	Name     string            `json:"name"`
	Env      map[string]string `json:"env"`
	Scripted []struct {
		Status     int     `json:"status"`
		Body       *string `json:"body"`
		RetryAfter *string `json:"retryAfter"`
		Throws     *string `json:"throws"`
	} `json:"scripted"`
	Email          nodeEmail `json:"email"`
	IdempotencyKey string    `json:"idempotencyKey"`
	Calls          []struct {
		URL     string            `json:"url"`
		Method  string            `json:"method"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	} `json:"calls"`
	Delays []float64 `json:"delays"`
	Result *struct {
		ID string `json:"id"`
	} `json:"result"`
	Error *struct {
		Name          string `json:"name"`
		Configuration bool   `json:"configuration"`
		Message       string `json:"message"`
	} `json:"error"`
}

type editionRow struct {
	ID         int64  `json:"id"`
	EditionKey string `json:"edition_key"`
	Subject    string `json:"subject"`
	Articles   string `json:"articles"`
	CreatedAt  string `json:"created_at"`
}

type deliveryRow struct {
	SubscriberID      int64   `json:"subscriber_id"`
	EditionKey        string  `json:"edition_key"`
	Status            string  `json:"status"`
	ProviderMessageID *string `json:"provider_message_id"`
	Error             *string `json:"error"`
	CreatedAt         string  `json:"created_at"`
	SentAt            *string `json:"sent_at"`
}

type sentEmail struct {
	To             string            `json:"to"`
	Subject        string            `json:"subject"`
	IdempotencyKey string            `json:"idempotencyKey"`
	HTML           string            `json:"html"`
	Text           string            `json:"text"`
	Headers        map[string]string `json:"headers"`
	Tags           []goldenTag       `json:"tags"`
}

type subscriberRow struct {
	ID                 int64   `json:"id"`
	Email              string  `json:"email"`
	Status             string  `json:"status"`
	SourcePlacement    *string `json:"source_placement"`
	ConfirmationSentAt *string `json:"confirmation_sent_at"`
	ConfirmedAt        *string `json:"confirmed_at"`
	UnsubscribedAt     *string `json:"unsubscribed_at"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

var (
	goldenOnce  sync.Once
	goldenValue nodeGolden
	goldenErr   error
)

func golden(t testing.TB) *nodeGolden {
	t.Helper()
	goldenOnce.Do(func() {
		data, err := os.ReadFile(filepath.Join("testdata", "node-golden.json"))
		if err != nil {
			goldenErr = err
			return
		}
		goldenErr = json.Unmarshal(data, &goldenValue)
	})
	if goldenErr != nil {
		t.Fatalf("load node-golden.json: %v", goldenErr)
	}
	return &goldenValue
}
```

Create `internal/newsletter/migrations_test.go`:

```go
package newsletter

import (
	"context"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

func normalizedSQL(sql string) string {
	return strings.Join(strings.Fields(strings.TrimSuffix(strings.TrimSpace(sql), ";")), " ")
}

func TestNewsletterMigrationCreatesNodesExactSchema(t *testing.T) {
	descriptors := Migrations()
	if len(descriptors) != 1 || descriptors[0].Version != 6 || descriptors[0].Name != "newsletter" {
		t.Fatalf("descriptors = %#v", descriptors)
	}
	db, _ := testutil.OpenDatabase(t)
	for i := 0; i < 2; i++ {
		if err := migrate.Run(context.Background(), db, Migrations()); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	// Node's schema as better-sqlite3 left it in sqlite_schema (recorded).
	want := map[string]string{}
	for _, object := range golden(t).Schema {
		want[object.Name] = normalizedSQL(object.SQL)
	}
	if len(want) != 5 {
		t.Fatalf("recorded schema objects = %d, want 5", len(want))
	}
	rows, err := db.Query(`SELECT name, sql FROM sqlite_schema WHERE name IN ('subscribers','newsletter_deliveries','newsletter_editions','idx_subscribers_status','idx_newsletter_deliveries_edition')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var name, sql string
		if err := rows.Scan(&name, &sql); err != nil {
			t.Fatal(err)
		}
		got[name] = normalizedSQL(sql)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for name, sql := range want {
		if got[name] != sql {
			t.Errorf("%s schema =\n%s\nwant (Node)\n%s", name, got[name], sql)
		}
	}
	if len(got) != len(want) {
		t.Errorf("schema objects = %v", got)
	}
}

func TestNewsletterMigrationEnforcesNodesConstraints(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, Migrations()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscribers (email) VALUES ('a@example.invalid')`); err != nil {
		t.Fatal(err)
	}
	var status, createdAt, updatedAt string
	if err := db.QueryRow(`SELECT status, created_at, updated_at FROM subscribers`).Scan(&status, &createdAt, &updatedAt); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || len(createdAt) != len("2006-01-02 15:04:05") || len(updatedAt) != len(createdAt) {
		t.Fatalf("subscriber defaults = %q %q %q", status, createdAt, updatedAt)
	}
	if _, err := db.Exec(`INSERT INTO newsletter_deliveries (subscriber_id, edition_key) VALUES (1, '2026-09-20')`); err != nil {
		t.Fatal(err)
	}
	var deliveryStatus string
	if err := db.QueryRow(`SELECT status FROM newsletter_deliveries`).Scan(&deliveryStatus); err != nil || deliveryStatus != "sending" {
		t.Fatalf("delivery default status = %q, %v", deliveryStatus, err)
	}
	if _, err := db.Exec(`INSERT INTO newsletter_editions (edition_key, subject, articles) VALUES ('2026-09-20', 's', '[]')`); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO subscribers (email) VALUES ('a@example.invalid')`,
		`INSERT INTO subscribers (email) VALUES (NULL)`,
		`INSERT INTO newsletter_deliveries (subscriber_id, edition_key) VALUES (1, '2026-09-20')`,
		`INSERT INTO newsletter_deliveries (subscriber_id, edition_key) VALUES (999, '2026-09-21')`,
		`INSERT INTO newsletter_editions (edition_key, subject, articles) VALUES ('2026-09-20', 's', '[]')`,
		`INSERT INTO newsletter_editions (edition_key, articles) VALUES ('2026-09-21', '[]')`,
	} {
		if _, err := db.Exec(statement); err == nil {
			t.Errorf("constraint not enforced: %s", statement)
		}
	}
	// ON DELETE CASCADE removes a subscriber's deliveries.
	if _, err := db.Exec(`DELETE FROM subscribers WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	var deliveries int
	if err := db.QueryRow(`SELECT COUNT(*) FROM newsletter_deliveries`).Scan(&deliveries); err != nil || deliveries != 0 {
		t.Fatalf("deliveries after subscriber delete = %d, %v", deliveries, err)
	}
}

// Production databases already have these tables (Node creates them at every
// start). migrate.Run refuses unmanaged databases, so the future adoption
// command will stamp them; the migration SQL itself must be a no-op there,
// including on a legacy subscribers table whose columns Node added with
// ALTER TABLE (apps/server/src/db.ts:116-137) and therefore sit at the end
// without NOT NULL defaults.
func TestNewsletterMigrationSQLKeepsTablesNodeAlreadyCreated(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		db, _ := testutil.OpenDatabase(t)
		if legacy {
			for _, statement := range []string{
				`CREATE TABLE subscribers (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT NOT NULL UNIQUE, created_at TEXT)`,
				`ALTER TABLE subscribers ADD COLUMN status TEXT NOT NULL DEFAULT 'pending'`,
				`ALTER TABLE subscribers ADD COLUMN source_placement TEXT`,
				`ALTER TABLE subscribers ADD COLUMN confirmation_sent_at TEXT`,
				`ALTER TABLE subscribers ADD COLUMN confirmed_at TEXT`,
				`ALTER TABLE subscribers ADD COLUMN unsubscribed_at TEXT`,
				`ALTER TABLE subscribers ADD COLUMN updated_at TEXT`,
			} {
				if _, err := db.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
		}
		for _, object := range golden(t).Schema {
			if legacy && object.Name == "subscribers" {
				continue
			}
			if object.Type == "table" {
				if _, err := db.Exec(object.SQL); err != nil {
					t.Fatalf("create Node %s: %v", object.Name, err)
				}
			}
		}
		for _, object := range golden(t).Schema {
			if object.Type == "index" {
				if _, err := db.Exec(object.SQL); err != nil {
					t.Fatalf("create Node %s: %v", object.Name, err)
				}
			}
		}
		if _, err := db.Exec(`INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (503, 'old@example.invalid', 'active', 'c', 'u')`); err != nil {
			t.Fatal(err)
		}
		var before string
		if err := db.QueryRow(`SELECT group_concat(sql, ';') FROM sqlite_schema`).Scan(&before); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(Migrations()[0].SQL); err != nil {
			t.Fatalf("legacy=%t: %v", legacy, err)
		}
		var after string
		if err := db.QueryRow(`SELECT group_concat(sql, ';') FROM sqlite_schema`).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if before != after {
			t.Fatalf("legacy=%t: migration changed a Node schema:\n%s\n->\n%s", legacy, before, after)
		}
		result, err := db.Exec(`INSERT INTO subscribers (email, status, created_at, updated_at) VALUES ('new@example.invalid', 'active', 'c', 'u')`)
		if err != nil {
			t.Fatal(err)
		}
		if id, err := result.LastInsertId(); err != nil || id != 504 {
			t.Fatalf("legacy=%t: next subscriber id = %d, %v", legacy, id, err)
		}
	}
}
```

In `internal/app/migrations_test.go`, expect six descriptors and the newsletter objects:

```diff
diff --git a/apps/server-go/internal/app/migrations_test.go b/apps/server-go/internal/app/migrations_test.go
index 9036e19..b4b5ddf 100644
--- a/apps/server-go/internal/app/migrations_test.go
+++ b/apps/server-go/internal/app/migrations_test.go
@@ -13,12 +13,13 @@ import (
 
 func TestApplicationMigrationsHaveStableGlobalOrderAndAreIdempotent(t *testing.T) {
 	descriptors := app.Migrations()
-	if len(descriptors) != 5 ||
+	if len(descriptors) != 6 ||
 		descriptors[0].Version != 1 || descriptors[0].Name != "editorial authors" ||
 		descriptors[1].Version != 2 || descriptors[1].Name != "content categories and articles" ||
 		descriptors[2].Version != 3 || descriptors[2].Name != "newsroom candidates" ||
 		descriptors[3].Version != 4 || descriptors[3].Name != "newsroom published index" ||
-		descriptors[4].Version != 5 || descriptors[4].Name != "media library" {
+		descriptors[4].Version != 5 || descriptors[4].Name != "media library" ||
+		descriptors[5].Version != 6 || descriptors[5].Name != "newsletter" {
 		t.Fatalf("descriptors = %#v", descriptors)
 	}
 	firstChecksum, secondChecksum := descriptors[0].Checksum(), descriptors[1].Checksum()
@@ -97,6 +98,13 @@ func assertSchemaCompatibility(t *testing.T, db *sql.DB) {
 		}
 	}
 
+	for _, name := range []string{"subscribers", "newsletter_deliveries", "newsletter_editions", "idx_subscribers_status", "idx_newsletter_deliveries_edition"} {
+		var count int
+		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_schema WHERE name = ?`, name).Scan(&count); err != nil || count != 1 {
+			t.Errorf("newsletter schema object %s = %d, %v", name, count, err)
+		}
+	}
+
 	if _, err := db.Exec(`INSERT INTO authors(id,name,email,password_hash) VALUES (1,'Author','author@example.invalid','hash')`); err != nil {
 		t.Fatal(err)
 	}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -p 2 ./internal/newsletter/ ./internal/app/ -run Migration`
Expected: FAIL: `internal/newsletter` does not compile (`undefined: Migrations`), and `internal/app` reports `descriptors = …` (five descriptors).

- [ ] **Step 3: Implement.** Create `internal/newsletter/migrations/006_newsletter.sql`:

```sql
CREATE TABLE IF NOT EXISTS subscribers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending',
    source_placement TEXT,
    confirmation_sent_at TEXT,
    confirmed_at TEXT,
    unsubscribed_at TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS newsletter_deliveries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    subscriber_id INTEGER NOT NULL,
    edition_key TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'sending',
    provider_message_id TEXT,
    error TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    sent_at TEXT,
    UNIQUE(subscriber_id, edition_key),
    FOREIGN KEY (subscriber_id) REFERENCES subscribers(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS newsletter_editions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    edition_key TEXT NOT NULL UNIQUE,
    subject TEXT NOT NULL,
    articles TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_subscribers_status ON subscribers(status);
CREATE INDEX IF NOT EXISTS idx_newsletter_deliveries_edition ON newsletter_deliveries(edition_key, status);
```

Create `internal/newsletter/migrations.go` (it carries the package documentation):

```go
// Package newsletter ports the Node newsletter (apps/server/src/newsletter and
// the newsletter routes of apps/server/src/routes/public.ts): immediate
// signup, signed confirm and unsubscribe links, the public edition archive,
// and the daily digest delivered through Resend.
package newsletter

import (
	_ "embed"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
)

// newsletterSchema is Node's final newsletter schema (apps/server/src/db.ts:72-103,
// 138-139), verbatim. CREATE ... IF NOT EXISTS keeps it a no-op on a
// database Node created, including one whose subscribers columns Node added
// with ALTER TABLE.
//
//go:embed migrations/006_newsletter.sql
var newsletterSchema string

// Migrations returns a fresh slice containing the newsletter's immutable schema descriptors.
func Migrations() []migrate.Descriptor {
	return []migrate.Descriptor{
		{Version: 6, Name: "newsletter", SQL: newsletterSchema},
	}
}
```

Append the descriptor in `internal/app/app.go`:

```diff
diff --git a/apps/server-go/internal/app/app.go b/apps/server-go/internal/app/app.go
index cebfd05..7621302 100644
--- a/apps/server-go/internal/app/app.go
+++ b/apps/server-go/internal/app/app.go
@@ -22,6 +22,7 @@ import (
 	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
 	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/indexnow"
 	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
+	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsletter"
 	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
 	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
 	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/settings"
@@ -188,7 +189,8 @@ func Migrations() []migrate.Descriptor {
 	descriptors := editorial.Migrations()
 	descriptors = append(descriptors, content.Migrations()...)
 	descriptors = append(descriptors, newsroom.Migrations()...)
-	return append(descriptors, media.Migrations()...)
+	descriptors = append(descriptors, media.Migrations()...)
+	return append(descriptors, newsletter.Migrations()...)
 }
 
 func (a *App) Address() string       { return a.address }
```

- [ ] **Step 4: Run them to verify they pass**

Run: `go test -p 2 ./internal/newsletter/ ./internal/app/ ./internal/devseed/`
Expected: `ok` for all three.

- [ ] **Step 5: Commit**

```bash
git add internal/newsletter/golden_test.go internal/newsletter/migrations_test.go internal/newsletter/migrations internal/newsletter/migrations.go internal/app/app.go internal/app/migrations_test.go
git commit -m "Add Node's newsletter tables as migration 6"
```

---

### Task 4: JavaScript semantics

The Node code leans on JavaScript's string, number, and date behavior in ways Go's standard library does not share: `trim` and `\s` include Unicode spaces (Go's `\s` is ASCII); lengths and `slice` count UTF-16 units, and a cut through a surrogate pair is stored by better-sqlite3 as WTF-8 (recorded: `78 ED A0 BD`); `toLowerCase`/`toUpperCase` apply SpecialCasing (`ß` → `SS`, `İ` → `i̇`) and the final-sigma rule, which `unicode.ToLower` does not; `JSON.stringify` does not escape `<`, `>`, `&`, U+2028, or U+2029 (encoding/json does); `Date.parse` reads offset-less date-times in the process zone and accepts V8's variations (lowercase `t`/`z`, a space for `T`, hour 24, day overflow, offsets without a colon). `jscase.go` is generated from `node-case-mappings.json`: the 102 multi-character uppercase mappings.

**Files:** Create `internal/newsletter/jsvalues_test.go`, `internal/newsletter/jsvalues.go`, `internal/newsletter/jscase.go`

- [ ] **Step 1: Write the failing tests.** Create `internal/newsletter/jsvalues_test.go`:

```go
package newsletter

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"
)

func istanbulZone(t testing.TB) *time.Location {
	t.Helper()
	location, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		t.Fatal(err)
	}
	return location
}

func TestEscapeHTMLMatchesNode(t *testing.T) {
	for _, vector := range golden(t).Emails.EscapeHTML {
		if got := escapeHTML(vector.Input); got != vector.Output {
			t.Errorf("escapeHTML(%q) = %q, want %q", vector.Input, got, vector.Output)
		}
	}
}

func TestEncodeURIComponentMatchesNode(t *testing.T) {
	for _, vector := range golden(t).Emails.EncodeURIComponent {
		if got := encodeURIComponent(vector.Input); got != vector.Output {
			t.Errorf("encodeURIComponent(%q) = %q, want %q", vector.Input, got, vector.Output)
		}
	}
}

func TestJSONStringMatchesJSONStringify(t *testing.T) {
	var recorded struct {
		Primitives struct {
			Stringified []stringPair `json:"stringified"`
		} `json:"primitives"`
	}
	data, err := os.ReadFile(filepath.Join("testdata", "node-golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &recorded); err != nil {
		t.Fatal(err)
	}
	if len(recorded.Primitives.Stringified) == 0 {
		t.Fatal("no JSON.stringify vectors")
	}
	for _, vector := range recorded.Primitives.Stringified {
		var builder strings.Builder
		writeJSONString(&builder, vector.Input)
		if builder.String() != vector.Output {
			t.Errorf("JSON string of %q = %q, want %q", vector.Input, builder.String(), vector.Output)
		}
	}
	// A lone surrogate (stored by better-sqlite3 as WTF-8) is escaped like
	// JSON.stringify's well-formed output.
	var builder strings.Builder
	writeJSONString(&builder, "x\xed\xa0\xbd")
	if builder.String() != `"x\ud83d"` {
		t.Errorf("lone surrogate = %s", builder.String())
	}
}

func TestSliceUTF16MatchesStringSlice(t *testing.T) {
	for _, vector := range golden(t).Primitives.Truncations {
		want := vector.Slice80
		// encoding/json decodes the lone high surrogate Node produced as
		// U+FFFD; better-sqlite3 stores it as WTF-8 (recorded: 78EDA0BD).
		if strings.HasSuffix(want, "\ufffd") {
			want = strings.TrimSuffix(want, "\ufffd") + "\xed\xa0\xbd"
		}
		if got := sliceUTF16(vector.Input, 80); got != want {
			t.Errorf("sliceUTF16(%q, 80) = %q, want %q", vector.Input, got, want)
		}
	}
	if got := sliceUTF16("\u00e9\U0001f680", 2); got != "\u00e9\xed\xa0\xbd" {
		t.Errorf("split pair = %q", got)
	}
	if got := utf16Length("a\U0001f680\u00e9"); got != 4 {
		t.Errorf("utf16Length = %d", got)
	}
}

func TestParseIntMatchesNode(t *testing.T) {
	for _, vector := range golden(t).Primitives.ParseInts {
		got, ok := parseInt10(vector.Input)
		if vector.Value == nil {
			if ok {
				t.Errorf("parseInt(%q) = %v, want NaN", vector.Input, got)
			}
			continue
		}
		if !ok || got != *vector.Value {
			t.Errorf("parseInt(%q) = %v, %t, want %v", vector.Input, got, ok, *vector.Value)
		}
	}
}

// V8's legacy (non-ISO) date formats are not ported: Go treats them as
// unparseable, so such an article is left out of the digest instead of
// being parsed in the process time zone. No code path writes them.
var legacyOnlyDates = map[string]bool{
	"Sat, 19 Sep 2026 12:00:00 GMT": true,
	"September 19, 2026":            true,
	"19 Sep 2026 12:00":             true,
	"2026-9-19":                     true,
	"10000-01-01":                   true,
}

func TestParseArticleTimestampMatchesNodesDateParse(t *testing.T) {
	local := istanbulZone(t)
	vectors := golden(t).Primitives.DatesParsed
	if len(vectors) < 50 {
		t.Fatalf("only %d date vectors", len(vectors))
	}
	for _, vector := range vectors {
		got, ok := parseArticleTimestamp(vector.Input, local)
		if legacyOnlyDates[vector.Input] {
			if ok {
				t.Errorf("legacy date %q parsed as %d, want unparseable in Go", vector.Input, got)
			}
			continue
		}
		if vector.MS == nil {
			if ok {
				t.Errorf("Date.parse(%q) = %d, want NaN", vector.Input, got)
			}
			continue
		}
		if !ok || got != *vector.MS {
			t.Errorf("Date.parse(%q) = %d, %t, want %d", vector.Input, got, ok, *vector.MS)
		}
	}
}

func TestIstanbulEditionKeysMatchIntl(t *testing.T) {
	for _, vector := range golden(t).Primitives.IstanbulDates {
		instant, err := time.Parse(time.RFC3339Nano, vector.Instant)
		if err != nil {
			t.Fatal(err)
		}
		if got := editionKey(instant); got != vector.Edition {
			t.Errorf("editionKey(%s) = %q, want %q", vector.Instant, got, vector.Edition)
		}
	}
}

func TestISOTimestampMatchesToISOString(t *testing.T) {
	for instant, want := range map[time.Time]string{
		time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC):                         "2026-09-20T12:00:00.000Z",
		time.Date(2026, 9, 20, 15, 4, 5, 999_999_999, istanbulZone(t)):        "2026-09-20T12:04:05.999Z",
		time.Date(2026, 1, 2, 3, 4, 5, 6_000_000, time.FixedZone("x", -3600)): "2026-01-02T04:04:05.006Z",
	} {
		if got := isoTimestamp(instant); got != want {
			t.Errorf("isoTimestamp(%v) = %q, want %q", instant, got, want)
		}
	}
}

type caseMappings struct {
	UnicodeVersion string           `json:"unicodeVersion"`
	Upper          [][2]any         `json:"upper"`
	Lower          [][2]any         `json:"lower"`
	Contextual     []contextualCase `json:"contextual"`
}

type contextualCase struct {
	Input string `json:"input"`
	Lower string `json:"lower"`
}

func loadCaseMappings(t *testing.T) caseMappings {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "node-case-mappings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mappings caseMappings
	if err := json.Unmarshal(data, &mappings); err != nil {
		t.Fatal(err)
	}
	return mappings
}

// newerThanGo reports mappings that involve code points Go's Unicode tables
// (15.0) do not assign yet; Node's ICU tables are newer (recorded
// unicodeVersion). Such characters cannot be mapped by Go's unicode package.
func newerThanGo(values ...string) bool {
	for _, value := range values {
		for _, r := range value {
			if !unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S, unicode.Z, unicode.Cc, unicode.Cf, unicode.Co) {
				return true
			}
		}
	}
	return false
}

func TestJavaScriptCaseMappingMatchesNodeForEveryCodePoint(t *testing.T) {
	mappings := loadCaseMappings(t)
	check := func(name string, table [][2]any, convert func(string) string) {
		mismatches := 0
		for _, entry := range table {
			r := rune(entry[0].(float64))
			want := entry[1].(string)
			if got := convert(string(r)); got != want {
				if newerThanGo(string(r), want) {
					continue
				}
				mismatches++
				if mismatches < 20 {
					t.Errorf("%s(U+%04X) = %q, want %q", name, r, got, want)
				}
			}
		}
		if mismatches > 0 {
			t.Errorf("%s: %d mismatches", name, mismatches)
		}
	}
	check("toUpperCase", mappings.Upper, jsToUpper)
	check("toLowerCase", mappings.Lower, jsToLower)
	// Code points absent from Node's tables map to themselves in Node.
	mapped := map[rune]bool{}
	for _, entry := range mappings.Upper {
		mapped[rune(entry[0].(float64))] = true
	}
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if r >= 0xd800 && r <= 0xdfff || mapped[r] {
			continue
		}
		if got := jsToUpper(string(r)); got != string(r) {
			t.Errorf("toUpperCase(U+%04X) = %q, want unchanged", r, got)
		}
	}
	for _, vector := range mappings.Contextual {
		if got := jsToLower(vector.Input); got != vector.Lower {
			t.Errorf("toLowerCase(%q) = %q, want %q (Final_Sigma)", vector.Input, got, vector.Lower)
		}
	}
}

func TestJavaScriptWhitespaceTrim(t *testing.T) {
	if got := jsTrim("\u00a0\u2028 a b\ufeff\t"); got != "a b" {
		t.Errorf("jsTrim = %q", got)
	}
	if got := jsTrim("\u200ba\u200b"); got != "\u200ba\u200b" {
		t.Errorf("zero width space is not whitespace: %q", got)
	}
}

func TestRoundHalfUpMatchesMathRound(t *testing.T) {
	for input, want := range map[float64]float64{0.5: 1, 1.5: 2, 2.5: 3, 0.49999999999999994: 0, 1.4999: 1, 109.0 / 220: 0} {
		if got := mathRound(input); got != want {
			t.Errorf("Math.round(%v) = %v, want %v", input, got, want)
		}
	}
	if !math.IsNaN(mathRound(math.NaN())) {
		t.Error("Math.round(NaN) is NaN")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -p 2 ./internal/newsletter/`
Expected: FAIL to compile:

```
internal/newsletter/jsvalues_test.go:25:13: undefined: escapeHTML
internal/newsletter/jsvalues_test.go:33:13: undefined: encodeURIComponent
internal/newsletter/jsvalues_test.go:57:3: undefined: writeJSONString
```

- [ ] **Step 3: Implement.** Create `internal/newsletter/jsvalues.go`:

```go
package newsletter

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	// Edition keys are Europe/Istanbul dates; embed the zone database so they
	// never depend on the host's zoneinfo.
	_ "time/tzdata"
	"unicode"
	"unicode/utf8"
)

// This file ports the JavaScript semantics the Node newsletter relies on
// implicitly: String.prototype.trim/toLowerCase/toUpperCase/slice and the
// UTF-16 length, \s, encodeURIComponent, JSON.stringify for strings,
// parseInt(value, 10), Math.round, Date.prototype.toISOString, Date.parse,
// and Intl's Europe/Istanbul calendar date. Each is pinned against vectors
// recorded from Node (testdata/node-golden.json).

// isJSWhitespace is ECMAScript WhiteSpace plus LineTerminator: the set that
// \s matches and String.prototype.trim removes.
func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', '\u00a0', '\u1680', '\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff':
		return true
	}
	return r >= '\u2000' && r <= '\u200a'
}

// jsTrim is String.prototype.trim.
func jsTrim(s string) string { return strings.TrimFunc(s, isJSWhitespace) }

// nextUnit decodes the next character of s as JavaScript sees it. It
// returns the character, its length in UTF-16 code units, and its size in
// bytes. A lone surrogate stored as WTF-8 by better-sqlite3 (ED A0..BF xx)
// is one code unit; any other invalid byte is U+FFFD, as better-sqlite3
// decodes it.
func nextUnit(s string) (rune, int, int) {
	if len(s) >= 3 && s[0] == 0xed && s[1] >= 0xa0 && s[1] <= 0xbf && s[2] >= 0x80 && s[2] <= 0xbf {
		return 0xd000 | rune(s[1]&0x3f)<<6 | rune(s[2]&0x3f), 1, 3
	}
	r, size := utf8.DecodeRuneInString(s)
	if r >= 0x10000 {
		return r, 2, size
	}
	return r, 1, size
}

// utf16Length is String.prototype.length.
func utf16Length(s string) int {
	length := 0
	for len(s) > 0 {
		_, units, size := nextUnit(s)
		length += units
		s = s[size:]
	}
	return length
}

// sliceUTF16 is String.prototype.slice(0, n). When the cut splits a
// surrogate pair, JavaScript keeps the lone high surrogate, which
// better-sqlite3 writes as its three-byte WTF-8 form; so does sliceUTF16.
func sliceUTF16(s string, n int) string {
	units, offset := 0, 0
	for offset < len(s) {
		r, width, size := nextUnit(s[offset:])
		if units+width > n {
			if width == 2 && units+1 == n {
				high := 0xd800 + (r-0x10000)>>10
				return s[:offset] + string([]byte{0xe0 | byte(high>>12), 0x80 | byte(high>>6)&0x3f, 0x80 | byte(high)&0x3f})
			}
			break
		}
		units += width
		offset += size
	}
	return s[:offset]
}

// escapeHTML is the Node email module's escapeHtml.
var escapeHTML = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#039;").Replace

// encodeURIComponent is the JavaScript global of the same name. Invalid
// UTF-8 and lone surrogates are encoded as U+FFFD (JavaScript would throw
// URIError for a lone surrogate).
func encodeURIComponent(s string) string {
	const hex = "0123456789ABCDEF"
	var builder strings.Builder
	for len(s) > 0 {
		r, _, size := nextUnit(s)
		s = s[size:]
		if r < utf8.RuneSelf && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.!~*'()", r)) {
			builder.WriteByte(byte(r))
			continue
		}
		if r >= 0xd800 && r <= 0xdfff {
			r = utf8.RuneError
		}
		var encoded [utf8.UTFMax]byte
		for _, b := range encoded[:utf8.EncodeRune(encoded[:], r)] {
			builder.WriteByte('%')
			builder.WriteByte(hex[b>>4])
			builder.WriteByte(hex[b&0x0f])
		}
	}
	return builder.String()
}

// writeJSONString writes s the way JSON.stringify serializes a string: only
// '"', '\\', and control characters are escaped (U+2028, U+2029, '<', '>',
// and '&' are not, unlike encoding/json), and lone surrogates become \udxxx.
func writeJSONString(builder *strings.Builder, s string) {
	const hex = "0123456789abcdef"
	builder.WriteByte('"')
	for len(s) > 0 {
		r, _, size := nextUnit(s)
		raw := s[:size]
		s = s[size:]
		switch {
		case r == '"':
			builder.WriteString(`\"`)
		case r == '\\':
			builder.WriteString(`\\`)
		case r == '\b':
			builder.WriteString(`\b`)
		case r == '\f':
			builder.WriteString(`\f`)
		case r == '\n':
			builder.WriteString(`\n`)
		case r == '\r':
			builder.WriteString(`\r`)
		case r == '\t':
			builder.WriteString(`\t`)
		case r < 0x20 || r >= 0xd800 && r <= 0xdfff:
			builder.WriteString(`\u`)
			builder.WriteByte(hex[r>>12&0xf])
			builder.WriteByte(hex[r>>8&0xf])
			builder.WriteByte(hex[r>>4&0xf])
			builder.WriteByte(hex[r&0xf])
		case r == utf8.RuneError && size == 1:
			builder.WriteRune(utf8.RuneError)
		default:
			builder.WriteString(raw)
		}
	}
	builder.WriteByte('"')
}

// jsToUpper is String.prototype.toUpperCase (no locale): Go's single-rune
// mapping plus SpecialCasing's unconditional multi-character mappings.
func jsToUpper(s string) string {
	var builder strings.Builder
	for _, r := range s {
		if special, ok := upperSpecialCasing[r]; ok {
			builder.WriteString(special)
			continue
		}
		builder.WriteRune(unicode.ToUpper(r))
	}
	return builder.String()
}

// jsToLower is String.prototype.toLowerCase (no locale): Go's single-rune
// mapping, U+0130 to "i\u0307", and the Final_Sigma rule for U+03A3.
func jsToLower(s string) string {
	runes := []rune(s)
	var builder strings.Builder
	for index, r := range runes {
		switch r {
		case '\u0130':
			builder.WriteString("i\u0307")
		case '\u03a3':
			if finalSigma(runes, index) {
				builder.WriteRune('\u03c2')
			} else {
				builder.WriteRune('\u03c3')
			}
		default:
			builder.WriteRune(unicode.ToLower(r))
		}
	}
	return builder.String()
}

// finalSigma implements the Final_Sigma casing context of Unicode 3.13: the
// sigma follows a cased letter (skipping case-ignorable characters) and is
// not followed by one.
func finalSigma(runes []rune, index int) bool {
	before := false
	for i := index - 1; i >= 0; i-- {
		if cased(runes[i]) {
			before = true
			break
		}
		if !caseIgnorable(runes[i]) {
			break
		}
	}
	if !before {
		return false
	}
	for i := index + 1; i < len(runes); i++ {
		if cased(runes[i]) {
			return false
		}
		if !caseIgnorable(runes[i]) {
			break
		}
	}
	return true
}

func cased(r rune) bool {
	return unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r) ||
		unicode.Is(unicode.Other_Lowercase, r) || unicode.Is(unicode.Other_Uppercase, r)
}

// caseIgnorable is Unicode's Case_Ignorable: Mn, Me, Cf, Lm, Sk, and the
// Word_Break MidLetter, MidNumLet, and Single_Quote characters.
func caseIgnorable(r rune) bool {
	switch r {
	case '\'', '.', ':', '\u00b7', '\u0387', '\u055f', '\u05f4', '\u2018', '\u2019', '\u2024', '\u2027',
		'\ufe13', '\ufe52', '\ufe55', '\uff07', '\uff0e', '\uff1a':
		return true
	}
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Lm, unicode.Sk)
}

// parseInt10 is parseInt(value, 10); ok is false for NaN.
func parseInt10(value string) (float64, bool) {
	value = strings.TrimLeftFunc(value, isJSWhitespace)
	sign := 1.0
	if value != "" && (value[0] == '+' || value[0] == '-') {
		if value[0] == '-' {
			sign = -1
		}
		value = value[1:]
	}
	end := 0
	for end < len(value) && value[end] >= '0' && value[end] <= '9' {
		end++
	}
	if end == 0 {
		return math.NaN(), false
	}
	parsed, _ := strconv.ParseFloat(value[:end], 64)
	return sign * parsed, true
}

// mathRound is Math.round: the nearest integer, ties toward +Infinity.
func mathRound(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	floor := math.Floor(x)
	if x-floor >= 0.5 {
		return floor + 1
	}
	return floor
}

// isoTimestamp is Date.prototype.toISOString for years 0000-9999.
func isoTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// istanbul is the publication time zone. The embedded time/tzdata makes
// loading it infallible.
var istanbul = func() *time.Location {
	location, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		panic(err)
	}
	return location
}()

// editionKey is Node's formatIstanbulDate: the YYYY-MM-DD calendar date in
// Europe/Istanbul (Intl.DateTimeFormat("en-CA", {timeZone: "Europe/Istanbul"})).
func editionKey(now time.Time) string {
	return now.In(istanbul).Format("2006-01-02")
}

var sqliteTimestamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`)

// parseArticleTimestamp is Node's parseArticleTimestamp: SQLite's
// "YYYY-MM-DD HH:MM:SS" is read as UTC, anything else goes to Date.parse.
// It returns milliseconds since the epoch; ok is false for NaN. local is the
// zone V8 uses for date-times without an offset (the process time zone).
func parseArticleTimestamp(value string, local *time.Location) (int64, bool) {
	if sqliteTimestamp.MatchString(value) {
		value = strings.Replace(value, " ", "T", 1) + "Z"
	}
	return dateParse(value, local)
}

// dateParse is the part of V8's Date.parse that article timestamps use: the
// ECMAScript date-time string format and V8's accepted variations of it
// (lowercase t/z, a space instead of T, more than three fraction digits,
// offsets without a colon, hour 24, days 29-31 that overflow into the next
// month). Date-only forms are UTC; date-times without an offset are local.
// V8's legacy fallback formats ("Sep 19 2026", RFC 2822 dates, 1-digit
// months, 5-digit years without a sign) are not ported and report NaN.
func dateParse(value string, local *time.Location) (int64, bool) {
	p := dateScanner{s: value}
	year, ok := p.year()
	if !ok {
		return 0, false
	}
	month, day := 1, 1
	fullDate := false
	if p.consume('-') {
		if month, ok = p.fixed(2); !ok {
			return 0, false
		}
		if p.consume('-') {
			if day, ok = p.fixed(2); !ok {
				return 0, false
			}
			fullDate = true
		}
	}
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return 0, false
	}
	utcDate := func(hour, minute, second, millisecond int) int64 {
		return time.Date(year, time.Month(month), day, hour, minute, second, millisecond*int(time.Millisecond), time.UTC).UnixMilli()
	}
	if p.done() {
		return utcDate(0, 0, 0, 0), true
	}
	if fullDate && (p.peek() == 'Z' || p.peek() == 'z') && len(p.s)-p.i == 1 {
		return utcDate(0, 0, 0, 0), true
	}
	if separator := p.next(); separator != 'T' && separator != 't' && separator != ' ' {
		return 0, false
	}
	hour, ok := p.fixed(2)
	if !ok || !p.consume(':') {
		return 0, false
	}
	minute, ok := p.fixed(2)
	if !ok {
		return 0, false
	}
	second, millisecond := 0, 0
	if p.consume(':') {
		if second, ok = p.fixed(2); !ok {
			return 0, false
		}
		if p.consume('.') {
			digits := p.run()
			if digits == "" {
				return 0, false
			}
			digits = (digits + "00")[:3]
			millisecond, _ = strconv.Atoi(digits)
		}
	}
	if hour > 24 || minute > 59 || second > 59 || hour == 24 && (minute != 0 || second != 0 || millisecond != 0) {
		return 0, false
	}
	if p.done() {
		return time.Date(year, time.Month(month), day, hour, minute, second, millisecond*int(time.Millisecond), local).UnixMilli(), true
	}
	switch sign := p.next(); sign {
	case 'Z', 'z':
		if !p.done() {
			return 0, false
		}
		return utcDate(hour, minute, second, millisecond), true
	case '+', '-':
		offsetHours, ok := p.fixed(2)
		if !ok {
			return 0, false
		}
		p.consume(':')
		offsetMinutes, ok := p.fixed(2)
		if !ok || !p.done() || offsetHours > 23 || offsetMinutes > 59 {
			return 0, false
		}
		offset := int64(offsetHours*60+offsetMinutes) * int64(time.Minute/time.Millisecond)
		if sign == '-' {
			offset = -offset
		}
		return utcDate(hour, minute, second, millisecond) - offset, true
	}
	return 0, false
}

type dateScanner struct {
	s string
	i int
}

func (p *dateScanner) done() bool { return p.i == len(p.s) }

func (p *dateScanner) peek() byte {
	if p.done() {
		return 0
	}
	return p.s[p.i]
}

func (p *dateScanner) next() byte {
	c := p.peek()
	if !p.done() {
		p.i++
	}
	return c
}

func (p *dateScanner) consume(c byte) bool {
	if p.peek() == c && !p.done() {
		p.i++
		return true
	}
	return false
}

// fixed reads exactly n ASCII digits.
func (p *dateScanner) fixed(n int) (int, bool) {
	if len(p.s)-p.i < n {
		return 0, false
	}
	value := 0
	for _, c := range []byte(p.s[p.i : p.i+n]) {
		if c < '0' || c > '9' {
			return 0, false
		}
		value = value*10 + int(c-'0')
	}
	p.i += n
	return value, true
}

// run reads one or more ASCII digits.
func (p *dateScanner) run() string {
	start := p.i
	for !p.done() && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
		p.i++
	}
	return p.s[start:p.i]
}

// year reads YYYY or a signed six-digit \u00b1YYYYYY year.
func (p *dateScanner) year() (int, bool) {
	if c := p.peek(); c == '+' || c == '-' {
		p.i++
		year, ok := p.fixed(6)
		if c == '-' {
			year = -year
		}
		return year, ok
	}
	return p.fixed(4)
}
```

Create `internal/newsletter/jscase.go` (generated from `testdata/node-case-mappings.json`: each entry whose mapping has more than one code point):

```go
package newsletter

// upperSpecialCasing holds the unconditional multi-character uppercase
// mappings of Unicode SpecialCasing.txt that String.prototype.toUpperCase
// applies and Go's unicode.ToUpper (a single-rune mapping) cannot. The
// entries are exactly the multi-code-point results in
// testdata/node-case-mappings.json, recorded from Node.
var upperSpecialCasing = map[rune]string{
	0x00df: "\u0053\u0053",
	0x0149: "\u02bc\u004e",
	0x01f0: "\u004a\u030c",
	0x0390: "\u0399\u0308\u0301",
	0x03b0: "\u03a5\u0308\u0301",
	0x0587: "\u0535\u0552",
	0x1e96: "\u0048\u0331",
	0x1e97: "\u0054\u0308",
	0x1e98: "\u0057\u030a",
	0x1e99: "\u0059\u030a",
	0x1e9a: "\u0041\u02be",
	0x1f50: "\u03a5\u0313",
	0x1f52: "\u03a5\u0313\u0300",
	0x1f54: "\u03a5\u0313\u0301",
	0x1f56: "\u03a5\u0313\u0342",
	0x1f80: "\u1f08\u0399",
	0x1f81: "\u1f09\u0399",
	0x1f82: "\u1f0a\u0399",
	0x1f83: "\u1f0b\u0399",
	0x1f84: "\u1f0c\u0399",
	0x1f85: "\u1f0d\u0399",
	0x1f86: "\u1f0e\u0399",
	0x1f87: "\u1f0f\u0399",
	0x1f88: "\u1f08\u0399",
	0x1f89: "\u1f09\u0399",
	0x1f8a: "\u1f0a\u0399",
	0x1f8b: "\u1f0b\u0399",
	0x1f8c: "\u1f0c\u0399",
	0x1f8d: "\u1f0d\u0399",
	0x1f8e: "\u1f0e\u0399",
	0x1f8f: "\u1f0f\u0399",
	0x1f90: "\u1f28\u0399",
	0x1f91: "\u1f29\u0399",
	0x1f92: "\u1f2a\u0399",
	0x1f93: "\u1f2b\u0399",
	0x1f94: "\u1f2c\u0399",
	0x1f95: "\u1f2d\u0399",
	0x1f96: "\u1f2e\u0399",
	0x1f97: "\u1f2f\u0399",
	0x1f98: "\u1f28\u0399",
	0x1f99: "\u1f29\u0399",
	0x1f9a: "\u1f2a\u0399",
	0x1f9b: "\u1f2b\u0399",
	0x1f9c: "\u1f2c\u0399",
	0x1f9d: "\u1f2d\u0399",
	0x1f9e: "\u1f2e\u0399",
	0x1f9f: "\u1f2f\u0399",
	0x1fa0: "\u1f68\u0399",
	0x1fa1: "\u1f69\u0399",
	0x1fa2: "\u1f6a\u0399",
	0x1fa3: "\u1f6b\u0399",
	0x1fa4: "\u1f6c\u0399",
	0x1fa5: "\u1f6d\u0399",
	0x1fa6: "\u1f6e\u0399",
	0x1fa7: "\u1f6f\u0399",
	0x1fa8: "\u1f68\u0399",
	0x1fa9: "\u1f69\u0399",
	0x1faa: "\u1f6a\u0399",
	0x1fab: "\u1f6b\u0399",
	0x1fac: "\u1f6c\u0399",
	0x1fad: "\u1f6d\u0399",
	0x1fae: "\u1f6e\u0399",
	0x1faf: "\u1f6f\u0399",
	0x1fb2: "\u1fba\u0399",
	0x1fb3: "\u0391\u0399",
	0x1fb4: "\u0386\u0399",
	0x1fb6: "\u0391\u0342",
	0x1fb7: "\u0391\u0342\u0399",
	0x1fbc: "\u0391\u0399",
	0x1fc2: "\u1fca\u0399",
	0x1fc3: "\u0397\u0399",
	0x1fc4: "\u0389\u0399",
	0x1fc6: "\u0397\u0342",
	0x1fc7: "\u0397\u0342\u0399",
	0x1fcc: "\u0397\u0399",
	0x1fd2: "\u0399\u0308\u0300",
	0x1fd3: "\u0399\u0308\u0301",
	0x1fd6: "\u0399\u0342",
	0x1fd7: "\u0399\u0308\u0342",
	0x1fe2: "\u03a5\u0308\u0300",
	0x1fe3: "\u03a5\u0308\u0301",
	0x1fe4: "\u03a1\u0313",
	0x1fe6: "\u03a5\u0342",
	0x1fe7: "\u03a5\u0308\u0342",
	0x1ff2: "\u1ffa\u0399",
	0x1ff3: "\u03a9\u0399",
	0x1ff4: "\u038f\u0399",
	0x1ff6: "\u03a9\u0342",
	0x1ff7: "\u03a9\u0342\u0399",
	0x1ffc: "\u03a9\u0399",
	0xfb00: "\u0046\u0046",
	0xfb01: "\u0046\u0049",
	0xfb02: "\u0046\u004c",
	0xfb03: "\u0046\u0046\u0049",
	0xfb04: "\u0046\u0046\u004c",
	0xfb05: "\u0053\u0054",
	0xfb06: "\u0053\u0054",
	0xfb13: "\u0544\u0546",
	0xfb14: "\u0544\u0535",
	0xfb15: "\u0544\u053b",
	0xfb16: "\u054e\u0546",
	0xfb17: "\u0544\u053d",
}
```

- [ ] **Step 4: Run them to verify they pass**

Run: `go test -p 2 ./internal/newsletter/`
Expected: `ok`. `TestParseArticleTimestampMatchesNodesDateParse` covers all 59 recorded `Date.parse` inputs (the five legacy-only formats are asserted unparseable); `TestJavaScriptCaseMappingMatchesNodeForEveryCodePoint` walks all 1,114,112 code points.

- [ ] **Step 5: Commit**

```bash
git add internal/newsletter/jsvalues.go internal/newsletter/jscase.go internal/newsletter/jsvalues_test.go
git commit -m "Port the JavaScript string, number, and date semantics the newsletter relies on"
```

---

### Task 5: Tokens

Confirm and unsubscribe links carry `base64url(JSON.stringify({v:1,id,purpose[,exp]}))` + `.` + `base64url(HMAC-SHA256(secret, payload))`. Go must mint the same bytes (21 recorded tokens over three secrets, one non-ASCII) and accept exactly what Node accepts (40 recorded verification cases). Two Node quirks decide the implementation: `token.split(".")` destructured into three names only rejects a *non-empty* third segment, and `Buffer.from(payload, "base64url")` accepts the standard alphabet, skips invalid characters, and stops at `=`. JSON parsing uses `json.Unmarshal` into a map (strict like `JSON.parse`, last duplicate key wins), with numbers parsed like JavaScript (`1.0`, `1e2`, `1e400` → Infinity).

**Files:** Create `internal/newsletter/tokens_test.go`, `internal/newsletter/tokens.go`

- [ ] **Step 1: Write the failing tests.** Create `internal/newsletter/tokens_test.go`:

```go
package newsletter

import (
	"testing"
	"time"
)

// Existing confirm and unsubscribe links in sent emails were signed by Node;
// Go must produce and accept exactly the same tokens.
func TestCreateTokenMatchesNodeByteForByte(t *testing.T) {
	creates := golden(t).Tokens.Creates
	if len(creates) != 21 {
		t.Fatalf("recorded token vectors = %d", len(creates))
	}
	for _, vector := range creates {
		var expiresAt *time.Time
		if vector.ExpiresAt != nil {
			parsed, err := time.Parse(time.RFC3339Nano, *vector.ExpiresAt)
			if err != nil {
				t.Fatal(err)
			}
			expiresAt = &parsed
		}
		got := CreateToken(vector.ID, Purpose(vector.Purpose), vector.Secret, expiresAt)
		if got != vector.Token {
			t.Errorf("CreateToken(%d, %s, exp %v) = %s, want %s", vector.ID, vector.Purpose, vector.ExpiresAt, got, vector.Token)
		}
	}
}

func TestVerifyTokenMatchesNode(t *testing.T) {
	verifies := golden(t).Tokens.Verifies
	if len(verifies) < 40 {
		t.Fatalf("recorded verification vectors = %d", len(verifies))
	}
	for _, vector := range verifies {
		now, err := time.Parse(time.RFC3339Nano, vector.Now)
		if err != nil {
			t.Fatal(err)
		}
		id, ok := VerifyToken(vector.Token, Purpose(vector.Purpose), vector.Secret, now)
		switch {
		case vector.Result == nil && ok:
			t.Errorf("%s: VerifyToken = %d, want null", vector.Name, id)
		case vector.Result != nil && (!ok || id != *vector.Result):
			t.Errorf("%s: VerifyToken = %d, %t, want %d", vector.Name, id, ok, *vector.Result)
		}
	}
}

func TestNodeBase64DecodingIsLenientLikeBuffer(t *testing.T) {
	// Recorded with Buffer.from(value, "base64url").toString("latin1").
	for input, want := range map[string]string{
		"YWJj": "abc", "YWJjZA": "abcd", "YWJjZA==": "abcd", "YWJjZA=": "abcd", "YW Jj": "abc",
		"YW!Jj": "abc", "YWJj=ZGVm": "abc", "YW=Jj": "a", "Y": "", "YW": "a", "+/+/": "\xfb\xff\xbf",
		"-_-_": "\xfb\xff\xbf", "YWJ\njZA": "abcd", "YWJjZA===": "abcd", "=YWJj": "", "YWJjZ": "abc", "YWJ*j": "abc",
	} {
		if got := string(nodeBase64Decode(input)); got != want {
			t.Errorf("nodeBase64Decode(%q) = %q, want %q", input, got, want)
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -p 2 ./internal/newsletter/ -run Token`
Expected: FAIL to compile:

```
internal/newsletter/tokens_test.go:24:10: undefined: CreateToken
internal/newsletter/tokens_test.go:24:33: undefined: Purpose
internal/newsletter/tokens_test.go:41:13: undefined: VerifyToken
```

- [ ] **Step 3: Implement.** Create `internal/newsletter/tokens.go`:

```go
package newsletter

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

// Purpose is the token purpose Node signs into every link.
type Purpose string

const (
	PurposeConfirm     Purpose = "confirm"
	PurposeUnsubscribe Purpose = "unsubscribe"
)

// CreateToken is Node's createNewsletterToken (apps/server/src/newsletter/tokens.ts):
// base64url(JSON.stringify({v:1,id,purpose[,exp]})) + "." +
// base64url(HMAC-SHA256(secret, payload)), with exp in whole seconds.
func CreateToken(subscriberID int64, purpose Purpose, secret string, expiresAt *time.Time) string {
	var payload strings.Builder
	payload.WriteString(`{"v":1,"id":`)
	payload.WriteString(strconv.FormatInt(subscriberID, 10))
	payload.WriteString(`,"purpose":`)
	writeJSONString(&payload, string(purpose))
	if expiresAt != nil {
		payload.WriteString(`,"exp":`)
		payload.WriteString(strconv.FormatFloat(math.Floor(float64(expiresAt.UnixMilli())/1000), 'f', -1, 64))
	}
	payload.WriteByte('}')
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload.String()))
	return encoded + "." + sign(encoded, secret)
}

func sign(payload, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyToken is Node's verifyNewsletterToken: it returns the subscriber id
// of a token signed with secret for purpose that has not expired at now.
// Like Node it accepts a token followed by "." or "..anything" (the third
// dot-separated segment must be empty), decodes the payload leniently like
// Buffer.from(value, "base64url"), and compares signatures in constant time.
func VerifyToken(token string, purpose Purpose, secret string, now time.Time) (int64, bool) {
	parts := strings.Split(token, ".")
	payload := parts[0]
	var signature, extra string
	if len(parts) > 1 {
		signature = parts[1]
	}
	if len(parts) > 2 {
		extra = parts[2]
	}
	if payload == "" || signature == "" || extra != "" {
		return 0, false
	}
	if !hmac.Equal([]byte(signature), []byte(sign(payload, secret))) {
		return 0, false
	}

	// JSON.parse: one JSON value (surrounding whitespace allowed) that must
	// be an object for the property reads below to succeed.
	var value map[string]json.RawMessage
	if err := json.Unmarshal(nodeBase64Decode(payload), &value); err != nil || value == nil {
		return 0, false
	}
	if version, ok := jsNumberValue(value["v"]); !ok || version != 1 {
		return 0, false
	}
	var got string
	if raw := value["purpose"]; len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &got) != nil || got != string(purpose) {
		return 0, false
	}
	id, ok := jsNumberValue(value["id"])
	if !ok || math.IsInf(id, 0) || id != math.Trunc(id) || id < 1 {
		return 0, false
	}
	if exp, present := value["exp"]; present {
		seconds, ok := jsNumberValue(exp)
		if !ok || math.IsInf(seconds, 0) || seconds < math.Floor(float64(now.UnixMilli())/1000) {
			return 0, false
		}
	}
	if id > math.MaxInt64 {
		// Number.isInteger accepts it, but no SQLite row id can match it.
		return 0, false
	}
	return int64(id), true
}

// jsNumberValue returns a JSON number as the JavaScript Number JSON.parse
// produces (out-of-range literals become +Infinity or -Infinity); ok is false for any
// other JSON value, including a missing one.
func jsNumberValue(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 || raw[0] != '-' && (raw[0] < '0' || raw[0] > '9') {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(string(raw), 64)
	if err != nil && !math.IsInf(parsed, 0) {
		return 0, false
	}
	return parsed, true
}

// nodeBase64Decode is Buffer.from(value, "base64url"): both the URL-safe and
// the standard alphabet are accepted, other characters are skipped, decoding
// stops at the first "=", and a trailing partial group yields what it can.
func nodeBase64Decode(value string) []byte {
	sextets := make([]byte, 0, len(value))
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c == '=':
			i = len(value)
			continue
		case c >= 'A' && c <= 'Z':
			sextets = append(sextets, c-'A')
		case c >= 'a' && c <= 'z':
			sextets = append(sextets, c-'a'+26)
		case c >= '0' && c <= '9':
			sextets = append(sextets, c-'0'+52)
		case c == '+' || c == '-':
			sextets = append(sextets, 62)
		case c == '/' || c == '_':
			sextets = append(sextets, 63)
		}
	}
	decoded := make([]byte, 0, len(sextets)*3/4)
	for len(sextets) >= 4 {
		decoded = append(decoded, sextets[0]<<2|sextets[1]>>4, sextets[1]<<4|sextets[2]>>2, sextets[2]<<6|sextets[3])
		sextets = sextets[4:]
	}
	switch len(sextets) {
	case 2:
		decoded = append(decoded, sextets[0]<<2|sextets[1]>>4)
	case 3:
		decoded = append(decoded, sextets[0]<<2|sextets[1]>>4, sextets[1]<<4|sextets[2]>>2)
	}
	return decoded
}
```

- [ ] **Step 4: Run them to verify they pass**

Run: `go test -p 2 ./internal/newsletter/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/newsletter/tokens.go internal/newsletter/tokens_test.go
git commit -m "Sign and verify newsletter tokens exactly like Node"
```

---

### Task 6: Reading time and the email templates

`readingMinutes` strips `<script>`/`<style>` bodies with a backreference regex (`/<(script|style)\b[^>]*>[\s\S]*?<\/\1>/gi`), which RE2 cannot express, so `stripScriptAndStyle` scans explicitly (its extra unit vectors were checked against V8). The email templates are copied from the template literals verbatim, including every newline and indentation run; the tests compare byte for byte with Node's output for six digests (single, one minute, grouped, escaping and URI encoding, Unicode categories, empty) and two welcome emails.

**Files:** Create `internal/newsletter/email_test.go`, `internal/newsletter/reading.go`, `internal/newsletter/email.go`

- [ ] **Step 1: Write the failing tests.** Create `internal/newsletter/email_test.go`:

```go
package newsletter

import (
	"reflect"
	"strings"
	"testing"
)

func TestReadingMinutesMatchesNode(t *testing.T) {
	vectors := golden(t).ReadingMinutes
	if len(vectors) < 30 {
		t.Fatalf("recorded reading-time vectors = %d", len(vectors))
	}
	for _, vector := range vectors {
		input := ""
		if vector.Input != nil {
			input = *vector.Input
		}
		if got := readingMinutes(input); got != vector.Minutes {
			t.Errorf("readingMinutes(%.60q) = %d, want %d", input, got, vector.Minutes)
		}
	}
}

// Expected values were checked against V8 with the Node regex
// /<(script|style)\b[^>]*>[\s\S]*?<\/\1>/gi.
func TestStripScriptAndStyleFollowsTheBackreferenceRegex(t *testing.T) {
	for input, want := range map[string]string{
		"a<script>x</script>b":                "a b",
		"a<SCRIPT>x</Script>b":                "a b",
		"a<script>x</style>y</script>b":       "a b",
		"a<style>x</script>b":                 "a<style>x</script>b",
		"a<scriptx>x</scriptx>b":              "a<scriptx>x</scriptx>b",
		"a<script>x</script >b":               "a<script>x</script >b",
		"<script>1</script><style>2</style>3": "  3",
		"a<script":                            "a<script",
		"<script>unclosed <style>s</style>":   "<script>unclosed  ",
		"<script\n>x</script>":                " ",
	} {
		if got := stripScriptAndStyle(input); got != want {
			t.Errorf("stripScriptAndStyle(%q) = %q, want %q", input, got, want)
		}
	}
}

func headerMap(headers []Header) map[string]string {
	if headers == nil {
		return nil
	}
	values := map[string]string{}
	for _, header := range headers {
		values[header.Name] = header.Value
	}
	return values
}

func tagsOf(tags []Tag) []goldenTag {
	if tags == nil {
		return nil
	}
	converted := make([]goldenTag, len(tags))
	for i, tag := range tags {
		converted[i] = goldenTag{Name: tag.Name, Value: tag.Value}
	}
	return converted
}

func assertEmail(t *testing.T, name string, got Email, want nodeEmail) {
	t.Helper()
	if got.To != want.To || got.Subject != want.Subject {
		t.Errorf("%s: to/subject = %q %q, want %q %q", name, got.To, got.Subject, want.To, want.Subject)
	}
	if got.HTML != want.HTML {
		t.Errorf("%s: HTML differs from Node's renderer at byte %d\ngot:  %q\nwant: %q", name, firstDifference(got.HTML, want.HTML), got.HTML, want.HTML)
	}
	if got.Text != want.Text {
		t.Errorf("%s: text = %q\nwant %q", name, got.Text, want.Text)
	}
	if !reflect.DeepEqual(headerMap(got.Headers), want.Headers) || !reflect.DeepEqual(tagsOf(got.Tags), want.Tags) {
		t.Errorf("%s: headers/tags = %v %v, want %v %v", name, got.Headers, got.Tags, want.Headers, want.Tags)
	}
	if len(got.Headers) == 2 && (got.Headers[0].Name != "List-Unsubscribe" || got.Headers[1].Name != "List-Unsubscribe-Post") {
		t.Errorf("%s: header order = %v", name, got.Headers)
	}
}

func firstDifference(a, b string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

func TestWelcomeEmailIsByteIdenticalToNode(t *testing.T) {
	for _, vector := range golden(t).Emails.Welcome {
		assertEmail(t, "welcome to "+vector.To, WelcomeEmail(vector.To, vector.SiteURL, vector.UnsubscribeURL), vector.Email)
	}
}

func TestDigestEmailIsByteIdenticalToNode(t *testing.T) {
	digests := golden(t).Emails.Digests
	if len(digests) != 6 {
		t.Fatalf("recorded digests = %d", len(digests))
	}
	for _, vector := range digests {
		articles := make([]DigestArticle, len(vector.Articles))
		for i, article := range vector.Articles {
			articles[i] = DigestArticle(article)
		}
		assertEmail(t, "digest "+vector.Name, DigestEmail(vector.To, articles, vector.SiteURL, vector.UnsubscribeURL), vector.Email)
		if got := DigestSubject(articles); got != vector.Email.Subject {
			t.Errorf("DigestSubject(%s) = %q", vector.Name, got)
		}
	}
}

func TestDigestGroupsByCategoryInSelectionOrder(t *testing.T) {
	sections := groupByCategory([]DigestArticle{{Title: "1", Category: "AI"}, {Title: "2", Category: "Code"}, {Title: "3", Category: "AI"}, {Title: "4", Category: "ai"}})
	var got []string
	for _, section := range sections {
		var titles []string
		for _, article := range section.articles {
			titles = append(titles, article.Title)
		}
		got = append(got, section.category+":"+strings.Join(titles, ","))
	}
	if want := []string{"AI:1,3", "Code:2", "ai:4"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sections = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -p 2 ./internal/newsletter/`
Expected: FAIL to compile:

```
internal/newsletter/email_test.go:19:13: undefined: readingMinutes
internal/newsletter/email_test.go:40:13: undefined: stripScriptAndStyle
internal/newsletter/email_test.go:46:26: undefined: Header
```

- [ ] **Step 3: Implement.** Create `internal/newsletter/reading.go`:

```go
package newsletter

import (
	"regexp"
	"strings"
)

// wordsPerMinute is Node's WORDS_PER_MINUTE (apps/server/src/newsletter/reading-time.ts).
const wordsPerMinute = 220

var (
	htmlTag    = regexp.MustCompile(`<[^>]+>`)
	nbspEntity = regexp.MustCompile(`&[nN][bB][sS][pP];`)
	// JavaScript's /i folds ASCII letters only; (?i) in Go would also fold
	// the Kelvin sign and the long s, so the classes are spelled out.
	otherEntity = regexp.MustCompile(`&[a-zA-Z]+;|&#[0-9]+;`)
)

// readingMinutes is Node's readingMinutes: tags (and script/style bodies)
// are removed before counting whitespace-separated words, at 220 words per
// minute, rounded like Math.round, and never less than 1. An empty or NULL
// body is 1 minute.
func readingMinutes(html string) int {
	if html == "" {
		return 1
	}
	text := stripScriptAndStyle(html)
	text = htmlTag.ReplaceAllString(text, " ")
	text = nbspEntity.ReplaceAllString(text, " ")
	text = otherEntity.ReplaceAllString(text, "")
	text = jsTrim(text)
	if text == "" {
		return 1
	}
	words := len(strings.FieldsFunc(text, isJSWhitespace))
	return int(max(1, mathRound(float64(words)/wordsPerMinute)))
}

// stripScriptAndStyle replaces every match of Node's
// /<(script|style)\b[^>]*>[\s\S]*?<\/\1>/gi with a space. RE2 has no
// backreferences, so the scan is explicit: an opening tag whose name is
// followed by a non-word character runs to the next ">", then lazily to
// the first "</name>" with the same name (case-insensitively); without one,
// the regex fails at that position and scanning resumes one byte later.
func stripScriptAndStyle(html string) string {
	var builder strings.Builder
	i := 0
	for i < len(html) {
		if end, ok := scriptOrStyleAt(html, i); ok {
			builder.WriteByte(' ')
			i = end
			continue
		}
		builder.WriteByte(html[i])
		i++
	}
	return builder.String()
}

func scriptOrStyleAt(html string, start int) (int, bool) {
	if html[start] != '<' {
		return 0, false
	}
	for _, name := range []string{"script", "style"} {
		nameEnd := start + 1 + len(name)
		if nameEnd > len(html) || !asciiEqualFold(html[start+1:nameEnd], name) {
			continue
		}
		if nameEnd < len(html) && isWordByte(html[nameEnd]) {
			return 0, false
		}
		open := strings.IndexByte(html[nameEnd:], '>')
		if open < 0 {
			return 0, false
		}
		bodyStart := nameEnd + open + 1
		closing := "</" + name + ">"
		for i := bodyStart; i+len(closing) <= len(html); i++ {
			if asciiEqualFold(html[i:i+len(closing)], closing) {
				return i + len(closing), true
			}
		}
		return 0, false
	}
	return 0, false
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

// asciiEqualFold compares case-insensitively over ASCII only, like /i
// without the u flag for these ASCII patterns.
func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if x >= 'A' && x <= 'Z' {
			x += 'a' - 'A'
		}
		if y >= 'A' && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}
```

Create `internal/newsletter/email.go`:

```go
package newsletter

import (
	"strconv"
	"strings"
)

// Email is Node's NewsletterEmail. Headers keep Node's object key order,
// which is the order they appear in the Resend request body.
type Email struct {
	To      string
	Subject string
	HTML    string
	Text    string
	Headers []Header
	Tags    []Tag
}

type Header struct {
	Name  string
	Value string
}

type Tag struct {
	Name  string
	Value string
}

// DigestArticle is one story in a digest and in the stored edition JSON
// (field order and names are Node's).
type DigestArticle struct {
	Title          string `json:"title"`
	Slug           string `json:"slug"`
	Excerpt        string `json:"excerpt"`
	Category       string `json:"category"`
	ReadingMinutes int    `json:"readingMinutes"`
}

// The templates below are apps/server/src/newsletter/email.ts verbatim,
// including every newline and indentation run inside the template literals.

func emailLayout(content, footer string) string {
	return `<!doctype html>
<html>
  <body style="margin:0;background:#0b0b10;color:#f5f5f7;font-family:Arial,sans-serif">
    <div style="max-width:620px;margin:0 auto;padding:36px 20px">
      <div style="font-size:12px;font-weight:800;letter-spacing:.16em;text-transform:uppercase;color:#9b87f5;margin-bottom:24px">AI &amp; Tech News</div>
      <div style="background:#15151d;border:1px solid #292936;border-radius:8px;padding:28px">` + content + `</div>
      <div style="color:#8f8f9c;font-size:12px;line-height:1.6;padding:20px 4px">` + footer + `</div>
    </div>
  </body>
</html>`
}

func button(label, url string) string {
	return `<a href="` + escapeHTML(url) + `" style="display:inline-block;background:#7c5cff;color:#fff;text-decoration:none;font-weight:700;border-radius:5px;padding:13px 20px">` + escapeHTML(label) + `</a>`
}

func unsubscribeHeaders(unsubscribeURL string) []Header {
	return []Header{
		{Name: "List-Unsubscribe", Value: "<" + unsubscribeURL + ">"},
		{Name: "List-Unsubscribe-Post", Value: "List-Unsubscribe=One-Click"},
	}
}

// WelcomeEmail is Node's welcomeEmail, sent when a pending subscriber confirms.
func WelcomeEmail(to, siteURL, unsubscribeURL string) Email {
	content := `
    <h1 style="margin:0 0 14px;font-size:26px;line-height:1.2">Welcome to AI &amp; Tech News</h1>
    <p style="color:#c8c8d0;line-height:1.65;margin:0 0 22px">Your subscription is confirmed. We will send you a concise daily digest of the AI and technology stories worth knowing.</p>
    ` + button("Read the latest stories", siteURL)
	return Email{
		To:      to,
		Subject: "Welcome to AI & Tech News",
		HTML:    emailLayout(content, `You can <a href="`+escapeHTML(unsubscribeURL)+`" style="color:#b7a7ff">unsubscribe at any time</a>.`),
		Text:    "Welcome to AI & Tech News. Your subscription is confirmed.\n\nRead the latest stories: " + siteURL + "\n\nUnsubscribe: " + unsubscribeURL,
		Headers: unsubscribeHeaders(unsubscribeURL),
		Tags:    []Tag{{Name: "email_type", Value: "welcome"}},
	}
}

// DigestSubject is Node's digestSubject: the lead story's title, with the
// number of further stories.
func DigestSubject(articles []DigestArticle) string {
	if len(articles) == 0 {
		return "Today in AI and technology"
	}
	if rest := len(articles) - 1; rest > 0 {
		return articles[0].Title + " (+" + strconv.Itoa(rest) + " more)"
	}
	return articles[0].Title
}

type digestSection struct {
	category string
	articles []DigestArticle
}

// groupByCategory is Node's groupByCategory: sections in order of first
// appearance, articles in selection order, categories compared exactly.
func groupByCategory(articles []DigestArticle) []digestSection {
	var sections []digestSection
	for _, article := range articles {
		found := false
		for i := range sections {
			if sections[i].category == article.Category {
				sections[i].articles = append(sections[i].articles, article)
				found = true
				break
			}
		}
		if !found {
			sections = append(sections, digestSection{category: article.Category, articles: []DigestArticle{article}})
		}
	}
	return sections
}

func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}
	return many
}

// DigestEmail is Node's digestEmail.
func DigestEmail(to string, articles []DigestArticle, siteURL, unsubscribeURL string) Email {
	sections := groupByCategory(articles)

	var sectionsHTML strings.Builder
	textSections := make([]string, 0, len(sections))
	for _, section := range sections {
		var items strings.Builder
		textItems := make([]string, 0, len(section.articles))
		for _, article := range section.articles {
			url := siteURL + "/article/" + encodeURIComponent(article.Slug)
			minutes := strconv.Itoa(article.ReadingMinutes)
			items.WriteString(`<div style="padding:0 0 18px">
        <a href="` + escapeHTML(url) + `" style="color:#f5f5f7;text-decoration:none;font-size:19px;line-height:1.3;font-weight:800">` + escapeHTML(article.Title) + `</a>
        <span style="color:#8f8f9c;font-size:13px;white-space:nowrap"> (` + minutes + ` minute read)</span>
        <p style="color:#b8b8c2;line-height:1.55;margin:7px 0 0">` + escapeHTML(article.Excerpt) + `</p>
      </div>`)
			textItems = append(textItems, article.Title+" ("+minutes+" minute read)\n"+article.Excerpt+"\n"+url)
		}
		sectionsHTML.WriteString(`<div style="padding:18px 0 0;border-top:1px solid #292936">
        <div style="color:#9b87f5;font-size:11px;font-weight:800;letter-spacing:.12em;text-transform:uppercase;margin-bottom:12px">` + escapeHTML(section.category) + `</div>
        ` + items.String() + `
      </div>`)
		textSections = append(textSections, jsToUpper(section.category)+"\n\n"+strings.Join(textItems, "\n\n"))
	}

	totalMinutes := 0
	for _, article := range articles {
		totalMinutes += article.ReadingMinutes
	}
	content := `
    <h1 style="margin:0 0 8px;font-size:26px;line-height:1.2">Today in AI and technology</h1>
    <p style="color:#c8c8d0;line-height:1.65;margin:0 0 18px">` + strconv.Itoa(len(articles)) + ` ` + plural(len(articles), "story", "stories") +
		` worth knowing, about ` + strconv.Itoa(totalMinutes) + ` ` + plural(totalMinutes, "minute", "minutes") + ` of reading.</p>
    ` + sectionsHTML.String() + `
    <div style="padding-top:14px;border-top:1px solid #292936">` + button("See all stories", siteURL) + `</div>`
	return Email{
		To:      to,
		Subject: DigestSubject(articles),
		HTML:    emailLayout(content, `You subscribed at aiandtech.news. <a href="`+escapeHTML(unsubscribeURL)+`" style="color:#b7a7ff">Unsubscribe</a>.`),
		Text:    "Today in AI and technology\n\n" + strings.Join(textSections, "\n\n") + "\n\nSee all stories: " + siteURL + "\n\nUnsubscribe: " + unsubscribeURL,
		Headers: unsubscribeHeaders(unsubscribeURL),
		Tags:    []Tag{{Name: "email_type", Value: "daily_digest"}},
	}
}
```

- [ ] **Step 4: Run them to verify they pass**

Run: `go test -p 2 ./internal/newsletter/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/newsletter/reading.go internal/newsletter/email.go internal/newsletter/email_test.go
git commit -m "Render the welcome and digest emails byte for byte like Node"
```

---

### Task 7: The Resend sender

The test replays all 37 recorded sender cases against an `httptest` stand-in (`scriptedResend`): the exact request body string, the `Authorization`, `Content-Type`, and `Idempotency-Key` headers, the number of attempts, every delay (through an injected `Sleep`), and the returned id or error message and class. A transport failure is produced by hijacking and closing the connection (Node: `TypeError("fetch failed")`, not retried). The body is built with the `JSON.stringify` port, not encoding/json, so it is byte-identical (key order, no HTML escaping). Only `DefaultResendEndpoint` is ever used outside tests.

**Files:** Create `internal/newsletter/resend_test.go`, `internal/newsletter/resend.go`

- [ ] **Step 1: Write the failing tests.** Create `internal/newsletter/resend_test.go`:

```go
package newsletter

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type recordedRequest struct {
	method  string
	path    string
	headers http.Header
	body    string
}

// scriptedResend is an httptest stand-in for the Resend API. It never
// forwards anything anywhere.
type scriptedResend struct {
	mu        sync.Mutex
	responses []scriptedResponse
	requests  []recordedRequest
}

type scriptedResponse struct {
	status     int
	body       string
	retryAfter *string
	disconnect bool
}

func (s *scriptedResend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.requests = append(s.requests, recordedRequest{method: r.Method, path: r.URL.Path, headers: r.Header.Clone(), body: string(body)})
	next := s.responses[min(len(s.requests)-1, len(s.responses)-1)]
	s.mu.Unlock()
	if next.disconnect {
		connection, _, err := http.NewResponseController(w).Hijack()
		if err == nil {
			_ = connection.Close()
		}
		return
	}
	if next.retryAfter != nil {
		w.Header().Set("Retry-After", *next.retryAfter)
	}
	w.WriteHeader(next.status)
	_, _ = io.WriteString(w, next.body)
}

// snapshot returns the recorded requests under the lock (the handler runs
// on the server's goroutines).
func (s *scriptedResend) snapshot() []recordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recordedRequest(nil), s.requests...)
}

func startResend(t *testing.T, responses ...scriptedResponse) (*scriptedResend, *httptest.Server) {
	t.Helper()
	fake := &scriptedResend{responses: responses}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return fake, server
}

// orderedHeaders restores Node's header order (object key order), which a
// Go map cannot carry.
func orderedHeaders(headers map[string]string) []Header {
	if headers == nil {
		return nil
	}
	ordered := []Header{}
	for _, name := range []string{"List-Unsubscribe", "List-Unsubscribe-Post"} {
		if value, ok := headers[name]; ok {
			ordered = append(ordered, Header{Name: name, Value: value})
		}
	}
	return ordered
}

func emailFromNode(email nodeEmail) Email {
	var tags []Tag
	if email.Tags != nil {
		tags = []Tag{}
		for _, tag := range email.Tags {
			tags = append(tags, Tag{Name: tag.Name, Value: tag.Value})
		}
	}
	return Email{To: email.To, Subject: email.Subject, HTML: email.HTML, Text: email.Text, Headers: orderedHeaders(email.Headers), Tags: tags}
}

func TestResendSenderMatchesNodeRequestsRetriesAndErrors(t *testing.T) {
	cases := golden(t).Sender
	if len(cases) < 30 {
		t.Fatalf("recorded sender cases = %d", len(cases))
	}
	for _, vector := range cases {
		t.Run(vector.Name, func(t *testing.T) {
			var responses []scriptedResponse
			for _, scripted := range vector.Scripted {
				response := scriptedResponse{status: scripted.Status, retryAfter: scripted.RetryAfter, disconnect: scripted.Throws != nil}
				if scripted.Body != nil {
					response.body = *scripted.Body
				}
				responses = append(responses, response)
			}
			fake, server := startResend(t, responses...)
			var delays []float64
			sender := NewResendSender(ResendConfig{
				APIKey: vector.Env["RESEND_API_KEY"], From: vector.Env["NEWSLETTER_FROM"], ReplyTo: vector.Env["NEWSLETTER_REPLY_TO"],
				Endpoint: server.URL + "/emails", Client: server.Client(),
				Sleep: func(_ context.Context, delay time.Duration) error {
					delays = append(delays, float64(delay)/float64(time.Millisecond))
					return nil
				},
			})
			id, err := sender.Send(context.Background(), emailFromNode(vector.Email), vector.IdempotencyKey)

			requests := fake.snapshot()
			if len(requests) != len(vector.Calls) {
				t.Fatalf("requests = %d, want %d", len(requests), len(vector.Calls))
			}
			for i, call := range vector.Calls {
				got := requests[i]
				if call.URL != DefaultResendEndpoint || got.method != call.Method || got.path != "/emails" {
					t.Errorf("request %d = %s %s (Node: %s %s)", i, got.method, got.path, call.Method, call.URL)
				}
				for name, value := range call.Headers {
					if got.headers.Get(name) != value {
						t.Errorf("request %d header %s = %q, want %q", i, name, got.headers.Get(name), value)
					}
				}
				if got.body != call.Body {
					t.Errorf("request %d body differs at byte %d\ngot:  %s\nwant: %s", i, firstDifference(got.body, call.Body), got.body, call.Body)
				}
			}
			if len(delays) != len(vector.Delays) {
				t.Fatalf("delays = %v, want %v", delays, vector.Delays)
			}
			for i := range delays {
				if delays[i] != vector.Delays[i] {
					t.Errorf("delay %d = %vms, want %vms", i, delays[i], vector.Delays[i])
				}
			}
			switch {
			case vector.Result != nil:
				if err != nil || id != vector.Result.ID {
					t.Fatalf("Send = %q, %v, want %q", id, err, vector.Result.ID)
				}
			case vector.Error != nil:
				if err == nil {
					t.Fatalf("Send = %q, want error %q", id, vector.Error.Message)
				}
				if err.Error() != vector.Error.Message {
					t.Errorf("error = %q, want %q", err.Error(), vector.Error.Message)
				}
				var configuration *ConfigurationError
				if errors.As(err, &configuration) != vector.Error.Configuration {
					t.Errorf("configuration error = %t, want %t", !vector.Error.Configuration, vector.Error.Configuration)
				}
			}
		})
	}
}

func TestResendSenderStopsWaitingWhenTheContextEnds(t *testing.T) {
	retryAfter := "3600"
	_, server := startResend(t, scriptedResponse{status: 503, retryAfter: &retryAfter})
	sender := NewResendSender(ResendConfig{APIKey: "re_synthetic", From: "News <news@example.invalid>", Endpoint: server.URL, Client: server.Client()})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := sender.Send(ctx, Email{To: "a@example.invalid"}, "key")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 5*time.Second {
		t.Fatalf("Send = %v after %v, want the context error promptly", err, time.Since(started))
	}
}

// The expected delays were checked in Node 22: Number() of each value,
// Headers.get joining repeated headers with ", ", and setTimeout clamping
// (TimeoutOverflowWarning above 2^31-1 ms, at least 1ms).
func TestResendRetryDelayFollowsNodeTimers(t *testing.T) {
	for _, tt := range []struct {
		retryAfter []string
		attempt    int
		want       time.Duration
	}{
		{nil, 0, 650 * time.Millisecond},
		{nil, 1, 1300 * time.Millisecond},
		{[]string{"1", "2"}, 0, 650 * time.Millisecond}, // Headers.get joins: "1, 2" is NaN
		{[]string{"0.0001"}, 0, time.Millisecond},       // setTimeout clamps below 1ms to 1ms
		{[]string{"3000000"}, 0, time.Millisecond},      // beyond 2^31-1 ms Node fires after 1ms
		{[]string{"2147483.647"}, 0, 2147483647 * time.Millisecond},
		{[]string{"+2"}, 0, 2 * time.Second},
		{[]string{"-0x2"}, 0, 650 * time.Millisecond},
		{[]string{"0b11"}, 0, 3 * time.Second},
		{[]string{"0o7"}, 0, 7 * time.Second},
		{[]string{"\u00a02\u2028"}, 0, 2 * time.Second},
	} {
		header := http.Header{}
		for _, value := range tt.retryAfter {
			header.Add("Retry-After", value)
		}
		if got := retryDelay(header, tt.attempt); got != tt.want {
			t.Errorf("retryDelay(%q, %d) = %v, want %v", tt.retryAfter, tt.attempt, got, tt.want)
		}
	}
}

func TestNewResendSenderHasBoundedTimeouts(t *testing.T) {
	sender := NewResendSender(ResendConfig{})
	if sender.endpoint != DefaultResendEndpoint || sender.client.Timeout != ResendAttemptTimeout || ResendAttemptTimeout > time.Minute {
		t.Fatalf("defaults = %q, %v", sender.endpoint, sender.client.Timeout)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -p 2 ./internal/newsletter/`
Expected: FAIL to compile:

```
internal/newsletter/resend_test.go:115:14: undefined: NewResendSender
internal/newsletter/resend_test.go:115:30: undefined: ResendConfig
internal/newsletter/resend_test.go:131:20: undefined: DefaultResendEndpoint
```

- [ ] **Step 3: Implement.** Create `internal/newsletter/resend.go`:

```go
package newsletter

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/jsonbody"
)

const (
	// DefaultResendEndpoint is the URL Node posts every email to.
	DefaultResendEndpoint = "https://api.resend.com/emails"
	// ResendAttemptTimeout bounds one HTTP attempt (connect, request, and
	// response). Node's fetch has no deadline of its own.
	ResendAttemptTimeout = 30 * time.Second
	// resendAttempts is Node's retry budget: three attempts in total.
	resendAttempts = 3
	// maxResendResponseBytes caps how much of a response body is read.
	maxResendResponseBytes = 1 << 20
	// maxTimerDelay is Node's TIMEOUT_MAX (2^31-1 ms); setTimeout fires
	// after 1ms instead of any longer delay.
	maxTimerDelay = 2147483647
)

// ConfigurationError is Node's NewsletterConfigurationError: the newsletter
// is missing a setting it needs for this operation.
type ConfigurationError struct{ message string }

func (e *ConfigurationError) Error() string { return e.message }

var errDeliveryNotConfigured = &ConfigurationError{message: "Newsletter delivery is not configured"}

// fetchError is a transport failure. Node's fetch rejects with
// TypeError("fetch failed") and the sender does not retry it; the message
// stored for the delivery is the same, the cause is kept for logs.
type fetchError struct{ cause error }

func (e *fetchError) Error() string { return "fetch failed" }
func (e *fetchError) Unwrap() error { return e.cause }

// providerError is a Resend error response, with Node's message.
type providerError struct {
	status  int
	message string
}

func (e *providerError) Error() string { return e.message }

// ResendConfig holds the raw settings; like Node they are trimmed on use.
type ResendConfig struct {
	APIKey  string // RESEND_API_KEY
	From    string // NEWSLETTER_FROM
	ReplyTo string // NEWSLETTER_REPLY_TO
	// Endpoint defaults to DefaultResendEndpoint. Tests point it at an
	// httptest server; nothing else should change it.
	Endpoint string
	// Client defaults to a client with ResendAttemptTimeout.
	Client *http.Client
	// Sleep waits between attempts; it defaults to a context-aware timer.
	Sleep func(context.Context, time.Duration) error
}

// ResendSender is Node's createResendSender.
type ResendSender struct {
	apiKey, from, replyTo string
	endpoint              string
	client                *http.Client
	sleep                 func(context.Context, time.Duration) error
}

func NewResendSender(cfg ResendConfig) *ResendSender {
	sender := &ResendSender{
		apiKey: jsTrim(cfg.APIKey), from: jsTrim(cfg.From), replyTo: jsTrim(cfg.ReplyTo),
		endpoint: cfg.Endpoint, client: cfg.Client, sleep: cfg.Sleep,
	}
	if sender.endpoint == "" {
		sender.endpoint = DefaultResendEndpoint
	}
	if sender.client == nil {
		sender.client = &http.Client{Timeout: ResendAttemptTimeout}
	}
	if sender.sleep == nil {
		sender.sleep = sleepContext
	}
	return sender
}

// Send posts email with Node's request body, headers, and Idempotency-Key.
// Like Node it makes at most three attempts, retrying only 429 and 5xx
// responses after Retry-After seconds (or 650ms, then 1300ms); transport
// failures, other statuses, and a 2xx without an id fail at once.
func (s *ResendSender) Send(ctx context.Context, email Email, idempotencyKey string) (string, error) {
	if s.apiKey == "" || s.from == "" {
		return "", errDeliveryNotConfigured
	}
	body := s.requestBody(email)
	for attempt := 0; attempt < resendAttempts; attempt++ {
		status, header, response, err := s.post(ctx, body, idempotencyKey)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", &fetchError{cause: err}
		}
		ok := status >= 200 && status <= 299
		if ok {
			if response.null {
				return "", &providerError{status: status, message: "Cannot read properties of null (reading 'id')"}
			}
			if id := response.field("id"); id.Truthy() {
				return id.JSString(), nil
			}
		}
		if retryable := status == http.StatusTooManyRequests || status >= 500; !retryable || attempt == resendAttempts-1 {
			return "", &providerError{status: status, message: response.errorMessage(status)}
		}
		if err := s.sleep(ctx, retryDelay(header, attempt)); err != nil {
			return "", err
		}
	}
	return "", &providerError{message: "Email provider retry limit reached"}
}

// requestBody is JSON.stringify of Node's request object, key order included.
func (s *ResendSender) requestBody(email Email) string {
	var body strings.Builder
	field := func(name string) {
		if body.Len() > 1 {
			body.WriteByte(',')
		}
		writeJSONString(&body, name)
		body.WriteByte(':')
	}
	body.WriteByte('{')
	field("from")
	writeJSONString(&body, s.from)
	field("to")
	body.WriteByte('[')
	writeJSONString(&body, email.To)
	body.WriteByte(']')
	field("subject")
	writeJSONString(&body, email.Subject)
	field("html")
	writeJSONString(&body, email.HTML)
	field("text")
	writeJSONString(&body, email.Text)
	if s.replyTo != "" {
		field("reply_to")
		writeJSONString(&body, s.replyTo)
	}
	if email.Headers != nil {
		field("headers")
		body.WriteByte('{')
		for i, header := range email.Headers {
			if i > 0 {
				body.WriteByte(',')
			}
			writeJSONString(&body, header.Name)
			body.WriteByte(':')
			writeJSONString(&body, header.Value)
		}
		body.WriteByte('}')
	}
	if email.Tags != nil {
		field("tags")
		body.WriteByte('[')
		for i, tag := range email.Tags {
			if i > 0 {
				body.WriteByte(',')
			}
			body.WriteString(`{"name":`)
			writeJSONString(&body, tag.Name)
			body.WriteString(`,"value":`)
			writeJSONString(&body, tag.Value)
			body.WriteByte('}')
		}
		body.WriteByte(']')
	}
	body.WriteByte('}')
	return body.String()
}

func (s *ResendSender) post(ctx context.Context, body, idempotencyKey string) (int, http.Header, resendResponse, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, ResendAttemptTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, s.endpoint, strings.NewReader(body))
	if err != nil {
		return 0, nil, resendResponse{}, err
	}
	request.Header.Set("Authorization", "Bearer "+s.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", idempotencyKey)
	response, err := s.client.Do(request)
	if err != nil {
		return 0, nil, resendResponse{}, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResendResponseBytes))
	if err != nil {
		return 0, nil, resendResponse{}, err
	}
	return response.StatusCode, response.Header, parseResendResponse(data), nil
}

// resendResponse is `await response.json().catch(() => ({}))`: a body that
// is not JSON reads as {}; JSON null stays null (property reads on it throw
// in Node); any other non-object value has no properties.
type resendResponse struct {
	null   bool
	fields map[string]json.RawMessage
}

func parseResendResponse(data []byte) resendResponse {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return resendResponse{}
	}
	if value == nil {
		return resendResponse{null: true}
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return resendResponse{}
	}
	return resendResponse{fields: fields}
}

func (r resendResponse) field(name string) jsonbody.Value {
	raw, ok := r.fields[name]
	if !ok {
		return jsonbody.Value{}
	}
	return jsonbody.FromRaw(raw)
}

// errorMessage is new Error(body.message || body.error || `Email provider
// returned ${status}`).message.
func (r resendResponse) errorMessage(status int) string {
	if r.null {
		return "Cannot read properties of null (reading 'message')"
	}
	for _, name := range []string{"message", "error"} {
		if value := r.field(name); value.Truthy() {
			return value.JSString()
		}
	}
	return "Email provider returned " + strconv.Itoa(status)
}

// retryDelay is Node's wait before the next attempt: Number(Retry-After)
// seconds when finite and positive, else 650ms times the attempt number,
// with setTimeout's clamping (below 1ms or above 2^31-1 ms fires after 1ms).
func retryDelay(header http.Header, attempt int) time.Duration {
	milliseconds := 650 * float64(attempt+1)
	if values := header.Values("Retry-After"); len(values) > 0 {
		if seconds, ok := jsNumber(strings.Join(values, ", ")); ok && !math.IsInf(seconds, 0) && seconds > 0 {
			milliseconds = seconds * 1000
		}
	}
	if milliseconds < 1 || milliseconds > maxTimerDelay {
		milliseconds = 1
	}
	return time.Duration(milliseconds * float64(time.Millisecond))
}

var jsDecimalLiteral = regexp.MustCompile(`^[+-]?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

// jsNumber is Number(value) for a string; ok is false for NaN.
func jsNumber(value string) (float64, bool) {
	value = jsTrim(value)
	if value == "" {
		return 0, true
	}
	switch value {
	case "Infinity", "+Infinity":
		return math.Inf(1), true
	case "-Infinity":
		return math.Inf(-1), true
	}
	if len(value) > 2 && value[0] == '0' {
		base := 0
		switch value[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			integer, ok := new(big.Int).SetString(value[2:], base)
			if !ok || strings.ContainsAny(value[2:], "_+-") {
				return 0, false
			}
			parsed, _ := new(big.Float).SetInt(integer).Float64()
			return parsed, true
		}
	}
	if !jsDecimalLiteral.MatchString(value) {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil && !math.IsInf(parsed, 0) {
		return 0, false
	}
	return parsed, true
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
```

- [ ] **Step 4: Run them to verify they pass, with the race detector**

Run: `go test -p 2 ./internal/newsletter/ && go test -p 2 -race ./internal/newsletter/ -run Resend`
Expected: `ok` twice (the fake server records requests under its mutex; read them with `snapshot`).

- [ ] **Step 5: Commit**

```bash
git add internal/newsletter/resend.go internal/newsletter/resend_test.go
git commit -m "Send email through Resend with Node's request body, retries, and errors"
```

---

### Task 8: Signup, confirmation, unsubscribe, and the archive

`store.go` holds Node's SQL verbatim. Node runs each check-then-write pair back to back on one thread; Go runs them in one transaction (the pool has a single connection, so transactions serialize in-process). No transaction spans network I/O: the welcome email is sent after the confirm transaction commits, on a context detached from the request (Node keeps going when the client leaves). The tests replay the recorded Node scenarios with the recorder's seed rows and compare every column of every row, the results, and the welcome emails (confirm uses the tokens Node minted, so links from real emails are covered).

**Files:** Create `internal/newsletter/service_test.go`, `internal/newsletter/store.go`, `internal/newsletter/service.go`

- [ ] **Step 1: Write the failing tests.** Create `internal/newsletter/service_test.go`:

```go
package newsletter

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

const testTokenSecret = "test-newsletter-token-secret-with-32-characters"

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func migratedDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, Migrations()); err != nil {
		t.Fatal(err)
	}
	return db
}

func exec(t *testing.T, db *sql.DB, statement string, args ...any) {
	t.Helper()
	if _, err := db.Exec(statement, args...); err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
}

func mustTime(t testing.TB, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

// recordingSender records every email like the Node recorder's sendEmail
// stub and answers message-<n>, failing for the addresses in fail.
type recordingSender struct {
	mu   sync.Mutex
	sent []sentRecord
	fail map[string]error
}

type sentRecord struct {
	email Email
	key   string
}

func (r *recordingSender) Send(_ context.Context, email Email, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, sentRecord{email: email, key: key})
	if err := r.fail[email.To]; err != nil {
		return "", err
	}
	return "message-" + strconv.Itoa(len(r.sent)), nil
}

func newTestService(t *testing.T, db *sql.DB, sender Sender, cfg ServiceConfig) *Service {
	t.Helper()
	if cfg.SiteURL == "" {
		cfg.SiteURL = "https://aiandtech.news"
	}
	if cfg.Local == nil {
		cfg.Local = istanbulZone(t)
	}
	if cfg.Pace == nil {
		cfg.Pace = func(context.Context) error { return nil }
	}
	service, err := NewService(NewSQLiteStore(db), sender, cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	return service
}

// nodeText maps a string read back from the Node recording to the bytes
// Node stored: a lone surrogate better-sqlite3 wrote as WTF-8 (ED A0 BD)
// was read back as three U+FFFD characters.
func nodeText(value string) string {
	return strings.ReplaceAll(value, "\ufffd\ufffd\ufffd", "\xed\xa0\xbd")
}

func nullable(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func subscriberRows(t *testing.T, db *sql.DB) []subscriberRow {
	t.Helper()
	rows, err := db.Query(`SELECT id, email, status, source_placement, confirmation_sent_at, confirmed_at, unsubscribed_at, created_at, updated_at FROM subscribers ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var result []subscriberRow
	for rows.Next() {
		var row subscriberRow
		var placement, sent, confirmed, unsubscribed sql.NullString
		if err := rows.Scan(&row.ID, &row.Email, &row.Status, &placement, &sent, &confirmed, &unsubscribed, &row.CreatedAt, &row.UpdatedAt); err != nil {
			t.Fatal(err)
		}
		row.SourcePlacement, row.ConfirmationSentAt, row.ConfirmedAt, row.UnsubscribedAt = nullable(placement), nullable(sent), nullable(confirmed), nullable(unsubscribed)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertSubscriberRows(t *testing.T, got, want []subscriberRow) {
	t.Helper()
	for i := range want {
		if want[i].SourcePlacement != nil {
			text := nodeText(*want[i].SourcePlacement)
			want[i].SourcePlacement = &text
		}
	}
	if len(got) != len(want) {
		t.Fatalf("subscribers = %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			gotJSON, _ := json.Marshal(got[i])
			wantJSON, _ := json.Marshal(want[i])
			t.Errorf("subscriber row %d =\n%s\nwant (Node)\n%s", i, gotJSON, wantJSON)
		}
	}
}

func TestSubscribeReplaysNodesSignupScenario(t *testing.T) {
	recorded := golden(t).Subscriptions
	db := migratedDatabase(t)
	// The recorder's seed rows (record-node.ts, recordSubscriptions).
	exec(t, db, `INSERT INTO subscribers (id, email, status, confirmed_at, created_at, updated_at) VALUES (10, 'pending@example.com', 'pending', NULL, 'c', 'u')`)
	exec(t, db, `INSERT INTO subscribers (id, email, status, source_placement, confirmed_at, unsubscribed_at, created_at, updated_at) VALUES (11, 'gone@example.com', 'unsubscribed', 'old', '2026-01-01T00:00:00.000Z', '2026-02-01T00:00:00.000Z', 'c', 'u')`)
	exec(t, db, `INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (12, 'MixedCase@Example.com', 'active', 'c', 'u')`)
	service := newTestService(t, db, &recordingSender{fail: map[string]error{}}, ServiceConfig{})
	for _, outcome := range recorded.Outcomes {
		state, err := service.Subscribe(context.Background(), outcome.Address, outcome.Placement, mustTime(t, outcome.At))
		switch {
		case outcome.Error != nil:
			if !errors.Is(err, ErrInvalidEmail) || err.Error() != outcome.Error.Message {
				t.Errorf("Subscribe(%q) = %q, %v, want %s", outcome.Address, state, err, outcome.Error.Message)
			}
		case err != nil || string(state) != outcome.Result.State:
			t.Errorf("Subscribe(%q) = %q, %v, want %q", outcome.Address, state, err, outcome.Result.State)
		}
	}
	assertSubscriberRows(t, subscriberRows(t, db), recorded.Subscribers)
}

func TestNormalizeEmailMatchesNode(t *testing.T) {
	normalized := golden(t).Subscriptions.Normalized
	if len(normalized) < 20 {
		t.Fatalf("recorded email vectors = %d", len(normalized))
	}
	for _, vector := range normalized {
		got, ok := normalizeEmail(vector.Input)
		switch {
		case vector.Error != nil && ok:
			t.Errorf("normalizeEmail(%q) = %q, want %s", vector.Input, got, *vector.Error)
		case vector.Email != nil && (!ok || got != *vector.Email):
			t.Errorf("normalizeEmail(%q) = %q, %t, want %q", vector.Input, got, ok, *vector.Email)
		}
	}
}

func TestSubscribeRunsWithoutAnyEmailOrTokenSettings(t *testing.T) {
	db := migratedDatabase(t)
	sender := NewResendSender(ResendConfig{Endpoint: "http://127.0.0.1:1/never"})
	service := newTestService(t, db, sender, ServiceConfig{TokenSecret: ""})
	if state, err := service.Subscribe(context.Background(), "reader@example.com", "inline", time.Now()); err != nil || state != StateSubscribed {
		t.Fatalf("Subscribe = %q, %v", state, err)
	}
}

func TestConcurrentSignupsForOneAddressStoreOneRow(t *testing.T) {
	db := migratedDatabase(t)
	service := newTestService(t, db, &recordingSender{}, ServiceConfig{})
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := service.Subscribe(context.Background(), "Same@Example.com", "inline", time.Now()); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM subscribers`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("rows = %d, %v", count, err)
	}
}

func TestConfirmAndUnsubscribeReplayNodeWithNodeMintedTokens(t *testing.T) {
	recorded := golden(t).ConfirmAndUnsubscribe
	db := migratedDatabase(t)
	// The recorder's seed rows (record-node.ts, recordConfirmAndUnsubscribe).
	for _, row := range []struct {
		id                          int64
		email, status               string
		confirmedAt, unsubscribedAt any
	}{
		{1, "pending@example.com", "pending", nil, nil},
		{2, "active@example.com", "active", "2026-01-01T00:00:00.000Z", nil},
		{3, "gone@example.com", "unsubscribed", "2026-01-01T00:00:00.000Z", "2026-02-01T00:00:00.000Z"},
		{4, "pending-fail@example.com", "pending", nil, nil},
	} {
		exec(t, db, `INSERT INTO subscribers (id, email, status, confirmed_at, unsubscribed_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'c', 'u')`,
			row.id, row.email, row.status, row.confirmedAt, row.unsubscribedAt)
	}
	sender := &recordingSender{fail: map[string]error{"pending-fail@example.com": errors.New("provider down")}}
	service := newTestService(t, db, sender, ServiceConfig{TokenSecret: testTokenSecret})
	now := mustTime(t, recorded.Now)
	for _, vector := range recorded.Confirms {
		result, err := service.Confirm(context.Background(), vector.Token, now)
		if err != nil || string(result.State) != vector.Result.State || result.WelcomeSent != vector.Result.WelcomeSent {
			t.Errorf("confirm %s = %+v, %v, want %+v", vector.Name, result, err, vector.Result)
		}
	}
	for _, vector := range recorded.Unsubscribes {
		state, err := service.Unsubscribe(context.Background(), vector.Token, now)
		if err != nil || string(state) != vector.Result {
			t.Errorf("unsubscribe %s = %q, %v, want %q", vector.Name, state, err, vector.Result)
		}
	}
	if len(sender.sent) != len(recorded.Sent) {
		t.Fatalf("sent %d emails, want %d", len(sender.sent), len(recorded.Sent))
	}
	for i, want := range recorded.Sent {
		if sender.sent[i].key != want.IdempotencyKey {
			t.Errorf("welcome %d idempotency key = %q, want %q", i, sender.sent[i].key, want.IdempotencyKey)
		}
		assertEmail(t, "welcome "+want.To, sender.sent[i].email, want.nodeEmail)
	}
	assertSubscriberRows(t, subscriberRows(t, db), recorded.Subscribers)
}

func TestEditionsReplayNode(t *testing.T) {
	recorded := golden(t).Editions
	db := migratedDatabase(t)
	// The recorder's seed rows (record-node.ts, recordEditions).
	for _, row := range [][4]string{
		{"2026-09-17", "Object", `{"not":"array"}`, "2026-09-17T05:00:00.000Z"},
		{"2026-09-18", "Malformed", "{malformed", "2026-09-18T05:00:00.000Z"},
		{"2026-09-19", "Loose", ` [ {"title":"T","extra":[1,2],"readingMinutes":1.5}, 7, null ] `, "2026-09-19T05:00:00.000Z"},
		{"2026-09-20", "Typed", `[{"title":"<T&>","slug":"s","excerpt":"e","category":"c","readingMinutes":2}]`, "2026-09-20T05:00:00.000Z"},
		{"2026-09-16T", "Odd key", "[]", "x"},
	} {
		exec(t, db, `INSERT INTO newsletter_editions (edition_key, subject, articles, created_at) VALUES (?, ?, ?, ?)`, row[0], row[1], row[2], row[3])
	}
	service := newTestService(t, db, &recordingSender{}, ServiceConfig{})
	for _, vector := range recorded.Lists {
		editions, err := service.ListEditions(context.Background(), float64(vector.Limit))
		if err != nil {
			t.Fatal(err)
		}
		assertSameJSON(t, "list "+strconv.Itoa(vector.Limit), editions, vector.Editions)
	}
	for _, vector := range recorded.Gets {
		edition, ok, err := service.Edition(context.Background(), vector.Key)
		if err != nil {
			t.Fatal(err)
		}
		if string(vector.Edition) == "null" {
			if ok {
				t.Errorf("edition %q found, Node: null", vector.Key)
			}
			continue
		}
		assertSameJSON(t, "edition "+vector.Key, edition, vector.Edition)
	}
	edition, _, _ := service.Edition(context.Background(), "2026-09-19")
	assertSameJSON(t, "JSON text", edition, json.RawMessage(recorded.JSONText))
}

// assertSameJSON compares the JSON encoding of got with Node's JSON,
// semantically (encoding/json escapes <, >, & where JSON.stringify does not).
func assertSameJSON(t *testing.T, name string, got, want any) {
	t.Helper()
	gotData, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantData, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var gotValue, wantValue any
	gotDecoder := json.NewDecoder(bytes.NewReader(gotData))
	gotDecoder.UseNumber()
	wantDecoder := json.NewDecoder(bytes.NewReader(wantData))
	wantDecoder.UseNumber()
	if gotDecoder.Decode(&gotValue) != nil || wantDecoder.Decode(&wantValue) != nil || !reflect.DeepEqual(gotValue, wantValue) {
		t.Errorf("%s = %s\nwant (Node) %s", name, gotData, wantData)
	}
}

func TestSiteOriginMatchesNodesSafeSiteURL(t *testing.T) {
	for _, vector := range golden(t).SiteURLs {
		input := ""
		if vector.Input != nil {
			input = *vector.Input
		}
		got, err := SiteOrigin(input)
		switch {
		case input == "ftp://localhost":
			// Documented difference: Go accepts only http and https.
			if err == nil {
				t.Errorf("SiteOrigin(%q) = %q, want a configuration error", input, got)
			}
		case vector.Error != nil:
			if err == nil {
				t.Errorf("SiteOrigin(%q) = %q, want error %q", input, got, vector.Error.Message)
			}
			var configuration *ConfigurationError
			if !errors.As(err, &configuration) {
				t.Errorf("SiteOrigin(%q) error %v is not a configuration error", input, err)
			}
			if vector.Error.Configuration && err.Error() != vector.Error.Message {
				t.Errorf("SiteOrigin(%q) error = %q, want %q", input, err, vector.Error.Message)
			}
		default:
			if err != nil || got != *vector.SiteURL {
				t.Errorf("SiteOrigin(%q) = %q, %v, want %q", input, got, err, *vector.SiteURL)
			}
		}
	}
}

func TestTokenSecretRequirementMatchesNode(t *testing.T) {
	recorded := golden(t).TokenSecrets
	db := migratedDatabase(t)
	for _, vector := range recorded.Results {
		secret := ""
		if vector.Secret != nil {
			secret = *vector.Secret
		}
		service := newTestService(t, db, &recordingSender{}, ServiceConfig{TokenSecret: secret})
		state, err := service.Unsubscribe(context.Background(), "a.b", time.Now())
		if vector.Error != nil {
			var configuration *ConfigurationError
			if !errors.As(err, &configuration) || err.Error() != vector.Error.Message {
				t.Errorf("secret %q: Unsubscribe = %q, %v, want %q", secret, state, err, vector.Error.Message)
			}
			continue
		}
		if err != nil || string(state) != *vector.Unsubscribe {
			t.Errorf("secret %q: Unsubscribe = %q, %v, want %q", secret, state, err, *vector.Unsubscribe)
		}
	}
	// The trimmed secret is the HMAC key.
	exec(t, db, `INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (1, 'a@b.c', 'active', 'c', 'u')`)
	service := newTestService(t, db, &recordingSender{}, ServiceConfig{TokenSecret: "  " + testTokenSecret + "  "})
	state, err := service.Unsubscribe(context.Background(), CreateToken(1, PurposeUnsubscribe, testTokenSecret, nil), mustTime(t, "2026-09-20T12:00:00.000Z"))
	if err != nil || string(state) != recorded.TrimmedKeyResult {
		t.Fatalf("padded secret: %q, %v, want %q", state, err, recorded.TrimmedKeyResult)
	}
}

func TestConfirmDoesNotSendTwiceForConcurrentClicks(t *testing.T) {
	db := migratedDatabase(t)
	exec(t, db, `INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (1, 'pending@example.com', 'pending', 'c', 'u')`)
	sender := &recordingSender{}
	service := newTestService(t, db, sender, ServiceConfig{TokenSecret: testTokenSecret})
	token := CreateToken(1, PurposeConfirm, testTokenSecret, nil)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := service.Confirm(context.Background(), token, time.Now()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(sender.sent) != 1 {
		t.Fatalf("welcome emails = %d, want 1", len(sender.sent))
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -p 2 ./internal/newsletter/`
Expected: FAIL to compile:

```
internal/newsletter/service_test.go:74:54: undefined: Sender
internal/newsletter/service_test.go:74:66: undefined: ServiceConfig
internal/newsletter/service_test.go:74:82: undefined: Service
```

- [ ] **Step 3: Implement.** Create `internal/newsletter/store.go`:

```go
package newsletter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// SQLiteStore holds the newsletter SQL. Every statement is Node's
// (apps/server/src/newsletter/service.ts) with the same parameters. The
// check-then-write pairs Node runs back to back on its single thread run
// in one transaction here, so concurrent Go requests cannot interleave them.
type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{db: db} }

type subscriber struct {
	id     int64
	email  string
	status string
}

func (s *SQLiteStore) inTx(ctx context.Context, run func(*sql.Tx) error) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()
	if err = run(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func scanSubscriber(row *sql.Row) (subscriber, bool, error) {
	var found subscriber
	var status sql.NullString
	err := row.Scan(&found.id, &found.email, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return subscriber{}, false, nil
	}
	if err != nil {
		return subscriber{}, false, err
	}
	found.status = status.String
	return found, true, nil
}

// Subscribe is requestSubscription's SQL: an active address is left alone,
// any other existing row (pending from the old double opt-in, or
// unsubscribed) is reactivated, and a new address is inserted as active.
func (s *SQLiteStore) Subscribe(ctx context.Context, email, source, timestamp string) (SubscriptionState, error) {
	state := StateSubscribed
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		existing, found, err := scanSubscriber(tx.QueryRowContext(ctx, `SELECT id, email, status FROM subscribers WHERE lower(email) = ?`, email))
		if err != nil {
			return fmt.Errorf("find subscriber: %w", err)
		}
		if found && existing.status == "active" {
			state = StateAlreadyActive
			return nil
		}
		if found {
			_, err = tx.ExecContext(ctx, `UPDATE subscribers
           SET status = 'active',
               source_placement = ?,
               confirmed_at = COALESCE(confirmed_at, ?),
               unsubscribed_at = NULL,
               updated_at = ?
           WHERE id = ?`, source, timestamp, timestamp, existing.id)
			if err != nil {
				return fmt.Errorf("reactivate subscriber: %w", err)
			}
			return nil
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO subscribers (email, status, source_placement, confirmed_at, created_at, updated_at)
         VALUES (?, 'active', ?, ?, ?, ?)`, email, source, timestamp, timestamp, timestamp)
		if err != nil {
			return fmt.Errorf("insert subscriber: %w", err)
		}
		return nil
	})
	return state, err
}

// Confirm is confirmSubscription's SQL. It returns the subscriber's email
// when this call confirmed it (the caller then sends the welcome email).
func (s *SQLiteStore) Confirm(ctx context.Context, id int64, timestamp string) (ConfirmationState, string, error) {
	state := ConfirmationInvalid
	var email string
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		existing, found, err := scanSubscriber(tx.QueryRowContext(ctx, `SELECT id, email, status FROM subscribers WHERE id = ?`, id))
		if err != nil {
			return fmt.Errorf("find subscriber: %w", err)
		}
		switch {
		case !found || existing.status == "unsubscribed":
			return nil
		case existing.status == "active":
			state = ConfirmationAlreadyConfirmed
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE subscribers
         SET status = 'active', confirmed_at = ?, unsubscribed_at = NULL, updated_at = ?
         WHERE id = ?`, timestamp, timestamp, existing.id); err != nil {
			return fmt.Errorf("confirm subscriber: %w", err)
		}
		state, email = ConfirmationConfirmed, existing.email
		return nil
	})
	return state, email, err
}

// Unsubscribe is unsubscribe's SQL.
func (s *SQLiteStore) Unsubscribe(ctx context.Context, id int64, timestamp string) (UnsubscribeState, error) {
	state := UnsubscribeInvalid
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		existing, found, err := scanSubscriber(tx.QueryRowContext(ctx, `SELECT id, email, status FROM subscribers WHERE id = ?`, id))
		if err != nil {
			return fmt.Errorf("find subscriber: %w", err)
		}
		switch {
		case !found:
			return nil
		case existing.status == "unsubscribed":
			state = UnsubscribeAlreadyUnsubscribed
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE subscribers
         SET status = 'unsubscribed', unsubscribed_at = ?, updated_at = ?
         WHERE id = ?`, timestamp, timestamp, existing.id); err != nil {
			return fmt.Errorf("unsubscribe: %w", err)
		}
		state = UnsubscribeUnsubscribed
		return nil
	})
	return state, err
}

// ListEditions is listEditions' SQL; limit is already clamped to 1..100.
func (s *SQLiteStore) ListEditions(ctx context.Context, limit int) ([]Edition, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT edition_key, subject, articles, created_at
         FROM newsletter_editions
         ORDER BY edition_key DESC
         LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list editions: %w", err)
	}
	defer rows.Close()
	editions := []Edition{}
	for rows.Next() {
		var key, subject, articles, createdAt string
		if err := rows.Scan(&key, &subject, &articles, &createdAt); err != nil {
			return nil, fmt.Errorf("scan edition: %w", err)
		}
		editions = append(editions, toEdition(key, subject, articles, createdAt))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list editions: %w", err)
	}
	return editions, nil
}

// Edition is getEdition's SQL.
func (s *SQLiteStore) Edition(ctx context.Context, key string) (Edition, bool, error) {
	var subject, articles, createdAt string
	err := s.db.QueryRowContext(ctx, `SELECT edition_key, subject, articles, created_at FROM newsletter_editions WHERE edition_key = ?`, key).
		Scan(&key, &subject, &articles, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Edition{}, false, nil
	}
	if err != nil {
		return Edition{}, false, fmt.Errorf("get edition: %w", err)
	}
	return toEdition(key, subject, articles, createdAt), true, nil
}

// toEdition is Node's toEdition: the stored articles JSON is passed through
// when it is an array, whatever its elements; anything else (malformed JSON,
// an object) becomes [] so a bad row cannot take the archive down.
func toEdition(key, subject, articles, createdAt string) Edition {
	edition := Edition{Edition: key, Subject: subject, Articles: json.RawMessage("[]"), CreatedAt: createdAt}
	trimmed := strings.TrimLeft(articles, " \t\r\n")
	if strings.HasPrefix(trimmed, "[") && json.Valid([]byte(articles)) {
		edition.Articles = json.RawMessage(strings.TrimSpace(articles))
	}
	return edition
}
```

Create `internal/newsletter/service.go`:

```go
package newsletter

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// DefaultSiteURL is Node's NEWSLETTER_SITE_URL default.
	DefaultSiteURL = "https://aiandtech.news"
	// DefaultPacing is Node's pause between two digest deliveries, which
	// keeps the digest under Resend's rate limit.
	DefaultPacing = 550 * time.Millisecond
)

// ErrInvalidEmail is Node's TypeError("Valid email required").
var ErrInvalidEmail = errors.New("Valid email required")

type SubscriptionState string

const (
	StateSubscribed    SubscriptionState = "subscribed"
	StateAlreadyActive SubscriptionState = "already_active"
)

type ConfirmationState string

const (
	ConfirmationConfirmed        ConfirmationState = "confirmed"
	ConfirmationAlreadyConfirmed ConfirmationState = "already_confirmed"
	ConfirmationInvalid          ConfirmationState = "invalid"
)

// ConfirmationResult is the confirm route's JSON body.
type ConfirmationResult struct {
	State       ConfirmationState `json:"state"`
	WelcomeSent bool              `json:"welcomeSent"`
}

type UnsubscribeState string

const (
	UnsubscribeUnsubscribed        UnsubscribeState = "unsubscribed"
	UnsubscribeAlreadyUnsubscribed UnsubscribeState = "already_unsubscribed"
	UnsubscribeInvalid             UnsubscribeState = "invalid"
)

// Edition is one archived digest. Articles is the stored JSON array,
// passed through like Node's JSON.parse/JSON.stringify round trip.
type Edition struct {
	Edition   string          `json:"edition"`
	Subject   string          `json:"subject"`
	Articles  json.RawMessage `json:"articles"`
	CreatedAt string          `json:"createdAt"`
}

// Sender delivers one email; it returns the provider's message id.
type Sender interface {
	Send(ctx context.Context, email Email, idempotencyKey string) (string, error)
}

// ServiceConfig holds the raw settings; like Node they are trimmed on use.
type ServiceConfig struct {
	SiteURL     string // NEWSLETTER_SITE_URL
	TokenSecret string // NEWSLETTER_TOKEN_SECRET
	// Local is the zone for published_at values without an offset, which
	// V8's Date.parse reads in the process time zone. Defaults to time.Local.
	Local *time.Location
	// Pace waits between digest deliveries (Node: 550ms). Defaults to
	// DefaultPacing with a context-aware timer.
	Pace func(context.Context) error
}

// Service is Node's NewsletterService.
type Service struct {
	store       *SQLiteStore
	sender      Sender
	siteURL     string
	tokenSecret string
	local       *time.Location
	pace        func(context.Context) error
	logger      *slog.Logger
}

// NewService validates NEWSLETTER_SITE_URL like Node's constructor does, so
// a bad value fails composition (Node fails to start).
func NewService(store *SQLiteStore, sender Sender, cfg ServiceConfig, logger *slog.Logger) (*Service, error) {
	if store == nil || sender == nil || logger == nil {
		return nil, errors.New("newsletter: store, sender, and logger are required")
	}
	siteURL, err := SiteOrigin(cfg.SiteURL)
	if err != nil {
		return nil, err
	}
	service := &Service{store: store, sender: sender, siteURL: siteURL, tokenSecret: cfg.TokenSecret, local: cfg.Local, pace: cfg.Pace, logger: logger}
	if service.local == nil {
		service.local = time.Local
	}
	if service.pace == nil {
		service.pace = func(ctx context.Context) error { return sleepContext(ctx, DefaultPacing) }
	}
	return service, nil
}

// SiteOrigin is Node's safeSiteUrl: the origin of NEWSLETTER_SITE_URL
// (default https://aiandtech.news), which must be https unless the host is
// localhost. Go accepts only http and https URLs with ASCII hosts; Node's
// WHATWG parser would also take other schemes on localhost and IDN hosts.
func SiteOrigin(raw string) (string, error) {
	value := jsTrim(raw)
	if value == "" {
		value = DefaultSiteURL
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.Opaque != "" {
		return "", &ConfigurationError{message: "NEWSLETTER_SITE_URL must be a valid URL"}
	}
	scheme := strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(parsed.Hostname())
	if scheme != "https" && hostname != "localhost" {
		return "", &ConfigurationError{message: "NEWSLETTER_SITE_URL must use HTTPS"}
	}
	if scheme != "https" && scheme != "http" {
		return "", &ConfigurationError{message: "NEWSLETTER_SITE_URL must be an http or https URL"}
	}
	for _, r := range hostname {
		if r >= utf8.RuneSelf {
			return "", &ConfigurationError{message: "NEWSLETTER_SITE_URL must use an ASCII host name"}
		}
	}
	if strings.Contains(hostname, ":") {
		hostname = "[" + hostname + "]"
	}
	origin := scheme + "://" + hostname
	if port := parsed.Port(); port != "" && !(scheme == "https" && port == "443") && !(scheme == "http" && port == "80") {
		number, err := strconv.Atoi(port)
		if err != nil || number > 65535 {
			return "", &ConfigurationError{message: "NEWSLETTER_SITE_URL must be a valid URL"}
		}
		origin += ":" + strconv.Itoa(number)
	}
	return origin, nil
}

// requireTokenSecret is Node's requireTokenSecret: the trimmed secret must
// have at least 32 UTF-16 code units.
func (s *Service) requireTokenSecret() (string, error) {
	secret := jsTrim(s.tokenSecret)
	if utf16Length(secret) < 32 {
		return "", &ConfigurationError{message: "NEWSLETTER_TOKEN_SECRET must contain at least 32 characters"}
	}
	return secret, nil
}

// normalizeEmail is Node's normalizeEmail: trimmed, lowercased, matching
// /^[^\s@]+@[^\s@]+\.[^\s@]+$/, and at most 254 UTF-16 code units.
func normalizeEmail(raw string) (string, bool) {
	value := jsToLower(jsTrim(raw))
	at := strings.IndexByte(value, '@')
	if at <= 0 || utf16Length(value) > 254 || strings.ContainsFunc(value, isJSWhitespace) {
		return "", false
	}
	domain := value[at+1:]
	if strings.IndexByte(domain, '@') >= 0 {
		return "", false
	}
	// [^\s@]+\.[^\s@]+: some dot with a character before and after it.
	if len(domain) < 3 || !strings.Contains(domain[1:len(domain)-1], ".") {
		return "", false
	}
	return value, true
}

func (s *Service) unsubscribeURL(subscriberID int64, secret string) string {
	return s.siteURL + "/api/newsletter/unsubscribe?token=" + encodeURIComponent(CreateToken(subscriberID, PurposeUnsubscribe, secret, nil))
}

// Subscribe is requestSubscription: the address becomes active at once and
// nothing is sent, so it works without any email or token settings.
func (s *Service) Subscribe(ctx context.Context, rawEmail, placement string, now time.Time) (SubscriptionState, error) {
	email, ok := normalizeEmail(rawEmail)
	if !ok {
		return "", ErrInvalidEmail
	}
	return s.store.Subscribe(ctx, email, sliceUTF16(placement, 80), isoTimestamp(now))
}

// Confirm is confirmSubscription, kept for confirm links in emails sent by
// the old double opt-in flow. Confirming a pending subscriber sends the
// welcome email (idempotency key newsletter-welcome-<id>); a failed send is
// logged and reported as welcomeSent: false, never as an error.
func (s *Service) Confirm(ctx context.Context, token string, now time.Time) (ConfirmationResult, error) {
	secret, err := s.requireTokenSecret()
	if err != nil {
		return ConfirmationResult{}, err
	}
	id, ok := VerifyToken(token, PurposeConfirm, secret, now)
	if !ok {
		return ConfirmationResult{State: ConfirmationInvalid}, nil
	}
	state, email, err := s.store.Confirm(ctx, id, isoTimestamp(now))
	if err != nil || state != ConfirmationConfirmed {
		return ConfirmationResult{State: state}, err
	}
	unsubscribeURL := s.unsubscribeURL(id, secret)
	if _, err := s.sender.Send(context.WithoutCancel(ctx), WelcomeEmail(email, s.siteURL, unsubscribeURL), "newsletter-welcome-"+strconv.FormatInt(id, 10)); err != nil {
		s.logger.ErrorContext(ctx, "Failed to send newsletter welcome email", "error", err, "subscriber_id", id)
		return ConfirmationResult{State: ConfirmationConfirmed}, nil
	}
	return ConfirmationResult{State: ConfirmationConfirmed, WelcomeSent: true}, nil
}

// Unsubscribe is unsubscribe: a valid unsubscribe token for any existing
// subscriber (pending ones included) unsubscribes it.
func (s *Service) Unsubscribe(ctx context.Context, token string, now time.Time) (UnsubscribeState, error) {
	secret, err := s.requireTokenSecret()
	if err != nil {
		return "", err
	}
	id, ok := VerifyToken(token, PurposeUnsubscribe, secret, now)
	if !ok {
		return UnsubscribeInvalid, nil
	}
	return s.store.Unsubscribe(ctx, id, isoTimestamp(now))
}

// ListEditions is listEditions: newest edition key first, limit clamped to 1..100.
func (s *Service) ListEditions(ctx context.Context, limit float64) ([]Edition, error) {
	return s.store.ListEditions(ctx, int(min(max(limit, 1), 100)))
}

// Edition is getEdition; ok is false when the key does not exist.
func (s *Service) Edition(ctx context.Context, key string) (Edition, bool, error) {
	return s.store.Edition(ctx, key)
}
```

- [ ] **Step 4: Run them to verify they pass**

Run: `go test -p 2 ./internal/newsletter/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/newsletter/store.go internal/newsletter/service.go internal/newsletter/service_test.go
git commit -m "Port newsletter signup, confirmation, unsubscribe, and the edition archive"
```

---

### Task 9: The daily digest

Node's `sendDailyDigest`, with its SQL, its filter, `slice(0, 5)`, the edition upsert before any delivery, and the per-subscriber loop. Node's `SELECT status` + upsert becomes one conditional upsert (`… DO UPDATE SET status = 'sending', error = NULL WHERE status <> 'sent'`; zero rows changed means `sent`, so skipped). The outcome of each send is written on a context detached from cancellation with a 10s bound, so Go never leaves a row `sending` because of its own shutdown. Runs are serialized by `digestMu`, detached from the caller's context (Node does not stop when the cron's HTTP request goes away), and bounded by the lifecycle context `Bind` sets (shutdown stops the run between deliveries). The 550ms pause is injected (`ServiceConfig.Pace`) so tests count pauses instead of sleeping.

The tests replay the four recorded digest scenarios and compare the result counts, the edition row byte for byte (the stored articles JSON is `JSON.stringify` output), every delivery row, every email (HTML and text), and the pauses. Four Go-only tests pin serialization, request detachment, shutdown, and the token-secret check.

**Files:** Create `internal/newsletter/digest_test.go`, `internal/newsletter/digest.go`; modify `internal/newsletter/service.go`

- [ ] **Step 1: Write the failing tests.** Create `internal/newsletter/digest_test.go`:

```go
package newsletter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// createContentTables creates Node's categories, authors, and articles
// tables (apps/server/src/db.ts:18-56); the newsletter only reads them.
func createContentTables(t *testing.T, db *sql.DB) {
	t.Helper()
	exec(t, db, `CREATE TABLE categories (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      name TEXT NOT NULL,
      slug TEXT NOT NULL UNIQUE,
      description TEXT NOT NULL DEFAULT '',
      color TEXT NOT NULL DEFAULT '#6366f1'
    )`)
	exec(t, db, `CREATE TABLE authors (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      name TEXT NOT NULL,
      email TEXT NOT NULL UNIQUE,
      password_hash TEXT NOT NULL,
      avatar TEXT,
      bio TEXT,
      role TEXT NOT NULL DEFAULT 'editor' CHECK(role IN ('admin', 'editor'))
    )`)
	exec(t, db, `CREATE TABLE articles (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      title TEXT NOT NULL,
      slug TEXT NOT NULL UNIQUE,
      excerpt TEXT NOT NULL DEFAULT '',
      content TEXT NOT NULL DEFAULT '',
      featured_image TEXT,
      category_id INTEGER NOT NULL,
      author_id INTEGER NOT NULL,
      status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft', 'published', 'scheduled')),
      published_at TEXT,
      meta_title TEXT,
      meta_description TEXT,
      source TEXT,
      source_url TEXT,
      view_count INTEGER NOT NULL DEFAULT 0,
      created_at TEXT NOT NULL DEFAULT (datetime('now')),
      updated_at TEXT NOT NULL DEFAULT (datetime('now')),
      FOREIGN KEY (category_id) REFERENCES categories(id),
      FOREIGN KEY (author_id) REFERENCES authors(id)
    )`)
	exec(t, db, `INSERT INTO authors (id, name, email, password_hash) VALUES (1, 'A', 'a@example.invalid', 'x')`)
}

func words(count int) string {
	return strings.TrimSuffix(strings.Repeat("word ", count), " ")
}

func insertArticle(t *testing.T, db *sql.DB, title, slug, excerpt, content string, category int, status string, publishedAt any) {
	t.Helper()
	exec(t, db, `INSERT INTO articles (title, slug, excerpt, content, category_id, author_id, status, published_at) VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
		title, slug, excerpt, content, category, status, publishedAt)
}

// countingPace records how often the digest paused between deliveries.
type countingPace struct{ calls atomic.Int64 }

func (c *countingPace) pace(context.Context) error { c.calls.Add(1); return nil }

func (c *countingPace) delays() []int {
	delays := make([]int, c.calls.Load())
	for i := range delays {
		delays[i] = int(DefaultPacing / time.Millisecond)
	}
	return delays
}

func editionRows(t *testing.T, db *sql.DB) []editionRow {
	t.Helper()
	rows, err := db.Query(`SELECT id, edition_key, subject, articles, created_at FROM newsletter_editions ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var result []editionRow
	for rows.Next() {
		var row editionRow
		if err := rows.Scan(&row.ID, &row.EditionKey, &row.Subject, &row.Articles, &row.CreatedAt); err != nil {
			t.Fatal(err)
		}
		result = append(result, row)
	}
	return result
}

func deliveryRows(t *testing.T, db *sql.DB) []deliveryRow {
	t.Helper()
	rows, err := db.Query(`SELECT subscriber_id, edition_key, status, provider_message_id, error, created_at, sent_at FROM newsletter_deliveries ORDER BY subscriber_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var result []deliveryRow
	for rows.Next() {
		var row deliveryRow
		var provider, message, sentAt sql.NullString
		if err := rows.Scan(&row.SubscriberID, &row.EditionKey, &row.Status, &provider, &message, &row.CreatedAt, &sentAt); err != nil {
			t.Fatal(err)
		}
		row.ProviderMessageID, row.Error, row.SentAt = nullable(provider), nullable(message), nullable(sentAt)
		result = append(result, row)
	}
	return result
}

func assertDeliveryRows(t *testing.T, name string, got, want []deliveryRow) {
	t.Helper()
	for i := range want {
		if want[i].Error != nil {
			text := nodeText(*want[i].Error)
			want[i].Error = &text
		}
	}
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.Marshal(got)
		wantJSON, _ := json.Marshal(want)
		t.Errorf("%s deliveries =\n%s\nwant (Node)\n%s", name, gotJSON, wantJSON)
	}
}

func assertResult(t *testing.T, name string, got DigestResult, want goldenDigestResult) {
	t.Helper()
	if got.Edition != want.Edition || got.Articles != want.Articles || got.Sent != want.Sent || got.Skipped != want.Skipped || got.Failed != want.Failed {
		t.Errorf("%s result = %+v, want %+v", name, got, want)
	}
}

func assertEditions(t *testing.T, got, want []editionRow, compareIDs bool) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("editions = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if !compareIDs {
			got[i].ID = want[i].ID
		}
		if got[i] != want[i] {
			t.Errorf("edition row %d =\n%+v\nwant (Node, byte for byte)\n%+v", i, got[i], want[i])
		}
	}
}

func assertSent(t *testing.T, got []sentRecord, want []sentEmail) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("sent %d emails, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].key != want[i].IdempotencyKey || got[i].email.To != want[i].To {
			t.Errorf("email %d = %s %s, want %s %s", i, got[i].email.To, got[i].key, want[i].To, want[i].IdempotencyKey)
		}
		if want[i].HTML != "" {
			expected := nodeEmail{To: want[i].To, Subject: want[i].Subject, HTML: want[i].HTML, Text: want[i].Text, Headers: want[i].Headers, Tags: want[i].Tags}
			if expected.Headers == nil && expected.Tags == nil {
				// This recording kept the rendered content only.
				expected.Headers, expected.Tags = headerMap(got[i].email.Headers), tagsOf(got[i].email.Tags)
			}
			assertEmail(t, "digest to "+want[i].To, got[i].email, expected)
		}
	}
}

func TestDigestSelectsArticlesLikeNode(t *testing.T) {
	recorded := golden(t).DigestSelection
	db := migratedDatabase(t)
	createContentTables(t, db)
	exec(t, db, `INSERT INTO categories (id, name, slug) VALUES (1, 'AI', 'ai'), (2, 'Programming', 'programming')`)
	for _, raw := range recorded.Articles {
		var article []json.RawMessage
		if err := json.Unmarshal(raw, &article); err != nil {
			t.Fatal(err)
		}
		var slug, status string
		var publishedAt *string
		var category, count int
		for i, target := range []any{&slug, &status, &publishedAt, &category, &count} {
			if err := json.Unmarshal(article[i], target); err != nil {
				t.Fatal(err)
			}
		}
		insertArticle(t, db, "Title "+slug, slug, "Excerpt "+slug, "<p>"+words(count)+"</p>", category, status, publishedAt)
	}
	exec(t, db, `INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (1, 'one@example.com', 'active', 'x', 'x')`)
	sender := &recordingSender{}
	pace := &countingPace{}
	service := newTestService(t, db, sender, ServiceConfig{TokenSecret: testTokenSecret, Pace: pace.pace})
	result, err := service.SendDailyDigest(context.Background(), mustTime(t, recorded.Now))
	if err != nil {
		t.Fatal(err)
	}
	assertResult(t, "selection", result, recorded.Result)
	assertEditions(t, editionRows(t, db), recorded.Editions, true)
	assertDeliveryRows(t, "selection", deliveryRows(t, db), recorded.Delivery)
	assertSent(t, sender.sent, recorded.Sent)
	if !reflect.DeepEqual(pace.delays(), append([]int{}, recorded.Delays...)) {
		t.Errorf("pauses = %v, want %v", pace.delays(), recorded.Delays)
	}
}

func TestDigestOnlyConsidersTheTwentyNewestPublishedAtTextsLikeNode(t *testing.T) {
	recorded := golden(t).DigestWindow
	db := migratedDatabase(t)
	createContentTables(t, db)
	exec(t, db, `INSERT INTO categories (id, name, slug) VALUES (1, 'AI', 'ai')`)
	for i := 0; i < 20; i++ {
		insertArticle(t, db, "Future "+strconv.Itoa(i), "future-"+strconv.Itoa(i), "x", "", 1, "published", "2027-01-"+strconv.Itoa(101 + i)[1:]+"T00:00:00.000Z")
	}
	insertArticle(t, db, "Recent", "recent", "x", "", 1, "published", "2026-09-20T04:00:00.000Z")
	service := newTestService(t, db, &recordingSender{}, ServiceConfig{TokenSecret: testTokenSecret})
	result, err := service.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:00:00.000Z"))
	if err != nil {
		t.Fatal(err)
	}
	assertResult(t, "window", result, recorded.Result)
	if rows := editionRows(t, db); len(rows) != 0 {
		t.Errorf("an empty digest recorded an edition: %+v", rows)
	}
}

func TestDigestTakesTheFiveNewestAndRendersThemLikeNode(t *testing.T) {
	recorded := golden(t).DigestTopFive
	db := migratedDatabase(t)
	createContentTables(t, db)
	exec(t, db, `INSERT INTO categories (id, name, slug) VALUES (1, 'AI', 'ai'), (2, 'Code', 'code')`)
	for index, stamp := range recorded.Stamps {
		insertArticle(t, db, "Story "+strconv.Itoa(index), "story-"+strconv.Itoa(index), "Excerpt "+strconv.Itoa(index), "<p>"+words(index*150)+"</p>", index%2+1, "published", stamp)
	}
	exec(t, db, `INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (3, 'three@example.com', 'active', 'x', 'x')`)
	sender := &recordingSender{}
	service := newTestService(t, db, sender, ServiceConfig{SiteURL: "https://aiandtech.news/some/path?q=1", TokenSecret: testTokenSecret})
	result, err := service.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:00:00.000Z"))
	if err != nil {
		t.Fatal(err)
	}
	assertResult(t, "top five", result, recorded.Result)
	assertEditions(t, editionRows(t, db), recorded.Editions, false)
	assertSent(t, sender.sent, recorded.Sent)
}

// seedDeliveryLifecycle reproduces record-node.ts recordDeliveryLifecycle.
func seedDeliveryLifecycle(t *testing.T, db *sql.DB) {
	t.Helper()
	createContentTables(t, db)
	exec(t, db, `INSERT INTO categories (id, name, slug) VALUES (1, 'AI', 'ai')`)
	insertArticle(t, db, "Lead story", "lead", "Lead excerpt", "<p>lead</p>", 1, "published", "2026-09-20 04:00:00")
	for _, subscriber := range golden(t).DeliveryLifecycle.Subscribers {
		var id int64
		var email, status string
		for i, target := range []any{&id, &email, &status} {
			if err := json.Unmarshal(subscriber[i], target); err != nil {
				t.Fatal(err)
			}
		}
		exec(t, db, `INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (?, ?, ?, 'x', 'x')`, id, email, status)
	}
	exec(t, db, `INSERT INTO newsletter_deliveries (subscriber_id, edition_key, status, provider_message_id, error, created_at)
    VALUES (5, '2026-09-20', 'sending', 'old-id', 'old error', '2026-09-20T04:59:00.000Z')`)
	exec(t, db, `INSERT INTO newsletter_deliveries (subscriber_id, edition_key, status, provider_message_id, created_at, sent_at)
    VALUES (6, '2026-09-20', 'sent', 'sent-id', '2026-09-20T04:59:00.000Z', '2026-09-20T04:59:30.000Z')`)
}

func TestDigestDeliveryStatesAndRetriesMatchNode(t *testing.T) {
	recorded := golden(t).DeliveryLifecycle
	db := migratedDatabase(t)
	seedDeliveryLifecycle(t, db)
	sender := &recordingSender{fail: map[string]error{"fails@example.com": errors.New(strings.Repeat("\u00e9", 499) + "\U0001f680 provider rejected")}}
	pace := &countingPace{}
	service := newTestService(t, db, sender, ServiceConfig{TokenSecret: testTokenSecret, Pace: pace.pace})

	first, err := service.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:00:00.000Z"))
	if err != nil {
		t.Fatal(err)
	}
	assertResult(t, "first run", first, recorded.First)
	assertDeliveryRows(t, "first run", deliveryRows(t, db), recorded.AfterFirst)
	if got := pace.delays(); !reflect.DeepEqual(got, recorded.FirstDelays) {
		t.Errorf("first run pauses = %v, want %v", got, recorded.FirstDelays)
	}

	// The failed delivery is retried with the same idempotency key; sent
	// ones are skipped.
	sender.fail = nil
	pace.calls.Store(0)
	second, err := service.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:10:00.000Z"))
	if err != nil {
		t.Fatal(err)
	}
	assertResult(t, "second run", second, recorded.Second)
	assertDeliveryRows(t, "second run", deliveryRows(t, db), recorded.AfterSecond)
	if got := pace.delays(); !reflect.DeepEqual(got, recorded.SecondDelays) {
		t.Errorf("second run pauses = %v, want %v", got, recorded.SecondDelays)
	}
	assertSent(t, sender.sent, recorded.Sent)
	assertEditions(t, editionRows(t, db)[:1], []editionRow{{
		ID: 1, EditionKey: recorded.Editions[0].EditionKey, Subject: recorded.Editions[0].Subject,
		Articles: recorded.Editions[0].Articles, CreatedAt: recorded.Editions[0].CreatedAt,
	}}, true)
}

func TestDigestRequiresTheTokenSecretBeforeTouchingAnything(t *testing.T) {
	db := migratedDatabase(t)
	seedDeliveryLifecycle(t, db)
	sender := &recordingSender{}
	service := newTestService(t, db, sender, ServiceConfig{TokenSecret: "short"})
	_, err := service.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:00:00.000Z"))
	var configuration *ConfigurationError
	if !errors.As(err, &configuration) {
		t.Fatalf("SendDailyDigest error = %v, want a configuration error", err)
	}
	if len(sender.sent) != 0 || len(editionRows(t, db)) != 0 {
		t.Fatal("a digest without a token secret sent or recorded something")
	}
}

func TestUnconfiguredDeliveryFailsEachDeliveryLikeNode(t *testing.T) {
	db := migratedDatabase(t)
	seedDeliveryLifecycle(t, db)
	service := newTestService(t, db, NewResendSender(ResendConfig{Endpoint: "http://127.0.0.1:1/never"}), ServiceConfig{TokenSecret: testTokenSecret})
	result, err := service.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:00:00.000Z"))
	if err != nil || result.Sent != 0 || result.Failed != 3 || result.Skipped != 1 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	for _, row := range deliveryRows(t, db) {
		if row.Status == "failed" && (row.Error == nil || *row.Error != "Newsletter delivery is not configured") {
			t.Errorf("failed delivery error = %v", row.Error)
		}
	}
}

// Go-only guard: two digest requests at once (a cron retry and a manual
// POST) run one after the other, so the second sees the first one's sent
// rows instead of sending the same edition again.
func TestConcurrentDigestRunsNeverSendTwice(t *testing.T) {
	db := migratedDatabase(t)
	seedDeliveryLifecycle(t, db)
	sender := &recordingSender{}
	service := newTestService(t, db, sender, ServiceConfig{TokenSecret: testTokenSecret})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := service.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:00:00.000Z")); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	keys := map[string]int{}
	for _, sent := range sender.sent {
		keys[sent.key]++
	}
	if len(sender.sent) != 3 || len(keys) != 3 {
		t.Fatalf("sent = %v, want each of the 3 unsent subscribers exactly once", keys)
	}
}

// Like Node, a digest keeps going when the HTTP client goes away (Vercel's
// function times out long before a large digest ends).
func TestDigestIsNotCancelledWithTheRequest(t *testing.T) {
	db := migratedDatabase(t)
	seedDeliveryLifecycle(t, db)
	service := newTestService(t, db, &recordingSender{}, ServiceConfig{TokenSecret: testTokenSecret})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := service.SendDailyDigest(ctx, mustTime(t, "2026-09-20T05:00:00.000Z"))
	if err != nil || result.Sent != 3 {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

// Shutdown (the bound lifecycle context) stops a digest between deliveries
// and leaves no row in 'sending'; the next run resumes with the same keys.
func TestShutdownStopsTheDigestBetweenDeliveries(t *testing.T) {
	db := migratedDatabase(t)
	seedDeliveryLifecycle(t, db)
	lifecycle, stop := context.WithCancel(context.Background())
	sender := &recordingSender{}
	service := newTestService(t, db, sender, ServiceConfig{TokenSecret: testTokenSecret, Pace: func(ctx context.Context) error {
		stop()
		<-ctx.Done()
		return ctx.Err()
	}})
	service.Bind(lifecycle)
	result, err := service.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:00:00.000Z"))
	if !errors.Is(err, context.Canceled) || result.Sent != 1 {
		t.Fatalf("result = %+v, %v, want one delivery then cancellation", result, err)
	}
	for _, row := range deliveryRows(t, db) {
		if row.Status == "sending" && row.SubscriberID != 5 {
			t.Errorf("row left sending: %+v", row)
		}
	}
	resumed := newTestService(t, db, sender, ServiceConfig{TokenSecret: testTokenSecret})
	second, err := resumed.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:05:00.000Z"))
	if err != nil || second.Sent != 2 || second.Skipped != 2 {
		t.Fatalf("resumed = %+v, %v", second, err)
	}
}

func TestDigestArticlesJSONIsJSONStringify(t *testing.T) {
	got := articlesJSON([]DigestArticle{{Title: "<T&>\u2028", Slug: "s", Excerpt: "\"e\"", Category: "c", ReadingMinutes: 2}})
	want := `[{"title":"<T&>` + "\u2028" + `","slug":"s","excerpt":"\"e\"","category":"c","readingMinutes":2}]`
	if got != want {
		t.Fatalf("articlesJSON = %s, want %s", got, want)
	}
	if articlesJSON(nil) != "[]" {
		t.Fatal("empty list")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -p 2 ./internal/newsletter/`
Expected: FAIL to compile:

```
internal/newsletter/digest_test.go:137:50: undefined: DigestResult
internal/newsletter/digest_test.go:203:25: service.SendDailyDigest undefined (type *Service has no field or method SendDailyDigest)
internal/newsletter/digest_test.go:226:25: service.SendDailyDigest undefined (type *Service has no field or method SendDailyDigest)
```

- [ ] **Step 3: Implement.** Create `internal/newsletter/digest.go`:

```go
package newsletter

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// digestWindow is how far back the digest looks: 30 hours.
	digestWindow = 30 * time.Hour
	// digestCandidates and digestSize are Node's LIMIT 20 and slice(0, 5).
	digestCandidates = 20
	digestSize       = 5
	// deliveryWriteTimeout bounds recording a delivery outcome once the
	// send has returned, even when the digest is being cancelled.
	deliveryWriteTimeout = 10 * time.Second
)

// DigestResult is the digest route's counts (Node's DigestResult).
type DigestResult struct {
	Edition  string `json:"edition"`
	Articles int    `json:"articles"`
	Sent     int    `json:"sent"`
	Skipped  int    `json:"skipped"`
	Failed   int    `json:"failed"`
}

type articleRow struct {
	title, slug, excerpt, category string
	content, publishedAt           sql.NullString
}

// RecentPublishedArticles is sendDailyDigest's article query: the twenty
// published articles with the greatest published_at text.
func (s *SQLiteStore) RecentPublishedArticles(ctx context.Context) ([]articleRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.title, a.slug, a.excerpt, a.content, a.published_at, c.name AS category_name
         FROM articles a
         JOIN categories c ON c.id = a.category_id
         WHERE a.status = 'published'
         ORDER BY a.published_at DESC
         LIMIT `+strconv.Itoa(digestCandidates))
	if err != nil {
		return nil, fmt.Errorf("select digest articles: %w", err)
	}
	defer rows.Close()
	var articles []articleRow
	for rows.Next() {
		var row articleRow
		if err := rows.Scan(&row.title, &row.slug, &row.excerpt, &row.content, &row.publishedAt, &row.category); err != nil {
			return nil, fmt.Errorf("scan digest article: %w", err)
		}
		articles = append(articles, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("select digest articles: %w", err)
	}
	return articles, nil
}

// SaveEdition records the edition before any delivery, so the archive
// shows what it contained even if sends fail; a rerun replaces the subject
// and articles but keeps the first created_at.
func (s *SQLiteStore) SaveEdition(ctx context.Context, key, subject, articles, createdAt string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO newsletter_editions (edition_key, subject, articles, created_at)
         VALUES (?, ?, ?, ?)
         ON CONFLICT(edition_key) DO UPDATE SET subject = excluded.subject, articles = excluded.articles`,
		key, subject, articles, createdAt)
	if err != nil {
		return fmt.Errorf("save edition: %w", err)
	}
	return nil
}

// ActiveSubscribers returns the digest's recipients in id order.
func (s *SQLiteStore) ActiveSubscribers(ctx context.Context) ([]subscriber, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, email, status FROM subscribers WHERE status = 'active' ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("select subscribers: %w", err)
	}
	defer rows.Close()
	var subscribers []subscriber
	for rows.Next() {
		var row subscriber
		if err := rows.Scan(&row.id, &row.email, &row.status); err != nil {
			return nil, fmt.Errorf("scan subscriber: %w", err)
		}
		subscribers = append(subscribers, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("select subscribers: %w", err)
	}
	return subscribers, nil
}

// ClaimDelivery is Node's check-then-upsert as one statement: a delivery
// already 'sent' for this edition is left alone (claimed is false, the
// subscriber is skipped); otherwise the row is inserted, or reset to
// 'sending' with its error cleared, exactly as Node's upsert does.
func (s *SQLiteStore) ClaimDelivery(ctx context.Context, subscriberID int64, edition, createdAt string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO newsletter_deliveries (subscriber_id, edition_key, status, created_at)
           VALUES (?, ?, 'sending', ?)
           ON CONFLICT(subscriber_id, edition_key)
           DO UPDATE SET status = 'sending', error = NULL
           WHERE newsletter_deliveries.status <> 'sent'`, subscriberID, edition, createdAt)
	if err != nil {
		return false, fmt.Errorf("claim delivery: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("claim delivery: %w", err)
	}
	return changed > 0, nil
}

func (s *SQLiteStore) MarkSent(ctx context.Context, subscriberID int64, edition, providerMessageID, sentAt string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE newsletter_deliveries
             SET status = 'sent', provider_message_id = ?, sent_at = ?, error = NULL
             WHERE subscriber_id = ? AND edition_key = ?`, providerMessageID, sentAt, subscriberID, edition)
	if err != nil {
		return fmt.Errorf("mark delivery sent: %w", err)
	}
	return nil
}

func (s *SQLiteStore) MarkFailed(ctx context.Context, subscriberID int64, edition, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE newsletter_deliveries
             SET status = 'failed', error = ?
             WHERE subscriber_id = ? AND edition_key = ?`, message, subscriberID, edition)
	if err != nil {
		return fmt.Errorf("mark delivery failed: %w", err)
	}
	return nil
}

// articlesJSON is JSON.stringify(articles) for the stored edition.
func articlesJSON(articles []DigestArticle) string {
	var builder strings.Builder
	builder.WriteByte('[')
	for i, article := range articles {
		if i > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(`{"title":`)
		writeJSONString(&builder, article.Title)
		builder.WriteString(`,"slug":`)
		writeJSONString(&builder, article.Slug)
		builder.WriteString(`,"excerpt":`)
		writeJSONString(&builder, article.Excerpt)
		builder.WriteString(`,"category":`)
		writeJSONString(&builder, article.Category)
		builder.WriteString(`,"readingMinutes":`)
		builder.WriteString(strconv.Itoa(article.ReadingMinutes))
		builder.WriteByte('}')
	}
	builder.WriteByte(']')
	return builder.String()
}

// Bind sets the lifecycle context that bounds digest runs. App.Run binds it
// to the process's shutdown context.
func (s *Service) Bind(ctx context.Context) {
	s.lifecycle.Lock()
	s.lifecycle.ctx = ctx
	s.lifecycle.Unlock()
}

func (s *Service) boundLifecycle() context.Context {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if s.lifecycle.ctx != nil {
		return s.lifecycle.ctx
	}
	return context.Background()
}

// SendDailyDigest is Node's sendDailyDigest. Like Node, a started digest is
// not stopped when the caller (the cron's HTTP request) goes away: it runs
// on a context detached from ctx and bounded only by the bound lifecycle
// (shutdown), which stops it between deliveries. Runs are serialized, so two
// overlapping requests never deliver the same edition twice from this
// process; Node relied on Resend's idempotency key for that.
func (s *Service) SendDailyDigest(ctx context.Context, now time.Time) (DigestResult, error) {
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	stop := context.AfterFunc(s.boundLifecycle(), cancel)
	defer stop()

	s.digestMu.Lock()
	defer s.digestMu.Unlock()
	return s.sendDailyDigest(runCtx, now)
}

func (s *Service) sendDailyDigest(ctx context.Context, now time.Time) (DigestResult, error) {
	secret, err := s.requireTokenSecret()
	if err != nil {
		return DigestResult{}, err
	}
	edition := editionKey(now)
	nowMS := now.UnixMilli()
	cutoff := nowMS - digestWindow.Milliseconds()
	rows, err := s.store.RecentPublishedArticles(ctx)
	if err != nil {
		return DigestResult{}, err
	}
	articles := make([]DigestArticle, 0, digestSize)
	for _, row := range rows {
		publishedAt, ok := parseArticleTimestamp(row.publishedAt.String, s.local)
		if !row.publishedAt.Valid || !ok || publishedAt > nowMS || publishedAt < cutoff {
			continue
		}
		articles = append(articles, DigestArticle{
			Title: row.title, Slug: row.slug, Excerpt: row.excerpt, Category: row.category,
			ReadingMinutes: readingMinutes(row.content.String),
		})
		if len(articles) == digestSize {
			break
		}
	}

	result := DigestResult{Edition: edition, Articles: len(articles)}
	if len(articles) == 0 {
		return result, nil
	}
	timestamp := isoTimestamp(now)
	if err := s.store.SaveEdition(ctx, edition, DigestSubject(articles), articlesJSON(articles), timestamp); err != nil {
		return result, err
	}
	subscribers, err := s.store.ActiveSubscribers(ctx)
	if err != nil {
		return result, err
	}
	for index, recipient := range subscribers {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		claimed, err := s.store.ClaimDelivery(ctx, recipient.id, edition, timestamp)
		if err != nil {
			return result, err
		}
		if !claimed {
			result.Skipped++
			continue
		}
		key := "newsletter-digest-" + edition + "-" + strconv.FormatInt(recipient.id, 10)
		providerID, sendErr := s.sender.Send(ctx, DigestEmail(recipient.email, articles, s.siteURL, s.unsubscribeURL(recipient.id, secret)), key)
		// The outcome is recorded even while shutting down, so a claimed row
		// never stays 'sending' because of cancellation.
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deliveryWriteTimeout)
		if sendErr != nil {
			err = s.store.MarkFailed(writeCtx, recipient.id, edition, sliceUTF16(sendErr.Error(), 500))
			result.Failed++
			s.logger.WarnContext(ctx, "newsletter digest delivery failed", "edition", edition, "subscriber_id", recipient.id, "error", sendErr)
		} else {
			err = s.store.MarkSent(writeCtx, recipient.id, edition, providerID, timestamp)
			result.Sent++
		}
		cancel()
		if err != nil {
			return result, err
		}
		if index < len(subscribers)-1 {
			if err := s.pace(ctx); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}
```

Add the serialization and lifecycle fields to `Service` in `internal/newsletter/service.go`:

```diff
diff --git a/apps/server-go/internal/newsletter/service.go b/apps/server-go/internal/newsletter/service.go
index daeb503..6aeab29 100644
--- a/apps/server-go/internal/newsletter/service.go
+++ b/apps/server-go/internal/newsletter/service.go
@@ -8,6 +8,7 @@ import (
 	"net/url"
 	"strconv"
 	"strings"
+	"sync"
 	"time"
 	"unicode/utf8"
 )
@@ -87,6 +88,14 @@ type Service struct {
 	local       *time.Location
 	pace        func(context.Context) error
 	logger      *slog.Logger
+
+	// digestMu serializes digest runs.
+	digestMu sync.Mutex
+	// lifecycle bounds digest runs (see Bind).
+	lifecycle struct {
+		sync.Mutex
+		ctx context.Context
+	}
 }
 
 // NewService validates NEWSLETTER_SITE_URL like Node's constructor does, so
```

- [ ] **Step 4: Run them to verify they pass, with the race detector**

Run: `go test -p 2 ./internal/newsletter/ && go test -p 2 -race ./internal/newsletter/`
Expected: `ok` twice.

- [ ] **Step 5: Commit**

```bash
git add internal/newsletter/digest.go internal/newsletter/digest_test.go internal/newsletter/service.go
git commit -m "Port the daily digest with Node's selection, delivery states, and pacing"
```

---

### Task 10: The routes

`http.go` ports the eight route handlers of `routes/public.ts`. Bodies are parsed with `jsonbody.Decode` (express.json semantics) before anything else, as `express.json()` runs before Node's routes, so a malformed body neither reaches the service nor counts against the throttle. `typeof req.query.token === "string"` under Express's `qs` means a repeated parameter is an array, hence "exactly one value". The digest gets a 15-minute write deadline and confirm 2 minutes (for the welcome email's three attempts); the server's 30s default would cut them off where Node has no limit. `url.PathUnescape` restores Express's decoded route parameter (chi hands over the escaped form when the path has escapes).

**Files:** Create `internal/newsletter/http_test.go`, `internal/newsletter/http.go`

- [ ] **Step 1: Write the failing tests.** Create `internal/newsletter/http_test.go`:

```go
package newsletter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

const testCronSecret = "synthetic-cron-secret"

// fakeService lets HTTP tests choose each use case's outcome.
type fakeService struct {
	mu            sync.Mutex
	subscribeErr  error
	subscribed    []string
	confirmResult ConfirmationResult
	confirmErr    error
	tokens        []string
	unsubscribe   UnsubscribeState
	unsubErr      error
	limits        []float64
	editions      []Edition
	editionsErr   error
	editionKeys   []string
	edition       *Edition
	digest        DigestResult
	digestErr     error
}

func (f *fakeService) Subscribe(_ context.Context, email, placement string, _ time.Time) (SubscriptionState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subscribed = append(f.subscribed, email+"|"+placement)
	if f.subscribeErr != nil {
		return "", f.subscribeErr
	}
	if email == "already@example.com" {
		return StateAlreadyActive, nil
	}
	if email == "" || !strings.Contains(email, "@") {
		return "", ErrInvalidEmail
	}
	return StateSubscribed, nil
}

func (f *fakeService) Confirm(_ context.Context, token string, _ time.Time) (ConfirmationResult, error) {
	f.tokens = append(f.tokens, token)
	return f.confirmResult, f.confirmErr
}

func (f *fakeService) Unsubscribe(_ context.Context, token string, _ time.Time) (UnsubscribeState, error) {
	f.tokens = append(f.tokens, token)
	return f.unsubscribe, f.unsubErr
}

func (f *fakeService) ListEditions(_ context.Context, limit float64) ([]Edition, error) {
	f.limits = append(f.limits, limit)
	return f.editions, f.editionsErr
}

func (f *fakeService) Edition(_ context.Context, key string) (Edition, bool, error) {
	f.editionKeys = append(f.editionKeys, key)
	if f.edition == nil {
		return Edition{}, false, f.editionsErr
	}
	return *f.edition, true, nil
}

func (f *fakeService) SendDailyDigest(_ context.Context, _ time.Time) (DigestResult, error) {
	return f.digest, f.digestErr
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func router(handler *Handler) http.Handler {
	mux := chi.NewRouter()
	mux.Route("/api", handler.Mount)
	return mux
}

func serve(t *testing.T, handler http.Handler, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertResponse(t *testing.T, response *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	if response.Code != status || response.Body.String() != body {
		t.Fatalf("response = %d %s, want %d %s", response.Code, response.Body.String(), status, body)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
}

func newHandler(service newsletterService, now *clock) *Handler {
	return NewHandler(service, "  "+testCronSecret+"\n", now.Now, discardLogger())
}

func TestSubscribeRespondsLikeNode(t *testing.T) {
	now := &clock{now: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)}
	service := &fakeService{}
	handler := router(newHandler(service, now))
	assertResponse(t, serve(t, handler, "POST", "/api/subscribe", `{"email":"reader@example.com","placement":"inline"}`, nil),
		200, `{"success":true,"state":"subscribed","message":"You're subscribed. The next digest will arrive in your inbox."}`)
	assertResponse(t, serve(t, handler, "POST", "/api/subscribe", `{"email":"already@example.com"}`, nil),
		200, `{"success":true,"state":"already_active","message":"You're already subscribed."}`)
	// Non-string values read as "" and "unknown", like typeof checks in Node.
	assertResponse(t, serve(t, handler, "POST", "/api/subscribe", `{"email":5,"placement":["x"]}`, nil), 400, `{"error":"Valid email required"}`)
	assertResponse(t, serve(t, handler, "POST", "/api/subscribe", `[1]`, nil), 400, `{"error":"Valid email required"}`)
	if got := service.subscribed[len(service.subscribed)-2:]; got[0] != "|unknown" || got[1] != "|unknown" {
		t.Errorf("service saw %v", got)
	}
	assertResponse(t, serve(t, handler, "POST", "/api/subscribe", `{"email":`, nil), 400, `{"error":"Invalid request body"}`)

	service.subscribeErr = errors.New("database is locked")
	assertResponse(t, serve(t, handler, "POST", "/api/subscribe", `{"email":"a@b.c"}`, nil), 502, `{"error":"We could not complete your signup. Please try again."}`)
	service.subscribeErr = &ConfigurationError{message: "x"}
	assertResponse(t, serve(t, handler, "POST", "/api/subscribe", `{"email":"a@b.c"}`, nil), 503, `{"error":"Newsletter signup is temporarily unavailable"}`)
}

func TestSignupThrottleIsNodesGlobalRollingMinute(t *testing.T) {
	now := &clock{now: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)}
	service := &fakeService{}
	handler := router(newHandler(service, now))
	// Malformed bodies never reach the route, so they are not counted.
	for i := 0; i < 5; i++ {
		serve(t, handler, "POST", "/api/subscribe", `{`, nil)
	}
	// Invalid addresses are counted.
	for i := 0; i < 30; i++ {
		if response := serve(t, handler, "POST", "/api/subscribe", `{"email":"nope"}`, nil); response.Code != 400 {
			t.Fatalf("attempt %d = %d", i, response.Code)
		}
		now.Advance(time.Second)
	}
	// 30 attempts at t=0..29s: the 31st at t=30s is refused.
	assertResponse(t, serve(t, handler, "POST", "/api/subscribe", `{"email":"a@b.c"}`, nil), 429, `{"error":"Too many signup attempts. Please try again shortly."}`)
	// At t=60s the attempt from t=0 is still inside the window (not older
	// than 60s); at t=60.001s it has expired.
	now.Advance(30 * time.Second)
	if response := serve(t, handler, "POST", "/api/subscribe", `{"email":"a@b.c"}`, nil); response.Code != 429 {
		t.Fatalf("at the window edge = %d, want 429", response.Code)
	}
	now.Advance(time.Millisecond)
	if response := serve(t, handler, "POST", "/api/subscribe", `{"email":"a@b.c"}`, nil); response.Code != 200 {
		t.Fatalf("after the window = %d, want 200", response.Code)
	}
}

func TestConfirmRespondsLikeNode(t *testing.T) {
	now := &clock{now: time.Now()}
	service := &fakeService{confirmResult: ConfirmationResult{State: ConfirmationConfirmed, WelcomeSent: true}}
	handler := router(newHandler(service, now))
	assertResponse(t, serve(t, handler, "GET", "/api/newsletter/confirm?token=abc.def", "", nil), 200, `{"state":"confirmed","welcomeSent":true}`)
	// A missing or repeated token is "" (typeof req.query.token !== "string").
	serve(t, handler, "GET", "/api/newsletter/confirm", "", nil)
	serve(t, handler, "GET", "/api/newsletter/confirm?token=a&token=b", "", nil)
	if got := service.tokens; len(got) != 3 || got[0] != "abc.def" || got[1] != "" || got[2] != "" {
		t.Errorf("tokens = %q", got)
	}
	service.confirmErr = &ConfigurationError{message: "x"}
	assertResponse(t, serve(t, handler, "GET", "/api/newsletter/confirm?token=a", "", nil), 503, `{"state":"unavailable","error":"Newsletter confirmation is temporarily unavailable"}`)
	service.confirmErr = errors.New("disk I/O error")
	assertResponse(t, serve(t, handler, "GET", "/api/newsletter/confirm?token=a", "", nil), 500, `{"state":"invalid","error":"Confirmation failed"}`)
}

func TestUnsubscribeTakesTheQueryTokenThenTheBodyTokenLikeNode(t *testing.T) {
	now := &clock{now: time.Now()}
	service := &fakeService{unsubscribe: UnsubscribeUnsubscribed}
	handler := router(newHandler(service, now))
	assertResponse(t, serve(t, handler, "POST", "/api/newsletter/unsubscribe?token=query", `{"token":"body"}`, nil), 200, `{"state":"unsubscribed"}`)
	serve(t, handler, "POST", "/api/newsletter/unsubscribe", `{"token":"body"}`, nil)
	serve(t, handler, "GET", "/api/newsletter/unsubscribe?token=get", "", nil)
	serve(t, handler, "POST", "/api/newsletter/unsubscribe?token=a&token=b", `{"token":"fallback"}`, nil)
	serve(t, handler, "POST", "/api/newsletter/unsubscribe", `{"token":7}`, nil)
	if got := strings.Join(service.tokens, ","); got != "query,body,get,fallback," {
		t.Errorf("tokens = %q", got)
	}
	assertResponse(t, serve(t, handler, "POST", "/api/newsletter/unsubscribe?token=a", `{"token":`, nil), 400, `{"error":"Invalid request body"}`)
	service.unsubErr = &ConfigurationError{message: "x"}
	assertResponse(t, serve(t, handler, "GET", "/api/newsletter/unsubscribe?token=a", "", nil), 503, `{"state":"unavailable","error":"Unsubscribe is temporarily unavailable"}`)
	service.unsubErr = errors.New("disk I/O error")
	assertResponse(t, serve(t, handler, "POST", "/api/newsletter/unsubscribe?token=a", "", nil), 500, `{"state":"invalid","error":"Unsubscribe failed"}`)
}

func TestEditionsLimitIsParsedLikeNode(t *testing.T) {
	now := &clock{now: time.Now()}
	service := &fakeService{editions: []Edition{}}
	handler := router(newHandler(service, now))
	cases := map[string]float64{
		"":                                   30,
		"?limit=":                            30,
		"?limit=2":                           2,
		"?limit=%207":                        7,
		"?limit=7abc":                        7,
		"?limit=-3":                          -3,
		"?limit=0":                           0,
		"?limit=abc":                         30,
		"?limit=1e3":                         1,
		"?limit=12.9":                        12,
		"?limit=5&limit=9":                   5,
		"?limit=&limit=9":                    30, // String(["", "9"]) is ",9"
		"?limit=" + strings.Repeat("9", 400): 30, // parseInt gives Infinity
	}
	for query, want := range cases {
		service.limits = nil
		assertResponse(t, serve(t, handler, "GET", "/api/newsletter/editions"+query, "", nil), 200, `{"editions":[]}`)
		if len(service.limits) != 1 || service.limits[0] != want {
			t.Errorf("limit for %q = %v, want %v", query, service.limits, want)
		}
	}
	service.editionsErr = errors.New("disk I/O error")
	assertResponse(t, serve(t, handler, "GET", "/api/newsletter/editions", "", nil), 500, `{"error":"Internal server error"}`)
}

func TestEditionRespondsLikeNode(t *testing.T) {
	now := &clock{now: time.Now()}
	service := &fakeService{}
	handler := router(newHandler(service, now))
	assertResponse(t, serve(t, handler, "GET", "/api/newsletter/editions/2026-09-19", "", nil), 404, `{"error":"Edition not found"}`)
	service.edition = &Edition{Edition: "2026-09-19", Subject: "S", Articles: []byte(`[{"title":"T"}]`), CreatedAt: "2026-09-19T05:00:00.000Z"}
	assertResponse(t, serve(t, handler, "GET", "/api/newsletter/editions/2026%2D09%2D19", "", nil), 200,
		`{"edition":{"edition":"2026-09-19","subject":"S","articles":[{"title":"T"}],"createdAt":"2026-09-19T05:00:00.000Z"}}`)
	if got := service.editionKeys; got[len(got)-1] != "2026-09-19" {
		t.Errorf("decoded key = %q", got)
	}
}

func TestDigestAuthorizationMatchesNode(t *testing.T) {
	now := &clock{now: time.Date(2026, 9, 20, 5, 0, 0, 0, time.UTC)}
	service := &fakeService{digest: DigestResult{Edition: "2026-09-20", Articles: 1, Sent: 2}}
	handler := router(newHandler(service, now))
	for header, authorized := range map[string]bool{
		"":                                false,
		"Bearer wrong":                    false,
		"Bearer " + testCronSecret + "x":  false,
		"Bearer":                          false,
		"Bearer ":                         false,
		"Bearer " + testCronSecret:        true,
		"bearer \t " + testCronSecret:     true,
		"BEARER " + testCronSecret:        true,
		testCronSecret:                    true, // the bare secret, like Node
		"Basic " + testCronSecret:         false,
		"Bearer Bearer " + testCronSecret: false,
	} {
		for _, method := range []string{"GET", "POST"} {
			headers := map[string]string{}
			if header != "" {
				headers["Authorization"] = header
			}
			response := serve(t, handler, method, "/api/newsletter/digest", "", headers)
			if authorized {
				assertResponse(t, response, 200, `{"success":true,"edition":"2026-09-20","articles":1,"sent":2,"skipped":0,"failed":0}`)
			} else {
				assertResponse(t, response, 401, `{"error":"Unauthorized"}`)
			}
		}
	}
	// An empty (or blank) configured secret refuses every request.
	for _, secret := range []string{"", "   "} {
		empty := router(NewHandler(service, secret, now.Now, discardLogger()))
		for _, header := range []string{"Bearer ", "Bearer " + secret, ""} {
			assertResponse(t, serve(t, empty, "GET", "/api/newsletter/digest", "", map[string]string{"Authorization": header}), 401, `{"error":"Unauthorized"}`)
		}
	}
}

func TestDigestResponsesMatchNode(t *testing.T) {
	now := &clock{now: time.Date(2026, 9, 20, 5, 0, 0, 0, time.UTC)}
	service := &fakeService{digest: DigestResult{Edition: "2026-09-20", Articles: 2, Sent: 1, Skipped: 3, Failed: 1}}
	handler := router(newHandler(service, now))
	auth := map[string]string{"Authorization": "Bearer " + testCronSecret}
	assertResponse(t, serve(t, handler, "POST", "/api/newsletter/digest", "", auth), 200, `{"success":false,"edition":"2026-09-20","articles":2,"sent":1,"skipped":3,"failed":1}`)
	service.digestErr = &ConfigurationError{message: "NEWSLETTER_TOKEN_SECRET must contain at least 32 characters"}
	assertResponse(t, serve(t, handler, "GET", "/api/newsletter/digest", "", auth), 503, `{"error":"Newsletter delivery is not configured"}`)
	service.digestErr = errors.New("database is locked")
	assertResponse(t, serve(t, handler, "GET", "/api/newsletter/digest", "", auth), 500, `{"error":"Newsletter digest failed"}`)
}

// deadlineRecorder is a ResponseWriter that supports write deadlines, like
// a real connection, so the extension can be observed.
type deadlineRecorder struct {
	*httptest.ResponseRecorder
	writeDeadline time.Time
}

func (d *deadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	d.writeDeadline = deadline
	return nil
}

func TestDigestExtendsItsWriteDeadline(t *testing.T) {
	service := &fakeService{}
	handler := router(newHandler(service, &clock{now: time.Now()}))
	request := httptest.NewRequest("GET", "/api/newsletter/digest", nil)
	request.Header.Set("Authorization", "Bearer "+testCronSecret)
	recorder := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	started := time.Now()
	handler.ServeHTTP(recorder, request)
	if recorder.writeDeadline.Before(started.Add(DigestWriteTimeout)) || recorder.Code != 200 {
		t.Fatalf("write deadline = %v (status %d), want at least %v from now", recorder.writeDeadline.Sub(started), recorder.Code, DigestWriteTimeout)
	}
	// An unauthorized request does not get the long deadline.
	unauthorized := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	handler.ServeHTTP(unauthorized, httptest.NewRequest("GET", "/api/newsletter/digest", nil))
	if !unauthorized.writeDeadline.IsZero() {
		t.Fatal("unauthorized digest request extended its deadline")
	}
}

func TestConfirmExtendsItsWriteDeadlineForTheWelcomeEmail(t *testing.T) {
	service := &fakeService{confirmResult: ConfirmationResult{State: ConfirmationInvalid}}
	handler := router(newHandler(service, &clock{now: time.Now()}))
	recorder := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	started := time.Now()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/newsletter/confirm?token=a", nil))
	if recorder.writeDeadline.Before(started.Add(ConfirmWriteTimeout)) || recorder.Code != 200 {
		t.Fatalf("write deadline = %v (status %d), want at least %v from now", recorder.writeDeadline.Sub(started), recorder.Code, ConfirmWriteTimeout)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -p 2 ./internal/newsletter/`
Expected: FAIL to compile:

```
internal/newsletter/http_test.go:97:22: undefined: Handler
internal/newsletter/http_test.go:127:25: undefined: newsletterService
internal/newsletter/http_test.go:127:57: undefined: Handler
```

- [ ] **Step 3: Implement.** Create `internal/newsletter/http.go`:

```go
package newsletter

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/jsonbody"
)

const (
	// signupWindow and signupLimit are Node's global signup throttle: at most
	// 30 attempts in any rolling minute, across all clients.
	signupWindow = 60 * time.Second
	signupLimit  = 30
	// DigestWriteTimeout replaces the server's 30s write timeout for the
	// digest route, which sends one email every ~550ms. Node has no timeout;
	// the digest itself keeps running after this (or after the client
	// leaves), like Node's.
	DigestWriteTimeout = 15 * time.Minute
	// ConfirmWriteTimeout covers the welcome email a confirmation sends
	// (up to three Resend attempts of ResendAttemptTimeout each), which
	// would not fit in the server's 30s write timeout.
	ConfirmWriteTimeout = 2 * time.Minute
)

type newsletterService interface {
	Subscribe(ctx context.Context, rawEmail, placement string, now time.Time) (SubscriptionState, error)
	Confirm(ctx context.Context, token string, now time.Time) (ConfirmationResult, error)
	Unsubscribe(ctx context.Context, token string, now time.Time) (UnsubscribeState, error)
	ListEditions(ctx context.Context, limit float64) ([]Edition, error)
	Edition(ctx context.Context, key string) (Edition, bool, error)
	SendDailyDigest(ctx context.Context, now time.Time) (DigestResult, error)
}

// Handler serves the public newsletter routes of apps/server/src/routes/public.ts.
type Handler struct {
	service    newsletterService
	cronSecret string
	now        func() time.Time
	logger     *slog.Logger

	signupMu       sync.Mutex
	signupAttempts []int64
}

// NewHandler takes the raw cron secret (NEWSLETTER_CRON_SECRET, else
// CRON_SECRET); like Node it is trimmed per request and an empty one
// rejects every digest request.
func NewHandler(service newsletterService, cronSecret string, now func() time.Time, logger *slog.Logger) *Handler {
	return &Handler{service: service, cronSecret: cronSecret, now: now, logger: logger}
}

// Mount registers the routes relative to /api. None of them requires a
// dashboard login; the digest checks the cron secret itself.
func (h *Handler) Mount(router chi.Router) {
	router.Post("/subscribe", h.subscribe)
	router.Get("/newsletter/confirm", h.confirm)
	// Vercel's /api/newsletter/unsubscribe forwards with POST; email clients
	// use the link (GET) or List-Unsubscribe-Post (POST).
	router.Get("/newsletter/unsubscribe", h.unsubscribe)
	router.Post("/newsletter/unsubscribe", h.unsubscribe)
	router.Get("/newsletter/editions", h.editions)
	router.Get("/newsletter/editions/{edition}", h.edition)
	// Vercel Cron invokes GET; POST is the secured manual retry (and what
	// the website's cron route forwards).
	router.Get("/newsletter/digest", h.digest)
	router.Post("/newsletter/digest", h.digest)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		body = []byte(`{"error":"Internal server error"}`)
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func errorBody(message string) map[string]string { return map[string]string{"error": message} }

type stateError struct {
	State string `json:"state"`
	Error string `json:"error"`
}

// allowSignup is Node's signupAttempts window: attempts older than 60s are
// dropped, and a 31st attempt inside the window is refused (and not counted).
func (h *Handler) allowSignup(now time.Time) bool {
	timestamp := now.UnixMilli()
	h.signupMu.Lock()
	defer h.signupMu.Unlock()
	for len(h.signupAttempts) > 0 && h.signupAttempts[0] < timestamp-signupWindow.Milliseconds() {
		h.signupAttempts = h.signupAttempts[1:]
	}
	if len(h.signupAttempts) >= signupLimit {
		return false
	}
	h.signupAttempts = append(h.signupAttempts, timestamp)
	return true
}

func (h *Handler) subscribe(w http.ResponseWriter, r *http.Request) {
	// express.json() runs before the route, so a malformed body is rejected
	// before it counts as a signup attempt.
	body, err := jsonbody.Decode(w, r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("Invalid request body"))
		return
	}
	now := h.now()
	if !h.allowSignup(now) {
		writeJSON(w, http.StatusTooManyRequests, errorBody("Too many signup attempts. Please try again shortly."))
		return
	}
	email := body.Get("email").StringOrEmpty()
	placement, ok := body.Get("placement").Str()
	if !ok {
		placement = "unknown"
	}
	// Like Node, a signup the client abandons still completes.
	state, err := h.service.Subscribe(context.WithoutCancel(r.Context()), email, placement, now)
	var configuration *ConfigurationError
	switch {
	case errors.Is(err, ErrInvalidEmail):
		writeJSON(w, http.StatusBadRequest, errorBody(err.Error()))
		return
	case errors.As(err, &configuration):
		writeJSON(w, http.StatusServiceUnavailable, errorBody("Newsletter signup is temporarily unavailable"))
		return
	case err != nil:
		h.logger.ErrorContext(r.Context(), "Newsletter signup failed", "error", err)
		writeJSON(w, http.StatusBadGateway, errorBody("We could not complete your signup. Please try again."))
		return
	}
	message := "You're subscribed. The next digest will arrive in your inbox."
	if state == StateAlreadyActive {
		message = "You're already subscribed."
	}
	writeJSON(w, http.StatusOK, struct {
		Success bool              `json:"success"`
		State   SubscriptionState `json:"state"`
		Message string            `json:"message"`
	}{true, state, message})
}

// queryString is `typeof req.query[name] === "string" ? value : undefined`
// under Express's qs parser: a repeated parameter becomes an array.
func queryString(r *http.Request, name string) (string, bool) {
	values := r.URL.Query()[name]
	if len(values) != 1 {
		return "", false
	}
	return values[0], true
}

func (h *Handler) confirm(w http.ResponseWriter, r *http.Request) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(ConfirmWriteTimeout))
	token, _ := queryString(r, "token")
	result, err := h.service.Confirm(context.WithoutCancel(r.Context()), token, h.now())
	var configuration *ConfigurationError
	switch {
	case errors.As(err, &configuration):
		writeJSON(w, http.StatusServiceUnavailable, stateError{"unavailable", "Newsletter confirmation is temporarily unavailable"})
	case err != nil:
		h.logger.ErrorContext(r.Context(), "Newsletter confirmation failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, stateError{"invalid", "Confirmation failed"})
	default:
		writeJSON(w, http.StatusOK, result)
	}
}

func (h *Handler) unsubscribe(w http.ResponseWriter, r *http.Request) {
	body, err := jsonbody.Decode(w, r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("Invalid request body"))
		return
	}
	token, ok := queryString(r, "token")
	if !ok {
		token = body.Get("token").StringOrEmpty()
	}
	state, err := h.service.Unsubscribe(context.WithoutCancel(r.Context()), token, h.now())
	var configuration *ConfigurationError
	switch {
	case errors.As(err, &configuration):
		writeJSON(w, http.StatusServiceUnavailable, stateError{"unavailable", "Unsubscribe is temporarily unavailable"})
	case err != nil:
		h.logger.ErrorContext(r.Context(), "Newsletter unsubscribe failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, stateError{"invalid", "Unsubscribe failed"})
	default:
		writeJSON(w, http.StatusOK, struct {
			State UnsubscribeState `json:"state"`
		}{state})
	}
}

// editionsLimit is parseInt(String(req.query.limit || "30"), 10), falling
// back to 30 when that is not finite. A repeated parameter is an array,
// whose String() joins the values with commas.
func editionsLimit(r *http.Request) float64 {
	raw := "30"
	switch values := r.URL.Query()["limit"]; {
	case len(values) == 1 && values[0] != "":
		raw = values[0]
	case len(values) > 1:
		raw = strings.Join(values, ",")
	}
	limit, ok := parseInt10(raw)
	if !ok || math.IsInf(limit, 0) {
		return 30
	}
	return limit
}

func (h *Handler) editions(w http.ResponseWriter, r *http.Request) {
	editions, err := h.service.ListEditions(r.Context(), editionsLimit(r))
	if err != nil {
		h.logger.ErrorContext(r.Context(), "list newsletter editions", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody("Internal server error"))
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Editions []Edition `json:"editions"`
	}{editions})
}

func (h *Handler) edition(w http.ResponseWriter, r *http.Request) {
	// Express decodes route parameters; chi hands over the escaped form when
	// the request path carries escapes.
	key, err := url.PathUnescape(chi.URLParam(r, "edition"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("Invalid request"))
		return
	}
	edition, found, err := h.service.Edition(r.Context(), key)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "get newsletter edition", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody("Internal server error"))
		return
	}
	if !found {
		writeJSON(w, http.StatusNotFound, errorBody("Edition not found"))
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Edition Edition `json:"edition"`
	}{edition})
}

var bearerPrefix = regexp.MustCompile(`^(?i:bearer)[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]+`)

// cronAuthorized is Node's check: the Authorization header, with a leading
// "Bearer " (any case, any whitespace) removed if present, must equal the
// trimmed cron secret, compared in constant time; an empty secret refuses
// everything. Like Node, the bare secret without "Bearer " is accepted too.
func (h *Handler) cronAuthorized(r *http.Request) bool {
	expected := jsTrim(h.cronSecret)
	values := r.Header.Values("Authorization")
	if expected == "" || len(values) == 0 {
		return false
	}
	supplied := bearerPrefix.ReplaceAllLiteralString(values[0], "")
	return supplied != "" && subtle.ConstantTimeCompare([]byte(supplied), []byte(expected)) == 1
}

func (h *Handler) digest(w http.ResponseWriter, r *http.Request) {
	if !h.cronAuthorized(r) {
		writeJSON(w, http.StatusUnauthorized, errorBody("Unauthorized"))
		return
	}
	// Writers without deadline support (test recorders) are skipped.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(DigestWriteTimeout))
	result, err := h.service.SendDailyDigest(r.Context(), h.now())
	var configuration *ConfigurationError
	switch {
	case errors.As(err, &configuration):
		writeJSON(w, http.StatusServiceUnavailable, errorBody("Newsletter delivery is not configured"))
	case err != nil:
		h.logger.ErrorContext(r.Context(), "Newsletter digest failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody("Newsletter digest failed"))
	default:
		writeJSON(w, http.StatusOK, struct {
			Success  bool   `json:"success"`
			Edition  string `json:"edition"`
			Articles int    `json:"articles"`
			Sent     int    `json:"sent"`
			Skipped  int    `json:"skipped"`
			Failed   int    `json:"failed"`
		}{result.Failed == 0, result.Edition, result.Articles, result.Sent, result.Skipped, result.Failed})
	}
}
```

- [ ] **Step 4: Run them to verify they pass**

Run: `go test -p 2 ./internal/newsletter/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/newsletter/http.go internal/newsletter/http_test.go
git commit -m "Serve the newsletter routes with Node's statuses, bodies, throttle, and cron auth"
```

---

### Task 11: Composition and the Node newsletter contracts

`internal/app` composes the service (a bad `NEWSLETTER_SITE_URL` fails composition), mounts the routes inside `/api` beside the other public routes, logs when delivery is unconfigured, and binds digest runs to `Run`'s context so shutdown stops them. Three package variables (`resendEndpoint`, `newResendHTTPClient`, `newsletterPace`) exist only so tests can point delivery at an `httptest` Resend stand-in and skip the pause (`StubNewsletterDeliveryForTest`).

The contract test replays operations 7-14 in canonical order on the synthetic capture data. `$NEWSLETTER_*_TOKEN` are minted with `newsletter.CreateToken` and must match Node's recorded SHA-256 vectors and payloads before replay, so the replay proves both directions of token compatibility; `$CRON_AUTHORIZATION` is `Bearer <CRON_SECRET>`. It then checks the capture's email effects (the welcome email for 501, the digest to 501 and the new subscriber 504, nothing on `digestPost`) and the stored edition. The test name contains `Contract`, so `make contracts-check` runs it.

**Files:** Modify `internal/app/app.go`, `internal/app/export_test.go`; create `internal/app/newsletter_contract_test.go`

- [ ] **Step 1: Write the failing tests.** Create `internal/app/newsletter_contract_test.go`:

```go
package app_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/contracttest"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsletter"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

// The synthetic Node capture's newsletter settings
// (apps/server/scripts/contract-server.ts:17-19, 171-176, 184).
const (
	contractNewsletterSecret = "synthetic-contract-newsletter-secret-000000000000"
	contractCronSecret       = "synthetic-contract-cron-secret-never-production"
	contractResendKey        = "re_synthetic_contract_key_never_production"
)

// newsletterOperations is every newsletter operation in the canonical Node
// fixture, in canonical order.
var newsletterOperations = []string{
	"newsletter.subscribe", "newsletter.confirm", "newsletter.unsubscribeGet", "newsletter.unsubscribePost",
	"newsletter.editions", "newsletter.edition", "newsletter.digestGet", "newsletter.digestPost",
}

// seedContractNewsletter reproduces the synthetic Node capture's subscribers
// and editions (apps/server/scripts/contracts/synthetic-seed.ts:64-84).
func seedContractNewsletter(t *testing.T, db *sql.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO subscribers (id, email, status, source_placement, confirmation_sent_at, confirmed_at, unsubscribed_at, created_at, updated_at) VALUES
(501, 'pending@example.invalid', 'pending', 'contract', '2026-09-18T00:00:00.000Z', NULL, NULL, '2026-09-18T00:00:00.000Z', '2026-09-18T00:00:00.000Z'),
(502, 'active@example.invalid', 'active', 'contract', NULL, '2026-09-18T01:00:00.000Z', NULL, '2026-09-18T00:00:00.000Z', '2026-09-18T01:00:00.000Z'),
(503, 'unsubscribed@example.invalid', 'unsubscribed', 'contract', NULL, '2026-09-17T01:00:00.000Z', '2026-09-18T02:00:00.000Z', '2026-09-17T00:00:00.000Z', '2026-09-18T02:00:00.000Z')`,
		`INSERT INTO newsletter_editions (id, edition_key, subject, articles, created_at) VALUES
(601, '2026-09-19', 'Synthetic daily digest', '[{"title":"Synthetic Published Newer","slug":"synthetic-published-newer","excerpt":"Newer synthetic excerpt","category":"Synthetic AI","readingMinutes":1}]', '2026-09-19T13:00:00.000Z'),
(602, '2026-09-18', 'Synthetic malformed digest', '{malformed', '2026-09-18T13:00:00.000Z')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}

// fakeResend is an httptest stand-in for the Resend API that records each
// email and answers like the Node capture's recorder (synthetic-email-<n>).
type fakeResend struct {
	mu       sync.Mutex
	requests []resendRequest
}

type resendRequest struct {
	Authorization  string
	IdempotencyKey string
	Body           struct {
		From    string            `json:"from"`
		To      []string          `json:"to"`
		Subject string            `json:"subject"`
		HTML    string            `json:"html"`
		Text    string            `json:"text"`
		ReplyTo string            `json:"reply_to"`
		Headers map[string]string `json:"headers"`
		Tags    []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"tags"`
	}
}

func (f *fakeResend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var request resendRequest
	request.Authorization = r.Header.Get("Authorization")
	request.IdempotencyKey = r.Header.Get("Idempotency-Key")
	data, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(data, &request.Body)
	f.mu.Lock()
	f.requests = append(f.requests, request)
	count := len(f.requests)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"id":"synthetic-email-`+strconv.Itoa(count)+`"}`)
}

func (f *fakeResend) snapshot() []resendRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]resendRequest(nil), f.requests...)
}

func newsletterApplication(t *testing.T, mutate func(*config.Config)) (http.Handler, *sql.DB, *fakeResend) {
	t.Helper()
	resend := &fakeResend{}
	server := httptest.NewServer(resend)
	t.Cleanup(server.Close)
	app.StubNewsletterDeliveryForTest(t, server.URL+"/emails", server.Client(), func(context.Context) error { return nil })

	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	seedContractArticles(t, db)
	seedContractNewsletter(t, db)
	fixed := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	cfg := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4402", DatabasePath: filepath.Join(t.TempDir(), "unused.db"),
		JWTSecret: authTestSecret, UploadsDir: t.TempDir(),
		NewsletterSiteURL: "https://site.example.invalid", NewsletterTokenSecret: contractNewsletterSecret,
		NewsletterCronSecret: contractCronSecret, ResendAPIKey: contractResendKey,
		NewsletterFrom: "Synthetic Contract <newsletter@example.invalid>", NewsletterReplyTo: "reply@example.invalid"}
	if mutate != nil {
		mutate(&cfg)
	}
	application, err := app.NewWithDatabaseAt(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db, func() time.Time { return fixed })
	if err != nil {
		t.Fatal(err)
	}
	return application.Handler(), db, resend
}

// resolveNewsletterBindings mints the fixture's newsletter tokens with the Go
// implementation and checks each against Node's recorded SHA-256 and
// payload: links Node signed must be the links Go signs.
func resolveNewsletterBindings(t *testing.T, contract *contracttest.Contract) map[string]string {
	t.Helper()
	secrets := map[string]string{"NEWSLETTER_TOKEN_SECRET": contractNewsletterSecret, "CRON_SECRET": contractCronSecret}
	values := map[string]string{}
	for _, binding := range contract.Replay.Bindings {
		resolver := binding.Resolver
		switch resolver.Type {
		case "newsletterToken":
			secret, ok := secrets[resolver.SecretRef]
			if !ok {
				t.Fatalf("binding %s references an unavailable test secret", binding.Placeholder)
			}
			expiresAt, err := time.Parse(time.RFC3339Nano, resolver.ExpiresAt)
			if err != nil {
				t.Fatal(err)
			}
			token := newsletter.CreateToken(int64(resolver.SubscriberID), newsletter.Purpose(resolver.Purpose), secret, &expiresAt)
			var vector struct {
				Payload json.RawMessage `json:"payload"`
				SHA256  string          `json:"sha256"`
			}
			if err := json.Unmarshal(binding.Vector, &vector); err != nil || vector.SHA256 == "" {
				t.Fatalf("binding %s has no vector", binding.Placeholder)
			}
			digest := sha256.Sum256([]byte(token))
			if hex.EncodeToString(digest[:]) != vector.SHA256 {
				t.Fatalf("binding %s: the Go token's SHA-256 does not match Node's recorded vector", binding.Placeholder)
			}
			payload, err := base64.RawURLEncoding.DecodeString(strings.Split(token, ".")[0])
			if err != nil {
				t.Fatal(err)
			}
			var got, want any
			if json.Unmarshal(payload, &got) != nil || json.Unmarshal(vector.Payload, &want) != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("binding %s payload = %s, want %s", binding.Placeholder, payload, vector.Payload)
			}
			values[binding.Placeholder] = token
		case "secretRefTemplate":
			secret, ok := secrets[resolver.SecretRef]
			if !ok {
				t.Fatalf("binding %s references an unavailable test secret", binding.Placeholder)
			}
			values[binding.Placeholder] = strings.ReplaceAll(resolver.Value, "${secret}", secret)
		}
	}
	for _, placeholder := range []string{"$NEWSLETTER_CONFIRM_TOKEN", "$NEWSLETTER_UNSUBSCRIBE_ACTIVE_TOKEN", "$NEWSLETTER_UNSUBSCRIBE_OLD_TOKEN", "$CRON_AUTHORIZATION"} {
		if values[placeholder] == "" {
			t.Fatalf("binding %s was not resolved", placeholder)
		}
	}
	return values
}

func TestNewsletterMatchesApprovedNodeContractSequence(t *testing.T) {
	handler, db, resend := newsletterApplication(t, nil)
	contract, err := contracttest.Load(filepath.Join("..", "..", "contracts", "fixtures", "node-contracts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixtureOperations []string
	for _, operation := range contract.Operations {
		if strings.HasPrefix(operation.OperationID, "newsletter.") {
			fixtureOperations = append(fixtureOperations, operation.OperationID)
		}
	}
	if !reflect.DeepEqual(fixtureOperations, newsletterOperations) {
		t.Fatalf("fixture newsletter operations = %v, want %v", fixtureOperations, newsletterOperations)
	}
	if !contract.FixedClock.Equal(time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("fixed clock = %v", contract.FixedClock)
	}
	bindings := resolveNewsletterBindings(t, contract)
	previous := -1
	for _, id := range newsletterOperations {
		previous = assertAfter(t, contract, id, previous)
		operation, _ := contract.Operation(id)
		resolved, err := resolveAuthOperation(operation, bindings)
		if err != nil {
			t.Fatalf("resolve %s: %v", id, err)
		}
		// Replay errors print response bodies only, never tokens or secrets.
		if err := contracttest.Replay(handler, resolved); err != nil {
			t.Fatalf("%s replay: %v", id, err)
		}
	}

	// The capture's email effects: the welcome email for the confirmed
	// pending subscriber, then one digest to each active subscriber (the
	// confirmed 501 and the newly subscribed 504); digestPost skips both.
	requests := resend.snapshot()
	var got []string
	for _, request := range requests {
		got = append(got, strings.Join(request.Body.To, ",")+" "+request.IdempotencyKey+" "+request.Body.Subject)
		if request.Authorization != "Bearer "+contractResendKey || request.Body.From != "Synthetic Contract <newsletter@example.invalid>" || request.Body.ReplyTo != "reply@example.invalid" {
			t.Errorf("request %s: authorization matches = %t, from = %q, reply_to = %q", request.IdempotencyKey, request.Authorization == "Bearer "+contractResendKey, request.Body.From, request.Body.ReplyTo)
		}
		if unsubscribe := request.Body.Headers["List-Unsubscribe"]; !strings.HasPrefix(unsubscribe, "<https://site.example.invalid/api/newsletter/unsubscribe?token=") {
			t.Errorf("request %s List-Unsubscribe = %q", request.IdempotencyKey, unsubscribe)
		}
	}
	want := []string{
		"pending@example.invalid newsletter-welcome-501 Welcome to AI & Tech News",
		"pending@example.invalid newsletter-digest-2026-09-20-501 Synthetic Published Newer",
		"capture@example.invalid newsletter-digest-2026-09-20-504 Synthetic Published Newer",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("emails = %q, want %q", got, want)
	}
	var sent int
	if err := db.QueryRow(`SELECT COUNT(*) FROM newsletter_deliveries WHERE edition_key = '2026-09-20' AND status = 'sent'`).Scan(&sent); err != nil || sent != 2 {
		t.Fatalf("sent deliveries = %d, %v", sent, err)
	}
	var subject, articles string
	if err := db.QueryRow(`SELECT subject, articles FROM newsletter_editions WHERE edition_key = '2026-09-20'`).Scan(&subject, &articles); err != nil {
		t.Fatal(err)
	}
	if subject != "Synthetic Published Newer" || articles != `[{"title":"Synthetic Published Newer","slug":"synthetic-published-newer","excerpt":"Newer synthetic excerpt","category":"Synthetic AI","readingMinutes":1}]` {
		t.Fatalf("edition = %q %s", subject, articles)
	}
}

func TestNewsletterSignupWorksWithoutAnyNewsletterSettings(t *testing.T) {
	handler, _, resend := newsletterApplication(t, func(cfg *config.Config) {
		cfg.NewsletterSiteURL, cfg.NewsletterTokenSecret, cfg.NewsletterCronSecret = "", "", ""
		cfg.ResendAPIKey, cfg.NewsletterFrom, cfg.NewsletterReplyTo = "", "", ""
	})
	assertAuthResponse(t, request(t, handler, "POST", "/api/subscribe", `{"email":"new@example.invalid","placement":"footer"}`, ""),
		200, `{"success":true,"state":"subscribed","message":"You're subscribed. The next digest will arrive in your inbox."}`)
	assertAuthResponse(t, request(t, handler, "GET", "/api/newsletter/unsubscribe?token=a.b", "", ""),
		503, `{"state":"unavailable","error":"Unsubscribe is temporarily unavailable"}`)
	assertAuthResponse(t, request(t, handler, "GET", "/api/newsletter/confirm?token=a.b", "", ""),
		503, `{"state":"unavailable","error":"Newsletter confirmation is temporarily unavailable"}`)
	// No cron secret: the digest refuses everything.
	assertAuthResponse(t, request(t, handler, "GET", "/api/newsletter/digest", "", "Bearer "),
		401, `{"error":"Unauthorized"}`)
	if len(resend.snapshot()) != 0 {
		t.Fatal("an unconfigured newsletter reached the email provider")
	}
}

func TestNewsletterRoutesNeedNoDashboardLogin(t *testing.T) {
	handler, _, _ := newsletterApplication(t, nil)
	assertAuthResponse(t, request(t, handler, "GET", "/api/newsletter/editions?limit=1", "", ""), 200,
		`{"editions":[{"edition":"2026-09-19","subject":"Synthetic daily digest","articles":[{"title":"Synthetic Published Newer","slug":"synthetic-published-newer","excerpt":"Newer synthetic excerpt","category":"Synthetic AI","readingMinutes":1}],"createdAt":"2026-09-19T13:00:00.000Z"}]}`)
	assertAuthResponse(t, request(t, handler, "GET", "/api/newsletter/editions/2026-09-17", "", ""), 404, `{"error":"Edition not found"}`)
	assertAuthResponse(t, request(t, handler, "POST", "/api/newsletter/digest", "", "Bearer wrong"), 401, `{"error":"Unauthorized"}`)
}

func TestCompositionFailsOnAnInvalidNewsletterSiteURL(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	for _, siteURL := range []string{"http://example.com", "not a url"} {
		cfg := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4402", DatabasePath: filepath.Join(t.TempDir(), "unused.db"),
			JWTSecret: authTestSecret, UploadsDir: t.TempDir(), NewsletterSiteURL: siteURL}
		if _, err := app.NewWithDatabase(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db); err == nil || !strings.Contains(err.Error(), "NEWSLETTER_SITE_URL") {
			t.Errorf("NEWSLETTER_SITE_URL=%q: composition error = %v", siteURL, err)
		}
	}
}
```

Add the stub to `internal/app/export_test.go`:

```diff
diff --git a/apps/server-go/internal/app/export_test.go b/apps/server-go/internal/app/export_test.go
index 5773d10..3eb723a 100644
--- a/apps/server-go/internal/app/export_test.go
+++ b/apps/server-go/internal/app/export_test.go
@@ -18,3 +18,17 @@ func StubObjectStorageForTest(t testing.TB, open func(context.Context, config.Co
 	newPublicHTTPClient = func() *http.Client { return client }
 	t.Cleanup(func() { openPublisherStorage, newPublicHTTPClient = originalOpen, originalClient })
 }
+
+// StubNewsletterDeliveryForTest points newsletter delivery at a test server
+// (an httptest Resend stand-in) and replaces the 550ms pause between digest
+// deliveries, so app tests never reach the real Resend API or wait.
+func StubNewsletterDeliveryForTest(t testing.TB, endpoint string, client *http.Client, pace func(context.Context) error) {
+	t.Helper()
+	originalEndpoint, originalClient, originalPace := resendEndpoint, newResendHTTPClient, newsletterPace
+	resendEndpoint = endpoint
+	newResendHTTPClient = func() *http.Client { return client }
+	newsletterPace = pace
+	t.Cleanup(func() {
+		resendEndpoint, newResendHTTPClient, newsletterPace = originalEndpoint, originalClient, originalPace
+	})
+}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -p 2 ./internal/app/ -run Newsletter`
Expected: FAIL to compile:

```
internal/app/export_test.go:27:52: undefined: resendEndpoint
internal/app/export_test.go:27:68: undefined: newResendHTTPClient
internal/app/export_test.go:27:89: undefined: newsletterPace
```

- [ ] **Step 3: Implement.** Compose, mount, and bind in `internal/app/app.go`:

```diff
diff --git a/apps/server-go/internal/app/app.go b/apps/server-go/internal/app/app.go
index 7621302..4ddf50f 100644
--- a/apps/server-go/internal/app/app.go
+++ b/apps/server-go/internal/app/app.go
@@ -7,6 +7,7 @@ import (
 	"fmt"
 	"log/slog"
 	"net/http"
+	"strings"
 	"sync"
 	"time"
 
@@ -34,6 +35,8 @@ type App struct {
 	server        *httpserver.Server
 	background    []func(context.Context)
 	feedCollector *collector.Collector
+	// newsletter is bound to Run's context so shutdown stops a digest run.
+	newsletter *newsletter.Service
 	// drains run after the server and background tasks stop, to finish
 	// fire-and-forget work such as dashboard IndexNow submissions.
 	drains []func()
@@ -121,6 +124,20 @@ func NewWithDatabaseAt(cfg config.Config, logger *slog.Logger, db *sql.DB, now f
 			objectAPI, newPublicHTTPClient())
 	}
 	mediaStorage := media.NewStorage(uploads, s3Uploads)
+	newsletterService, err := newsletter.NewService(newsletter.NewSQLiteStore(db),
+		newsletter.NewResendSender(newsletter.ResendConfig{
+			APIKey: cfg.ResendAPIKey, From: cfg.NewsletterFrom, ReplyTo: cfg.NewsletterReplyTo,
+			Endpoint: resendEndpoint, Client: newResendHTTPClient(),
+		}),
+		newsletter.ServiceConfig{SiteURL: cfg.NewsletterSiteURL, TokenSecret: cfg.NewsletterTokenSecret, Pace: newsletterPace},
+		logger)
+	if err != nil {
+		return nil, fmt.Errorf("newsletter: %w", err)
+	}
+	if strings.TrimSpace(cfg.ResendAPIKey) == "" || strings.TrimSpace(cfg.NewsletterFrom) == "" {
+		logger.Info("Newsletter delivery not configured (RESEND_API_KEY, NEWSLETTER_FROM); signups still work, digest deliveries will fail")
+	}
+	newsletterHandler := newsletter.NewHandler(newsletterService, cfg.NewsletterCronSecret, now, logger)
 	dashboardMedia := media.NewHandler(media.NewLibrary(media.NewSQLiteLibrary(db), mediaStorage, now), mediaStorage, logger)
 	handler := httpserver.NewRouter(logger, func(router chi.Router) {
 		health.MountPublic(router)
@@ -128,6 +145,7 @@ func NewWithDatabaseAt(cfg config.Config, logger *slog.Logger, db *sql.DB, now f
 		categories.MountPublic(router)
 		authors.MountPublic(router)
 		auth.Mount(router)
+		newsletterHandler.Mount(router)
 		newsroomHandler.Mount(router, auth.RequireAuth)
 		// Node guards every dashboard path, including unknown ones, with
 		// requireAuth before routing (dashboard.ts:94).
@@ -139,7 +157,7 @@ func NewWithDatabaseAt(cfg config.Config, logger *slog.Logger, db *sql.DB, now f
 		})
 	}, uploads.MountStatic)
 	server := httpserver.NewServer(cfg.Address, handler, logger)
-	application := &App{address: cfg.Address, handler: handler, server: server, drains: []func(){drainIndexNow}}
+	application := &App{address: cfg.Address, handler: handler, server: server, newsletter: newsletterService, drains: []func(){drainIndexNow}}
 	if feedCollector != nil {
 		application.feedCollector = feedCollector
 		application.background = append(application.background, func(ctx context.Context) {
@@ -168,6 +186,16 @@ func NewWithDatabaseAt(cfg config.Config, logger *slog.Logger, db *sql.DB, now f
 	return application, nil
 }
 
+// resendEndpoint, newResendHTTPClient, and newsletterPace configure newsletter
+// delivery. They are variables only so app tests can point delivery at an
+// httptest server and skip Node's 550ms pause; nil means the newsletter
+// package's defaults (a client with ResendAttemptTimeout, 550ms pacing).
+var (
+	resendEndpoint                                  = newsletter.DefaultResendEndpoint
+	newResendHTTPClient                             = func() *http.Client { return nil }
+	newsletterPace      func(context.Context) error = nil
+)
+
 // newPublicHTTPClient builds the client that verifies uploaded objects
 // through their public URL. It is a variable so app tests can stay offline.
 var newPublicHTTPClient = func() *http.Client { return &http.Client{} }
@@ -203,6 +231,10 @@ func (a *App) Handler() http.Handler { return a.handler }
 // instead of being abandoned mid-write on shutdown.
 func (a *App) Run(ctx context.Context) error {
 	tasksCtx, cancel := context.WithCancel(ctx)
+	if a.newsletter != nil {
+		// A digest outlives its HTTP request (like Node) but not shutdown.
+		a.newsletter.Bind(ctx)
+	}
 	if a.feedCollector != nil {
 		a.feedCollector.Bind(tasksCtx)
 	}
```

- [ ] **Step 4: Run them to verify they pass, then the contract check**

Run: `go test -p 2 ./internal/app/ && go test -p 2 -race ./internal/newsletter ./internal/app && make contracts-check`
Expected: `ok`; the race run passes; `contracts-check` reports the mirror in sync and `ok` for `internal/contracttest` and `internal/app -run Contract`.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/export_test.go internal/app/newsletter_contract_test.go
git commit -m "Mount the newsletter routes and replay the Node newsletter contracts"
```

---

### Task 12: Documentation

**Files:** Modify `README.md`, `../../docs/plans/2026-09-20-go-api-migration.md`

- [ ] **Step 1: Update the README** (capability statement, configuration table, redaction, architecture, and a Newsletter section):

````diff
diff --git a/apps/server-go/README.md b/apps/server-go/README.md
index 29f1e69..6907948 100644
--- a/apps/server-go/README.md
+++ b/apps/server-go/README.md
@@ -2,7 +2,7 @@
 
 This directory is an isolated Go module for the API migration. It does not share a Go module or developer commands with the existing Node server.
 
-Tasks 5 through 11 currently provide the four public article reads, public category and author listings, compatible login, current-user, and logout endpoints, a pure Go port of the retained publishing policy, the authenticated dashboard content administration (dashboard article list/get/create/update/delete, category list/create/update/delete, and site settings get/put), and the dashboard media library (media list/upload/delete plus static `/uploads/*` serving from a local directory). This is a migration slice, not a claim of production or cutover readiness; newsletter and cutover verification are still pending.
+Tasks 5 through 12 currently provide the four public article reads, public category and author listings, compatible login, current-user, and logout endpoints, a pure Go port of the retained publishing policy, the authenticated dashboard content administration (dashboard article list/get/create/update/delete, category list/create/update/delete, and site settings get/put), the dashboard media library (media list/upload/delete plus static `/uploads/*` serving from a local directory), and the newsletter (signup, confirm and unsubscribe links, the edition archive, and the daily digest delivered through Resend). This is a migration slice, not a claim of production or cutover readiness; cutover verification is still pending.
 
 ## Safety defaults
 
@@ -52,11 +52,18 @@ Review contract changes in this order:
 | `S3_FEATURE_IMAGE_BUCKET` | none | Required when `PUBLISHER_ENABLED=1`; bucket that stores generated feature images |
 | `S3_FEATURE_IMAGE_PREFIX` | `features` | Key prefix under which feature images are stored |
 | `S3_FEATURE_IMAGE_PUBLIC_URL` | none | Required when `PUBLISHER_ENABLED=1`; public `https://` base URL feature images are served from |
+| `NEWSLETTER_SITE_URL` | `https://aiandtech.news` | Origin used in every email link. Like Node it must be `https` (plain `http` only for `localhost`); only its origin is used. An invalid value fails composition, as it stops Node from starting. Go accepts only `http`/`https` URLs with ASCII host names |
+| `NEWSLETTER_TOKEN_SECRET` | none | Signs and verifies confirm and unsubscribe links (Node's HMAC-SHA256 token format, byte for byte). **Must be the Node value**, or links in emails already sent stop working. Needs at least 32 characters after trimming; otherwise confirm and unsubscribe answer `503` and the digest `503`, while signup keeps working |
+| `NEWSLETTER_CRON_SECRET` | value of `CRON_SECRET` | Secret for `GET`/`POST /api/newsletter/digest` (`Authorization: Bearer <secret>`). Node reads `NEWSLETTER_CRON_SECRET \|\| CRON_SECRET`, so only an empty value falls back. Empty (or blank) refuses every digest request with `401` |
+| `CRON_SECRET` | none | Used only when `NEWSLETTER_CRON_SECRET` is empty; the Vercel project's name for the same secret |
+| `RESEND_API_KEY` | none | Resend API key. Without it (or without `NEWSLETTER_FROM`) nothing is sent: each digest delivery is recorded as failed with `Newsletter delivery is not configured`, like Node |
+| `NEWSLETTER_FROM` | none | Sender, for example `AI & Tech News <news@aiandtech.news>` (a verified Resend domain) |
+| `NEWSLETTER_REPLY_TO` | none | Optional `reply_to` for every email |
 | `INDEXNOW_ENABLED` | `0` | Submits article URLs to IndexNow: each publisher-published article, and dashboard publishes, unpublishes, published-slug renames, and deletions of published articles. Off by default, so local runs never ping IndexNow for an article that only exists in the dev database |
 
 AWS credentials always come from the default AWS SDK credential chain (for example `AWS_PROFILE`), never from a file in this repo.
 
-Configuration is represented by `internal/config.Config` and validated before runtime resources are opened. String and Go-syntax formatting redact `JWT_SECRET` and `GEMINI_API_KEY`. Health-only `app.New` does not require the secret, while database-backed composition fails before serving when it is absent. When `PUBLISHER_ENABLED=1`, `Validate` additionally requires `GEMINI_API_KEY`, `AWS_REGION`, `S3_FEATURE_IMAGE_BUCKET`, `S3_FEATURE_IMAGE_PUBLIC_URL` (as an `https://` URL) and a `PUBLISHER_INTERVAL` of at least `10s`. `INDEXNOW_ENABLED` uses the same strict on/off parser as the other `*_ENABLED` flags and needs no other setting.
+Configuration is represented by `internal/config.Config` and validated before runtime resources are opened. String and Go-syntax formatting redact `JWT_SECRET`, `GEMINI_API_KEY`, `RESEND_API_KEY`, `NEWSLETTER_TOKEN_SECRET`, and the cron secret. The newsletter settings are optional at startup, like in Node. Health-only `app.New` does not require the secret, while database-backed composition fails before serving when it is absent. When `PUBLISHER_ENABLED=1`, `Validate` additionally requires `GEMINI_API_KEY`, `AWS_REGION`, `S3_FEATURE_IMAGE_BUCKET`, `S3_FEATURE_IMAGE_PUBLIC_URL` (as an `https://` URL) and a `PUBLISHER_INTERVAL` of at least `10s`. `INDEXNOW_ENABLED` uses the same strict on/off parser as the other `*_ENABLED` flags and needs no other setting.
 
 At startup, an enabled publisher also loads AWS credentials and calls `HeadBucket` on `S3_FEATURE_IMAGE_BUCKET`, failing composition fast if credentials are missing/rejected or the bucket cannot be reached. A `403` from `HeadBucket` is tolerated: least-privilege importer IAM users that can only put/delete objects under their prefix (no `s3:ListBucket`) do not have permission to call it, and a denied write is still caught as a system fault at publish time.
 
@@ -79,7 +86,7 @@ make contracts-accept  # explicitly accept the reviewed canonical Node fixture
 go run ./cmd/migrate   # explicitly migrate the guarded configured database
 ```
 
-`cmd/api` opens and closes its configured database but never migrates or seeds it. Run `cmd/migrate` as an explicit deployment step first. `cmd/api` also creates `UPLOADS_DIR` if it is missing. `app.New` remains a no-I/O health-only composition; `app.NewWithDatabase` audibly mounts article, category, author, authentication, newsroom, authenticated `/api/dashboard` content, settings, and media routes, and the static `/uploads/*` files around a caller-owned database and uploads directory.
+`cmd/api` opens and closes its configured database but never migrates or seeds it. Run `cmd/migrate` as an explicit deployment step first. `cmd/api` also creates `UPLOADS_DIR` if it is missing. `app.New` remains a no-I/O health-only composition; `app.NewWithDatabase` audibly mounts article, category, author, authentication, newsroom, authenticated `/api/dashboard` content, settings, and media routes, the public newsletter routes, and the static `/uploads/*` files around a caller-owned database and uploads directory.
 
 The migration runner supports fresh databases and databases already managed by its ledger. It intentionally cannot stamp or adopt an existing unmanaged database initialized by the Node server, although `cmd/api` can read that compatible schema. Cutover requires a future explicit full-schema verifier/adoption command; do not weaken `migrate.Run` or partially stamp an unmanaged database.
 
@@ -92,7 +99,7 @@ The service is a single binary with a `cmd` plus `internal` layout:
 - `editorial` owns the foundational v1 authors schema and public author read stack, while `content` owns the v2 categories/articles schema and the public article and category read stacks. Their public handlers mount relative route manifests from the composition root.
 - `settings` owns the dashboard site-settings behavior but no migration: newsroom migration 3 creates the shared `settings` table with Node's schema.
 - `media` owns both article-image stores: generated feature images in S3 and the dashboard media library (migration 5, Node's `media` table), whose files live in `UPLOADS_DIR`.
-- Future cohesive capabilities such as `newsletter` own their domain, repository, service, HTTP handlers, relative route mounting, and migration SQL.
+- `newsletter` owns migration 6 (Node's `subscribers`, `newsletter_deliveries`, and `newsletter_editions` tables) and the public newsletter routes; it only reads `articles` and `categories`. Like every capability it owns its domain, repository, service, HTTP handlers, relative route mounting, and migration SQL.
 - `internal/database/migrate` owns only the migration ledger and runner; it does not own capability schema SQL. The runner owns migration transaction boundaries, so descriptors must not contain transaction control, `VACUUM`, `ATTACH`, `DETACH`, or `PRAGMA` statements. `ATTACH`, `DETACH`, and `PRAGMA` are rejected because their file, attachment, or connection effects can survive a rollback.
 - Narrow infrastructure packages live under `internal` and are named for their purpose.
 - Interfaces are declared by the consuming package at the point of use. Constructors return concrete types unless a consumer needs an interface.
@@ -119,6 +126,25 @@ Every file operation goes through an `os.Root` opened on `UPLOADS_DIR`, so neith
 
 At cutover the Node uploads directory is copied with the database; point `UPLOADS_DIR` at the copy so every existing `/uploads/...` URL in `articles.featured_image`, `authors.avatar`, and `media.url` keeps resolving.
 
+## Newsletter
+
+`internal/newsletter` ports `apps/server/src/newsletter/*` and the newsletter routes of `apps/server/src/routes/public.ts`; the plan, with every kept Node behavior and every Go difference, is `docs/superpowers/plans/2026-09-24-go-newsletter.md`. The routes are public (no dashboard login): `POST /api/subscribe`, `GET /api/newsletter/confirm`, `GET` and `POST /api/newsletter/unsubscribe`, `GET /api/newsletter/editions`, `GET /api/newsletter/editions/:edition`, and `GET` and `POST /api/newsletter/digest` (cron secret). The eight Node newsletter contracts replay in `internal/app/newsletter_contract_test.go`.
+
+Parity is pinned by vectors recorded from the Node implementation (`internal/newsletter/testdata/node-golden.json`, regenerated by `testdata/record-node.ts`, see its header), not inferred:
+
+- **Signup** activates the address at once and sends nothing, so it works without any email settings; resubmitting is idempotent, and pending (old double opt-in) and unsubscribed rows are reactivated. Addresses are trimmed and lowercased with JavaScript semantics (Unicode whitespace, `İ`, final sigma) and validated with Node's pattern and 254-code-unit limit. Signups share Node's global throttle of 30 attempts per rolling minute (`429`).
+- **Tokens** are byte-identical: `base64url(JSON)` + `.` + `base64url(HMAC-SHA256)`, the secret trimmed, verification as lenient as Node's (a trailing `.` is accepted, the payload is decoded like `Buffer.from(…, "base64url")`, `exp` compared in whole seconds). Links in emails Node already sent keep working as long as `NEWSLETTER_TOKEN_SECRET` is unchanged.
+- **Editions** are keyed by the Europe/Istanbul calendar date (the zone database is embedded, so the host's zoneinfo does not matter). The archive passes stored article JSON through when it is an array and shows `[]` otherwise.
+- **Digest selection** is Node's: the 20 published articles with the greatest `published_at` text, kept when published in the last 30 hours and not in the future (Date.parse semantics: SQLite timestamps and date-only values are UTC, date-times without an offset are read in the process time zone), first 5, grouped by category in the email.
+- **Emails** (HTML and text) and the **Resend request body** are byte-identical to Node's. Idempotency keys are Node's, `newsletter-welcome-<subscriber>` and `newsletter-digest-<edition>-<subscriber>`. At most three attempts; only `429` and `5xx` are retried, after `Retry-After` seconds or 650ms then 1300ms; transport errors (recorded as `fetch failed`), other statuses, and a `2xx` without an id fail at once. Each attempt has a 30s timeout.
+- **Deliveries** move `sending` -> `sent` (provider message id, `sent_at`) or `failed` (error truncated to 500 UTF-16 units). The next run retries `failed` and `sending` rows with the same idempotency key and skips `sent` ones. Deliveries are 550ms apart.
+
+Go-only behavior: digest runs are serialized within the process, so overlapping requests (a cron retry and a manual `POST`) never deliver the same edition twice; like Node, a started digest keeps running when the HTTP client goes away, but shutdown stops it between deliveries (the outcome of an in-flight send is still recorded). The digest route has a 15-minute write deadline and the confirm route 2 minutes (for the welcome email); other routes keep the server's timeouts. Error bodies for malformed JSON follow the earlier slices (`400 {"error":"Invalid request body"}`).
+
+**Node and Go on one database.** Only one of them should serve the digest: the Vercel cron calls whatever `API_URL` points at, so switching `API_URL` switches the digest. If both ever ran a digest for the same edition, the per-row `sent` check is not a lock between processes; what prevents a second email is Resend's idempotency key, which both use in the same format with byte-identical payloads: for 24 hours Resend answers a repeated key with the original send instead of sending again (a different payload under the same key is refused with `409` and recorded as failed). No test or code path in this repository calls the real Resend API: tests use `httptest` servers or fakes.
+
+At cutover, copy `NEWSLETTER_TOKEN_SECRET`, `RESEND_API_KEY`, `NEWSLETTER_FROM`, `NEWSLETTER_REPLY_TO`, `NEWSLETTER_SITE_URL`, and `NEWSLETTER_CRON_SECRET` (equal to the Vercel project's `CRON_SECRET`) from the Node environment, and run the Go process with the same `TZ` as the Node host (offset-less `published_at` values from the dashboard's date picker are read in that zone).
+
 ## Newsroom (editorial queue)
 
 `internal/newsroom` owns the `candidates` table (migration 3) and the
````

- [ ] **Step 2: Record task 12's status** in the migration plan:

```diff
diff --git a/docs/plans/2026-09-20-go-api-migration.md b/docs/plans/2026-09-20-go-api-migration.md
index f6c7f72..e92b407 100644
--- a/docs/plans/2026-09-20-go-api-migration.md
+++ b/docs/plans/2026-09-20-go-api-migration.md
@@ -250,6 +250,8 @@ A capability may omit files/layers it does not need. No `utils`, `common`, `inte
 
 ### Task 12: Implement newsletter and Resend integration
 
+**Status (2026-09-24): Complete for the preserved migration slice via `docs/superpowers/plans/2026-09-24-go-newsletter.md`: `internal/newsletter` ports signup, confirm and unsubscribe links (byte-identical HMAC tokens), the edition archive, and the daily digest through Resend (byte-identical emails and request bodies, Node's idempotency keys, retry classes, delivery states, and Europe/Istanbul edition dates), with migration 6 (Node's newsletter tables) and the eight Node newsletter contracts replaying in `internal/app/newsletter_contract_test.go`. Parity vectors were recorded from the Node implementation (`internal/newsletter/testdata/node-golden.json`). This does not establish production or cutover readiness.**
+
 **Objective:** Port subscription, token compatibility, edition archive, digest selection, idempotent delivery, retries, and unsubscribe behavior.
 
 **Files:** Create module files/tests and capability-owned migration SQL under `internal/newsletter`; extend `internal/app` to add the newsletter descriptor in order.
```

- [ ] **Step 3: Verify everything**

```bash
gofmt -l .                      # no output
go vet -p 2 ./...
go test -p 2 ./...
go test -p 2 -race ./internal/newsletter ./internal/app
make contracts-check
```

Expected: no `gofmt` output, `vet` clean, every package `ok`, race `ok`, contracts in sync.

- [ ] **Step 4: Commit**

```bash
git add README.md ../../docs/plans/2026-09-20-go-api-migration.md
git commit -m "Document the Go newsletter port"
```

---

## Self-review checklist (for the executing agent)

- Every expected value in a parity test comes from `node-golden.json` (or was checked in Node and says so); none was written from reading the TypeScript.
- No test composes a `ResendSender` with the default endpoint and a key; `grep -rln "api.resend.com" internal` lists only `resend.go` (`DefaultResendEndpoint`) and `testdata/node-golden.json` (the URL Node's stubbed `fetch` was called with).
- No transaction spans a network call; no `rows` iterator is open while another statement runs (the pool has one connection).
- `NEWSLETTER_TOKEN_SECRET`, `RESEND_API_KEY`, and the cron secret never appear in logs, errors, or formatted config.
- The digest never leaves a row `sending` because of Go's own cancellation (`TestShutdownStopsTheDigestBetweenDeliveries`).
- Migration 6 is `IF NOT EXISTS` throughout and leaves a Node-created schema byte-identical (`TestNewsletterMigrationSQLKeepsTablesNodeAlreadyCreated`).
- The branch has exactly the twelve commits above, with those messages and no co-author trailers.

---

## Follow-up after review (applied on `go-newsletter`)

Each item was test-first where it changes behavior; the commits follow the twelve task commits.

| Commit message | Change | Tests |
|---|---|---|
| Cap Resend Retry-After waits at 60 seconds and log transport failure causes | `maxRetryDelay`; `fetchError.LogValue` | `TestResendRetryDelayFollowsNodeTimers`, `TestFetchErrorsLogTheirCauseButStoreNodesMessage` |
| Decode the edition route parameter once, like Express | unescape `chi.URLParam` only when `r.URL.RawPath` is set, so `/editions/100%25` looks up `100%` (404) instead of answering 400 | `TestEditionKeyIsDecodedOnceLikeExpress` |
| Bind welcome emails to shutdown, track in-flight newsletter work, and let queued digests exit at shutdown | `Service.begin` (detached from the request, cancelled by the lifecycle, counted in flight), `Service.Wait`, the digest lock as a one-slot channel selected against the run context | `TestShutdownDuringASendRecordsAFailureAndTheRetryReusesTheKey`, `TestQueuedDigestRunsExitAtShutdown`, `TestWaitReturnsOnceInFlightRunsEnd`, `TestShutdownCancelsTheWelcomeEmail` |
| Wait for in-flight newsletter work after the server stops | `App.Run` → `run(ctx, serve)`; after the server and background tasks stop, `Service.Wait` with `newsletterDrainTimeout` (15s), before the other drains | `TestRunWaitsForAnInFlightDigestAfterTheServerStops` (via `app.RunWithServeForTest`, no listener) |
| Limit newsletter signups per client IP with a global ceiling | Approved change 1 | see above |
| Keep an article's publish time when the dashboard editor saves it | Approved change 3 (dashboard; `tsc --noEmit` clean) | helper round trip checked in Node |
| Pin that ISO timestamps from the dashboard parse the same in every zone | Go test only | `TestDashboardISOTimestampsAreTheSameInstantInEveryZone` |
| Pin that migration 6 fails on a subscribers table without status | Go test only | `TestNewsletterMigrationFailsOnASubscribersTableWithoutStatus` |
| Document the newsletter review follow-ups | README (approved changes, shutdown, same-day re-runs, supported databases) and this plan | |
