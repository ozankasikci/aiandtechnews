# Newsroom Publisher Core Implementation Plan (Phase 4a)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn a due queued candidate into a published article in the Go API: claim it (with minimum spacing), fetch and extract the source page, rewrite it with Gemini, get a featured image from an `Illustrator`, insert the article exactly like the Node importer, mark the candidate published and ping IndexNow — with transient failures retried and permanent failures recorded.

**Architecture:** New store seams in `internal/newsroom` (atomic claim with a minimum-gap rule, published/failed/requeue/reset transitions). New packages: `internal/gemini` (text client), `internal/indexnow`, and `internal/publisher` (source-page extraction, rewriter, SQLite article writer, orchestration + loop). The image step is the `publisher.Illustrator` interface, implemented in phase 4b (S3 illustrations); this plan does **not** wire the publisher into the app — 4b does, once an illustrator exists.

**Tech Stack:** Go 1.25 standard library, existing `content` policy, `collector.Fetcher`, `modernc.org/sqlite`.

**Roadmap:** `docs/superpowers/plans/2026-09-23-newsroom-pipeline-roadmap.md` (phase 4). **Node reference:** `apps/server/scripts/news-importer.ts` — `extractMetaContent` (:314), `extractCanonicalUrl`/`extractOgImage`/`extractJsonLdArticleBody`/`extractSourceText` (:409-496), `callGeminiText` (:529), `parseRewrittenArticle`/`rewriteArticle` (:559-623), `categorize`/`ensureCategories`/`ensureEditorialAuthor`/`isStored` (:667-705), `importFeedItem` (:707-841); `apps/server/src/indexnow.ts`.

**Working directory:** `apps/server-go`. **Commits:** plain sentences, no prefixes, no co-author trailers. **Safety:** temp DBs, httptest and fakes only — no real Gemini/IndexNow calls in tests.

**Compatibility rules (the website reads these rows through Node):**
- `articles.published_at`, `created_at`, `updated_at` use SQLite `datetime('now')` format: `2006-01-02 15:04:05` (UTC), exactly as Node writes them.
- Author "TechNews Editorial" (`content.EditorialAuthor()`), categories and colors as in Node, `status='published'`, `view_count=0`, `source` = publication name, `source_url` = canonical URL.
- Length limits (`800` chars of source text, `14000` cap, `500`-char JSON-LD body) count UTF-16 code units like JavaScript `.length`.

---

## File structure

| File | Responsibility |
|---|---|
| Modify `internal/newsroom/store.go` (+ `store_test.go`) | `ClaimDue`, `MarkPublished`, `MarkFailed`, `Requeue`, `ResetProcessing` |
| Modify `internal/newsroom/service.go` | Update the spacing comment (spacing now also enforced at claim time) |
| Modify `internal/collector/entities.go` (+ callers) | Export `DecodeHTMLEntities` for the publisher |
| Create `internal/publisher/text.go` | UTF-16 length/truncate helpers |
| Create `internal/publisher/source.go` (+ test) | Canonical URL, og:image, JSON-LD body, paragraph extraction |
| Create `internal/gemini/client.go` (+ test) | Gemini `generateContent` text client with typed errors |
| Create `internal/publisher/errors.go` | `Permanent` error marker |
| Create `internal/publisher/rewrite.go` (+ test) | Prompt (verbatim port), JSON parse, 2 attempts, validation |
| Create `internal/publisher/articles.go` (+ test) | Categorize, ensure categories/author, duplicate check, insert + readback |
| Create `internal/indexnow/client.go` (+ test) | IndexNow submission |
| Create `internal/publisher/publisher.go` (+ test) | Orchestration, failure classification, `Recover`, `Loop` |

---

### Task 1: Store seams for publishing

**Files:** Modify `internal/newsroom/store.go`, `internal/newsroom/store_test.go`

- [ ] **Step 1: Write failing tests** (append to `store_test.go`)

```go
// seedArticle inserts an author, category and article so article_id foreign keys resolve.
func seedArticle(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	mustExec(t, db, `INSERT OR IGNORE INTO authors (id, name, email, password_hash, role) VALUES (1, 'A', 'a@example.invalid', 'x', 'admin')`)
	mustExec(t, db, `INSERT OR IGNORE INTO categories (id, name, slug) VALUES (1, 'AI', 'ai')`)
	result, err := db.Exec(`INSERT INTO articles (title, slug, category_id, author_id, status) VALUES ('T', ?, 1, 1, 'published')`,
		fmt.Sprintf("slug-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	return id
}

func queueAt(t *testing.T, store *newsroom.SQLiteStore, url string, at time.Time) int64 {
	t.Helper()
	id := insert(t, store, url, t0)
	if err := store.MarkQueued(context.Background(), id, newsroom.StatusPending, at, t0); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestClaimDueTakesEarliestDueOneAtATimeWithMinimumGap(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	a := queueAt(t, store, "https://example.com/a", t0)
	b := queueAt(t, store, "https://example.com/b", t0.Add(10*time.Minute))
	gap := 30 * time.Minute

	if _, ok, err := store.ClaimDue(ctx, t0.Add(-time.Minute), gap); err != nil || ok {
		t.Fatalf("nothing due yet: ok=%v err=%v", ok, err)
	}
	claimed, ok, err := store.ClaimDue(ctx, t0.Add(20*time.Minute), gap)
	if err != nil || !ok || claimed.ID != a || claimed.Status != newsroom.StatusProcessing || claimed.Attempts != 1 {
		t.Fatalf("first claim = %+v ok=%v err=%v", claimed, ok, err)
	}
	if _, ok, _ := store.ClaimDue(ctx, t0.Add(20*time.Minute), gap); ok {
		t.Fatal("must not claim while another candidate is processing")
	}

	if err := store.MarkPublished(ctx, a, seedArticle(t, db), t0.Add(20*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.ClaimDue(ctx, t0.Add(45*time.Minute), gap); ok {
		t.Fatal("must wait the minimum gap after the last publish")
	}
	claimed, ok, err = store.ClaimDue(ctx, t0.Add(51*time.Minute), gap)
	if err != nil || !ok || claimed.ID != b {
		t.Fatalf("second claim = %+v ok=%v err=%v", claimed, ok, err)
	}

	published := mustGet(t, store, a)
	if published.Status != newsroom.StatusPublished || published.ScheduledFor != nil || published.ArticleSlug == nil {
		t.Fatalf("published = %+v", published)
	}
}

func TestRequeueFailAndResetProcessing(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	a := queueAt(t, store, "https://example.com/a", t0)

	if _, _, err := store.ClaimDue(ctx, t0, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.Requeue(ctx, a, "Gemini returned 503", t0.Add(5*time.Minute), t0); err != nil {
		t.Fatal(err)
	}
	requeued := mustGet(t, store, a)
	if requeued.Status != newsroom.StatusQueued || *requeued.ScheduledFor != "2026-09-20T12:05:00Z" ||
		requeued.LastError == nil || *requeued.LastError != "Gemini returned 503" || requeued.Attempts != 1 {
		t.Fatalf("requeued = %+v", requeued)
	}

	claimed, ok, _ := store.ClaimDue(ctx, t0.Add(6*time.Minute), 0)
	if !ok || claimed.Attempts != 2 {
		t.Fatalf("second claim = %+v", claimed)
	}
	if err := store.MarkFailed(ctx, a, "source text too short", t0.Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}
	failed := mustGet(t, store, a)
	if failed.Status != newsroom.StatusFailed || failed.ScheduledFor != nil || *failed.LastError != "source text too short" {
		t.Fatalf("failed = %+v", failed)
	}
	if err := store.MarkFailed(ctx, a, "again", t0); !errors.Is(err, newsroom.ErrStaleTransition) {
		t.Fatalf("stale fail err = %v", err)
	}

	b := queueAt(t, store, "https://example.com/b", t0)
	if _, ok, _ := store.ClaimDue(ctx, t0, 0); !ok {
		t.Fatal("claim b")
	}
	count, err := store.ResetProcessing(ctx, t0.Add(time.Hour))
	if err != nil || count != 1 {
		t.Fatalf("reset count=%d err=%v", count, err)
	}
	reset := mustGet(t, store, b)
	if reset.Status != newsroom.StatusQueued || *reset.ScheduledFor != "2026-09-20T13:00:00Z" {
		t.Fatalf("reset = %+v", reset)
	}
}
```

Add `"database/sql"` and `"fmt"` to the test imports if missing.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/newsroom -run 'TestClaimDue|TestRequeueFail'`
Expected: FAIL — `store.ClaimDue undefined`.

- [ ] **Step 3: Implement** (add to `store.go`)

```go
// ClaimDue moves the earliest due queued candidate to processing (attempts+1)
// and returns it. It claims nothing while another candidate is processing or
// while the most recent publish is newer than minGap, which keeps spacing
// after transient retries, crash recovery or downtime.
func (s *SQLiteStore) ClaimDue(ctx context.Context, now time.Time, minGap time.Duration) (Candidate, bool, error) {
	stamp := formatTime(now)
	var id int64
	err := s.db.QueryRowContext(ctx, `UPDATE candidates
		SET status = 'processing', attempts = attempts + 1, updated_at = ?
		WHERE id = (SELECT id FROM candidates WHERE status = 'queued' AND scheduled_for <= ?
		            ORDER BY scheduled_for, id LIMIT 1)
		  AND status = 'queued'
		  AND NOT EXISTS (SELECT 1 FROM candidates WHERE status = 'processing')
		  AND NOT EXISTS (SELECT 1 FROM candidates WHERE status = 'published' AND published_at > ?)
		RETURNING id`, stamp, stamp, formatTime(now.Add(-minGap))).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Candidate{}, false, nil
	}
	if err != nil {
		return Candidate{}, false, fmt.Errorf("claim due candidate: %w", err)
	}
	candidate, err := s.Get(ctx, id)
	if err != nil {
		return Candidate{}, false, err
	}
	return candidate, true, nil
}

// MarkPublished records the article created from a processing candidate.
func (s *SQLiteStore) MarkPublished(ctx context.Context, id, articleID int64, now time.Time) error {
	stamp := formatTime(now)
	return s.transition(ctx, id, `UPDATE candidates SET status = 'published', article_id = ?, published_at = ?,
		scheduled_for = NULL, last_error = NULL, updated_at = ? WHERE id = ? AND status = 'processing'`,
		articleID, stamp, stamp, id)
}

// MarkFailed stops retrying a processing candidate and records why.
func (s *SQLiteStore) MarkFailed(ctx context.Context, id int64, reason string, now time.Time) error {
	return s.transition(ctx, id, `UPDATE candidates SET status = 'failed', scheduled_for = NULL, last_error = ?,
		updated_at = ? WHERE id = ? AND status = 'processing'`, reason, formatTime(now), id)
}

// Requeue returns a processing candidate to the queue after a transient failure.
func (s *SQLiteStore) Requeue(ctx context.Context, id int64, reason string, at, now time.Time) error {
	return s.transition(ctx, id, `UPDATE candidates SET status = 'queued', scheduled_for = ?, last_error = ?,
		updated_at = ? WHERE id = ? AND status = 'processing'`, formatTime(at), reason, formatTime(now), id)
}

// ResetProcessing requeues candidates left processing by a crash, due now.
func (s *SQLiteStore) ResetProcessing(ctx context.Context, now time.Time) (int64, error) {
	stamp := formatTime(now)
	result, err := s.db.ExecContext(ctx, `UPDATE candidates SET status = 'queued', scheduled_for = ?, updated_at = ?
		WHERE status = 'processing'`, stamp, stamp)
	if err != nil {
		return 0, fmt.Errorf("reset processing candidates: %w", err)
	}
	return result.RowsAffected()
}
```

- [ ] **Step 4: Update the spacing comment** in `internal/newsroom/service.go` on the `scheduling` field: replace the sentence about all `scheduled_for` writes going through `Service` with: "The publisher's `ClaimDue` also enforces a minimum gap since the last publish, so retries that reschedule outside the queue tail (`Requeue`, `ResetProcessing`) cannot publish two items back to back."

- [ ] **Step 5: Run tests** — `go test -race ./internal/newsroom` → PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/newsroom
git commit -m "Add publishing transitions and spaced claiming to the newsroom store"
```

---

### Task 2: Source page extraction

**Files:** Modify `internal/collector/entities.go` (rename `decodeHTMLEntities` → `DecodeHTMLEntities`, update callers in `feed.go` and `entities_internal_test.go`); create `internal/publisher/text.go`, `internal/publisher/source.go`, `internal/publisher/source_test.go`

- [ ] **Step 1: Export the entity decoder**

Rename the function to `DecodeHTMLEntities` with doc comment "DecodeHTMLEntities ports decodeHtmlEntities from news-importer.ts …" and update every call site (`grep -rn decodeHTMLEntities internal/collector`). Run `go test ./internal/collector` → PASS. Commit:

```bash
git add internal/collector
git commit -m "Export the importer entity decoder for the publisher"
```

- [ ] **Step 2: Write failing tests** — `internal/publisher/source_test.go`

```go
package publisher_test

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

const storyParagraph = "OpenAI users reported a change in model behavior after the latest release, and developers shared detailed examples of outputs that no longer matched their earlier results. The company has not confirmed a deliberate capability reduction, so the evidence remains based on user reports and comparative testing."

func jsLen(s string) int { return len(utf16.Encode([]rune(s))) }

// Port of news-policy.test.ts "extracts the richest article scope when a short article card precedes the real story".
func TestExtractSourceTextPrefersRichestScope(t *testing.T) {
	var paragraphs strings.Builder
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&paragraphs, "<p>%s Reported example %d describes a distinct test performed by a different user.</p>", storyParagraph, i)
	}
	html := "<html><body><article><p>Short card.</p></article><main>" + paragraphs.String() + "</main></body></html>"

	extracted := publisher.ExtractSourceText(html)
	if jsLen(extracted) < 800 || !strings.Contains(extracted, "developers shared detailed examples") {
		t.Fatalf("extracted (%d) = %q", jsLen(extracted), extracted)
	}
}

func TestExtractSourceTextDropsNoiseAndBoilerplate(t *testing.T) {
	long := strings.Repeat("Anthropic described the new model's safety evaluations in detail. ", 3)
	html := `<html><body>
		<nav><p>` + long + ` navigation copy</p></nav>
		<!-- <p>` + long + ` commented out</p> -->
		<article>
		  <p>` + long + `</p>
		  <p>Subscribe to our newsletter for the latest AI stories and analysis every single morning.</p>
		  <p>We use cookies to improve your experience on this site, read our cookie policy today.</p>
		  <p>` + long + `</p>
		  <p>Too short.</p>
		  <p>The rollout begins next week for enterprise customers, followed by consumer apps later this year.</p>
		</article>
		<footer><p>` + long + ` footer copy</p></footer>
	</body></html>`

	extracted := publisher.ExtractSourceText(html)
	for _, unwanted := range []string{"navigation copy", "commented out", "Subscribe", "cookies", "Too short", "footer copy"} {
		if strings.Contains(extracted, unwanted) {
			t.Errorf("extracted contains %q: %q", unwanted, extracted)
		}
	}
	if strings.Count(extracted, "safety evaluations") != 3 {
		t.Errorf("duplicate paragraph should appear once: %q", extracted)
	}
	if !strings.Contains(extracted, "The rollout begins next week") {
		t.Errorf("missing last paragraph: %q", extracted)
	}
}

func TestExtractSourceTextUsesJSONLDArticleBodyAndCaps(t *testing.T) {
	body := strings.Repeat("Google announced a smaller Gemini model that runs on device. ", 400)
	html := `<script type="application/ld+json">{"@graph":[{"@type":"WebPage"},{"@type":"NewsArticle","articleBody":"` + body + `"}]}</script><p>ignored</p>`
	extracted := publisher.ExtractSourceText(html)
	if jsLen(extracted) != 14000 || !strings.HasPrefix(extracted, "Google announced") {
		t.Fatalf("len=%d prefix=%q", jsLen(extracted), extracted[:40])
	}
}

func TestExtractCanonicalURL(t *testing.T) {
	page := "https://techcrunch.com/2026/09/23/story/?utm_source=rss"
	cases := map[string]string{
		`<link rel="canonical" href="https://techcrunch.com/2026/09/23/story/">`:     "https://techcrunch.com/2026/09/23/story",
		`<link href="/2026/09/23/relative/" rel="canonical">`:                        "https://techcrunch.com/2026/09/23/relative",
		`<link rel="canonical" href="https://elsewhere.example.com/copied-story/">`:  "https://techcrunch.com/2026/09/23/story",
		`<p>no canonical</p>`: "https://techcrunch.com/2026/09/23/story",
	}
	for html, want := range cases {
		if got := publisher.ExtractCanonicalURL(html, page); got != want {
			t.Errorf("ExtractCanonicalURL(%q) = %q, want %q", html, got, want)
		}
	}
}

func TestExtractOGImage(t *testing.T) {
	page := "https://www.theverge.com/2026/9/23/story"
	cases := map[string]string{
		`<meta property="og:image" content="https://cdn.example.com/a.jpg?w=1200&amp;h=630">`: "https://cdn.example.com/a.jpg?w=1200&h=630",
		`<meta content="/images/b.png" property="og:image">`:                                  "https://www.theverge.com/images/b.png",
		`<meta property="og:image" content="data:image/png;base64,AAAA">`:                    "",
		`<meta name="twitter:image" content="https://cdn.example.com/c.jpg">`:                 "",
	}
	for html, want := range cases {
		if got := publisher.ExtractOGImage(html, page); got != want {
			t.Errorf("ExtractOGImage(%q) = %q, want %q", html, got, want)
		}
	}
}
```

Note: `NormalizeSourceURL` strips trailing slashes (observed in phase 3), which is why canonical expectations have none.

- [ ] **Step 3: Run to verify failure** — `go test ./internal/publisher` → FAIL (package missing).

- [ ] **Step 4: Implement** — `internal/publisher/text.go`

```go
// Package publisher turns queued newsroom candidates into published articles.
package publisher

import "unicode/utf16"

// jsLength counts UTF-16 code units, matching JavaScript's String.length,
// so length thresholds behave exactly like the Node importer.
func jsLength(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// truncateJS returns the longest prefix of s whose JavaScript length is at most limit.
func truncateJS(s string, limit int) string {
	n := 0
	for i, r := range s {
		width := utf16.RuneLen(r)
		if n+width > limit {
			return s[:i]
		}
		n += width
	}
	return s
}
```

`internal/publisher/source.go`

```go
package publisher

import (
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/collector"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
)

const (
	maxSourceTextLength    = 14_000
	minJSONLDArticleLength = 500
	// MinSourceTextLength is the shortest extracted text accepted for a rewrite.
	MinSourceTextLength = 800
)

var (
	canonicalRelFirst  = regexp.MustCompile(`(?i)<link[^>]*rel=["'][^"']*canonical[^"']*["'][^>]*href=["']([^"']+)["'][^>]*>`)
	canonicalHrefFirst = regexp.MustCompile(`(?i)<link[^>]*href=["']([^"']+)["'][^>]*rel=["'][^"']*canonical[^"']*["'][^>]*>`)
	ogImagePropFirst   = regexp.MustCompile(`(?i)<meta[^>]*property=["']og:image["'][^>]*content=["']([^"']+)["'][^>]*>`)
	ogImageContentFirst = regexp.MustCompile(`(?i)<meta[^>]*content=["']([^"']+)["'][^>]*property=["']og:image["'][^>]*>`)
	jsonLDScript       = regexp.MustCompile(`(?is)<script[^>]*type=["']application/ld\+json["'][^>]*>(.*?)</script>`)
	htmlComment        = regexp.MustCompile(`(?s)<!--.*?-->`)
	noiseBlock         = regexp.MustCompile(`(?is)<(?:script|style|nav|header|footer|aside|form|iframe|svg|noscript)\b[^>]*>.*?</(?:script|style|nav|header|footer|aside|form|iframe|svg|noscript)>`)
	articleScope       = regexp.MustCompile(`(?is)<article\b[^>]*>(.*?)</article>`)
	mainScope          = regexp.MustCompile(`(?is)<main\b[^>]*>(.*?)</main>`)
	paragraphBlock     = regexp.MustCompile(`(?is)<p\b[^>]*>.*?</p>`)
	boilerplateStart   = regexp.MustCompile(`(?i)^(?:advertisement|subscribe|sign up|read more|all rights reserved)\b`)
	boilerplateMention = regexp.MustCompile(`(?i)cookie|newsletter preferences|privacy policy`)
)

// ExtractCanonicalURL ports extractCanonicalUrl: the page's canonical link if
// it points at an approved source, otherwise the normalized page URL.
func ExtractCanonicalURL(html, pageURL string) string {
	raw := pageURL
	if match := canonicalRelFirst.FindStringSubmatch(html); match != nil {
		raw = match[1]
	} else if match := canonicalHrefFirst.FindStringSubmatch(html); match != nil {
		raw = match[1]
	}
	raw = collector.DecodeHTMLEntities(raw)
	if base, err := url.Parse(pageURL); err == nil {
		if resolved, err := base.Parse(raw); err == nil {
			if _, approved := content.SourceForURL(resolved.String()); approved {
				if normalized, err := content.NormalizeSourceURL(resolved.String()); err == nil {
					return normalized
				}
			}
		}
	}
	if normalized, err := content.NormalizeSourceURL(pageURL); err == nil {
		return normalized
	}
	return pageURL
}

// ExtractOGImage ports extractOgImage: an absolute http(s) og:image URL or "".
func ExtractOGImage(html, pageURL string) string {
	var raw string
	if match := ogImagePropFirst.FindStringSubmatch(html); match != nil {
		raw = match[1]
	} else if match := ogImageContentFirst.FindStringSubmatch(html); match != nil {
		raw = match[1]
	}
	raw = collector.DecodeHTMLEntities(raw)
	if raw == "" {
		return ""
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return ""
	}
	resolved, err := base.Parse(raw)
	if err != nil || (resolved.Scheme != "http" && resolved.Scheme != "https") {
		return ""
	}
	return resolved.String()
}

// ExtractSourceText ports extractSourceText: a long JSON-LD articleBody when
// present, otherwise the richest paragraph set among <article>, <main> and the
// whole page, with navigation/boilerplate removed; capped at 14000 JS chars.
func ExtractSourceText(html string) string {
	if body := extractJSONLDArticleBody(html); jsLength(body) >= MinSourceTextLength {
		return truncateJS(body, maxSourceTextLength)
	}
	cleaned := noiseBlock.ReplaceAllString(htmlComment.ReplaceAllString(html, " "), " ")
	var scopes []string
	for _, match := range articleScope.FindAllStringSubmatch(cleaned, -1) {
		scopes = append(scopes, match[1])
	}
	if match := mainScope.FindStringSubmatch(cleaned); match != nil {
		scopes = append(scopes, match[1])
	}
	scopes = append(scopes, cleaned)

	best := ""
	for _, scope := range scopes {
		if text := extractParagraphs(scope); jsLength(text) > jsLength(best) {
			best = text
		}
	}
	return best
}

func extractParagraphs(scope string) string {
	seen := map[string]bool{}
	var paragraphs []string
	for _, block := range paragraphBlock.FindAllString(scope, -1) {
		text := collector.DecodeHTMLEntities(content.StripHTML(block))
		if jsLength(text) < 60 || boilerplateStart.MatchString(text) {
			continue
		}
		if boilerplateMention.MatchString(text) && jsLength(text) < 250 {
			continue
		}
		key := strings.ToLower(text)
		if seen[key] {
			continue
		}
		seen[key] = true
		paragraphs = append(paragraphs, text)
		if jsLength(strings.Join(paragraphs, "\n\n")) >= maxSourceTextLength {
			break
		}
	}
	return truncateJS(strings.Join(paragraphs, "\n\n"), maxSourceTextLength)
}

func extractJSONLDArticleBody(html string) string {
	for _, match := range jsonLDScript.FindAllStringSubmatch(html, -1) {
		var value any
		if err := json.Unmarshal([]byte(strings.TrimSpace(match[1])), &value); err != nil {
			continue
		}
		if body := findArticleBody(value); body != "" {
			return collector.DecodeHTMLEntities(content.StripHTML(body))
		}
	}
	return ""
}

// findArticleBody searches depth-first for an articleBody over 500 chars.
// Object keys are visited in sorted order for determinism (Node uses
// insertion order; pages carry at most one articleBody in practice).
func findArticleBody(value any) string {
	switch typed := value.(type) {
	case []any:
		for _, child := range typed {
			if body := findArticleBody(child); body != "" {
				return body
			}
		}
	case map[string]any:
		if body, ok := typed["articleBody"].(string); ok && jsLength(body) > minJSONLDArticleLength {
			return body
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if body := findArticleBody(typed[key]); body != "" {
				return body
			}
		}
	}
	return ""
}
```

- [ ] **Step 5: Run tests** — `go test ./internal/publisher -v` → PASS. If the JSON-LD cap test's prefix check fails because `StripHTML`/entity decoding collapses whitespace differently, adjust only the test's expectation (not the extraction).

- [ ] **Step 6: Commit**

```bash
git add internal/publisher
git commit -m "Extract canonical URL, og:image and article text from source pages"
```

---

### Task 3: Gemini text client

**Files:** Create `internal/gemini/client.go`, `internal/gemini/client_test.go`

- [ ] **Step 1: Write failing tests** — `internal/gemini/client_test.go`

```go
package gemini_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
)

func TestGenerateJSONSendsNodeCompatibleRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models/gemini-test:generateContent" || r.Header.Get("x-goog-api-key") != "k" {
			t.Errorf("path=%s key=%q", r.URL.Path, r.Header.Get("x-goog-api-key"))
		}
		var body struct {
			Contents []struct {
				Parts []struct{ Text string } `json:"parts"`
			} `json:"contents"`
			GenerationConfig struct {
				MaxOutputTokens  int     `json:"maxOutputTokens"`
				ResponseMimeType string  `json:"responseMimeType"`
				Temperature      float64 `json:"temperature"`
			} `json:"generationConfig"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Contents[0].Parts[0].Text != "prompt" || body.GenerationConfig.MaxOutputTokens != 4096 ||
			body.GenerationConfig.ResponseMimeType != "application/json" || body.GenerationConfig.Temperature != 0.4 {
			t.Errorf("body = %+v", body)
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":" {\"a\":"},{"text":"1} "}]}}]}`))
	}))
	defer server.Close()

	client := gemini.New("k", "gemini-test", gemini.WithBaseURL(server.URL))
	text, err := client.GenerateJSON(context.Background(), "prompt")
	if err != nil || text != `{"a":1}` {
		t.Fatalf("text=%q err=%v", text, err)
	}
}

func TestGenerateJSONClassifiesErrors(t *testing.T) {
	for _, tc := range []struct {
		status    int
		transient bool
	}{{429, true}, {503, true}, {400, false}, {403, false}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"error":{"message":"nope"}}`))
		}))
		_, err := gemini.New("k", "m", gemini.WithBaseURL(server.URL)).GenerateJSON(context.Background(), "p")
		server.Close()
		var apiErr *gemini.Error
		if !errors.As(err, &apiErr) || apiErr.Status != tc.status || apiErr.Transient() != tc.transient {
			t.Errorf("status %d: err=%v", tc.status, err)
		}
	}
}

func TestGenerateJSONRequiresKeyAndContent(t *testing.T) {
	if _, err := gemini.New("", "m").GenerateJSON(context.Background(), "p"); !errors.Is(err, gemini.ErrMissingAPIKey) {
		t.Fatalf("missing key err = %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[]}`))
	}))
	defer server.Close()
	if _, err := gemini.New("k", "m", gemini.WithBaseURL(server.URL)).GenerateJSON(context.Background(), "p"); !errors.Is(err, gemini.ErrEmptyResponse) {
		t.Fatalf("empty err = %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/gemini` → FAIL.

- [ ] **Step 3: Implement** — `internal/gemini/client.go`

```go
// Package gemini calls the Gemini generateContent API.
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultBaseURL   = "https://generativelanguage.googleapis.com/v1beta"
	DefaultTextModel = "gemini-3.5-flash-lite"
	textTimeout      = 90 * time.Second
	maxResponseBytes = 32 << 20
)

var (
	ErrMissingAPIKey = errors.New("GEMINI_API_KEY is not set")
	ErrEmptyResponse = errors.New("gemini returned no content")
)

// Error is a non-2xx Gemini response.
type Error struct {
	Status int
	Body   string
}

func (e *Error) Error() string { return fmt.Sprintf("gemini responded %d: %s", e.Status, e.Body) }

// Transient reports whether retrying later can succeed (rate limits, server errors).
func (e *Error) Transient() bool { return e.Status == http.StatusTooManyRequests || e.Status >= 500 }

type Client struct {
	apiKey    string
	textModel string
	baseURL   string
	http      *http.Client
}

type Option func(*Client)

func WithBaseURL(baseURL string) Option { return func(c *Client) { c.baseURL = strings.TrimRight(baseURL, "/") } }

func WithHTTPClient(client *http.Client) Option { return func(c *Client) { c.http = client } }

func New(apiKey, textModel string, options ...Option) *Client {
	if textModel == "" {
		textModel = DefaultTextModel
	}
	client := &Client{apiKey: apiKey, textModel: textModel, baseURL: DefaultBaseURL, http: &http.Client{}}
	for _, option := range options {
		option(client)
	}
	return client
}

type part struct {
	Text string `json:"text,omitempty"`
}

type generateResponse struct {
	Candidates []struct {
		Content struct {
			Parts []part `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// GenerateJSON sends a text prompt with the Node importer's settings
// (JSON output, 4096 tokens, temperature 0.4) and returns the joined text.
func (c *Client) GenerateJSON(ctx context.Context, prompt string) (string, error) {
	payload := map[string]any{
		"contents": []map[string]any{{"parts": []part{{Text: prompt}}}},
		"generationConfig": map[string]any{
			"maxOutputTokens":  4096,
			"responseMimeType": "application/json",
			"temperature":      0.4,
		},
	}
	ctx, cancel := context.WithTimeout(ctx, textTimeout)
	defer cancel()
	var response generateResponse
	if err := c.generate(ctx, c.textModel, payload, &response); err != nil {
		return "", err
	}
	if len(response.Candidates) == 0 {
		return "", ErrEmptyResponse
	}
	var text strings.Builder
	for _, p := range response.Candidates[0].Content.Parts {
		text.WriteString(p.Text)
	}
	if strings.TrimSpace(text.String()) == "" {
		return "", ErrEmptyResponse
	}
	return strings.TrimSpace(text.String()), nil
}

// generate posts to models/{model}:generateContent and decodes the response.
func (c *Client) generate(ctx context.Context, model string, payload any, into any) error {
	if c.apiKey == "" {
		return ErrMissingAPIKey
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/models/"+model+":generateContent", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("gemini request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read gemini response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		detail := string(data)
		if len(detail) > 300 {
			detail = detail[:300]
		}
		return &Error{Status: resp.StatusCode, Body: detail}
	}
	if err := json.Unmarshal(data, into); err != nil {
		return fmt.Errorf("decode gemini response: %w", err)
	}
	return nil
}
```

`generate` stays unexported; phase 4b adds image and vision methods on the same client.

- [ ] **Step 4: Run tests** — `go test -race ./internal/gemini -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gemini
git commit -m "Add a Gemini text client with transient error classification"
```

---

### Task 4: Rewriter

**Files:** Create `internal/publisher/errors.go`, `internal/publisher/rewrite.go`, `internal/publisher/rewrite_test.go`

- [ ] **Step 1: Write failing tests** — `internal/publisher/rewrite_test.go`

```go
package publisher_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

type scriptedText struct {
	responses []string
	errs      []error
	prompts   []string
}

func (s *scriptedText) GenerateJSON(_ context.Context, prompt string) (string, error) {
	i := len(s.prompts)
	s.prompts = append(s.prompts, prompt)
	if i < len(s.errs) && s.errs[i] != nil {
		return "", s.errs[i]
	}
	return s.responses[i], nil
}

func validArticleJSON() string {
	var body strings.Builder
	for i := 0; i < 6; i++ {
		body.WriteString("<p>Anthropic released a new model for developers on Tuesday. It handles longer tasks and costs less to run. Early testers said it followed instructions closely. The company plans a wider rollout next month.</p>")
	}
	return `{"title":"Anthropic releases a cheaper model for developers","excerpt":"The new model handles longer tasks at a lower price.","content":"` + body.String() + `"}`
}

var input = publisher.RewriteInput{
	Source: "TechCrunch", Title: "Anthropic launches new model", CanonicalURL: "https://techcrunch.com/2026/09/23/anthropic", SourceText: "Source reporting text.",
}

func TestRewriteBuildsPromptAndAcceptsValidArticle(t *testing.T) {
	text := &scriptedText{responses: []string{"```json\n" + validArticleJSON() + "\n```"}}
	article, err := publisher.NewRewriter(text).Rewrite(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "Anthropic releases a cheaper model for developers" {
		t.Fatalf("article = %+v", article)
	}
	prompt := text.prompts[0]
	for _, want := range []string{
		"You are TechNews Editorial.",
		"Write 150 to 800 words in 5 to 12 paragraphs.",
		"Publication: TechCrunch\nOriginal headline: Anthropic launches new model\nCanonical source URL: https://techcrunch.com/2026/09/23/anthropic",
		"Source reporting:\nSource reporting text.",
		`{"title":"Short factual headline","excerpt":"One sentence.","content":"<p>Article body.</p>"}`,
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestRewriteRetriesOnceWithCorrection(t *testing.T) {
	text := &scriptedText{responses: []string{"not json", validArticleJSON()}}
	if _, err := publisher.NewRewriter(text).Rewrite(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(text.prompts[1], "\n\nYour previous response was not valid JSON. Return only the required JSON object.") {
		t.Fatalf("second prompt suffix = %q", text.prompts[1][len(text.prompts[1])-120:])
	}
}

func TestRewriteFailsPermanentlyAfterTwoInvalidDrafts(t *testing.T) {
	short := `{"title":"Short","excerpt":"One.","content":"<p>Too short.</p>"}`
	text := &scriptedText{responses: []string{short, short}}
	_, err := publisher.NewRewriter(text).Rewrite(context.Background(), input)
	if err == nil || !publisher.IsPermanent(err) || len(text.prompts) != 2 {
		t.Fatalf("err=%v permanent=%v prompts=%d", err, publisher.IsPermanent(err), len(text.prompts))
	}
	if !strings.Contains(text.prompts[1], "Your previous draft failed these checks:") {
		t.Fatalf("missing validation correction")
	}
}

func TestRewriteClassifiesGeminiErrors(t *testing.T) {
	transient := &scriptedText{errs: []error{&gemini.Error{Status: 503}}}
	if _, err := publisher.NewRewriter(transient).Rewrite(context.Background(), input); err == nil || publisher.IsPermanent(err) {
		t.Fatalf("503 should be transient: %v", err)
	}
	permanent := &scriptedText{errs: []error{&gemini.Error{Status: 400}}}
	if _, err := publisher.NewRewriter(permanent).Rewrite(context.Background(), input); !publisher.IsPermanent(err) {
		t.Fatalf("400 should be permanent: %v", err)
	}
	missing := &scriptedText{errs: []error{gemini.ErrMissingAPIKey}}
	if _, err := publisher.NewRewriter(missing).Rewrite(context.Background(), input); !errors.Is(err, gemini.ErrMissingAPIKey) || publisher.IsPermanent(err) {
		t.Fatalf("missing key should stay transient (config fix, then retry): %v", err)
	}
}
```

If `validArticleJSON` fails `content.ValidateRewrittenArticle` for a reason other than the tested one (e.g. word count), extend its body until it passes — keep 5–12 `<p>` paragraphs and 150–800 words.

- [ ] **Step 2: Run to verify failure** — `go test ./internal/publisher -run Rewrite` → FAIL.

- [ ] **Step 3: Implement** — `internal/publisher/errors.go`

```go
package publisher

import "errors"

// permanentError marks failures that retrying cannot fix (policy, duplicate,
// unusable source, rejected rewrite). Everything else is treated as transient.
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent wraps err so the publisher marks the candidate failed instead of retrying.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

func IsPermanent(err error) bool {
	var permanent permanentError
	return errors.As(err, &permanent)
}
```

`internal/publisher/rewrite.go` — the prompt text is a verbatim port of `news-importer.ts:577-606`; keep every line identical.

```go
package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
)

const (
	minArticleWords = 150
	maxArticleWords = 800
	rewriteAttempts = 2
)

type TextGenerator interface {
	GenerateJSON(ctx context.Context, prompt string) (string, error)
}

type RewriteInput struct {
	Source       string
	Title        string
	CanonicalURL string
	SourceText   string
}

type Rewriter struct {
	text TextGenerator
}

func NewRewriter(text TextGenerator) *Rewriter { return &Rewriter{text: text} }

var (
	leadingFence  = regexp.MustCompile("(?i)^```(?:json)?\\s*")
	trailingFence = regexp.MustCompile("\\s*```$")
	jsonObject    = regexp.MustCompile(`(?s)\{.*\}`)
)

// Rewrite ports rewriteArticle: up to two drafts, the second carrying a
// correction for invalid JSON or failed validation.
func (r *Rewriter) Rewrite(ctx context.Context, input RewriteInput) (content.RewrittenArticle, error) {
	base := rewritePrompt(input)
	correction := ""
	var lastProblem string
	for attempt := 1; attempt <= rewriteAttempts; attempt++ {
		raw, err := r.text.GenerateJSON(ctx, base+correction)
		if err != nil {
			return content.RewrittenArticle{}, classifyGeminiError(err)
		}
		article, ok := parseRewrittenArticle(raw)
		if !ok {
			lastProblem = "response was not valid JSON"
			correction = "\n\nYour previous response was not valid JSON. Return only the required JSON object."
			continue
		}
		minWords, maxWords := minArticleWords, maxArticleWords
		problems := content.ValidateRewrittenArticle(article, content.ArticleValidationOptions{MinWords: &minWords, MaxWords: &maxWords})
		if len(problems) == 0 {
			return article, nil
		}
		lastProblem = strings.Join(problems, "; ")
		correction = "\n\nYour previous draft failed these checks: " + lastProblem +
			". Rewrite it again from the same source reporting and satisfy every requirement."
	}
	return content.RewrittenArticle{}, Permanent(fmt.Errorf("rewrite failed validation: %s", lastProblem))
}

func classifyGeminiError(err error) error {
	var apiErr *gemini.Error
	if errors.As(err, &apiErr) && !apiErr.Transient() {
		return Permanent(err)
	}
	return err
}

func parseRewrittenArticle(value string) (content.RewrittenArticle, bool) {
	cleaned := strings.TrimSpace(trailingFence.ReplaceAllString(leadingFence.ReplaceAllString(value, ""), ""))
	candidate := cleaned
	if match := jsonObject.FindString(cleaned); match != "" {
		candidate = match
	}
	var parsed struct {
		Title   *string `json:"title"`
		Excerpt *string `json:"excerpt"`
		Content *string `json:"content"`
	}
	if err := json.Unmarshal([]byte(candidate), &parsed); err != nil || parsed.Title == nil || parsed.Excerpt == nil || parsed.Content == nil {
		return content.RewrittenArticle{}, false
	}
	return content.RewrittenArticle{
		Title:   strings.TrimSpace(*parsed.Title),
		Excerpt: strings.TrimSpace(*parsed.Excerpt),
		Content: strings.TrimSpace(*parsed.Content),
	}, true
}

func rewritePrompt(input RewriteInput) string {
	return fmt.Sprintf(`You are TechNews Editorial. Rewrite the source reporting below as an original news article.

Requirements:
- Preserve every fact, name, number, date, and quotation accurately.
- Never invent quotations, statistics, motives, consequences, or unsupported details.
- Do not copy the source wording. Use a conversational but factual voice.
- Use short, punchy sentences and no filler.
- Never use an em dash.
- Never use the phrases "In a move that" or "It remains to be seen".
- Never use "groundbreaking", "revolutionary", or "game-changing".
- Write a short, direct, factual, non-clickbait headline, maximum 120 characters.
- Write one plain-sentence excerpt, maximum 180 characters, with no HTML.
- Write %d to %d words in 5 to 12 paragraphs.
- Open with a clear lede explaining what happened.
- Include relevant context and background.
- Include supported industry implications or analysis without presenting speculation as fact.
- End with the next known step. Do not invent a next step.
- Use only <p> and optional <h2> tags, with no tag attributes.
- Do not include the headline in the body.
- Do not add a source or sources footer. Attribution is stored separately.

Publication: %s
Original headline: %s
Canonical source URL: %s

Source reporting:
%s

Return only JSON with this exact shape:
{"title":"Short factual headline","excerpt":"One sentence.","content":"<p>Article body.</p>"}`,
		minArticleWords, maxArticleWords, input.Source, input.Title, input.CanonicalURL, input.SourceText)
}
```

Before committing, diff the prompt against Node: `sed -n 577,606p ../server/scripts/news-importer.ts` — every line must match apart from the template placeholders.

- [ ] **Step 4: Run tests** — `go test -race ./internal/publisher -run Rewrite -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/publisher/errors.go internal/publisher/rewrite.go internal/publisher/rewrite_test.go
git commit -m "Port the Gemini article rewrite with validation retries"
```

---

### Task 5: SQLite article writer

**Files:** Create `internal/publisher/articles.go`, `internal/publisher/articles_test.go`

- [ ] **Step 1: Write failing tests** — `internal/publisher/articles_test.go`

```go
package publisher_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

var publishNow = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	return db
}

func newArticle(slug, sourceURL, title, content string) publisher.NewArticle {
	return publisher.NewArticle{
		Title: title, Slug: slug, Excerpt: "One sentence.", Content: content,
		FeaturedImage: "https://images.example.com/a.webp", Source: "TechCrunch", SourceURL: sourceURL,
	}
}

func TestPublishInsertsArticleLikeNode(t *testing.T) {
	db := openDB(t)
	articles := publisher.NewSQLiteArticles(db, func() time.Time { return publishNow })
	ctx := context.Background()

	id, err := articles.Publish(ctx, newArticle("openai-model", "https://techcrunch.com/a", "OpenAI ships a model", "<p>Body</p>"))
	if err != nil {
		t.Fatal(err)
	}
	var status, publishedAt, createdAt, author, category, source, sourceURL, image string
	var views int
	err = db.QueryRow(`SELECT a.status, a.published_at, a.created_at, au.name, c.slug, a.source, a.source_url, a.featured_image, a.view_count
		FROM articles a JOIN authors au ON au.id = a.author_id JOIN categories c ON c.id = a.category_id WHERE a.id = ?`, id).
		Scan(&status, &publishedAt, &createdAt, &author, &category, &source, &sourceURL, &image, &views)
	if err != nil {
		t.Fatal(err)
	}
	if status != "published" || publishedAt != "2026-09-23 12:00:00" || createdAt != "2026-09-23 12:00:00" ||
		author != "TechNews Editorial" || category != "ai" || source != "TechCrunch" || sourceURL != "https://techcrunch.com/a" ||
		image != "https://images.example.com/a.webp" || views != 0 {
		t.Fatalf("row = %s %s %s %s %s %s %s %s %d", status, publishedAt, createdAt, author, category, source, sourceURL, image, views)
	}

	var categories int
	_ = db.QueryRow(`SELECT COUNT(*) FROM categories`).Scan(&categories)
	if categories != 6 {
		t.Fatalf("categories = %d, want the 6 Node categories", categories)
	}

	if exists, _ := articles.Exists(ctx, "https://techcrunch.com/other", "openai-model"); !exists {
		t.Fatal("Exists by slug")
	}
	if exists, _ := articles.Exists(ctx, "https://techcrunch.com/a", "other"); !exists {
		t.Fatal("Exists by source URL")
	}
	if _, err := articles.Publish(ctx, newArticle("openai-model", "https://techcrunch.com/b", "Again", "<p>x</p>")); !errors.Is(err, publisher.ErrDuplicateArticle) {
		t.Fatalf("duplicate err = %v", err)
	}
}

func TestPublishReusesEditorialAuthorByEmail(t *testing.T) {
	db := openDB(t)
	if _, err := db.Exec(`INSERT INTO authors (name, email, password_hash, role) VALUES ('Old Name', 'editorial@technews.dev', 'x', 'editor')`); err != nil {
		t.Fatal(err)
	}
	articles := publisher.NewSQLiteArticles(db, func() time.Time { return publishNow })
	if _, err := articles.Publish(context.Background(), newArticle("s", "https://techcrunch.com/s", "A tech story", "<p>Body</p>")); err != nil {
		t.Fatal(err)
	}
	var count int
	var name string
	_ = db.QueryRow(`SELECT COUNT(*), MAX(name) FROM authors`).Scan(&count, &name)
	if count != 1 || name != "TechNews Editorial" {
		t.Fatalf("authors count=%d name=%q", count, name)
	}
}

func TestCategorizeMatchesNodeRules(t *testing.T) {
	cases := []struct{ title, content, want string }{
		{"OpenAI ships a model", "", "ai"},
		{"NASA picks a lander", "", "science"},
		{"Chip news", "<p>The review benchmark was tested against rivals.</p>", "reviews"},
		{"Chip news", "<p>A single game mention.</p>", "tech"},
		{"Chip news", "<p>The streaming movie hit Netflix.</p>", "entertainment"},
	}
	for _, tc := range cases {
		if got := publisher.Categorize(tc.title, tc.content); got != tc.want {
			t.Errorf("Categorize(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/publisher -run 'Publish|Categorize'` → FAIL.

- [ ] **Step 3: Implement** — `internal/publisher/articles.go`

```go
package publisher

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
)

// sqliteDateTime matches SQLite datetime('now'), which Node writes and the website reads.
const sqliteDateTime = "2006-01-02 15:04:05"

var ErrDuplicateArticle = errors.New("article already published for this source URL or slug")

type NewArticle struct {
	Title         string
	Slug          string
	Excerpt       string
	Content       string
	FeaturedImage string
	Source        string
	SourceURL     string
}

// Categories in Node's CATEGORY_KEYWORDS insertion order; "tech" is the fallback.
var categoryKeywords = []struct {
	slug     string
	color    string
	keywords []string
}{
	{"ai", "#8b5cf6", []string{"artificial intelligence", "machine learning", "llm", "openai", "chatgpt", "anthropic", "gemini", "generative ai", "neural network", "ai model"}},
	{"science", "#10b981", []string{"science", "space", "nasa", "physics", "biology", "climate", "quantum", "telescope", "asteroid", "fusion", "genome"}},
	{"entertainment", "#ec4899", []string{"game", "gaming", "movie", "film", "streaming", "playstation", "xbox", "nintendo", "netflix", "spotify"}},
	{"reviews", "#f59e0b", []string{"review", "hands-on", "benchmark", "comparison", "tested", "unboxing"}},
	{"creators", "#f97316", []string{"creator", "youtube", "tiktok", "influencer", "podcast", "twitch", "patreon"}},
	{"tech", "#3b82f6", nil},
}

// Categorize ports categorize: a keyword in the headline wins; otherwise two
// keywords in the first 1000 characters of the body; otherwise "tech".
func Categorize(title, html string) string {
	headline := strings.ToLower(title)
	sample := truncateJS(strings.ToLower(content.StripHTML(html)), 1_000)
	for _, category := range categoryKeywords {
		for _, keyword := range category.keywords {
			if strings.Contains(headline, keyword) {
				return category.slug
			}
		}
	}
	for _, category := range categoryKeywords {
		matches := 0
		for _, keyword := range category.keywords {
			if strings.Contains(sample, keyword) {
				matches++
			}
		}
		if matches >= 2 {
			return category.slug
		}
	}
	return "tech"
}

// SQLiteArticles writes articles exactly as news-importer.ts does.
type SQLiteArticles struct {
	db  *sql.DB
	now func() time.Time
}

func NewSQLiteArticles(db *sql.DB, now func() time.Time) *SQLiteArticles {
	return &SQLiteArticles{db: db, now: now}
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func exists(ctx context.Context, q queryer, sourceURL, slug string) (bool, error) {
	var id int64
	err := q.QueryRowContext(ctx, `SELECT id FROM articles WHERE source_url = ? OR slug = ? LIMIT 1`, sourceURL, slug).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check existing article: %w", err)
	}
	return true, nil
}

// Exists ports isStored.
func (a *SQLiteArticles) Exists(ctx context.Context, sourceURL, slug string) (bool, error) {
	return exists(ctx, a.db, sourceURL, slug)
}

// Publish inserts a published article and verifies it by reading it back, in
// one transaction. It returns ErrDuplicateArticle if the source URL or slug
// already exists.
func (a *SQLiteArticles) Publish(ctx context.Context, article NewArticle) (id int64, err error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin article insert: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	for _, category := range categoryKeywords {
		name := strings.ToUpper(category.slug[:1]) + category.slug[1:]
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO categories (name, slug, description, color) VALUES (?, ?, ?, ?)`,
			name, category.slug, category.slug+" news", category.color); err != nil {
			return 0, fmt.Errorf("ensure category %s: %w", category.slug, err)
		}
	}
	var categoryID int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM categories WHERE slug = ?`, Categorize(article.Title, article.Content)).Scan(&categoryID); err != nil {
		return 0, fmt.Errorf("find category: %w", err)
	}
	authorID, err := ensureEditorialAuthor(ctx, tx)
	if err != nil {
		return 0, err
	}
	duplicate, err := exists(ctx, tx, article.SourceURL, article.Slug)
	if err != nil {
		return 0, err
	}
	if duplicate {
		return 0, ErrDuplicateArticle
	}

	stamp := a.now().UTC().Format(sqliteDateTime)
	result, err := tx.ExecContext(ctx, `INSERT INTO articles (
		title, slug, excerpt, content, featured_image, category_id, author_id,
		status, published_at, view_count, created_at, updated_at, source, source_url
	) VALUES (?, ?, ?, ?, ?, ?, ?, 'published', ?, 0, ?, ?, ?, ?)`,
		article.Title, article.Slug, article.Excerpt, article.Content, article.FeaturedImage, categoryID, authorID,
		stamp, stamp, stamp, article.Source, article.SourceURL)
	if err != nil {
		return 0, fmt.Errorf("insert article: %w", err)
	}
	if id, err = result.LastInsertId(); err != nil {
		return 0, fmt.Errorf("article id: %w", err)
	}

	var status, author, source string
	if err = tx.QueryRowContext(ctx, `SELECT a.status, au.name, a.source FROM articles a JOIN authors au ON au.id = a.author_id
		WHERE a.source_url = ? AND a.slug = ?`, article.SourceURL, article.Slug).Scan(&status, &author, &source); err != nil {
		return 0, fmt.Errorf("read back article: %w", err)
	}
	if status != "published" || author != content.EditorialAuthor().Name || source != article.Source {
		err = errors.New("post-insert readback did not match the publishing contract")
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit article: %w", err)
	}
	return id, nil
}

// ensureEditorialAuthor ports ensureEditorialAuthor (by name, then by email, then insert).
func ensureEditorialAuthor(ctx context.Context, tx *sql.Tx) (int64, error) {
	editorial := content.EditorialAuthor()
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM authors WHERE name = ?`, editorial.Name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("find editorial author: %w", err)
	}
	err = tx.QueryRowContext(ctx, `SELECT id FROM authors WHERE email = ?`, editorial.Email).Scan(&id)
	if err == nil {
		if _, err := tx.ExecContext(ctx, `UPDATE authors SET name = ?, bio = ? WHERE id = ?`, editorial.Name, editorial.Bio, id); err != nil {
			return 0, fmt.Errorf("update editorial author: %w", err)
		}
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("find editorial author by email: %w", err)
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO authors (name, email, password_hash, avatar, bio, role)
		VALUES (?, ?, 'not-a-login', NULL, ?, 'editor')`, editorial.Name, editorial.Email, editorial.Bio)
	if err != nil {
		return 0, fmt.Errorf("insert editorial author: %w", err)
	}
	return result.LastInsertId()
}
```

- [ ] **Step 4: Run tests** — `go test -race ./internal/publisher -run 'Publish|Categorize' -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/publisher/articles.go internal/publisher/articles_test.go
git commit -m "Write published articles the way the Node importer does"
```

---

### Task 6: IndexNow client

**Files:** Create `internal/indexnow/client.go`, `internal/indexnow/client_test.go`

- [ ] **Step 1: Write failing tests**

```go
package indexnow_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/indexnow"
)

func TestSubmitSlugsSendsNodePayload(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json; charset=utf-8" {
			t.Errorf("content type = %q", r.Header.Get("Content-Type"))
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := indexnow.New(indexnow.WithEndpoint(server.URL))
	if err := client.SubmitSlugs(context.Background(), []string{"a-story", "a-story", "b story"}); err != nil {
		t.Fatal(err)
	}
	urls := payload["urlList"].([]any)
	if payload["host"] != "www.aiandtech.news" || payload["key"] != indexnow.Key ||
		payload["keyLocation"] != "https://www.aiandtech.news/"+indexnow.Key+".txt" ||
		len(urls) != 2 || urls[0] != "https://www.aiandtech.news/article/a-story" || urls[1] != "https://www.aiandtech.news/article/b%20story" {
		t.Fatalf("payload = %v", payload)
	}
}

func TestSubmitSlugsReportsRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte("bad key"))
	}))
	defer server.Close()
	err := indexnow.New(indexnow.WithEndpoint(server.URL)).SubmitSlugs(context.Background(), []string{"a"})
	if err == nil || !strings.Contains(err.Error(), "422") || !strings.Contains(err.Error(), "bad key") {
		t.Fatalf("err = %v", err)
	}
	if err := indexnow.New(indexnow.WithEndpoint(server.URL)).SubmitSlugs(context.Background(), nil); err != nil {
		t.Fatalf("empty submit should be a no-op: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure** — FAIL (package missing).

- [ ] **Step 3: Implement** — `internal/indexnow/client.go`

```go
// Package indexnow notifies search engines about new article URLs (port of apps/server/src/indexnow.ts).
package indexnow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultEndpoint   = "https://api.indexnow.org/indexnow"
	DefaultSiteOrigin = "https://www.aiandtech.news"
	// Key is public by design: it is served at <origin>/<key>.txt.
	Key = "841819f6d7012eca7cd9104b4dd45d8e"
)

type Client struct {
	endpoint   string
	siteOrigin string
	http       *http.Client
}

type Option func(*Client)

func WithEndpoint(endpoint string) Option { return func(c *Client) { c.endpoint = endpoint } }

func New(options ...Option) *Client {
	client := &Client{endpoint: DefaultEndpoint, siteOrigin: DefaultSiteOrigin, http: &http.Client{Timeout: 15 * time.Second}}
	for _, option := range options {
		option(client)
	}
	return client
}

// ArticleURL mirrors articleUrl: <origin>/article/<encoded slug>.
func ArticleURL(siteOrigin, slug string) string {
	return strings.TrimRight(siteOrigin, "/") + "/article/" + url.PathEscape(slug)
}

// SubmitSlugs submits the article URLs for slugs; 200 and 202 are success.
func (c *Client) SubmitSlugs(ctx context.Context, slugs []string) error {
	seen := map[string]bool{}
	urls := make([]string, 0, len(slugs))
	for _, slug := range slugs {
		articleURL := ArticleURL(c.siteOrigin, slug)
		if !seen[articleURL] {
			seen[articleURL] = true
			urls = append(urls, articleURL)
		}
	}
	if len(urls) == 0 {
		return nil
	}
	origin, err := url.Parse(c.siteOrigin)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{
		"host":        origin.Host,
		"key":         Key,
		"keyLocation": c.siteOrigin + "/" + Key + ".txt",
		"urlList":     urls,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("indexnow request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("IndexNow rejected %d URL(s) with %d: %s", len(urls), resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	return nil
}
```

Note: `url.PathEscape("b story")` yields `b%20story`, matching `encodeURIComponent`. Slugs are `[a-z0-9-]` in practice.

- [ ] **Step 4: Run tests** — `go test -race ./internal/indexnow -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/indexnow
git commit -m "Add IndexNow submission for published articles"
```

---

### Task 7: Publisher orchestration

**Files:** Create `internal/publisher/publisher.go`, `internal/publisher/publisher_test.go`

- [ ] **Step 1: Write failing tests** — `internal/publisher/publisher_test.go`

```go
package publisher_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/collector"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

const sourceURL = "https://techcrunch.com/2026/09/23/anthropic-launches-new-model"

func sourcePage() string {
	var b strings.Builder
	b.WriteString(`<html><head><link rel="canonical" href="` + sourceURL + `"><meta property="og:image" content="https://techcrunch.com/lead.jpg"></head><body><article>`)
	for i := 0; i < 6; i++ {
		fmt.Fprintf(&b, "<p>%s Paragraph %d adds a distinct detail about the release and its pricing for developers.</p>", storyParagraph, i)
	}
	b.WriteString("</article></body></html>")
	return b.String()
}

type fakeFetcher struct {
	body string
	err  error
}

func (f fakeFetcher) FetchText(context.Context, string, string) (string, string, error) {
	return f.body, sourceURL, f.err
}

type fakeRewriter struct{ err error }

func (f fakeRewriter) Rewrite(context.Context, publisher.RewriteInput) (content.RewrittenArticle, error) {
	return content.RewrittenArticle{Title: "Anthropic releases a cheaper model", Excerpt: "It costs less.", Content: "<p>Body</p>"}, f.err
}

type fakeIllustrator struct {
	err       error
	reference string
	discarded bool
}

func (f *fakeIllustrator) Illustrate(_ context.Context, request publisher.IllustrationRequest) (publisher.Illustration, error) {
	f.reference = request.ReferenceImageURL
	if f.err != nil {
		return publisher.Illustration{}, f.err
	}
	return publisher.Illustration{URL: "https://images.example.com/x.webp", Discard: func(context.Context) { f.discarded = true }}, nil
}

type fakeNotifier struct{ slugs []string }

func (f *fakeNotifier) SubmitSlugs(_ context.Context, slugs []string) error {
	f.slugs = append(f.slugs, slugs...)
	return errors.New("indexnow down")
}

type harness struct {
	store       *newsroom.SQLiteStore
	illustrator *fakeIllustrator
	notifier    *fakeNotifier
	pub         *publisher.Publisher
	now         time.Time
	id          int64
}

func newHarness(t *testing.T, fetcher publisher.SourceFetcher, rewriter publisher.ArticleRewriter, illustratorErr error) *harness {
	t.Helper()
	db := openDB(t)
	store := newsroom.NewSQLiteStore(db)
	h := &harness{store: store, illustrator: &fakeIllustrator{err: illustratorErr}, notifier: &fakeNotifier{}, now: publishNow}
	ctx := context.Background()
	id, _, err := store.Insert(ctx, newsroom.NewCandidate{SourceURL: sourceURL, SourceName: "TechCrunch", FeedURL: "https://techcrunch.com/feed/", Title: "Anthropic launches new model"}, publishNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkQueued(ctx, id, newsroom.StatusPending, publishNow, publishNow); err != nil {
		t.Fatal(err)
	}
	h.id = id
	h.pub = publisher.New(publisher.Deps{
		Store: store, Fetcher: fetcher, Rewriter: rewriter, Illustrator: h.illustrator,
		Articles: publisher.NewSQLiteArticles(db, func() time.Time { return h.now }), Notifier: h.notifier,
		Now: func() time.Time { return h.now }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return h
}

func (h *harness) candidate(t *testing.T) newsroom.Candidate {
	t.Helper()
	candidate, err := h.store.Get(context.Background(), h.id)
	if err != nil {
		t.Fatal(err)
	}
	return candidate
}

func TestPublishNextPublishesDueCandidate(t *testing.T) {
	h := newHarness(t, fakeFetcher{body: sourcePage()}, fakeRewriter{}, nil)
	published, err := h.pub.PublishNext(context.Background())
	if err != nil || !published {
		t.Fatalf("published=%v err=%v", published, err)
	}
	candidate := h.candidate(t)
	if candidate.Status != newsroom.StatusPublished || candidate.ArticleSlug == nil || *candidate.ArticleSlug != "anthropic-releases-a-cheaper-model" {
		t.Fatalf("candidate = %+v", candidate)
	}
	if h.illustrator.reference != "https://techcrunch.com/lead.jpg" {
		t.Fatalf("reference image = %q", h.illustrator.reference)
	}
	if len(h.notifier.slugs) != 1 || h.notifier.slugs[0] != "anthropic-releases-a-cheaper-model" {
		t.Fatalf("indexnow slugs = %v (its failure must not fail the publish)", h.notifier.slugs)
	}
	if published, _ := h.pub.PublishNext(context.Background()); published {
		t.Fatal("nothing else is due")
	}
}

func TestPublishNextRetriesTransientFailuresThenFails(t *testing.T) {
	h := newHarness(t, fakeFetcher{err: errors.New("connection reset")}, fakeRewriter{}, nil)
	for attempt := 1; attempt <= publisher.MaxAttempts; attempt++ {
		if published, err := h.pub.PublishNext(context.Background()); err != nil || !published {
			t.Fatalf("attempt %d: published=%v err=%v", attempt, published, err)
		}
		candidate := h.candidate(t)
		if attempt < publisher.MaxAttempts {
			if candidate.Status != newsroom.StatusQueued || *candidate.ScheduledFor != h.now.Add(publisher.RetryDelay).Format(time.RFC3339) {
				t.Fatalf("attempt %d: candidate = %+v", attempt, candidate)
			}
			h.now = h.now.Add(publisher.RetryDelay)
			continue
		}
		if candidate.Status != newsroom.StatusFailed || !strings.Contains(*candidate.LastError, "connection reset") {
			t.Fatalf("final candidate = %+v", candidate)
		}
	}
}

func TestPublishNextFailsPermanentProblemsImmediately(t *testing.T) {
	cases := map[string]struct {
		fetcher  publisher.SourceFetcher
		rewriter publisher.ArticleRewriter
		want     string
	}{
		"redirect outside source": {fakeFetcher{err: fmt.Errorf("%w TechCrunch: https://evil.example", collector.ErrRedirectOutsideSource)}, fakeRewriter{}, "redirect"},
		"short source text":       {fakeFetcher{body: "<p>Too short.</p>"}, fakeRewriter{}, "too short"},
		"rejected rewrite":        {fakeFetcher{body: sourcePage()}, fakeRewriter{err: publisher.Permanent(errors.New("rewrite failed validation: word count"))}, "rewrite failed"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, tc.fetcher, tc.rewriter, nil)
			if _, err := h.pub.PublishNext(context.Background()); err != nil {
				t.Fatal(err)
			}
			candidate := h.candidate(t)
			if candidate.Status != newsroom.StatusFailed || !strings.Contains(*candidate.LastError, tc.want) {
				t.Fatalf("candidate = %+v", candidate)
			}
		})
	}
}

func TestPublishNextSkipsAlreadyPublishedStory(t *testing.T) {
	h := newHarness(t, fakeFetcher{body: sourcePage()}, fakeRewriter{}, nil)
	if _, err := h.pub.PublishNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A second candidate for the same canonical story must fail as a duplicate, before rewriting.
	ctx := context.Background()
	id, _, err := h.store.Insert(ctx, newsroom.NewCandidate{SourceURL: sourceURL + "?amp=1", SourceName: "TechCrunch", FeedURL: "x", Title: "Anthropic launches new model again"}, h.now)
	if err != nil || id == 0 {
		t.Skipf("normalizer merged the URL (id=%d err=%v); duplicate path covered by articles tests", id, err)
	}
	if err := h.store.MarkQueued(ctx, id, newsroom.StatusPending, h.now, h.now); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(time.Hour)
	if _, err := h.pub.PublishNext(ctx); err != nil {
		t.Fatal(err)
	}
	second, _ := h.store.Get(ctx, id)
	if second.Status != newsroom.StatusFailed || !strings.Contains(*second.LastError, "already published") {
		t.Fatalf("second = %+v", second)
	}
}

func TestRecoverRequeuesProcessingCandidates(t *testing.T) {
	h := newHarness(t, fakeFetcher{body: sourcePage()}, fakeRewriter{}, nil)
	if _, _, err := h.store.ClaimDue(context.Background(), h.now, 0); err != nil {
		t.Fatal(err)
	}
	if err := h.pub.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if candidate := h.candidate(t); candidate.Status != newsroom.StatusQueued {
		t.Fatalf("candidate = %+v", candidate)
	}
}

func TestIllustratorTransientFailureRequeues(t *testing.T) {
	h := newHarness(t, fakeFetcher{body: sourcePage()}, fakeRewriter{}, errors.New("image model timeout"))
	if _, err := h.pub.PublishNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if candidate := h.candidate(t); candidate.Status != newsroom.StatusQueued || !strings.Contains(*candidate.LastError, "image model timeout") {
		t.Fatalf("candidate = %+v", candidate)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/publisher -run 'PublishNext|Recover|Illustrator'` → FAIL (`publisher.New undefined`).

- [ ] **Step 3: Implement** — `internal/publisher/publisher.go`

```go
package publisher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/collector"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
)

const (
	// MaxAttempts is how many times a candidate is tried before it is marked failed.
	MaxAttempts = 3
	// RetryDelay is how long a transient failure waits before the next attempt.
	RetryDelay        = 5 * time.Minute
	bookkeepingTimeout = 10 * time.Second
)

type Store interface {
	PublishDelay(ctx context.Context) (newsroom.PublishDelay, error)
	ClaimDue(ctx context.Context, now time.Time, minGap time.Duration) (newsroom.Candidate, bool, error)
	MarkPublished(ctx context.Context, id, articleID int64, now time.Time) error
	MarkFailed(ctx context.Context, id int64, reason string, now time.Time) error
	Requeue(ctx context.Context, id int64, reason string, at, now time.Time) error
	ResetProcessing(ctx context.Context, now time.Time) (int64, error)
}

type SourceFetcher interface {
	FetchText(ctx context.Context, url, expectedSource string) (body, finalURL string, err error)
}

type ArticleRewriter interface {
	Rewrite(ctx context.Context, input RewriteInput) (content.RewrittenArticle, error)
}

type IllustrationRequest struct {
	Slug              string
	Title             string
	Excerpt           string
	Content           string
	ReferenceImageURL string
}

// Illustration is a stored featured image. Discard removes it when the
// article could not be published after the image was uploaded.
type Illustration struct {
	URL     string
	Discard func(context.Context)
}

// Illustrator produces the featured image (phase 4b: Gemini illustration + S3).
type Illustrator interface {
	Illustrate(ctx context.Context, request IllustrationRequest) (Illustration, error)
}

type ArticleStore interface {
	Exists(ctx context.Context, sourceURL, slug string) (bool, error)
	Publish(ctx context.Context, article NewArticle) (int64, error)
}

type Notifier interface {
	SubmitSlugs(ctx context.Context, slugs []string) error
}

type Deps struct {
	Store       Store
	Fetcher     SourceFetcher
	Rewriter    ArticleRewriter
	Illustrator Illustrator
	Articles    ArticleStore
	Notifier    Notifier
	Now         func() time.Time
	Logger      *slog.Logger
}

// Publisher moves due candidates to published articles, one per call.
type Publisher struct{ Deps }

func New(deps Deps) *Publisher { return &Publisher{Deps: deps} }

// Recover requeues candidates left processing by a crash.
func (p *Publisher) Recover(ctx context.Context) error {
	count, err := p.Store.ResetProcessing(ctx, p.Now())
	if err != nil {
		return err
	}
	if count > 0 {
		p.Logger.WarnContext(ctx, "requeued interrupted candidates", "count", count)
	}
	return nil
}

// Loop recovers, then tries one publish per interval until ctx is done.
func (p *Publisher) Loop(ctx context.Context, interval time.Duration) {
	if err := p.Recover(ctx); err != nil {
		p.Logger.ErrorContext(ctx, "publisher recovery failed", "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := p.PublishNext(ctx); err != nil && ctx.Err() == nil {
			p.Logger.ErrorContext(ctx, "publisher tick failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// PublishNext claims the next due candidate (respecting the minimum gap since
// the last publish) and publishes it. It reports whether a candidate was
// claimed; publishing failures are recorded on the candidate, not returned.
func (p *Publisher) PublishNext(ctx context.Context) (bool, error) {
	delay, err := p.Store.PublishDelay(ctx)
	if err != nil {
		return false, err
	}
	candidate, ok, err := p.Store.ClaimDue(ctx, p.Now(), time.Duration(delay.MinMinutes)*time.Minute)
	if err != nil || !ok {
		return false, err
	}
	articleID, slug, err := p.publish(ctx, candidate)
	if err != nil {
		return true, p.recordFailure(ctx, candidate, err)
	}
	bookkeeping, cancel := context.WithTimeout(context.WithoutCancel(ctx), bookkeepingTimeout)
	defer cancel()
	if err := p.Store.MarkPublished(bookkeeping, candidate.ID, articleID, p.Now()); err != nil {
		return true, fmt.Errorf("mark candidate %d published: %w", candidate.ID, err)
	}
	p.Logger.InfoContext(ctx, "published candidate", "candidate", candidate.ID, "article", articleID, "slug", slug)
	if err := p.Notifier.SubmitSlugs(bookkeeping, []string{slug}); err != nil {
		p.Logger.WarnContext(ctx, "IndexNow notification failed", "slug", slug, "error", err)
	}
	return true, nil
}

func (p *Publisher) publish(ctx context.Context, candidate newsroom.Candidate) (int64, string, error) {
	source := candidate.SourceName
	now := p.Now()
	if reason := content.AutomaticItemRejectionReason(candidate.Title, candidate.SourceURL, source, now); reason != "" {
		return 0, "", Permanent(fmt.Errorf("rejected by publishing policy: %s", reason))
	}
	initialSlug := content.Slugify(candidate.Title)
	if initialSlug == "" {
		return 0, "", Permanent(errors.New("headline produces an empty slug"))
	}
	if err := p.rejectDuplicate(ctx, candidate.SourceURL, initialSlug); err != nil {
		return 0, "", err
	}

	body, finalURL, err := p.Fetcher.FetchText(ctx, candidate.SourceURL, source)
	if err != nil {
		if errors.Is(err, collector.ErrRedirectOutsideSource) {
			return 0, "", Permanent(err)
		}
		return 0, "", fmt.Errorf("fetch source page: %w", err)
	}
	canonicalURL := ExtractCanonicalURL(body, finalURL)
	if canonicalSource, ok := content.SourceForURL(canonicalURL); !ok || canonicalSource != source {
		return 0, "", Permanent(fmt.Errorf("canonical URL is outside %s: %s", source, canonicalURL))
	}
	if reason := content.AutomaticItemRejectionReason(candidate.Title, canonicalURL, source, now); reason != "" {
		return 0, "", Permanent(fmt.Errorf("canonical URL rejected by publishing policy: %s", reason))
	}
	if err := p.rejectDuplicate(ctx, canonicalURL, initialSlug); err != nil {
		return 0, "", err
	}
	sourceText := ExtractSourceText(body)
	if jsLength(sourceText) < MinSourceTextLength {
		return 0, "", Permanent(errors.New("source text is too short for an accurate rewrite"))
	}

	article, err := p.Rewriter.Rewrite(ctx, RewriteInput{Source: source, Title: candidate.Title, CanonicalURL: canonicalURL, SourceText: sourceText})
	if err != nil {
		return 0, "", err
	}
	finalSlug := content.Slugify(article.Title)
	if finalSlug == "" {
		return 0, "", Permanent(errors.New("rewritten headline produces an empty slug"))
	}
	if err := p.rejectDuplicate(ctx, canonicalURL, finalSlug); err != nil {
		return 0, "", err
	}

	reference := ExtractOGImage(body, canonicalURL)
	if reference == "" && candidate.SourceImageURL != nil {
		reference = *candidate.SourceImageURL
	}
	illustration, err := p.Illustrator.Illustrate(ctx, IllustrationRequest{
		Slug: finalSlug, Title: article.Title, Excerpt: article.Excerpt, Content: article.Content, ReferenceImageURL: reference,
	})
	if err != nil {
		return 0, "", fmt.Errorf("featured image: %w", err)
	}
	articleID, err := p.Articles.Publish(ctx, NewArticle{
		Title: article.Title, Slug: finalSlug, Excerpt: article.Excerpt, Content: article.Content,
		FeaturedImage: illustration.URL, Source: source, SourceURL: canonicalURL,
	})
	if err != nil {
		if illustration.Discard != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), bookkeepingTimeout)
			illustration.Discard(cleanup)
			cancel()
		}
		if errors.Is(err, ErrDuplicateArticle) {
			return 0, "", Permanent(err)
		}
		return 0, "", err
	}
	return articleID, finalSlug, nil
}

func (p *Publisher) rejectDuplicate(ctx context.Context, sourceURL, slug string) error {
	duplicate, err := p.Articles.Exists(ctx, sourceURL, slug)
	if err != nil {
		return err
	}
	if duplicate {
		return Permanent(ErrDuplicateArticle)
	}
	return nil
}

// recordFailure retries transient failures (up to MaxAttempts) and marks the
// rest failed with a readable reason for the app.
func (p *Publisher) recordFailure(ctx context.Context, candidate newsroom.Candidate, cause error) error {
	bookkeeping, cancel := context.WithTimeout(context.WithoutCancel(ctx), bookkeepingTimeout)
	defer cancel()
	now := p.Now()
	reason := cause.Error()
	if !IsPermanent(cause) && candidate.Attempts < MaxAttempts {
		retryAt := now.Add(RetryDelay)
		if ctx.Err() != nil {
			retryAt = now // interrupted by shutdown, not by the source
		}
		p.Logger.WarnContext(ctx, "publish attempt failed; retrying", "candidate", candidate.ID, "attempt", candidate.Attempts, "error", cause)
		return p.Store.Requeue(bookkeeping, candidate.ID, reason, retryAt, now)
	}
	p.Logger.ErrorContext(ctx, "publishing failed", "candidate", candidate.ID, "attempts", candidate.Attempts, "error", cause)
	return p.Store.MarkFailed(bookkeeping, candidate.ID, reason, now)
}
```

- [ ] **Step 4: Run tests** — `go test -race ./internal/publisher -v` → PASS. The "already published" assertion depends on the duplicate error text: `ErrDuplicateArticle`'s message contains "already published".

- [ ] **Step 5: Add the discard test** (append to `publisher_test.go`) — exercise `Discard` by making the article store fail:

```go
type failingArticles struct{ publisher.ArticleStore }

func (failingArticles) Exists(context.Context, string, string) (bool, error) { return false, nil }
func (failingArticles) Publish(context.Context, publisher.NewArticle) (int64, error) {
	return 0, errors.New("database is locked")
}

func TestFailedInsertDiscardsUploadedImage(t *testing.T) {
	h := newHarness(t, fakeFetcher{body: sourcePage()}, fakeRewriter{}, nil)
	h.pub.Articles = failingArticles{}
	if _, err := h.pub.PublishNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !h.illustrator.discarded {
		t.Fatal("uploaded image was not discarded")
	}
	if candidate := h.candidate(t); candidate.Status != newsroom.StatusQueued {
		t.Fatalf("a locked database is transient; candidate = %+v", candidate)
	}
}
```

Run `go test -race ./internal/publisher -v` → PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/publisher/publisher.go internal/publisher/publisher_test.go
git commit -m "Publish due candidates with retries, duplicate checks and image cleanup"
```

- [ ] **Step 7: Full check** — `make check && make test-race && make contracts-check` → PASS.

---

## Not in this plan (phase 4b)

Gemini image + vision calls, the illustration loop and prompts from `s3-feature-images`, WebP encoding, S3 upload/verify/delete (the real `Illustrator`), `PUBLISHER_ENABLED` + Gemini/AWS configuration and app wiring, local end-to-end publish with a test S3 prefix, and the `NEWS_PUBLISHING_POLICY.md` update.
