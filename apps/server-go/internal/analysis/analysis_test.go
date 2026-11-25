package analysis_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/analysis"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	_ "modernc.org/sqlite"
)

var now = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

const copyText = "The earlier pilot limited access to a small group of development teams, while the rollout described here opens the same tools to every existing customer under the terms already set out in the announcement. For teams that could not join that pilot, the practical change is access through their current account rather than an invitation, with the same documented restrictions on how the tools can be used in production environments. The distinction matters for interpreting the rollout because wider availability changes who can try the tools without establishing that their performance or underlying capabilities have improved since the pilot."

type fakeText struct {
	answer  string
	answers []string
	err     error
	prompts []string
}

func (f *fakeText) GenerateJSON(_ context.Context, prompt string) (string, error) {
	f.prompts = append(f.prompts, prompt)
	if f.answers != nil {
		if len(f.prompts) > len(f.answers) {
			return "", errors.New("unexpected extra model call")
		}
		return f.answers[len(f.prompts)-1], f.err
	}
	return f.answer, f.err
}

type site struct{ slugs []string }

func (s *site) Notify(slugs []string) { s.slugs = append(s.slugs, slugs...) }

func exec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	exec(t, db, `PRAGMA foreign_keys=ON`)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	exec(t, db, `INSERT INTO authors(id,name,email,password_hash) VALUES(1,'Ed','ed@test.invalid','x')`)
	exec(t, db, `INSERT INTO categories(id,name,slug) VALUES(1,'AI','ai')`)
	exec(t, db, `INSERT INTO topics(id,slug,name,kind) VALUES(1,'tools','Tools','theme'),(2,'pilot','Pilot','theme')`)
	return db
}

func article(t *testing.T, db *sql.DB, id int, stamp time.Time, status string, topic bool) {
	t.Helper()
	exec(t, db, `INSERT INTO articles(id,title,slug,excerpt,content,category_id,author_id,status,published_at,source)
 VALUES(?,?,?,'Earlier pilot context','<p>The rollout opens access to all existing customers.</p>',1,1,?,?, 'News Outlet')`, id, fmt.Sprintf("Article %d", id), fmt.Sprintf("article-%d", id), status, stamp.Format(time.RFC3339))
	if topic {
		exec(t, db, `INSERT INTO article_topics(article_id,topic_id) VALUES(?,1)`, id)
	}
}

func draftJSON(d analysis.Draft) string { raw, _ := json.Marshal(d); return string(raw) }
func goodDraft() analysis.Draft {
	return analysis.Draft{Kind: "context", Text: copyText, Links: []string{"article-1"}}
}

func TestWorkerReadyAndFinalSkips(t *testing.T) {
	tests := []struct {
		name       string
		draft      analysis.Draft
		check      string
		status     string
		checkCalls int
	}{
		{"ready", goodDraft(), `{"ok":true,"reason":"supported"}`, "ready", 1},
		{"checker rejects", goodDraft(), `{"ok":false,"reason":"sentence repeats summary"}`, "skipped", 2},
		{"writer skips", analysis.Draft{Skip: true}, "", "skipped", 0},
		{"em dash", analysis.Draft{Kind: "implications", Text: copyText + "\u2014"}, "", "skipped", 0},
		{"hype", analysis.Draft{Kind: "implications", Text: strings.Replace(copyText, "earlier", "groundbreaking", 1)}, "", "skipped", 0},
		{"outlet name", analysis.Draft{Kind: "implications", Text: strings.Replace(copyText, "earlier pilot", "News Outlet pilot", 1)}, "", "skipped", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newDB(t)
			article(t, db, 1, now.Add(-48*time.Hour), "published", true)
			article(t, db, 2, now.Add(-time.Hour), "published", true)
			exec(t, db, `INSERT INTO article_summaries(article_id,tldr,why_it_matters) VALUES(2,'["A summary"]','Access matters')`)
			writer := &fakeText{answer: draftJSON(tt.draft)}
			checker := &fakeText{answer: tt.check}
			s := &site{}
			store := analysis.NewSQLiteStore(db, func() time.Time { return now })
			worker := analysis.NewWorker(store, analysis.Writer{Text: writer}, analysis.Checker{Text: checker}, func() time.Time { return now }, nil).WithRevalidator(s)
			tried, err := worker.RunOnce(context.Background())
			if err != nil || !tried {
				t.Fatalf("tried=%t err=%v", tried, err)
			}
			var status, kind, text, links, reason string
			var attempts int
			if err := db.QueryRow(`SELECT status,kind,text,links_json,reason,attempts FROM article_analysis WHERE article_id=2`).Scan(&status, &kind, &text, &links, &reason, &attempts); err != nil {
				t.Fatal(err)
			}
			if status != tt.status || attempts != 1 || len(checker.prompts) != tt.checkCalls {
				t.Fatalf("status=%s attempts=%d checker calls=%d", status, attempts, len(checker.prompts))
			}
			if status == "ready" {
				if kind != "context" || text != copyText || links != `["article-1"]` || !reflect.DeepEqual(s.slugs, []string{"article-2"}) {
					t.Fatalf("stored %s %s %s; refresh %v", kind, text, links, s.slugs)
				}
				for _, prompt := range []string{writer.prompts[0], checker.prompts[0]} {
					for _, want := range []string{"A summary", "Access matters", "Earlier pilot context", "article-1", "The rollout opens access"} {
						if !strings.Contains(prompt, want) {
							t.Errorf("missing evidence %q", want)
						}
					}
				}
			} else if kind != "" || text != "" || links != "[]" || reason == "" || len(s.slugs) != 0 {
				t.Fatalf("skip leaked data: %s %s %s %s %v", kind, text, links, reason, s.slugs)
			}
			// Final results are not selected for another attempt.
			pending, err := store.Pending(context.Background(), now.Add(-analysis.Window))
			if err != nil || pending.ID != 1 {
				t.Fatalf("pending=%+v err=%v", pending, err)
			}
		})
	}
}

func TestWriterDropsUnknownLinksAndCapsUniqueLinks(t *testing.T) {
	d := goodDraft()
	d.Links = []string{"unknown", "a", "a", "b", "c", "d", "https://bad.invalid"}
	text := &fakeText{answer: "```json\n" + draftJSON(d) + "\n```"}
	got, err := (analysis.Writer{Text: text}).Write(context.Background(), analysis.Article{}, []analysis.Related{{Slug: "a"}, {Slug: "b"}, {Slug: "c"}, {Slug: "d"}})
	if err != nil || !reflect.DeepEqual(got.Links, []string{"a", "b", "c"}) {
		t.Fatalf("draft=%+v err=%v", got, err)
	}
}

func TestTwoFailuresStopRetries(t *testing.T) {
	for _, stage := range []string{"writer error", "writer JSON", "checker error", "checker JSON", "checker missing verdict"} {
		t.Run(stage, func(t *testing.T) {
			db := newDB(t)
			article(t, db, 1, now.Add(-time.Hour), "published", true)
			writer := &fakeText{answer: draftJSON(analysis.Draft{Kind: "implications", Text: copyText})}
			checker := &fakeText{answer: `{"ok":true}`}
			switch stage {
			case "writer error":
				writer.err = errors.New("model failed")
			case "writer JSON":
				writer.answer = "bad JSON"
			case "checker error":
				checker.err = errors.New("check failed")
			case "checker JSON":
				checker.answer = "bad JSON"
			case "checker missing verdict":
				checker.answer = `{}`
			}
			store := analysis.NewSQLiteStore(db, func() time.Time { return now })
			worker := analysis.NewWorker(store, analysis.Writer{Text: writer}, analysis.Checker{Text: checker}, func() time.Time { return now }, nil)
			for i := 0; i < 2; i++ {
				if tried, err := worker.RunOnce(context.Background()); !tried || err == nil {
					t.Fatalf("attempt %d tried=%t err=%v", i, tried, err)
				}
			}
			if tried, err := worker.RunOnce(context.Background()); tried || err != nil {
				t.Fatalf("third attempt tried=%t err=%v", tried, err)
			}
			var status, text string
			var attempts int
			if err := db.QueryRow(`SELECT status,text,attempts FROM article_analysis`).Scan(&status, &text, &attempts); err != nil {
				t.Fatal(err)
			}
			if status != "failed" || text != "" || attempts != 2 || len(writer.prompts) != 2 {
				t.Fatalf("status=%s text=%s attempts=%d calls=%d", status, text, attempts, len(writer.prompts))
			}
		})
	}
}

func TestRelatedOnlyEarlierPublishedSharedTopicsNewestFirstMaxFive(t *testing.T) {
	db := newDB(t)
	for i := 1; i <= 10; i++ {
		article(t, db, i, now.Add(time.Duration(i-10)*time.Hour), "published", true)
	}
	article(t, db, 11, now.Add(-30*time.Minute), "draft", true)
	article(t, db, 12, now.Add(-30*time.Minute), "published", false)
	article(t, db, 13, now.Add(time.Hour), "published", true)
	// Multiple common topics must not duplicate an article.
	exec(t, db, `INSERT INTO article_topics VALUES(10,2),(9,2)`)
	related, err := analysis.NewReader(db).Related(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	var slugs []string
	for _, r := range related {
		slugs = append(slugs, r.Slug)
		if r.Title == "" || r.Excerpt == "" || r.PublishedAt == "" {
			t.Fatalf("missing evidence: %+v", r)
		}
	}
	if !reflect.DeepEqual(slugs, []string{"article-9", "article-8", "article-7", "article-6", "article-5"}) {
		t.Fatalf("related=%v", slugs)
	}
}

func TestPendingWindowAndReader(t *testing.T) {
	db := newDB(t)
	article(t, db, 1, now.Add(-15*24*time.Hour), "published", false)
	article(t, db, 2, now.Add(-analysis.Window), "published", false)
	article(t, db, 3, now.Add(-time.Hour), "draft", false)
	article(t, db, 4, now.Add(time.Hour), "published", false)
	store := analysis.NewSQLiteStore(db, func() time.Time { return now })
	a, err := store.Pending(context.Background(), now.Add(-analysis.Window))
	if err != nil || a.ID != 2 {
		t.Fatalf("pending=%+v err=%v", a, err)
	}
	if _, err := store.BySlug(context.Background(), "article-3"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("draft by slug: %v", err)
	}
	latest, err := store.Latest(context.Background(), 1)
	if err != nil || len(latest) != 1 || latest[0].ID != 4 {
		t.Fatalf("latest=%+v err=%v", latest, err)
	}
	related, err := store.Related(context.Background(), 2)
	if err != nil || len(related) != 0 {
		t.Fatalf("unrelated=%v err=%v", related, err)
	}
}

func TestStyleShape(t *testing.T) {
	for name, draft := range map[string]analysis.Draft{
		"short":           {Kind: "watch", Text: "A milestone is due. We know the date."},
		"long":            {Kind: "watch", Text: strings.Repeat("word ", 151) + ". Second sentence."},
		"one sentence":    {Kind: "watch", Text: strings.ReplaceAll(copyText, ". ", ", ")},
		"five sentences":  {Kind: "watch", Text: copyText + " More is known. More follows."},
		"bad kind":        {Kind: "opinion", Text: copyText},
		"no context link": {Kind: "context", Text: copyText},
		"html":            {Kind: "watch", Text: "<b>" + copyText + "</b>"},
	} {
		t.Run(name, func(t *testing.T) {
			if analysis.StyleReason(draft, analysis.Article{}, nil) == "" {
				t.Fatal("invalid shape accepted")
			}
		})
	}
	for _, word := range []string{"In a move that", "It remains to be seen", "groundbreaking", "revolutionary", "game-changing", "\u2014"} {
		if analysis.StyleReason(analysis.Draft{Kind: "watch", Text: copyText + word}, analysis.Article{}, nil) == "" {
			t.Errorf("accepted %q", word)
		}
	}
}

func TestWriterAndCheckerUseEvidenceBoundRulesAndDoNotRepeat(t *testing.T) {
	a := analysis.Article{Title: "Launch", Content: "<h2>Launch details</h2><p>The <b>first</b> fact.</p><p>A second fact.</p>", TLDR: []string{"Summary one.", "Summary two."}, WhyItMatters: "Already covered."}
	related := []analysis.Related{{Slug: "article-1", Title: "Earlier launch", Excerpt: "A pilot started on October 1."}}
	writer := &fakeText{answer: draftJSON(goodDraft())}
	checker := &fakeText{answer: `{"ok":true,"reason":"supported"}`}
	_, result, err := analysis.Evaluate(context.Background(), analysis.Writer{Text: writer}, analysis.Checker{Text: checker}, a, related)
	if err != nil || result.Status != "ready" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for _, prompt := range []string{writer.prompts[0], checker.prompts[0]} {
		for _, rule := range []string{
			"Every sentence must be directly traceable to a specific statement",
			"Use no knowledge beyond this evidence",
			"Do not infer motives, strategy, causal conclusions or market effects",
			`"signals", "reflects", "forces" or "threatens"`,
			"concrete facts from related excerpts (dates, earlier announcements, figures)",
			"future dates or concrete milestones explicitly stated in the article",
			"article itself names who is affected and how",
			"Do not restate or paraphrase anything in do_not_repeat",
			"Skip rather than stretch",
		} {
			if !strings.Contains(prompt, rule) {
				t.Errorf("missing rule %q", rule)
			}
		}
		_, payload, found := strings.Cut(prompt, "Evidence:\n")
		if !found {
			t.Fatal("missing evidence block")
		}
		var evidence struct {
			DoNotRepeat struct {
				TLDR  []string `json:"tldr"`
				Why   string   `json:"why_it_matters"`
				First string   `json:"first_paragraph"`
			} `json:"do_not_repeat"`
		}
		if err := json.NewDecoder(strings.NewReader(payload)).Decode(&evidence); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(evidence.DoNotRepeat.TLDR, a.TLDR) || evidence.DoNotRepeat.Why != a.WhyItMatters || evidence.DoNotRepeat.First != "The first fact." {
			t.Fatalf("do_not_repeat=%+v", evidence.DoNotRepeat)
		}
	}
}

func TestEvaluateOneRevisionAndWorkerStorage(t *testing.T) {
	revised := goodDraft()
	revised.Text = strings.Replace(copyText, "earlier pilot", "previous pilot", 1)
	tests := []struct {
		name          string
		revision      string
		secondVerdict string
		status        string
		checks        int
		wantError     bool
	}{
		{"accept revision", draftJSON(revised), `{"ok":true,"reason":"every sentence is supported"}`, "ready", 2, false},
		{"reject revision", draftJSON(revised), `{"ok":false,"reason":"still repeats the lede"}`, "skipped", 2, false},
		{"revision skips", draftJSON(analysis.Draft{Skip: true, Text: "Must not be saved"}), "", "skipped", 1, false},
		{"revision style fails", draftJSON(analysis.Draft{Kind: "watch", Text: copyText + "\u2014"}), "", "skipped", 1, false},
		{"revision malformed", `not JSON`, "", "failed", 1, true},
		{"revision checker malformed", draftJSON(revised), `not JSON`, "failed", 2, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newModels := func() (*fakeText, *fakeText) {
				return &fakeText{answers: []string{draftJSON(goodDraft()), tt.revision}}, &fakeText{answers: []string{`{"ok":false,"reason":"unsupported claim about competitors"}`, tt.secondVerdict}}
			}
			a := analysis.Article{Content: "<p>Already covered lede.</p>", TLDR: []string{"Already covered fact."}}
			related := []analysis.Related{{Slug: "article-1", Excerpt: "Earlier reporting."}}
			writer, checker := newModels()
			attempts, result, err := analysis.Evaluate(context.Background(), analysis.Writer{Text: writer}, analysis.Checker{Text: checker}, a, related)
			if (err != nil) != tt.wantError || result.Status != tt.status {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if len(writer.prompts) != 2 || len(checker.prompts) != tt.checks || len(attempts) != 2 {
				t.Fatalf("writers=%d checkers=%d attempts=%d", len(writer.prompts), len(checker.prompts), len(attempts))
			}
			if attempts[0].Draft.Text != copyText || attempts[0].Verdict == nil || attempts[0].Verdict.OK || attempts[0].Verdict.Reason != "unsupported claim about competitors" {
				t.Fatalf("first attempt=%+v", attempts[0])
			}
			if tt.checks == 2 && !tt.wantError && (attempts[1].Verdict == nil || attempts[1].Verdict.OK != (tt.status == "ready")) {
				t.Fatalf("second verdict=%+v", attempts[1].Verdict)
			}
			if tt.checks == 1 && attempts[1].Verdict != nil {
				t.Fatalf("unchecked revision has verdict: %+v", attempts[1])
			}
			if tt.status == "ready" {
				if result.Draft.Text != revised.Text {
					t.Fatal("kept original instead of revision")
				}
			} else if result.Draft.Text != "" || result.Draft.Kind != "" || len(result.Draft.Links) != 0 {
				t.Fatalf("non-ready result leaked draft: %+v", result)
			}
			for _, want := range []string{"fix exactly this rejection", "if it cannot be fixed using only the evidence, skip", `"rejected_draft"`, copyText, `"checker_reason":"unsupported claim about competitors"`, "do_not_repeat", "Already covered lede."} {
				if !strings.Contains(writer.prompts[1], want) {
					t.Errorf("revision prompt missing %q", want)
				}
			}
			// The checker receives only reporting and the current draft, never the
			// earlier rejection or writer's correction instructions.
			for _, prompt := range checker.prompts {
				if strings.Contains(prompt, "unsupported claim about competitors") || strings.Contains(prompt, "fix exactly this rejection") {
					t.Error("checker inherited revision feedback")
				}
			}
			// Exercise the same path through the worker to verify persisted status,
			// empty rejected text, unchanged article-attempt accounting and refreshes.
			db := newDB(t)
			article(t, db, 1, now.Add(-48*time.Hour), "published", true)
			article(t, db, 2, now.Add(-time.Hour), "published", true)
			writer, checker = newModels()
			site := &site{}
			worker := analysis.NewWorker(analysis.NewSQLiteStore(db, func() time.Time { return now }), analysis.Writer{Text: writer}, analysis.Checker{Text: checker}, func() time.Time { return now }, nil).WithRevalidator(site)
			tried, err := worker.RunOnce(context.Background())
			if !tried || (err != nil) != tt.wantError {
				t.Fatalf("tried=%t err=%v", tried, err)
			}
			var status, kind, text, links, reason string
			var storedAttempts int
			if err := db.QueryRow(`SELECT status,kind,text,links_json,reason,attempts FROM article_analysis WHERE article_id=2`).Scan(&status, &kind, &text, &links, &reason, &storedAttempts); err != nil {
				t.Fatal(err)
			}
			if status != tt.status || storedAttempts != 1 || len(writer.prompts) != 2 || len(checker.prompts) != tt.checks {
				t.Fatalf("stored status=%s attempts=%d writer=%d checker=%d", status, storedAttempts, len(writer.prompts), len(checker.prompts))
			}
			if status == "ready" {
				if kind != revised.Kind || text != revised.Text || links != `["article-1"]` || !reflect.DeepEqual(site.slugs, []string{"article-2"}) {
					t.Fatalf("stored revised analysis=%s %s %s refresh=%v", kind, text, links, site.slugs)
				}
			} else if kind != "" || text != "" || links != "[]" || reason == "" || len(site.slugs) != 0 {
				t.Fatalf("stored non-ready data=%s %s %s %s refresh=%v", kind, text, links, reason, site.slugs)
			}
		})
	}
}

func TestRelatedDropsOutletNamedTitlesAndExcerptsBeforeLimit(t *testing.T) {
	db := newDB(t)
	for i := 1; i <= 12; i++ {
		article(t, db, i, now.Add(time.Duration(i-12)*time.Hour), "published", true)
	}
	exec(t, db, `UPDATE articles SET title='TechCrunch reports a launch' WHERE id=11`)
	exec(t, db, `UPDATE articles SET excerpt='As reported by the vErGe' WHERE id=10`)
	exec(t, db, `UPDATE articles SET title='Details on arstechnica.com' WHERE id=9`)
	exec(t, db, `UPDATE articles SET excerpt='A <b>WIRED</b> report on the tools' WHERE id=8`)
	exec(t, db, `UPDATE articles SET title='VentureBeat: latest model' WHERE id=7`)
	exec(t, db, `UPDATE articles SET title='Rewired tools for developers' WHERE id=6`)
	related, err := analysis.NewReader(db).Related(context.Background(), 12)
	if err != nil {
		t.Fatal(err)
	}
	var slugs []string
	for _, a := range related {
		slugs = append(slugs, a.Slug)
	}
	if !reflect.DeepEqual(slugs, []string{"article-6", "article-5", "article-4", "article-3", "article-2"}) {
		t.Fatalf("related=%v", slugs)
	}
}

func TestOutletNamedItemsNeverReachEitherModelOrAllowedLinks(t *testing.T) {
	related := []analysis.Related{{Slug: "article-1", Title: "Earlier launch", Excerpt: "The pilot began in October."}}
	d := goodDraft()
	for i, name := range content.SourceNames() {
		slug := fmt.Sprintf("outlet-%d", i)
		related = append(related, analysis.Related{Slug: slug, Title: "Earlier announcement", Excerpt: "Reported by " + name})
		d.Links = append(d.Links, slug)
	}
	writer := &fakeText{answer: draftJSON(d)}
	checker := &fakeText{answer: `{"ok":true}`}
	_, result, err := analysis.Evaluate(context.Background(), analysis.Writer{Text: writer}, analysis.Checker{Text: checker}, analysis.Article{}, related)
	if err != nil || result.Status != "ready" || !reflect.DeepEqual(result.Draft.Links, []string{"article-1"}) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for _, prompt := range []string{writer.prompts[0], checker.prompts[0]} {
		if strings.Contains(prompt, "Reported by") || strings.Contains(prompt, "outlet-") {
			t.Fatal("outlet-named item reached a model")
		}
	}
}
