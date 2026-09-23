package publisher_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
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

	var categories []string
	rows, err := db.Query(`SELECT slug FROM categories ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			t.Fatal(err)
		}
		categories = append(categories, slug)
	}
	rows.Close()
	if strings.Join(categories, ",") != "tech,reviews,science,entertainment,ai,creators" {
		t.Fatalf("categories = %v, want the 6 Node categories in CATEGORY_COLORS order", categories)
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

// claimedCandidate inserts a candidate and claims it, leaving it processing.
func claimedCandidate(t *testing.T, db *sql.DB) (*newsroom.SQLiteStore, int64) {
	t.Helper()
	ctx := context.Background()
	store := newsroom.NewSQLiteStore(db)
	id, _, err := store.Insert(ctx, newsroom.NewCandidate{SourceURL: "https://techcrunch.com/c", SourceName: "TechCrunch", FeedURL: "https://techcrunch.com/feed/", Title: "Candidate"}, publishNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkQueued(ctx, id, newsroom.StatusPending, publishNow, publishNow); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.ClaimDue(ctx, publishNow, 0); err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	return store, id
}

func TestPublishMarksCandidatePublishedInTheSameTransaction(t *testing.T) {
	db := openDB(t)
	store, candidateID := claimedCandidate(t, db)
	articles := publisher.NewSQLiteArticles(db, func() time.Time { return publishNow })
	article := newArticle("openai-model", "https://techcrunch.com/a", "OpenAI ships a model", "<p>Body</p>")
	article.CandidateID = candidateID

	articleID, err := articles.Publish(context.Background(), article)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := store.Get(context.Background(), candidateID)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Status != newsroom.StatusPublished || candidate.ArticleSlug == nil || *candidate.ArticleSlug != "openai-model" ||
		candidate.ScheduledFor != nil || candidate.LastError != nil {
		t.Fatalf("candidate = %+v", candidate)
	}
	var linked int64
	var publishedAt string
	if err := db.QueryRow(`SELECT article_id, published_at FROM candidates WHERE id = ?`, candidateID).Scan(&linked, &publishedAt); err != nil {
		t.Fatal(err)
	}
	if linked != articleID || publishedAt != "2026-09-23T12:00:00Z" {
		t.Fatalf("article_id=%d (want %d) published_at=%q", linked, articleID, publishedAt)
	}
}

func TestPublishRollsBackWhenCandidateIsNotProcessing(t *testing.T) {
	db := openDB(t)
	store, candidateID := claimedCandidate(t, db)
	if err := store.MarkFailed(context.Background(), candidateID, "stopped elsewhere", publishNow); err != nil {
		t.Fatal(err)
	}
	articles := publisher.NewSQLiteArticles(db, func() time.Time { return publishNow })
	article := newArticle("openai-model", "https://techcrunch.com/a", "OpenAI ships a model", "<p>Body</p>")
	article.CandidateID = candidateID

	if _, err := articles.Publish(context.Background(), article); !errors.Is(err, publisher.ErrCandidateNotProcessing) {
		t.Fatalf("err = %v, want ErrCandidateNotProcessing", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM articles`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("articles = %d err=%v; the insert must roll back", count, err)
	}
}
