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
