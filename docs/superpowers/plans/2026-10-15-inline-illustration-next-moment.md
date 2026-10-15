# In-article Illustration: Next Moment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The second (in-article) illustration shows the next moment of its passage, avoids stock scenes, differs from the last 10 covers and the last 10 in-article images, and the 15 re-covered articles get redrawn ones.

**Architecture:** A second settings-backed history (`newsroom.inline_image_history`) feeds scene notes into the inline analyzer prompt next to the cover history's notes, reusing `RecentScenesRule`. The inline brief prompt gets the next-moment and banned-scene rules. A new command, `cmd/inline-redraw`, redraws chosen articles' inline images through the same pipeline, with dry run and an undo file.

**Tech Stack:** Go 1.25, SQLite (modernc), existing `internal/illustration`, `internal/inlineimage`, `internal/siterevalidate`, `internal/media` packages.

Spec: `docs/superpowers/specs/2026-10-15-inline-illustration-next-moment-design.md`.

All commands run from `apps/server-go`. Commit with `git -c user.name="Ozan Kaşıkçı" -c user.email=ozan@kasikci.io commit` (no prefixes, no co-author line).

---

### Task 1: A settings history under any key

**Files:**
- Modify: `internal/illustration/history.go`
- Test: `internal/illustration/history_test.go`

- [ ] **Step 1: Write the failing test** (append to `history_test.go`)

```go
func TestSettingsHistoryAtKeepsItsOwnKey(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	covers := illustration.NewSettingsHistory(db)
	inline := illustration.NewSettingsHistoryAt(db, illustration.InlineHistoryKey)
	if err := covers.Record(ctx, illustration.Choice{Style: "graphic", Scene: "a cover"}); err != nil {
		t.Fatal(err)
	}
	if err := inline.Record(ctx, illustration.Choice{Style: "neon", Scene: "an inline image"}); err != nil {
		t.Fatal(err)
	}
	gotCovers, _ := covers.Recent(ctx)
	gotInline, _ := inline.Recent(ctx)
	if len(gotCovers) != 1 || gotCovers[0].Scene != "a cover" || len(gotInline) != 1 || gotInline[0].Scene != "an inline image" {
		t.Fatalf("covers = %+v, inline = %+v", gotCovers, gotInline)
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/illustration/ -run TestSettingsHistoryAtKeepsItsOwnKey`
Expected: FAIL, `undefined: illustration.NewSettingsHistoryAt`.

- [ ] **Step 3: Implement** in `history.go`

Replace the `historyKey` constant block and the `SettingsHistory` type/constructor with:

```go
const (
	historyKey  = "newsroom.featured_image_history"
	historySize = 10
	// InlineHistoryKey holds the latest in-article illustrations.
	InlineHistoryKey = "newsroom.inline_image_history"
)

// SettingsHistory keeps the history as JSON in the settings table, under a
// "newsroom." key the dashboard never shows. Newest last.
type SettingsHistory struct {
	db  *sql.DB
	key string
}

// NewSettingsHistory is the featured (cover) image history.
func NewSettingsHistory(db *sql.DB) *SettingsHistory { return NewSettingsHistoryAt(db, historyKey) }

// NewSettingsHistoryAt keeps a history under another settings key.
func NewSettingsHistoryAt(db *sql.DB, key string) *SettingsHistory {
	return &SettingsHistory{db: db, key: key}
}
```

In `Recent` and `Record`, replace the two uses of `historyKey` with `h.key`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/illustration/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/illustration/history.go internal/illustration/history_test.go
git commit -m "Keep an image history under any settings key"
```

---

### Task 2: Next-moment rules in the inline brief prompt, and the scene list for inline briefs

**Files:**
- Modify: `internal/illustration/brief.go` (`BuildInlineAnalyzePrompt`)
- Modify: `internal/illustration/analyzer.go` (`AnalyzeInput.prompt`)
- Test: `internal/illustration/brief_test.go`

- [ ] **Step 1: Write the failing tests** (append to `brief_test.go`)

```go
func TestInlinePromptAsksForTheNextMoment(t *testing.T) {
	prompt := illustration.BuildInlineAnalyzePrompt("Big news", "The racks arrive.", styles.MustLoad(), true, illustration.Compositions, illustration.Shots)
	for _, want := range []string{"next concrete moment", "a person at a desk with papers", "a handshake"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("inline prompt lacks %q", want)
		}
	}
}

func TestInlineAnalyzeInputCarriesRecentScenes(t *testing.T) {
	input := illustration.AnalyzeInput{Title: "Big news", Section: "The racks arrive.", RecentScenes: []string{"a woman at a desk"}}
	if prompt := illustration.AnalyzePromptFor(input, styles.MustLoad()); !strings.Contains(prompt, "- a woman at a desk\n") {
		t.Fatalf("inline prompt lacks the recent scenes:\n%s", prompt)
	}
}
```

Add `"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration/styles"` to the test file's imports if it is not there.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/illustration/ -run 'TestInlinePromptAsksForTheNextMoment|TestInlineAnalyzeInputCarriesRecentScenes'`
Expected: FAIL (`undefined: illustration.AnalyzePromptFor`, and missing text).

- [ ] **Step 3: Implement**

In `brief.go`, in `BuildInlineAnalyzePrompt`, replace the sentence that starts `Build the scene on what this passage describes` with:

```go
Show the next concrete moment of this passage: its consequence, the people it affects or the place where it happens, caught in action. It must not repeat the featured illustration's subject, setting, characters or framing. Unless the passage is literally about it, never show a person at a desk with papers, a person at a keyboard or screen, a handshake, a humanoid robot, or an app window or dashboard. Wherever the keys below say "the story", "the news" or "the headline", read "this passage". There will be no photo collage and no logos in this image.
```

In `analyzer.go`, change `prompt` so both briefs get the scene list, and export it for tests:

```go
// prompt is the analyzer prompt for this input, with the recent scenes rule.
func (input AnalyzeInput) prompt(catalog styles.Catalog) string {
	var base string
	if input.Section != "" {
		base = BuildInlineAnalyzePrompt(input.Title, input.Section, catalog, input.Source != nil, input.Compositions, input.Shots)
	} else {
		base = BuildAnalyzePrompt(input.Title, input.Excerpt, input.ImageURL, catalog, input.Source != nil, input.Palettes, input.Compositions, input.PreferComposition, input.Shots, input.Brands)
	}
	return base + RecentScenesRule(input.RecentScenes)
}

// AnalyzePromptFor is the analyzer prompt an input produces (for tests and tools).
func AnalyzePromptFor(input AnalyzeInput, catalog styles.Catalog) string { return input.prompt(catalog) }
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/illustration/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/illustration/brief.go internal/illustration/analyzer.go internal/illustration/brief_test.go
git commit -m "In-article brief: show the next moment, ban stock scenes, and list recent scenes"
```

---

### Task 3: The inline pipeline reads and records scene history

**Files:**
- Modify: `internal/illustration/pipeline.go` (`PipelineDeps`)
- Modify: `internal/illustration/inline.go` (`InlineRequest`, `IllustrateInline`, `ProduceInline`, `produceInline`)
- Test: `internal/illustration/inline_test.go`

- [ ] **Step 1: Write the failing test**

In `inline_test.go`, give `fakeHistory` canned recent choices:

```go
type fakeHistory struct {
	recent   []illustration.Choice
	recorded []illustration.Choice
}

func (f *fakeHistory) Recent(context.Context) ([]illustration.Choice, error) { return f.recent, nil }
```

(keep the existing `Record` method). Then append:

```go
func TestIllustrateInlineUsesAndRecordsSceneHistory(t *testing.T) {
	fixture := newInlineFixture(t)
	fixture.history.recent = []illustration.Choice{{Style: "graphic", Scene: "a cover scene"}}
	inline := &fakeHistory{recent: []illustration.Choice{{Style: "neon", Scene: "an inline scene"}}}
	fixture.deps.InlineHistory = inline
	if _, err := illustration.NewPipeline(fixture.deps).IllustrateInline(context.Background(), fixture.request); err != nil {
		t.Fatal(err)
	}
	if got := fixture.analyzer.input.RecentScenes; !slices.Equal(got, []string{"a cover scene", "an inline scene"}) {
		t.Fatalf("recent scenes = %v", got)
	}
	if len(inline.recorded) != 1 || inline.recorded[0].Scene != "Two engineers push the rack. Rows of servers." {
		t.Fatalf("inline history recorded %+v", inline.recorded)
	}
	if len(fixture.history.recorded) != 0 {
		t.Fatalf("inline image recorded in the cover history: %+v", fixture.history.recorded)
	}
}

func TestProduceInlineKeepsTheRequestScenes(t *testing.T) {
	fixture := newInlineFixture(t)
	fixture.history.recent = []illustration.Choice{{Scene: "a cover scene"}}
	fixture.request.RecentScenes = []string{"given scene"}
	if _, err := illustration.NewPipeline(fixture.deps).ProduceInline(context.Background(), fixture.request); err != nil {
		t.Fatal(err)
	}
	if got := fixture.analyzer.input.RecentScenes; !slices.Equal(got, []string{"given scene"}) {
		t.Fatalf("recent scenes = %v", got)
	}
}
```

Add `"slices"` to the imports.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/illustration/ -run 'TestIllustrateInlineUsesAndRecordsSceneHistory|TestProduceInlineKeepsTheRequestScenes'`
Expected: FAIL (`unknown field InlineHistory`, `unknown field RecentScenes`).

- [ ] **Step 3: Implement**

In `pipeline.go`, add to `PipelineDeps` after `History`:

```go
	// InlineHistory remembers the latest in-article illustrations; nil
	// disables it. Only the inline path reads and writes it.
	InlineHistory History
```

In `inline.go`, add to `InlineRequest`:

```go
	// RecentScenes, when set, replaces the scene notes read from the
	// histories (for dry runs).
	RecentScenes []string
```

Add a helper in `inline.go`:

```go
// inlineScenes is what the latest covers and in-article images show, covers
// first, newest last within each. History errors only cost variety.
func (p *Pipeline) inlineScenes(ctx context.Context) []string {
	var scenes []string
	for _, history := range []History{p.deps.History, p.deps.InlineHistory} {
		if history == nil {
			continue
		}
		recent, err := history.Recent(ctx)
		if err != nil {
			p.deps.Logger.WarnContext(ctx, "image history unavailable", "error", err)
			continue
		}
		scenes = append(scenes, VarietyFrom(recent).RecentScenes...)
	}
	return scenes
}
```

In `ProduceInline`, right after `defer p.release()`, add:

```go
	if len(request.RecentScenes) == 0 {
		request.RecentScenes = p.inlineScenes(ctx)
	}
```

In `produceInline`, add `RecentScenes: request.RecentScenes` to the `AnalyzeInput{...}` literal.

In `IllustrateInline`, after `storeWebP` succeeds and before the `return`, add:

```go
	if p.deps.InlineHistory != nil && result.Report.Brief != nil {
		choice := Choice{Style: result.Report.Style, Composition: result.Report.Composition, Shot: result.Report.Shot, Scene: SceneNote(*result.Report.Brief)}
		if err := p.deps.InlineHistory.Record(ctx, choice); err != nil {
			p.deps.Logger.WarnContext(ctx, "could not record inline image history", "error", err)
		}
	}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/illustration/...`
Expected: PASS (including the existing `TestIllustrateInlineDrawsTheSectionInTheFeaturedStyle`, which still finds nothing in the cover history's `recorded`).

- [ ] **Step 5: Commit**

```bash
git add internal/illustration/pipeline.go internal/illustration/inline.go internal/illustration/inline_test.go
git commit -m "In-article images read the cover and inline scene history and record their own"
```

---

### Task 4: Wire the inline history into the app

**Files:**
- Modify: `internal/illustration/wiring.go`
- Modify: `internal/app/app.go:238-250`

- [ ] **Step 1: Implement**

In `wiring.go`, add to `PipelineConfig` after `History`:

```go
	InlineHistory History
```

and in `BuildPipelineDeps`, after `History:  cfg.History,`:

```go
		InlineHistory: cfg.InlineHistory,
```

In `app.go`, in the `illustration.PipelineConfig{...}` literal after the `History:` line:

```go
			InlineHistory: illustration.NewSettingsHistoryAt(db, illustration.InlineHistoryKey),
```

- [ ] **Step 2: Build and run all tests**

Run: `gofmt -l ./internal ./cmd; go build ./... && go test ./...`
Expected: no gofmt output, build succeeds, all tests pass.

- [ ] **Step 3: Commit**

```bash
git add internal/illustration/wiring.go internal/app/app.go
git commit -m "Wire the in-article image history into the app"
```

---

### Task 5: `cmd/inline-redraw`

Redraws the inline illustration of given published articles. Skips credited real photos and articles without an inline row. `-dry-run` writes PNGs and touches nothing. A live run first writes the undo file, then per article stores the new image, updates `article_images` (same paragraph slot) and finally asks the site to refresh those slugs.

**Files:**
- Create: `cmd/inline-redraw/main.go`
- Create: `cmd/inline-redraw/redraw.go`
- Test: `cmd/inline-redraw/redraw_test.go`

- [ ] **Step 1: Write the failing test** (`cmd/inline-redraw/redraw_test.go`)

```go
package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
)

type fakeArticles struct {
	rows    map[string]Current
	updated []string
	undoSeen bool
	undoPath string
}

func (f *fakeArticles) Current(_ context.Context, slug string) (Current, error) { return f.rows[slug], nil }
func (f *fakeArticles) Replace(_ context.Context, c Current, url, alt string) error {
	if _, err := os.Stat(f.undoPath); err == nil {
		f.undoSeen = true
	}
	f.updated = append(f.updated, c.Slug+"="+url)
	return nil
}

type fakeDrawer struct{ drawn []string }

func (f *fakeDrawer) Draw(_ context.Context, c Current) (illustration.InlineIllustration, error) {
	f.drawn = append(f.drawn, c.Slug)
	return illustration.InlineIllustration{URL: "https://img.test/" + c.Slug + "-new.webp", Alt: "new alt"}, nil
}

type fakeRefresher struct{ slugs []string }

func (f *fakeRefresher) Revalidate(_ context.Context, slugs []string) error { f.slugs = slugs; return nil }

func TestRedrawSkipsPhotosAndWritesUndoFirst(t *testing.T) {
	undo := filepath.Join(t.TempDir(), "undo.json")
	articles := &fakeArticles{undoPath: undo, rows: map[string]Current{
		"drawn":  {ID: 1, Slug: "drawn", URL: "https://img.test/drawn-old.webp", Alt: "old", AfterParagraph: 3},
		"photo":  {ID: 2, Slug: "photo", URL: "https://img.test/photo.webp", Credited: true},
		"no-row": {ID: 3, Slug: "no-row"},
	}}
	drawer, refresher := &fakeDrawer{}, &fakeRefresher{}
	report, err := Redraw(context.Background(), Deps{Articles: articles, Drawer: drawer, Refresher: refresher}, []string{"drawn", "photo", "no-row"}, undo)
	if err != nil {
		t.Fatal(err)
	}
	if len(drawer.drawn) != 1 || drawer.drawn[0] != "drawn" {
		t.Fatalf("drawn = %v", drawer.drawn)
	}
	if !articles.undoSeen || len(articles.updated) != 1 || articles.updated[0] != "drawn=https://img.test/drawn-new.webp" {
		t.Fatalf("updated = %v, undo written first = %v", articles.updated, articles.undoSeen)
	}
	var saved []Current
	raw, _ := os.ReadFile(undo)
	if err := json.Unmarshal(raw, &saved); err != nil || len(saved) != 1 || saved[0].URL != "https://img.test/drawn-old.webp" {
		t.Fatalf("undo = %s %v", raw, err)
	}
	if len(refresher.slugs) != 1 || refresher.slugs[0] != "drawn" {
		t.Fatalf("refreshed %v", refresher.slugs)
	}
	if len(report.Skipped) != 2 {
		t.Fatalf("skipped = %v", report.Skipped)
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./cmd/inline-redraw/`
Expected: FAIL (package has no `Redraw`, `Current`, `Deps`).

- [ ] **Step 3: Implement `redraw.go`**

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
)

// Current is an article and its inline image as it is now.
type Current struct {
	ID             int64  `json:"id"`
	Slug           string `json:"slug"`
	Title          string `json:"title"`
	Content        string `json:"-"`
	FeaturedImage  string `json:"featured_image"`
	SourceImage    string `json:"-"`
	URL            string `json:"url"`
	Alt            string `json:"alt"`
	AfterParagraph int    `json:"after_paragraph"`
	// Credited: the inline image is a real photo with a credit row.
	Credited bool `json:"-"`
}

// Articles reads and replaces inline images.
type Articles interface {
	Current(ctx context.Context, slug string) (Current, error)
	Replace(ctx context.Context, current Current, url, alt string) error
}

// Drawer draws and stores a new inline illustration.
type Drawer interface {
	Draw(ctx context.Context, current Current) (illustration.InlineIllustration, error)
}

// Refresher asks the site to refresh article pages.
type Refresher interface {
	Revalidate(ctx context.Context, slugs []string) error
}

type Deps struct {
	Articles  Articles
	Drawer    Drawer
	Refresher Refresher // nil skips the refresh
}

type Report struct {
	Redrawn []string
	Skipped []string
}

// Redraw replaces the inline illustration of each slug. It writes every
// redrawable article's current url and alt to undoPath before changing
// anything, skips real photos and articles with no inline image, and stops
// at the first failure (articles already replaced stay replaced).
func Redraw(ctx context.Context, deps Deps, slugs []string, undoPath string) (Report, error) {
	var report Report
	var todo []Current
	for _, slug := range slugs {
		current, err := deps.Articles.Current(ctx, slug)
		if err != nil {
			return report, err
		}
		if current.URL == "" || current.Credited {
			report.Skipped = append(report.Skipped, slug)
			continue
		}
		todo = append(todo, current)
	}
	raw, err := json.MarshalIndent(todo, "", " ")
	if err != nil {
		return report, err
	}
	if err := os.WriteFile(undoPath, raw, 0o644); err != nil {
		return report, fmt.Errorf("write undo file: %w", err)
	}
	for _, current := range todo {
		drawn, err := deps.Drawer.Draw(ctx, current)
		if err != nil {
			return report, fmt.Errorf("draw %s: %w", current.Slug, err)
		}
		if err := deps.Articles.Replace(ctx, current, drawn.URL, drawn.Alt); err != nil {
			if drawn.Discard != nil {
				drawn.Discard(ctx)
			}
			return report, fmt.Errorf("replace %s: %w", current.Slug, err)
		}
		report.Redrawn = append(report.Redrawn, current.Slug)
	}
	if deps.Refresher != nil && len(report.Redrawn) > 0 {
		if err := deps.Refresher.Revalidate(ctx, report.Redrawn); err != nil {
			return report, fmt.Errorf("refresh site: %w", err)
		}
	}
	return report, nil
}
```

- [ ] **Step 4: Run the test**

Run: `go test ./cmd/inline-redraw/`
Expected: PASS.

- [ ] **Step 5: Implement `main.go`** (real database, pipeline, S3 and site client)

```go
// Command inline-redraw redraws the in-article illustration of chosen
// published articles with the current rules. Real photos (credited) are
// kept. -dry-run writes <slug>.png per article into -out and changes
// nothing; a live run writes -undo first, then stores each new image,
// updates article_images in place and refreshes the site pages.
//
//	inline-redraw -db ~/aiandtechnews/data/technews.db -slugs slugs.txt -dry-run -out ./redraw
//	inline-redraw -db ~/aiandtechnews/data/technews.db -slugs slugs.txt -undo ./undo.json
//
// It reads the same environment as the API: GEMINI_*, CODEX_*, CUTOUT_BIN,
// FEATURED_IMAGE_CHAIN, FEATURED_IMAGE_ANALYZER, AWS_REGION, S3_FEATURE_IMAGE_*,
// SITE_REVALIDATE_URL and NEWSLETTER_CRON_SECRET.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/inlineimage"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/siterevalidate"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "inline-redraw: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string) error {
	flags := flag.NewFlagSet("inline-redraw", flag.ContinueOnError)
	dbPath := flags.String("db", getenv("DATABASE_PATH"), "SQLite database")
	slugsPath := flags.String("slugs", "", "file with one article slug per line (required)")
	dryRun := flags.Bool("dry-run", false, "draw into -out and change nothing")
	outDir := flags.String("out", "inline-redraw-out", "folder for dry-run images")
	undoPath := flags.String("undo", "inline-redraw-undo.json", "where a live run saves the old url and alt")
	if err := flags.Parse(args); err != nil {
		return err
	}
	slugs, err := readSlugs(*slugsPath)
	if err != nil {
		return err
	}
	chain, err := config.ParseImageChain(getenv("FEATURED_IMAGE_CHAIN"))
	if err != nil {
		return err
	}
	analyzers, err := config.ParseImageAnalyzers(getenv("FEATURED_IMAGE_ANALYZER"), chain)
	if err != nil {
		return err
	}
	db, err := database.OpenExisting(ctx, *dbPath, *dryRun)
	if err != nil {
		return err
	}
	defer db.Close()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	geminiClient := gemini.New(getenv("GEMINI_API_KEY"), getenv("GEMINI_TEXT_MODEL"),
		gemini.WithImageModel(getenv("GEMINI_IMAGE_MODEL")), gemini.WithImageSize(getenv("GEMINI_IMAGE_SIZE")),
		gemini.WithVisionModel(getenv("GEMINI_VISION_MODEL")))
	pipelineConfig := illustration.PipelineConfig{
		Chain: chain, Analyzers: analyzers, CodexBin: getenv("CODEX_BIN"), CodexNodeDir: getenv("CODEX_NODE_DIR"),
		CodexTimeout: config.DefaultCodexTimeout, CutoutBin: getenv("CUTOUT_BIN"), Gemini: geminiClient,
		History:       illustration.NewSettingsHistory(db),
		InlineHistory: illustration.NewSettingsHistoryAt(db, illustration.InlineHistoryKey),
		HTTP:          illustration.NewReferenceClient(), Logger: logger,
	}
	articles := &sqliteArticles{db: db, store: inlineimage.NewSQLiteStore(db, time.Now)}
	if *dryRun {
		// No store and no history writes: ProduceInline only reads.
		pipeline := illustration.NewPipeline(illustration.BuildPipelineDeps(pipelineConfig))
		return dryRunAll(ctx, pipeline, articles, slugs, *outDir)
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(getenv("AWS_REGION")))
	if err != nil {
		return fmt.Errorf("load AWS configuration: %w", err)
	}
	pipelineConfig.Store = media.NewStore(media.Config{
		Region: getenv("AWS_REGION"), Bucket: getenv("S3_FEATURE_IMAGE_BUCKET"), Prefix: getenv("S3_FEATURE_IMAGE_PREFIX"),
		PublicBaseURL: getenv("S3_FEATURE_IMAGE_PUBLIC_URL"),
	}, s3.NewFromConfig(awsCfg), &http.Client{Timeout: 30 * time.Second}, time.Now)
	pipeline := illustration.NewPipeline(illustration.BuildPipelineDeps(pipelineConfig))
	deps := Deps{Articles: articles, Drawer: pipelineDrawer{pipeline}}
	if url := getenv("SITE_REVALIDATE_URL"); url != "" {
		deps.Refresher = siterevalidate.NewClient(url, getenv("NEWSLETTER_CRON_SECRET"), &http.Client{Timeout: 60 * time.Second})
	}
	report, err := Redraw(ctx, deps, slugs, *undoPath)
	fmt.Printf("redrawn %d: %v\nskipped %d: %v\nundo file: %s\n", len(report.Redrawn), report.Redrawn, len(report.Skipped), report.Skipped, *undoPath)
	return err
}

func readSlugs(path string) ([]string, error) {
	if path == "" {
		return nil, errors.New("-slugs is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var slugs []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			slugs = append(slugs, line)
		}
	}
	return slugs, nil
}

func dryRunAll(ctx context.Context, pipeline *illustration.Pipeline, articles *sqliteArticles, slugs []string, outDir string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	for _, slug := range slugs {
		current, err := articles.Current(ctx, slug)
		if err != nil {
			return err
		}
		if current.URL == "" || current.Credited {
			fmt.Printf("skip %s (no inline image or a real photo)\n", slug)
			continue
		}
		result, err := pipeline.ProduceInline(ctx, inlineRequest(current))
		if err != nil {
			fmt.Printf("FAIL %s: %v\n", slug, err)
			continue
		}
		if err := os.WriteFile(filepath.Join(outDir, slug+".png"), result.Image, 0o644); err != nil {
			return err
		}
		fmt.Printf("ok %s | %s\n", slug, result.Alt)
	}
	return nil
}

func inlineRequest(current Current) illustration.InlineRequest {
	return illustration.InlineRequest{
		Slug: current.Slug, Title: current.Title, Section: inlineimage.SectionText(current.Content, current.AfterParagraph),
		FeaturedImageURL: current.FeaturedImage, SourceImageURL: current.SourceImage,
	}
}

type pipelineDrawer struct{ pipeline *illustration.Pipeline }

func (d pipelineDrawer) Draw(ctx context.Context, current Current) (illustration.InlineIllustration, error) {
	return d.pipeline.IllustrateInline(ctx, inlineRequest(current))
}

type sqliteArticles struct {
	db    *sql.DB
	store *inlineimage.SQLiteStore
}

func (a *sqliteArticles) Current(ctx context.Context, slug string) (Current, error) {
	article, err := a.store.BySlug(ctx, slug)
	if err != nil {
		return Current{}, err
	}
	current := Current{ID: article.ID, Slug: article.Slug, Title: article.Title, Content: article.Content,
		FeaturedImage: article.FeaturedImage, SourceImage: article.SourceImage}
	err = a.db.QueryRowContext(ctx, `SELECT i.url, i.alt, i.after_paragraph,
		EXISTS(SELECT 1 FROM article_image_credits c WHERE c.article_image_id = i.id)
		FROM article_images i WHERE i.article_id = ? AND i.status = 'ready'`, article.ID).
		Scan(&current.URL, &current.Alt, &current.AfterParagraph, &current.Credited)
	if errors.Is(err, sql.ErrNoRows) {
		return current, nil
	}
	return current, err
}

func (a *sqliteArticles) Replace(ctx context.Context, current Current, url, alt string) error {
	return a.store.MarkReady(ctx, current.ID, url, alt, current.AfterParagraph, 1, nil)
}
```

- [ ] **Step 6: Build, vet and test everything**

Run: `gofmt -l ./internal ./cmd; go vet ./cmd/inline-redraw/ && go build ./... && go test ./...`
Expected: no gofmt output, vet clean, all tests pass. If `config.DefaultCodexTimeout` or `media.Config` field names differ, match them to `cmd/imagegen-try/main.go` and `cmd/upload-featured-image/main.go`, which use the same calls.

- [ ] **Step 7: Commit**

```bash
git add cmd/inline-redraw
git commit -m "Add inline-redraw: redraw chosen articles' in-article illustrations with an undo file"
```

---

### Task 6: Dry run on the 15 articles and owner review (no live changes)

- [ ] **Step 1:** Push the branch to the mini repo and build the tool:

```bash
git push ssh://ozankasikci@kasikcis-mac-mini.local/Volumes/Samsung990PRO/AIAndTechNews/repo inline-next-moment
ssh ozankasikci@kasikcis-mac-mini.local 'W=/Volumes/Samsung990PRO/AIAndTechNews; source $W/env.sh; cd $W/repo && git worktree add -q $W/worktrees/inline-next inline-next-moment; cd $W/worktrees/inline-next/apps/server-go && go build -p 2 -o $W/bin/inline-redraw ./cmd/inline-redraw'
```

- [ ] **Step 2:** Make the slug list from `covers3/plan.json` and run the dry run detached (load `~/aiandtechnews/technews.env` into the environment, stdin from `/dev/null`):

```bash
ssh ozankasikci@kasikcis-mac-mini.local 'cd /Volumes/Samsung990PRO/AIAndTechNews/imagegen-test && python3 -c "import json;print(\"\n\".join(p[\"slug\"] for p in json.load(open(\"covers3/plan.json\"))))" > inline-slugs.txt && set -a && . ~/aiandtechnews/technews.env && set +a && nohup ../bin/inline-redraw -db ~/aiandtechnews/data/technews.db -slugs inline-slugs.txt -dry-run -out inline-redraw-dry > inline-redraw-dry.log 2>&1 < /dev/null &'
```

- [ ] **Step 3:** When the log shows all 15 lines, fetch the PNGs and the current inline images, build an old-vs-new sheet (cover | current inline | new inline per row) and show it to the owner. Stop here until the owner approves.

---

### Task 7: Deploy and redraw (only after the owner approves Task 6)

- [ ] **Step 1:** Merge to main and deploy the API:

```bash
git fetch origin && git rebase origin/main && go test ./...
git push ssh://ozankasikci@kasikcis-mac-mini.local/Volumes/Samsung990PRO/AIAndTechNews/repo +inline-next-moment
ssh ozankasikci@kasikcis-mac-mini.local '~/aiandtechnews/deploy-api.sh /Volumes/Samsung990PRO/AIAndTechNews/worktrees/inline-next'
git push origin inline-next-moment:main
```

Expected: deploy prints `Local API healthy. Public API: 200`.

- [ ] **Step 2:** Run the live redraw for the 15 slugs (same environment as Step 2 of Task 6, without `-dry-run`, with `-undo imagegen-test/inline-redraw-undo.json`). Check the printed counts and open two of the articles on the site to confirm the new image shows.

- [ ] **Step 3:** Update the `aitn-cover-images` memory with the in-article changes and the undo file path.
