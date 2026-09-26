package quiz_test

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/editorial"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/quiz"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

func openStore(t *testing.T) (*quiz.SQLiteStore, *sql.DB) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	descriptors := editorial.Migrations()
	descriptors = append(descriptors, content.Migrations()...)
	descriptors = append(descriptors, quiz.Migrations()...)
	// Content owns versions 2, 8 and 9, so order by version as app.Migrations does.
	slices.SortStableFunc(descriptors, func(a, b migrate.Descriptor) int { return cmp.Compare(a.Version, b.Version) })
	if err := migrate.Run(context.Background(), db, descriptors); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `INSERT INTO authors (id, name, email, password_hash) VALUES (1, 'Editor', 'editor@example.invalid', 'hash')`)
	mustExec(t, db, `INSERT INTO categories (id, name, slug) VALUES (1, 'AI', 'ai')`)
	return quiz.NewSQLiteStore(db), db
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func insertArticle(t *testing.T, db *sql.DB, id int64, slug, status, publishedAt, body string) {
	t.Helper()
	mustExec(t, db, `INSERT INTO articles (id, title, slug, content, category_id, author_id, status, published_at)
		VALUES (?, ?, ?, ?, 1, 1, ?, ?)`, id, "Title "+slug, slug, body, status, publishedAt)
}

var sampleQuestions = []quiz.Question{
	{Question: "Q1?", Options: []string{"a", "b", "c", "d"}, Answer: 2, Slug: "one", Title: "One", Evidence: "Evidence one is here."},
	{Question: "Q2?", Options: []string{"e", "f", "g", "h"}, Answer: 0, Slug: "two", Title: "Two", Evidence: "Evidence two is here."},
	{Question: "Q3?", Options: []string{"i", "j", "k", "l"}, Answer: 3, Slug: "three", Title: "Three", Evidence: "Evidence three is here."},
}

var storeNow = time.Date(2026, 9, 27, 5, 0, 0, 0, time.UTC)

func TestRecentArticlesReturnsPublishedArticlesFromTheWindowNewestFirstAsPlainText(t *testing.T) {
	store, db := openStore(t)
	insertArticle(t, db, 1, "old", "published", "2026-09-19T10:00:00.000Z", "<p>Old</p>")
	insertArticle(t, db, 2, "node-format", "published", "2026-09-21T10:00:00.000Z", "<p>Node &amp; <em>time</em>.</p><p>It&#8217;s&nbsp;here.</p>")
	insertArticle(t, db, 3, "go-format", "published", "2026-09-26T09:00:00Z", "<p>Newest</p>")
	insertArticle(t, db, 4, "draft", "draft", "2026-09-26T10:00:00Z", "<p>Draft</p>")
	insertArticle(t, db, 5, "sql-format", "published", "2026-09-24 08:00:00", "<p>Middle</p>")

	articles, err := store.RecentArticles(context.Background(), storeNow.Add(-7*24*time.Hour), 25)
	if err != nil {
		t.Fatal(err)
	}
	var slugs []string
	for _, article := range articles {
		slugs = append(slugs, article.Slug)
	}
	if want := []string{"go-format", "sql-format", "node-format"}; !slices.Equal(slugs, want) {
		t.Fatalf("slugs = %v, want %v", slugs, want)
	}
	if articles[2].ID != 2 || articles[2].Title != "Title node-format" || articles[2].Text != "Node & time. It’s here." {
		t.Fatalf("article = %+v", articles[2])
	}

	limited, err := store.RecentArticles(context.Background(), storeNow.Add(-7*24*time.Hour), 2)
	if err != nil || len(limited) != 2 {
		t.Fatalf("limited = %d, %v", len(limited), err)
	}
}

func TestSaveLatestPullDeleteAndExists(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()

	if _, err := store.Latest(ctx); !errors.Is(err, quiz.ErrNoQuiz) {
		t.Fatalf("Latest on empty = %v, want ErrNoQuiz", err)
	}
	if exists, err := store.Exists(ctx, "2026-09-26"); err != nil || exists {
		t.Fatalf("Exists on empty = %v, %v", exists, err)
	}

	first, err := store.Save(ctx, "2026-09-26", sampleQuestions, storeNow.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Save(ctx, "2026-09-27", sampleQuestions[:2], storeNow)
	if err != nil {
		t.Fatal(err)
	}
	if first.Number == 0 || second.Number != first.Number+1 || second.Day != "2026-09-27" {
		t.Fatalf("saved = %+v, %+v", first, second)
	}

	latest, err := store.Latest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Number != second.Number || latest.Day != "2026-09-27" || latest.Status != "published" || len(latest.Questions) != 2 {
		t.Fatalf("latest = %+v", latest)
	}
	if got := latest.Questions[0]; got.Question != "Q1?" || got.Answer != 2 || got.Title != "One" || got.Evidence != "Evidence one is here." || !slices.Equal(got.Options, []string{"a", "b", "c", "d"}) {
		t.Fatalf("question = %+v", got)
	}

	pulled, err := store.Pull(ctx, "2026-09-27")
	if err != nil || !pulled {
		t.Fatalf("Pull = %v, %v", pulled, err)
	}
	latest, err = store.Latest(ctx)
	if err != nil || latest.Day != "2026-09-26" {
		t.Fatalf("latest after pull = %+v, %v", latest, err)
	}
	// A pulled day still exists, so the loop never regenerates it.
	if exists, err := store.Exists(ctx, "2026-09-27"); err != nil || !exists {
		t.Fatalf("Exists(pulled) = %v, %v", exists, err)
	}
	if pulled, err := store.Pull(ctx, "2026-09-25"); err != nil || pulled {
		t.Fatalf("Pull(missing) = %v, %v", pulled, err)
	}

	if err := store.Delete(ctx, "2026-09-27"); err != nil {
		t.Fatal(err)
	}
	if exists, err := store.Exists(ctx, "2026-09-27"); err != nil || exists {
		t.Fatalf("Exists(deleted) = %v, %v", exists, err)
	}
}

func TestSaveReplacesTheDayKeepingItsNumber(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	original, err := store.Save(ctx, "2026-09-27", sampleQuestions, storeNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Pull(ctx, "2026-09-27"); err != nil {
		t.Fatal(err)
	}
	replaced, err := store.Save(ctx, "2026-09-27", sampleQuestions[1:], storeNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	latest, err := store.Latest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Number != original.Number || latest.Number != original.Number || latest.Status != "published" || len(latest.Questions) != 2 {
		t.Fatalf("original = %+v, replaced = %+v, latest = %+v", original, replaced, latest)
	}
}

func TestQuizStatusIsConstrained(t *testing.T) {
	_, db := openStore(t)
	if _, err := db.Exec(`INSERT INTO quizzes (day, questions, status, created_at) VALUES ('2026-09-27', '[]', 'draft', '2026-09-27T05:00:00Z')`); err == nil {
		t.Fatal("status 'draft' was accepted")
	}
}
