# Newsroom Collector Implementation Plan (Phase 3)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fetch the approved RSS/Atom feeds in the Go API and store policy-passing AI stories as pending `candidates`, on a timer and via `POST /api/newsroom/collect`, without publishing anything.

**Architecture:** New package `internal/collector` (fetcher, feed parser, collector run/loop) that depends on `internal/content` (policy) and `internal/newsroom` (store types, `ErrCollectInProgress`). `newsroom.SQLiteStore.Insert` also skips URLs/slugs already published as articles. The app wires the collector only when `COLLECTOR_ENABLED` is set; `App.Run` starts the loop alongside the HTTP server.

**Tech Stack:** Go 1.25 standard library (`net/http`, `regexp`, `html`), existing `content` policy, `modernc.org/sqlite`.

**Roadmap:** `docs/superpowers/plans/2026-09-23-newsroom-pipeline-roadmap.md` (phase 3).
**Node reference:** `apps/server/scripts/news-importer.ts` — `decodeHtmlEntities` (:60), `fetchText` (:132), `extractXmlValue`/`parseFeed` (:189-225), `fetchApprovedFeedItems` (:227-261).

**Working directory:** `apps/server-go`. **Commits:** plain sentences, no `feat:` prefixes, no co-author trailers. **Safety:** tests use temp DBs and fake HTTP; never touch `apps/server/data/technews.db`.

---

## File structure

| File | Responsibility |
|---|---|
| Modify `internal/newsroom/store.go` | `Insert` skips URLs/slugs already in `articles`; new `SetLastCollected` |
| Modify `internal/newsroom/store_test.go` | Tests for both |
| Create `internal/collector/entities.go` | Port of `decodeHtmlEntities` |
| Create `internal/collector/feed.go` | `FeedItem`, `ParseFeed` (port + summary/image extraction), date parsing |
| Create `internal/collector/feed_test.go` | RSS/Atom/CDATA/media/enclosure/img/30-item/invalid-URL fixtures |
| Create `internal/collector/fetch.go` | `Fetcher.FetchText` with approved-source redirect enforcement |
| Create `internal/collector/fetch_internal_test.go` | httptest-based redirect/status/size tests |
| Create `internal/collector/collector.go` | `Collector.Run`, `Start`, `Loop`, `Report` |
| Create `internal/collector/collector_test.go` | Fake fetcher + real temp SQLite store |
| Modify `internal/config/config.go` (+ test) | `COLLECTOR_ENABLED`, `COLLECTOR_INTERVAL` |
| Modify `internal/app/app.go` | Wire collector when enabled; background tasks in `Run` |
| Modify `Makefile`, `README.md` | `dev-api` enables the collector; docs |
| Modify (iOS) `omni-control-app/.../CandidateRow.swift` | Show feed publish time when present |

---

### Task 1: Store — skip already-published stories, record last collection

**Files:** Modify `internal/newsroom/store.go`, `internal/newsroom/store_test.go`

- [ ] **Step 1: Write failing tests** (append to `store_test.go`)

```go
func TestInsertSkipsStoriesAlreadyPublishedAsArticles(t *testing.T) {
	store, db := openStore(t)
	ctx := context.Background()
	mustExec(t, db, `INSERT INTO authors (name, email, password_hash, role) VALUES ('A', 'a@example.invalid', 'x', 'admin')`)
	mustExec(t, db, `INSERT INTO categories (name, slug) VALUES ('AI', 'ai')`)
	mustExec(t, db, `INSERT INTO articles (title, slug, category_id, author_id, status, source_url)
		VALUES ('Old', 'openai-ships-a-model', 1, 1, 'published', 'https://techcrunch.com/2026/09/01/old/')`)

	_, inserted, err := store.Insert(ctx, newsroom.NewCandidate{
		SourceURL: "https://techcrunch.com/2026/09/01/old/", SourceName: "TechCrunch", FeedURL: "https://techcrunch.com/feed/", Title: "Different title",
	}, t0)
	if err != nil || inserted {
		t.Fatalf("same source URL: inserted=%v err=%v", inserted, err)
	}
	_, inserted, err = store.Insert(ctx, newsroom.NewCandidate{
		SourceURL: "https://techcrunch.com/2026/09/02/new/", SourceName: "TechCrunch", FeedURL: "https://techcrunch.com/feed/", Title: "OpenAI ships a model",
	}, t0)
	if err != nil || inserted {
		t.Fatalf("same slug: inserted=%v err=%v", inserted, err)
	}
	insert(t, store, "https://techcrunch.com/2026/09/03/fresh/", t0)
}

func TestSetLastCollectedShowsInOverview(t *testing.T) {
	store, _ := openStore(t)
	if err := store.SetLastCollected(context.Background(), t0); err != nil {
		t.Fatal(err)
	}
	overview, err := store.Overview(context.Background(), t0.Add(-time.Hour))
	if err != nil || overview.LastCollectedAt == nil || *overview.LastCollectedAt != "2026-09-20T12:00:00Z" {
		t.Fatalf("overview = %+v err=%v", overview, err)
	}
}
```

Add to `helpers_test.go`:

```go
func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}
```

(If `store_test.go` already defines an equivalent helper from the review fixes, reuse it instead and skip this addition.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/newsroom -run 'TestInsertSkipsStories|TestSetLastCollected'`
Expected: FAIL — `store.SetLastCollected undefined`, and the first test reports `same source URL: inserted=true`.

- [ ] **Step 3: Implement**

In `store.go`, add `"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"` to imports and replace the `ExecContext` call inside `Insert` with:

```go
	result, err := s.db.ExecContext(ctx, `INSERT INTO candidates
		(source_url, source_name, feed_url, title, feed_summary, source_image_url, feed_published_at, discovered_at, status, updated_at)
		SELECT ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?
		WHERE NOT EXISTS (SELECT 1 FROM articles WHERE source_url = ? OR slug = ?)
		ON CONFLICT(source_url) DO NOTHING`,
		candidate.SourceURL, candidate.SourceName, candidate.FeedURL, candidate.Title, candidate.FeedSummary,
		candidate.SourceImageURL, feedPublished, stamp, stamp,
		candidate.SourceURL, content.Slugify(candidate.Title))
```

Update the `Insert` doc comment: "It reports false without error when the source URL is already a candidate (including rejected ones) or the story was already published as an article (same source URL or title slug)."

Add:

```go
// SetLastCollected records when the collector last completed a run.
func (s *SQLiteStore) SetLastCollected(ctx context.Context, at time.Time) error {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, settingLastCollected, formatTime(at)); err != nil {
		return fmt.Errorf("record last collection: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/newsroom ./internal/devseed ./internal/app`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/newsroom/store.go internal/newsroom/store_test.go internal/newsroom/helpers_test.go
git commit -m "Skip already-published stories when storing candidates"
```

---

### Task 2: Entity decoding and feed parsing

**Files:** Create `internal/collector/entities.go`, `internal/collector/feed.go`, `internal/collector/feed_test.go`

- [ ] **Step 1: Write failing tests** — `internal/collector/feed_test.go`

```go
package collector_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/collector"
)

const rssFeed = `<?xml version="1.0"?>
<rss version="2.0" xmlns:media="http://search.yahoo.com/mrss/" xmlns:content="http://purl.org/rss/1.0/modules/content/">
<channel><title>TechCrunch</title>
<item>
  <title><![CDATA[OpenAI &amp; Microsoft sign a new compute deal]]></title>
  <link>https://techcrunch.com/2026/09/23/openai-microsoft-compute/?utm_source=rss</link>
  <pubDate>Wed, 23 Sep 2026 10:02:00 +0000</pubDate>
  <description><![CDATA[<p>The agreement covers <b>five years</b> of GPU capacity.</p><img src="https://cdn.example.com/inline.jpg">]]></description>
  <media:content url="https://cdn.example.com/lead.jpg" medium="image" width="1200"/>
</item>
<item>
  <title>Anthropic opens a Tokyo office</title>
  <link>https://techcrunch.com/2026/09/22/anthropic-tokyo/</link>
  <pubDate>Tue, 22 Sep 2026 08:00:00 GMT</pubDate>
  <description>&lt;p&gt;Its first Asian office.&lt;/p&gt;&lt;img src=&quot;https://cdn.example.com/escaped.jpg&quot;&gt;</description>
  <enclosure url="https://cdn.example.com/podcast.mp3" type="audio/mpeg"/>
</item>
<item>
  <title>No link item</title>
</item>
<item>
  <title>Bad URL</title>
  <link>not a url</link>
</item>
</channel></rss>`

const atomFeed = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:media="http://search.yahoo.com/mrss/">
<entry>
  <title type="html">Google&#8217;s Gemini gets a smaller on-device model</title>
  <link rel="alternate" type="text/html" href="https://www.theregister.com/2026/09/23/gemini_nano/"/>
  <published>2026-09-23T09:30:00Z</published>
  <summary type="html">Runs &lt;em&gt;offline&lt;/em&gt; on recent phones.</summary>
  <media:thumbnail url="https://regmedia.example.com/thumb.png"/>
</entry>
<entry>
  <title>Enclosure image entry about AI</title>
  <link href="https://www.theregister.com/2026/09/22/ai_enclosure/"/>
  <updated>2026-09-22T09:30:00+02:00</updated>
  <content type="html">Long body text.</content>
  <enclosure type="image/jpeg" url="https://regmedia.example.com/enclosure.jpg"/>
</entry>
</feed>`

func TestParseRSSFeed(t *testing.T) {
	items := collector.ParseFeed(rssFeed, "TechCrunch")
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2 (items without link or with an invalid URL are skipped): %+v", len(items), items)
	}
	first := items[0]
	if first.Title != "OpenAI & Microsoft sign a new compute deal" {
		t.Errorf("title = %q", first.Title)
	}
	if first.URL != "https://techcrunch.com/2026/09/23/openai-microsoft-compute/" {
		t.Errorf("url = %q (tracking params must be normalized away)", first.URL)
	}
	if first.Source != "TechCrunch" {
		t.Errorf("source = %q", first.Source)
	}
	if first.PublishedAt == nil || !first.PublishedAt.Equal(time.Date(2026, 9, 23, 10, 2, 0, 0, time.UTC)) {
		t.Errorf("publishedAt = %v", first.PublishedAt)
	}
	if first.Summary != "The agreement covers five years of GPU capacity." {
		t.Errorf("summary = %q", first.Summary)
	}
	if first.ImageURL != "https://cdn.example.com/lead.jpg" {
		t.Errorf("image = %q (media:content wins over inline img)", first.ImageURL)
	}
	second := items[1]
	if second.Summary != "Its first Asian office." || second.ImageURL != "https://cdn.example.com/escaped.jpg" {
		t.Errorf("second = %+v (audio enclosure must be ignored, escaped img used)", second)
	}
	if second.PublishedAt == nil || !second.PublishedAt.Equal(time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("GMT pubDate = %v", second.PublishedAt)
	}
}

func TestParseAtomFeed(t *testing.T) {
	items := collector.ParseFeed(atomFeed, "The Register")
	if len(items) != 2 {
		t.Fatalf("items = %d", len(items))
	}
	if items[0].Title != "Google’s Gemini gets a smaller on-device model" ||
		items[0].URL != "https://www.theregister.com/2026/09/23/gemini_nano/" ||
		items[0].Summary != "Runs offline on recent phones." ||
		items[0].ImageURL != "https://regmedia.example.com/thumb.png" {
		t.Errorf("first entry = %+v", items[0])
	}
	if items[1].ImageURL != "https://regmedia.example.com/enclosure.jpg" || items[1].Summary != "Long body text." {
		t.Errorf("second entry = %+v", items[1])
	}
	if items[1].PublishedAt == nil || !items[1].PublishedAt.Equal(time.Date(2026, 9, 22, 7, 30, 0, 0, time.UTC)) {
		t.Errorf("updated with offset = %v", items[1].PublishedAt)
	}
}

func TestParseFeedTakesFirstThirtyItemsAndCapsSummary(t *testing.T) {
	var b strings.Builder
	b.WriteString("<rss><channel>")
	for i := 0; i < 35; i++ {
		fmt.Fprintf(&b, "<item><title>AI story %d</title><link>https://techcrunch.com/2026/09/%02d/ai-%d/</link><description>%s</description></item>", i, i%28+1, i, strings.Repeat("word ", 200))
	}
	b.WriteString("</channel></rss>")
	items := collector.ParseFeed(b.String(), "TechCrunch")
	if len(items) != 30 {
		t.Fatalf("items = %d, want 30", len(items))
	}
	if runes := []rune(items[0].Summary); len(runes) > 401 || !strings.HasSuffix(items[0].Summary, "…") {
		t.Errorf("summary length %d, suffix %q", len(runes), string(runes[len(runes)-1:]))
	}
}

func TestParseFeedIgnoresNonHTTPImages(t *testing.T) {
	feed := `<rss><channel><item><title>AI item</title><link>https://techcrunch.com/2026/09/23/ai/</link>
		<media:content url="data:image/png;base64,AAAA" medium="image"/><description>&lt;img src="javascript:alert(1)"&gt;</description></item></channel></rss>`
	items := collector.ParseFeed(feed, "TechCrunch")
	if len(items) != 1 || items[0].ImageURL != "" {
		t.Fatalf("items = %+v", items)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/collector`
Expected: FAIL — `no non-test Go files` / `undefined: collector.ParseFeed`.

- [ ] **Step 3: Implement** — `internal/collector/entities.go`

```go
// Package collector fetches the approved news feeds and stores policy-passing
// items as newsroom candidates. It never publishes.
package collector

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	cdataSection  = regexp.MustCompile(`(?s)<!\[CDATA\[(.*?)\]\]>`)
	hexEntity     = regexp.MustCompile(`(?i)&#x([0-9a-f]+);`)
	decimalEntity = regexp.MustCompile(`&#(\d+);`)
	namedEntities = []struct {
		pattern     *regexp.Regexp
		replacement string
	}{
		{regexp.MustCompile(`(?i)&nbsp;`), " "},
		{regexp.MustCompile(`(?i)&amp;`), "&"},
		{regexp.MustCompile(`(?i)&lt;`), "<"},
		{regexp.MustCompile(`(?i)&gt;`), ">"},
		{regexp.MustCompile(`(?i)&quot;`), `"`},
		{regexp.MustCompile(`(?i)&apos;`), "'"},
	}
)

// decodeHTMLEntities ports decodeHtmlEntities from news-importer.ts, in the
// same order: CDATA, numeric entities, named entities, whitespace collapse.
func decodeHTMLEntities(value string) string {
	value = cdataSection.ReplaceAllString(value, "$1")
	value = replaceNumericEntities(value, hexEntity, 16)
	value = replaceNumericEntities(value, decimalEntity, 10)
	for _, entity := range namedEntities {
		value = entity.pattern.ReplaceAllLiteralString(value, entity.replacement)
	}
	return strings.Join(strings.Fields(value), " ")
}

func replaceNumericEntities(value string, pattern *regexp.Regexp, base int) string {
	return pattern.ReplaceAllStringFunc(value, func(match string) string {
		code, err := strconv.ParseInt(pattern.FindStringSubmatch(match)[1], base, 32)
		if err != nil || !utf8.ValidRune(rune(code)) {
			return match
		}
		return string(rune(code))
	})
}
```

`internal/collector/feed.go`

```go
package collector

import (
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
)

const (
	maxItemsPerFeed = 30
	maxSummaryRunes = 400
)

// FeedItem is one parsed feed entry with a normalized source URL.
type FeedItem struct {
	Title       string
	URL         string
	Source      string
	FeedURL     string
	PublishedAt *time.Time
	Summary     string
	ImageURL    string
}

var (
	itemBlock    = regexp.MustCompile(`(?is)<(?:item|entry)\b.*?</(?:item|entry)>`)
	atomLink     = regexp.MustCompile(`(?i)<link\b[^>]*\bhref=["']([^"']+)["'][^>]*>`)
	mediaTag     = regexp.MustCompile(`(?i)<media:(?:content|thumbnail)\b[^>]*>`)
	enclosureTag = regexp.MustCompile(`(?i)<enclosure\b[^>]*>`)
	imgTag       = regexp.MustCompile(`(?i)<img\b[^>]*>`)
	attribute    = regexp.MustCompile(`(?i)([a-z][a-z0-9:_-]*)\s*=\s*(?:"([^"]*)"|'([^']*)')`)

	xmlValuePatterns = compileValuePatterns("title", "link", "pubDate", "published", "updated",
		"description", "summary", "content:encoded", "content")
)

type valuePattern struct{ cdata, regular *regexp.Regexp }

func compileValuePatterns(names ...string) map[string]valuePattern {
	patterns := make(map[string]valuePattern, len(names))
	for _, name := range names {
		quoted := regexp.QuoteMeta(name)
		patterns[name] = valuePattern{
			cdata:   regexp.MustCompile(`(?is)<` + quoted + `[^>]*><!\[CDATA\[(.*?)\]\]></` + quoted + `>`),
			regular: regexp.MustCompile(`(?is)<` + quoted + `[^>]*>(.*?)</` + quoted + `>`),
		}
	}
	return patterns
}

// ParseFeed ports parseFeed from news-importer.ts (regex-based so malformed
// feeds still yield items; first 30 items) and additionally extracts a plain
// summary and a lead image for the review app.
func ParseFeed(xml, source string) []FeedItem {
	blocks := itemBlock.FindAllString(xml, maxItemsPerFeed)
	items := make([]FeedItem, 0, len(blocks))
	for _, block := range blocks {
		title := extractXMLValue(block, "title")
		link := extractXMLValue(block, "link")
		if link == "" {
			if match := atomLink.FindStringSubmatch(block); match != nil {
				link = match[1]
			}
		}
		link = decodeHTMLEntities(link)
		if title == "" || link == "" {
			continue
		}
		normalized, err := content.NormalizeSourceURL(link)
		if err != nil {
			continue
		}
		item := FeedItem{
			Title:    title,
			URL:      normalized,
			Source:   source,
			Summary:  summarize(extractXMLValue(block, "description", "summary", "content:encoded", "content")),
			ImageURL: extractImageURL(block),
		}
		if published, ok := parseFeedDate(extractXMLValue(block, "pubDate", "published", "updated")); ok {
			item.PublishedAt = &published
		}
		items = append(items, item)
	}
	return items
}

func extractXMLValue(block string, names ...string) string {
	for _, name := range names {
		patterns := xmlValuePatterns[name]
		if match := patterns.cdata.FindStringSubmatch(block); match != nil && match[1] != "" {
			return decodeHTMLEntities(match[1])
		}
		if match := patterns.regular.FindStringSubmatch(block); match != nil && match[1] != "" {
			return decodeHTMLEntities(content.StripHTML(match[1]))
		}
	}
	return ""
}

// summarize turns a feed description into at most maxSummaryRunes of plain text.
func summarize(value string) string {
	text := decodeHTMLEntities(content.StripHTML(value))
	runes := []rune(text)
	if len(runes) <= maxSummaryRunes {
		return text
	}
	cut := string(runes[:maxSummaryRunes])
	if space := strings.LastIndex(cut, " "); space > len(cut)/2 {
		cut = cut[:space]
	}
	return strings.TrimRight(cut, " ,;:.-") + "…"
}

// extractImageURL prefers media:content/thumbnail, then an image enclosure,
// then the first <img> in the (possibly entity-escaped) item markup.
func extractImageURL(block string) string {
	for _, tag := range mediaTag.FindAllString(block, -1) {
		attrs := attributes(tag)
		if medium := strings.ToLower(attrs["medium"]); medium != "" && medium != "image" {
			continue
		}
		if kind := strings.ToLower(attrs["type"]); kind != "" && !strings.HasPrefix(kind, "image/") {
			continue
		}
		if imageURL := httpURL(attrs["url"]); imageURL != "" {
			return imageURL
		}
	}
	for _, tag := range enclosureTag.FindAllString(block, -1) {
		attrs := attributes(tag)
		if strings.HasPrefix(strings.ToLower(attrs["type"]), "image/") {
			if imageURL := httpURL(attrs["url"]); imageURL != "" {
				return imageURL
			}
		}
	}
	for _, tag := range imgTag.FindAllString(html.UnescapeString(block), -1) {
		if imageURL := httpURL(attributes(tag)["src"]); imageURL != "" {
			return imageURL
		}
	}
	return ""
}

func attributes(tag string) map[string]string {
	attrs := map[string]string{}
	for _, match := range attribute.FindAllStringSubmatch(tag, -1) {
		value := match[2]
		if value == "" {
			value = match[3]
		}
		attrs[strings.ToLower(match[1])] = html.UnescapeString(value)
	}
	return attrs
}

func httpURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}
	return parsed.String()
}

var feedDateLayouts = []string{
	time.RFC1123Z, time.RFC1123, time.RFC3339Nano, time.RFC3339,
	"Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST",
	"2 Jan 2006 15:04:05 -0700", "2006-01-02 15:04:05", "2006-01-02",
}

func parseFeedDate(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range feedDateLayouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/collector -v`
Expected: 4 tests PASS. If `TestParseRSSFeed` fails on the URL assertion, check `content.NormalizeSourceURL`'s tracking-parameter rules and adjust the fixture's expected URL to what the Node normalizer produces for the same input (the Go port has parity tests; do not change the normalizer).

- [ ] **Step 5: Commit**

```bash
git add internal/collector
git commit -m "Parse approved news feeds in Go with summaries and lead images"
```

---

### Task 3: Fetcher with approved-source redirect enforcement

**Files:** Create `internal/collector/fetch.go`, `internal/collector/fetch_internal_test.go`

- [ ] **Step 1: Write failing tests** — `internal/collector/fetch_internal_test.go`

```go
package collector

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// testFetcher maps each httptest server host to a source name.
func testFetcher(sources map[string]string) *Fetcher {
	fetcher := NewFetcher()
	fetcher.sourceFor = func(raw string) (string, bool) {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "", false
		}
		source, ok := sources[parsed.Host]
		return source, ok
	}
	return fetcher
}

func TestFetchTextSendsHeadersAndReturnsBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != userAgent || !strings.Contains(r.Header.Get("Accept"), "application/rss+xml") {
			t.Errorf("headers = %v", r.Header)
		}
		_, _ = w.Write([]byte("<rss/>"))
	}))
	defer server.Close()

	body, final, err := NewFetcher().FetchText(context.Background(), server.URL+"/feed", "")
	if err != nil || body != "<rss/>" || final != server.URL+"/feed" {
		t.Fatalf("body=%q final=%q err=%v", body, final, err)
	}
}

func TestFetchTextRejectsNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer server.Close()
	if _, _, err := NewFetcher().FetchText(context.Background(), server.URL, ""); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchTextEnforcesApprovedSourceAcrossRedirects(t *testing.T) {
	outside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("elsewhere")) }))
	defer outside.Close()
	approved := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/same":
			http.Redirect(w, r, "/final", http.StatusFound)
		case "/away":
			http.Redirect(w, r, outside.URL+"/x", http.StatusFound)
		default:
			_, _ = w.Write([]byte("article"))
		}
	}))
	defer approved.Close()
	fetcher := testFetcher(map[string]string{
		strings.TrimPrefix(approved.URL, "http://"): "TechCrunch",
		strings.TrimPrefix(outside.URL, "http://"):  "Elsewhere",
	})

	body, final, err := fetcher.FetchText(context.Background(), approved.URL+"/same", "TechCrunch")
	if err != nil || body != "article" || final != approved.URL+"/final" {
		t.Fatalf("same-source redirect: body=%q final=%q err=%v", body, final, err)
	}
	if _, _, err := fetcher.FetchText(context.Background(), approved.URL+"/away", "TechCrunch"); !errors.Is(err, ErrRedirectOutsideSource) {
		t.Fatalf("outside redirect err = %v", err)
	}
	if _, _, err := fetcher.FetchText(context.Background(), approved.URL+"/away", ""); err != nil {
		t.Fatalf("feeds (no expected source) may redirect anywhere: %v", err)
	}
}

func TestFetchTextStopsAfterFiveRedirectsAndTimesOut(t *testing.T) {
	loop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path+"x", http.StatusFound)
	}))
	defer loop.Close()
	if _, _, err := NewFetcher().FetchText(context.Background(), loop.URL+"/", ""); err == nil {
		t.Fatal("redirect loop should fail")
	}

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(time.Second):
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	fetcher := NewFetcher()
	fetcher.timeout = 50 * time.Millisecond
	if _, _, err := fetcher.FetchText(context.Background(), slow.URL, ""); err == nil {
		t.Fatal("slow response should time out")
	}
}

func TestFetchTextCapsBodySize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", maxBodyBytes+10)))
	}))
	defer server.Close()
	body, _, err := NewFetcher().FetchText(context.Background(), server.URL, "")
	if err != nil || len(body) != maxBodyBytes {
		t.Fatalf("len=%d err=%v", len(body), err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/collector -run FetchText`
Expected: FAIL — `undefined: NewFetcher`.

- [ ] **Step 3: Implement** — `internal/collector/fetch.go`

```go
package collector

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
)

const (
	userAgent      = "TechNews-Editorial-Importer/2.0"
	acceptHeader   = "text/html,application/xhtml+xml,application/rss+xml,application/atom+xml,application/xml;q=0.9,*/*;q=0.8"
	defaultTimeout = 20 * time.Second
	maxRedirects   = 5
	maxBodyBytes   = 5 << 20
)

// ErrRedirectOutsideSource reports a redirect that leaves the expected approved publication.
var ErrRedirectOutsideSource = errors.New("redirect leaves the approved source")

// Fetcher performs the importer's HTTP GETs (feeds now, source pages in the
// publisher). It ports fetchText + resolveApprovedArticleRedirect.
type Fetcher struct {
	transport http.RoundTripper
	timeout   time.Duration
	sourceFor func(string) (string, bool)
}

func NewFetcher() *Fetcher {
	return &Fetcher{transport: http.DefaultTransport, timeout: defaultTimeout, sourceFor: content.SourceForURL}
}

// FetchText returns the body (capped at 5 MB) and final URL. When
// expectedSource is non-empty, every redirect hop and the final URL must
// belong to that approved source.
func (f *Fetcher) FetchText(ctx context.Context, rawURL, expectedSource string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	client := &http.Client{
		Transport: f.transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return f.checkSource(req.URL.String(), expectedSource)
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", acceptHeader)

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", "", fmt.Errorf("fetch %s: status %d", rawURL, resp.StatusCode)
	}
	finalURL := resp.Request.URL.String()
	if err := f.checkSource(finalURL, expectedSource); err != nil {
		return "", "", err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", "", fmt.Errorf("read %s: %w", rawURL, err)
	}
	return string(body), finalURL, nil
}

func (f *Fetcher) checkSource(rawURL, expectedSource string) error {
	if expectedSource == "" {
		return nil
	}
	if source, ok := f.sourceFor(rawURL); !ok || source != expectedSource {
		return fmt.Errorf("%w %s: %s", ErrRedirectOutsideSource, expectedSource, rawURL)
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test -race ./internal/collector -run FetchText -v`
Expected: 5 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/collector/fetch.go internal/collector/fetch_internal_test.go
git commit -m "Add feed and source fetcher with approved-source redirect checks"
```

---

### Task 4: Collector run, background start and loop

**Files:** Create `internal/collector/collector.go`, `internal/collector/collector_test.go`

- [ ] **Step 1: Write failing tests** — `internal/collector/collector_test.go`

```go
package collector_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/collector"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

var now = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

type fakeFetcher struct {
	mu      sync.Mutex
	bodies  map[string]string
	calls   atomic.Int32
	block   chan struct{}
}

func (f *fakeFetcher) FetchText(ctx context.Context, url, _ string) (string, string, error) {
	f.calls.Add(1)
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return "", "", ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	body, ok := f.bodies[url]
	if !ok {
		return "", "", errors.New("feed unavailable")
	}
	return body, url, nil
}

func rss(items ...string) string {
	return "<rss><channel>" + concat(items) + "</channel></rss>"
}

func item(title, link, date string) string {
	return "<item><title>" + title + "</title><link>" + link + "</link><pubDate>" + date + "</pubDate><description>Summary of " + title + "</description></item>"
}

func concat(parts []string) (out string) {
	for _, part := range parts {
		out += part
	}
	return out
}

var feeds = []content.ApprovedFeed{
	{Source: "TechCrunch", URL: "https://techcrunch.com/feed/"},
	{Source: "The Verge", URL: "https://www.theverge.com/rss/index.xml"},
	{Source: "WIRED", URL: "https://www.wired.com/feed/rss"},
}

func setup(t *testing.T, fetcher collector.FeedFetcher) (*collector.Collector, *newsroom.SQLiteStore) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	store := newsroom.NewSQLiteStore(db)
	return collector.New(fetcher, store, feeds, func() time.Time { return now }, slog.New(slog.NewTextHandler(io.Discard, nil))), store
}

func pendingTitles(t *testing.T, store *newsroom.SQLiteStore) []string {
	t.Helper()
	page, err := store.List(context.Background(), []newsroom.Status{newsroom.StatusPending}, 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	titles := make([]string, len(page.Candidates))
	for i, candidate := range page.Candidates {
		titles[i] = candidate.Title
	}
	return titles
}

func TestRunStoresPolicyPassingItemsNewestFirst(t *testing.T) {
	fetcher := &fakeFetcher{bodies: map[string]string{
		"https://techcrunch.com/feed/": rss(
			item("OpenAI raises prices for its API", "https://techcrunch.com/2026/09/22/openai-prices/", "Tue, 22 Sep 2026 08:00:00 +0000"),
			item("Best laptops for students", "https://techcrunch.com/2026/09/22/best-laptops/", "Tue, 22 Sep 2026 09:00:00 +0000"),
			item("Anthropic launches a new Claude model", "https://techcrunch.com/2026/09/23/anthropic-claude/", "Wed, 23 Sep 2026 10:00:00 +0000"),
		),
		"https://www.theverge.com/rss/index.xml": rss(
			item("Anthropic launches a new Claude model", "https://www.theverge.com/2026/9/23/claude", "Wed, 23 Sep 2026 10:05:00 +0000"),
			item("Nvidia unveils an AI inference chip", "https://www.theverge.com/2026/9/23/nvidia-chip", "Wed, 23 Sep 2026 11:00:00 +0000"),
			item("Nvidia unveils an AI inference chip", "https://techcrunch.com/2026/09/23/wrong-source/", "Wed, 23 Sep 2026 11:00:00 +0000"),
		),
		// WIRED feed is unavailable: counted as a failure, the run continues.
	}}
	c, store := setup(t, fetcher)

	report, err := c.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Feeds != 3 || report.FeedFailures != 1 || report.Items != 6 || report.Inserted != 3 || report.Duplicates != 1 || report.Rejected != 2 {
		t.Fatalf("report = %+v", report)
	}
	got := pendingTitles(t, store)
	want := []string{"Nvidia unveils an AI inference chip", "Anthropic launches a new Claude model", "OpenAI raises prices for its API"}
	if len(got) != len(want) {
		t.Fatalf("titles = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("titles = %v, want %v", got, want)
		}
	}

	page, _ := store.List(context.Background(), []newsroom.Status{newsroom.StatusPending}, 1, 1)
	first := page.Candidates[0]
	if first.SourceName != "The Verge" || first.FeedSummary != "Summary of Nvidia unveils an AI inference chip" ||
		first.FeedPublishedAt == nil || *first.FeedPublishedAt != "2026-09-23T11:00:00Z" {
		t.Fatalf("first = %+v", first)
	}
	overview, _ := store.Overview(context.Background(), now.Add(-time.Hour))
	if overview.LastCollectedAt == nil || *overview.LastCollectedAt != "2026-09-23T12:00:00Z" {
		t.Fatalf("last collected = %v", overview.LastCollectedAt)
	}
}

func TestRunIsIdempotentAcrossRuns(t *testing.T) {
	fetcher := &fakeFetcher{bodies: map[string]string{
		"https://techcrunch.com/feed/": rss(item("OpenAI raises prices for its API", "https://techcrunch.com/2026/09/22/openai-prices/", "")),
	}}
	c, _ := setup(t, fetcher)
	if _, err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	report, err := c.Run(context.Background())
	if err != nil || report.Inserted != 0 || report.Known != 1 {
		t.Fatalf("second run = %+v err=%v", report, err)
	}
}

func TestStartRunsInBackgroundAndRejectsOverlap(t *testing.T) {
	fetcher := &fakeFetcher{bodies: map[string]string{}, block: make(chan struct{})}
	c, _ := setup(t, fetcher)

	requestCtx, cancelRequest := context.WithCancel(context.Background())
	if err := c.Start(requestCtx); err != nil {
		t.Fatal(err)
	}
	cancelRequest() // the HTTP request ends; the run must keep going
	if err := c.Start(context.Background()); !errors.Is(err, newsroom.ErrCollectInProgress) {
		t.Fatalf("overlapping start err = %v", err)
	}
	if _, err := c.Run(context.Background()); !errors.Is(err, newsroom.ErrCollectInProgress) {
		t.Fatalf("overlapping run err = %v", err)
	}
	close(fetcher.block)
	deadline := time.Now().Add(2 * time.Second)
	for c.Running() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if c.Running() {
		t.Fatal("background run did not finish")
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("start after finish err = %v", err)
	}
}

func TestLoopRunsImmediatelyThenOnInterval(t *testing.T) {
	fetcher := &fakeFetcher{bodies: map[string]string{}}
	c, _ := setup(t, fetcher)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Loop(ctx, 20*time.Millisecond); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for fetcher.calls.Load() < int32(2*len(feeds)) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if fetcher.calls.Load() < int32(2*len(feeds)) {
		t.Fatalf("feed fetches = %d, want at least two runs", fetcher.calls.Load())
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/collector -run 'TestRun|TestStart|TestLoop'`
Expected: FAIL — `undefined: collector.New`.

- [ ] **Step 3: Implement** — `internal/collector/collector.go`

```go
package collector

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
)

// RunTimeout bounds one collection run, including background runs started by
// POST /api/newsroom/collect.
const RunTimeout = 5 * time.Minute

type FeedFetcher interface {
	FetchText(ctx context.Context, url, expectedSource string) (body, finalURL string, err error)
}

type CandidateStore interface {
	Insert(ctx context.Context, candidate newsroom.NewCandidate, now time.Time) (int64, bool, error)
	SetLastCollected(ctx context.Context, at time.Time) error
}

// Report summarizes one run for logs.
type Report struct {
	Feeds        int
	FeedFailures int
	Items        int
	Rejected     int
	Duplicates   int
	Inserted     int
	Known        int
	Rejections   map[string]int
}

// Collector turns approved feed items into pending candidates. It never publishes.
type Collector struct {
	fetcher FeedFetcher
	store   CandidateStore
	feeds   []content.ApprovedFeed
	now     func() time.Time
	logger  *slog.Logger
	running atomic.Bool
}

func New(fetcher FeedFetcher, store CandidateStore, feeds []content.ApprovedFeed, now func() time.Time, logger *slog.Logger) *Collector {
	return &Collector{fetcher: fetcher, store: store, feeds: feeds, now: now, logger: logger}
}

// Running reports whether a run is in progress.
func (c *Collector) Running() bool { return c.running.Load() }

// Run collects synchronously. It returns newsroom.ErrCollectInProgress when a
// run is already active.
func (c *Collector) Run(ctx context.Context) (Report, error) {
	if !c.running.CompareAndSwap(false, true) {
		return Report{}, newsroom.ErrCollectInProgress
	}
	defer c.running.Store(false)
	ctx, cancel := context.WithTimeout(ctx, RunTimeout)
	defer cancel()
	return c.collect(ctx)
}

// Start implements newsroom.Collector: it begins a run on a context detached
// from the caller's (the HTTP request ends once 202 is written) and returns
// immediately.
func (c *Collector) Start(ctx context.Context) error {
	if !c.running.CompareAndSwap(false, true) {
		return newsroom.ErrCollectInProgress
	}
	go func() {
		defer c.running.Store(false)
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), RunTimeout)
		defer cancel()
		if _, err := c.collect(runCtx); err != nil {
			c.logger.ErrorContext(runCtx, "collection failed", "error", err)
		}
	}()
	return nil
}

// Loop collects immediately and then every interval until ctx is done.
func (c *Collector) Loop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := c.Run(ctx); err != nil && !errors.Is(err, newsroom.ErrCollectInProgress) && ctx.Err() == nil {
			c.logger.ErrorContext(ctx, "scheduled collection failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *Collector) collect(ctx context.Context) (Report, error) {
	results := make([][]FeedItem, len(c.feeds))
	var failures atomic.Int32
	var wg sync.WaitGroup
	for i, feed := range c.feeds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, _, err := c.fetcher.FetchText(ctx, feed.URL, "")
			if err != nil {
				failures.Add(1)
				c.logger.WarnContext(ctx, "feed fetch failed", "source", feed.Source, "error", err)
				return
			}
			items := ParseFeed(body, feed.Source)
			for j := range items {
				items[j].FeedURL = feed.URL
			}
			results[i] = items
		}()
	}
	wg.Wait()

	now := c.now()
	report := Report{Feeds: len(c.feeds), FeedFailures: int(failures.Load()), Rejections: map[string]int{}}
	seenURLs := map[string]bool{}
	seenSlugs := map[string]bool{}
	var accepted []FeedItem
	for _, items := range results {
		for _, item := range items {
			report.Items++
			if reason := content.AutomaticItemRejectionReason(item.Title, item.URL, item.Source, now); reason != "" {
				report.Rejected++
				report.Rejections[reason]++
				continue
			}
			slug := content.Slugify(item.Title)
			if seenURLs[item.URL] || seenSlugs[slug] {
				report.Duplicates++
				continue
			}
			seenURLs[item.URL] = true
			seenSlugs[slug] = true
			accepted = append(accepted, item)
		}
	}

	// Insert oldest first so the newest item gets the highest id and lists first.
	sort.SliceStable(accepted, func(i, j int) bool { return publishedUnix(accepted[i]) < publishedUnix(accepted[j]) })
	for _, item := range accepted {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		_, inserted, err := c.store.Insert(ctx, item.candidate(), now)
		if err != nil {
			return report, err
		}
		if inserted {
			report.Inserted++
		} else {
			report.Known++
		}
	}
	if err := c.store.SetLastCollected(ctx, now); err != nil {
		return report, err
	}
	c.logger.InfoContext(ctx, "collection finished",
		"feeds", report.Feeds, "feed_failures", report.FeedFailures, "items", report.Items,
		"rejected", report.Rejected, "duplicates", report.Duplicates, "inserted", report.Inserted, "known", report.Known)
	return report, nil
}

func publishedUnix(item FeedItem) int64 {
	if item.PublishedAt == nil {
		return 0
	}
	return item.PublishedAt.Unix()
}

func (item FeedItem) candidate() newsroom.NewCandidate {
	candidate := newsroom.NewCandidate{
		SourceURL:       item.URL,
		SourceName:      item.Source,
		FeedURL:         item.FeedURL,
		Title:           item.Title,
		FeedSummary:     item.Summary,
		FeedPublishedAt: item.PublishedAt,
	}
	if item.ImageURL != "" {
		image := item.ImageURL
		candidate.SourceImageURL = &image
	}
	return candidate
}
```

Note: the "Best laptops for students" item is rejected by the review/roundup rule and the TechCrunch-URL-in-The-Verge-feed item by the source mismatch rule, giving `Rejected == 2`. If the policy rejects a fixture title for another reason (e.g. the AI gate), adjust the fixture titles, not the policy.

- [ ] **Step 4: Run tests**

Run: `go test -race ./internal/collector -v`
Expected: all collector tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/collector/collector.go internal/collector/collector_test.go
git commit -m "Collect approved feed items into newsroom candidates"
```

---

### Task 5: Configuration and app wiring

**Files:** Modify `internal/config/config.go`, `internal/config/config_test.go`, `internal/app/app.go`, `Makefile`

- [ ] **Step 1: Write failing config tests** (append to `internal/config/config_test.go`; adapt the `lookup` helper name to the one that file already uses)

```go
func TestLoadCollectorSettings(t *testing.T) {
	root := t.TempDir()
	env := map[string]string{"JWT_SECRET": "x", "COLLECTOR_ENABLED": "1", "COLLECTOR_INTERVAL": "10m"}
	cfg, err := config.Load(func(key string) string { return env[key] }, root)
	if err != nil || !cfg.CollectorEnabled || cfg.CollectorInterval != 10*time.Minute {
		t.Fatalf("cfg = %+v err=%v", cfg, err)
	}

	cfg, err = config.Load(func(key string) string { return map[string]string{"JWT_SECRET": "x"}[key] }, root)
	if err != nil || cfg.CollectorEnabled || cfg.CollectorInterval != 30*time.Minute {
		t.Fatalf("defaults = %+v err=%v", cfg, err)
	}

	for _, bad := range []string{"soon", "10s"} {
		env["COLLECTOR_INTERVAL"] = bad
		if _, err := config.Load(func(key string) string { return env[key] }, root); err == nil {
			t.Fatalf("COLLECTOR_INTERVAL=%q should fail", bad)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/config -run TestLoadCollectorSettings`
Expected: FAIL — `cfg.CollectorEnabled undefined`.

- [ ] **Step 3: Implement config**

In `config.go`: add fields to `Config`:

```go
	// CollectorEnabled wires the feed collector (manual "Collect now" and the loop).
	CollectorEnabled  bool
	CollectorInterval time.Duration
```

Add `const DefaultCollectorInterval = 30 * time.Minute`. In `Load`, after `cfg.JWTSecret = ...`:

```go
	cfg.CollectorInterval = DefaultCollectorInterval
	switch strings.ToLower(lookup("COLLECTOR_ENABLED")) {
	case "1", "true", "yes":
		cfg.CollectorEnabled = true
	}
	if value := lookup("COLLECTOR_INTERVAL"); value != "" {
		interval, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("COLLECTOR_INTERVAL: %w", err)
		}
		cfg.CollectorInterval = interval
	}
```

In `Validate`, add: `if c.CollectorEnabled && c.CollectorInterval < time.Minute { return errors.New("COLLECTOR_INTERVAL must be at least 1m") }` (add imports `strings`, `time` as needed). Extend `String()` to include `CollectorEnabled` and `CollectorInterval`.

- [ ] **Step 4: Wire the app**

In `internal/app/app.go`:
- Import `internal/collector`.
- Add a field `background []func(context.Context)` to `App`.
- In `NewWithDatabaseAt`, replace the newsroom block with:

```go
	newsroomStore := newsroom.NewSQLiteStore(db)
	newsroomService, err := newsroom.NewService(newsroomStore, now, newsroom.RandomMinutes)
	if err != nil {
		return nil, err
	}
	var feedCollector *collector.Collector
	var newsroomCollector newsroom.Collector
	if cfg.CollectorEnabled {
		feedCollector = collector.New(collector.NewFetcher(), newsroomStore, content.ApprovedFeeds(), now, logger)
		newsroomCollector = feedCollector
	}
	newsroomHandler := newsroom.NewHandler(newsroomService, newsroomCollector, logger)
```

(`newsroomCollector` must stay a nil interface when disabled so `/collect` answers 503.)

- Build the `App` into a variable and register the loop:

```go
	application := &App{address: cfg.Address, handler: handler, server: server}
	if feedCollector != nil {
		application.background = append(application.background, func(ctx context.Context) {
			feedCollector.Loop(ctx, cfg.CollectorInterval)
		})
	}
	return application, nil
```

- Replace `Run`:

```go
// Run serves HTTP and runs background tasks (the collector loop) until ctx is
// done, then waits for the tasks to stop.
func (a *App) Run(ctx context.Context) error {
	tasksCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	for _, task := range a.background {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task(tasksCtx)
		}()
	}
	err := a.server.Run(ctx)
	cancel()
	wg.Wait()
	return err
}
```

- [ ] **Step 5: Makefile**

Change `dev-api` so local runs collect real feeds by default (harmless: collecting never publishes):

```make
dev-api:
	JWT_SECRET=$${JWT_SECRET:-$(DEV_JWT_SECRET)} COLLECTOR_ENABLED=$${COLLECTOR_ENABLED:-1} go run ./cmd/api
```

- [ ] **Step 6: Run everything**

Run: `make check && make test-race && make contracts-check`
Expected: all pass (`TestNewsroomInputValidation` still gets 503 from `/collect` because tests build `config.Config` without `CollectorEnabled`).

- [ ] **Step 7: Commit**

```bash
git add internal/config internal/app/app.go Makefile
git commit -m "Wire the feed collector behind COLLECTOR_ENABLED"
```

---

### Task 6: Show feed publish time in the app (omni-control-app repo)

**Files:** Modify `/Users/ozan/Projects/omni-control-app/OmniControl/Features/Newsroom/Presentation/Components/CandidateRow.swift`

Collected candidates share one `discovered_at` per run, so "5 MIN. AGO" would repeat for every row. Prefer the feed's publish time.

- [ ] **Step 1: Change `SourceLine`**

```swift
/// "THE VERGE · 2H AGO" line under a candidate; uses the feed's publish time when known.
struct SourceLine: View {
    let candidate: Candidate

    var body: some View {
        let date = candidate.feedPublishedAt ?? candidate.discoveredAt
        Text("\(candidate.sourceName) · \(date.formatted(.relative(presentation: .numeric, unitsStyle: .abbreviated)))".uppercased())
            .font(DSFont.body(11, relativeTo: .caption2))
            .trackingEm(0.06, size: 11)
            .foregroundStyle(DSColor.accent)
            .lineLimit(1)
    }
}
```

- [ ] **Step 2: Build and test**

Run (from omni-control-app): `xcodegen generate && xcodebuild test -project OmniControl.xcodeproj -scheme OmniControl -destination 'platform=iOS Simulator,name=iPhone 17 Pro' -derivedDataPath build`
Expected: `** TEST SUCCEEDED **`.

- [ ] **Step 3: Commit (omni-control-app)**

```bash
git add OmniControl/Features/Newsroom/Presentation/Components/CandidateRow.swift
git commit -m "Show feed publish time on candidate rows"
```

---

### Task 7: Live dev check and docs

**Files:** Modify `README.md` (server-go)

- [ ] **Step 1: Run against real feeds** (dev DB only)

```bash
make dev-seed
make dev-api   # COLLECTOR_ENABLED defaults to 1; first run starts immediately
```

In another terminal, after ~30 seconds:

```bash
sqlite3 ../../data/technews.db "SELECT source_name, COUNT(*) FROM candidates WHERE feed_url NOT LIKE '%example%' GROUP BY source_name;"
```

Expected: several approved sources with counts > 0; the API log shows `collection finished` with `inserted` > 0 and per-feed `feed fetch failed` warnings only for feeds that are genuinely down.

- [ ] **Step 2: Try it in the app**

Open the iOS app (Debug) → AI & Tech News → Settings → **Collect now**: expect "Collection started…", then pull to refresh on Candidates and see real stories with source images and feed times.

- [ ] **Step 3: README**

Replace the sentence "The collector and publisher are not implemented yet: `POST /api/newsroom/collect` answers `503` and queued items are not processed." in `README.md` with:

```markdown
The collector (`internal/collector`) fetches the approved feeds, applies the
publishing policy and stores new items as pending candidates; it never
publishes. It runs when `COLLECTOR_ENABLED=1` (every `COLLECTOR_INTERVAL`,
default `30m`, and on `POST /api/newsroom/collect`); otherwise that endpoint
answers `503`. `make dev-api` enables it. The publisher (queued → published)
is not implemented yet.
```

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "Document the newsroom collector"
```

---

## Out of scope (phase 4+)

Source page extraction, Gemini, illustration, S3, article insert, IndexNow, publisher loop, production deployment.
