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
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	_ "modernc.org/sqlite"
)

var now = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

const copyText = "The earlier pilot limited access to a small group of development teams, while the rollout described here opens the same tools to every existing customer under the terms already set out in the announcement. For teams that could not join that pilot, the practical change is access through their current account rather than an invitation, with the same documented restrictions on how the tools can be used in production environments. The distinction matters for interpreting the rollout because wider availability changes who can try the tools without establishing that their performance or underlying capabilities have improved since the pilot."

type fakeText struct {
	answer  string
	err     error
	prompts []string
}

func (f *fakeText) GenerateJSON(_ context.Context, prompt string) (string, error) {
	f.prompts = append(f.prompts, prompt)
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
		{"checker rejects", goodDraft(), `{"ok":false,"reason":"sentence repeats summary"}`, "skipped", 1},
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
