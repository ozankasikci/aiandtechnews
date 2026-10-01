package topics_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/topics"
)

var clock = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func newDB(t *testing.T, articles int) *sql.DB {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO authors (id, name, email, password_hash, role) VALUES (1, 'Ed', 'ed@example.invalid', 'x', 'editor')`)
	exec(`INSERT INTO categories (id, name, slug, description, color) VALUES (1, 'AI', 'ai', 'd', '#000')`)
	for i := 1; i <= articles; i++ {
		exec(`INSERT INTO articles (id, title, slug, excerpt, content, category_id, author_id, status, published_at, source, source_url)
			VALUES (?, ?, ?, 'excerpt', '<p>Body '||?||'</p>', 1, 1, 'published', ?, 'Src', 'https://src.example/'||?)`,
			i, fmt.Sprintf("Article %d", i), fmt.Sprintf("article-%d", i), i, fmt.Sprintf("2026-09-%02d 10:00:00", i), i)
		exec(`INSERT INTO article_summaries (article_id, tldr, why_it_matters) VALUES (?, '["Point one","Point two","Point three"]', 'Matters.')`, i)
	}
	return db
}

func TestKey(t *testing.T) {
	for in, want := range map[string]string{
		"OpenAI": "openai", "OpenAI Inc": "openai", "Open AI": "openai", "OpenAI, Inc.": "openai", "OpenAI's": "openai",
		"The Trade Desk": "tradedesk", "NVIDIA Corporation": "nvidia", "GPT-5": "gpt5", "Inc": "inc", "  ": "",
	} {
		if got := topics.Key(in); got != want {
			t.Errorf("Key(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseEntitiesKeepsValidOnes(t *testing.T) {
	got, err := topics.ParseEntities("```json\n" + `{"entities":[
		{"name":"Nvidia Corp","kind":"Company","aliases":["NVIDIA","Nvidia Corporation"]},
		{"name":"nvidia","kind":"company"},
		{"name":"Thing","kind":"animal"},
		{"name":"","kind":"theme"},
		{"name":"Humanoid robots","kind":"theme"},
		{"name":"A","kind":"theme"},
		{"name":"Rubin","kind":"product"},
		{"name":"Sam Altman","kind":"person"},
		{"name":"Fifth","kind":"theme"}]}` + "\n```")
	if err != nil || len(got) != 4 || got[0].Kind != "company" || got[1].Name != "Humanoid robots" || got[3].Name != "Sam Altman" {
		t.Fatalf("got %#v, %v", got, err)
	}
	if _, err := topics.ParseEntities("no json"); err == nil {
		t.Fatal("want an error")
	}
}

func TestAssignMergesAliasesAndReportsNewlyLiveTopics(t *testing.T) {
	db := newDB(t, 4)
	store := topics.NewStore(db, func() time.Time { return clock })
	ctx := context.Background()
	names := []topics.Entity{
		{Name: "OpenAI", Kind: "company"},
		{Name: "OpenAI Inc", Kind: "company", Aliases: []string{"Open AI"}},
		{Name: "Open AI", Kind: "company"},
	}
	var last topics.Assignment
	for i, entity := range names {
		a, err := store.Assign(ctx, int64(i+1), []topics.Entity{entity})
		if err != nil {
			t.Fatal(err)
		}
		last = a
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM topics`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("topics = %d, %v", count, err)
	}
	// The third article made it live: its pages, and all earlier ones, change.
	if fmt.Sprint(last.Topics) != "[openai]" || len(last.Articles) != 4 {
		t.Fatalf("assignment = %#v", last)
	}
	// A different entity named by an alias of an existing topic maps to it.
	a, err := store.Assign(ctx, 4, []topics.Entity{{Name: "ChatGPT maker", Kind: "company", Aliases: []string{"OpenAI"}}, {Name: "GPT-5", Kind: "product"}})
	if err != nil {
		t.Fatal(err)
	}
	counts, _ := store.Counts(ctx)
	if fmt.Sprint(a.Topics) != "[openai]" || len(counts) != 2 || counts[0].Slug != "openai" || counts[0].Articles != 4 || counts[1].Slug != "gpt-5" {
		t.Fatalf("assignment %#v counts %#v", a, counts)
	}
	// Tagging again is idempotent.
	if _, err := store.Assign(ctx, 4, []topics.Entity{{Name: "GPT-5", Kind: "product"}}); err != nil {
		t.Fatal(err)
	}
	untagged, err := store.Untagged(ctx, "", 0)
	if err != nil || len(untagged) != 0 {
		t.Fatalf("untagged = %v, %v", untagged, err)
	}
}

func TestSimulatorMatchesAssign(t *testing.T) {
	db := newDB(t, 2)
	store := topics.NewStore(db, func() time.Time { return clock })
	if _, err := store.Assign(context.Background(), 1, []topics.Entity{{Name: "Nvidia", Kind: "company"}}); err != nil {
		t.Fatal(err)
	}
	sim, err := topics.NewSimulator(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	slugs := sim.Add([]topics.Entity{{Name: "NVIDIA Corp", Kind: "company"}, {Name: "Rubin", Kind: "product"}})
	counts := sim.Counts()
	if fmt.Sprint(slugs) != "[nvidia rubin]" || len(counts) != 2 || counts[0].Slug != "nvidia" || counts[0].Articles != 2 {
		t.Fatalf("%v %#v", slugs, counts)
	}
}

type fakeText struct {
	answers []string
	prompts []string
}

func (f *fakeText) GenerateJSON(_ context.Context, prompt string) (string, error) {
	f.prompts = append(f.prompts, prompt)
	answer := f.answers[0]
	f.answers = f.answers[1:]
	return answer, nil
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func summaryJSON() string {
	sentence := "Nvidia ships accelerators for data centres and keeps adding capacity for the largest cloud customers this quarter. "
	return fmt.Sprintf(`{"summary":%q,"facts":["Fact one is stated.","Fact two is invented.","Fact three — has a dash."]}`,
		strings.Repeat(sentence, 7)+"Lastly, the supply picture is described as tight.")
}

func liveTopic(t *testing.T, db *sql.DB, store *topics.Store) {
	t.Helper()
	for i := int64(1); i <= 3; i++ {
		if _, err := store.Assign(context.Background(), i, []topics.Entity{{Name: "Nvidia", Kind: "company"}}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSummarizerStoresOnlyVerifiedText(t *testing.T) {
	db := newDB(t, 3)
	now := clock
	store := topics.NewStore(db, func() time.Time { return now })
	liveTopic(t, db, store)
	ctx := context.Background()

	text := &fakeText{answers: []string{
		summaryJSON(),
		`{"summary":[{"n":1,"supported":true},{"n":2,"supported":true},{"n":3,"supported":true},{"n":4,"supported":true},{"n":5,"supported":true},{"n":6,"supported":true},{"n":7,"supported":true},{"n":8,"supported":true}],
		 "facts":[{"n":1,"supported":true},{"n":2,"supported":false},{"n":3,"supported":true}]}`,
	}}
	summarizer := &topics.Summarizer{Store: store, Text: text, Logger: quiet()}
	changed, err := summarizer.Refresh(ctx)
	if err != nil || changed != 1 {
		t.Fatalf("changed = %d, %v", changed, err)
	}
	if strings.Contains(text.prompts[0], "src.example") || !strings.Contains(text.prompts[0], "Point one") || !strings.Contains(text.prompts[0], "Why it matters: Matters.") {
		t.Fatalf("prompt = %s", text.prompts[0])
	}
	var summary, facts string
	_ = db.QueryRow(`SELECT summary, facts_json FROM topics WHERE slug = 'nvidia'`).Scan(&summary, &facts)
	if !strings.HasPrefix(summary, "Nvidia ships") || facts != `["Fact one is stated.","Fact three, has a dash."]` {
		t.Fatalf("stored %q %s", summary, facts)
	}

	// Not due again: summarised just now.
	if due, _ := store.Due(ctx, 10); len(due) != 0 {
		t.Fatalf("due = %v", due)
	}
	// A new article a day later makes it due; an unsupported sentence keeps the old summary.
	now = clock.Add(25 * time.Hour)
	if _, err := store.Assign(ctx, 3, []topics.Entity{{Name: "Nvidia", Kind: "company"}}); err != nil {
		t.Fatal(err)
	}
	if due, _ := store.Due(ctx, 10); len(due) != 1 {
		t.Fatalf("due = %v", due)
	}
	text.answers = []string{summaryJSON(), `{"summary":[{"n":1,"supported":true}],"facts":[]}`}
	changed, _ = summarizer.Refresh(ctx)
	var after string
	_ = db.QueryRow(`SELECT summary FROM topics WHERE slug = 'nvidia'`).Scan(&after)
	if changed != 0 || after != summary {
		t.Fatalf("a failed check must keep the previous summary (changed %d)", changed)
	}
	// The rejected draft is not retried until another article arrives.
	if due, _ := store.Due(ctx, 10); len(due) != 0 {
		t.Fatalf("rejected topic due again: %v", due)
	}
}

func TestParseSummaryRejectsShortAndMarkdown(t *testing.T) {
	if _, _, err := topics.ParseSummary(`{"summary":"Too short.","facts":[]}`); err == nil {
		t.Fatal("short summary accepted")
	}
	if _, _, err := topics.ParseSummary(fmt.Sprintf(`{"summary":%q}`, strings.Repeat("word ", 150)+"# Heading")); err == nil {
		t.Fatal("markdown accepted")
	}
}
