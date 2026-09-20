package content_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

func TestSQLiteStoreArticleReadSemantics(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	seed(t, db)
	store := content.NewSQLiteStore(db)

	result, err := store.List(context.Background(), content.ListQuery{Page: 1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 2 || len(result.Articles) != 1 || result.Articles[0].ID != 301 {
		t.Fatalf("list = %#v", result)
	}
	filtered, err := store.List(context.Background(), content.ListQuery{Page: 1, Limit: 12, Category: "synthetic-code", Search: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Total != 1 || filtered.Articles[0].ID != 302 || filtered.Articles[0].FeaturedImage != nil || filtered.Articles[0].Author.Avatar != nil {
		t.Fatalf("filtered = %#v", filtered)
	}
	if filtered.Articles[0].PublishedAt == nil || *filtered.Articles[0].PublishedAt != "2026-09-18 09:00:00" {
		t.Fatalf("timestamp not preserved: %#v", filtered.Articles[0].PublishedAt)
	}

	article, err := store.PublishedBySlugAndIncrement(context.Background(), "synthetic-published-newer")
	if err != nil {
		t.Fatal(err)
	}
	if article.ViewCount != 42 || article.Category.Name != "Synthetic AI" || article.Author.Email != "editorial@example.invalid" {
		t.Fatalf("article = %#v", article)
	}
	after, err := store.ByID(context.Background(), 301)
	if err != nil {
		t.Fatal(err)
	}
	if after.ViewCount != 43 {
		t.Fatalf("view count = %d", after.ViewCount)
	}
	if _, err := store.PublishedBySlugAndIncrement(context.Background(), "synthetic-draft"); err != content.ErrNotFound {
		t.Fatalf("draft slug error = %v", err)
	}
	draft, err := store.ByID(context.Background(), 303)
	if err != nil || draft.Status != "draft" {
		t.Fatalf("draft ID = %#v, %v", draft, err)
	}
}

func TestSQLiteStoreHonorsCanceledContext(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	store := content.NewSQLiteStore(db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.List(ctx, content.ListQuery{Page: 1, Limit: 12}); !errors.Is(err, context.Canceled) {
		t.Fatalf("List error = %v", err)
	}
	if _, err := store.PublishedBySlugAndIncrement(ctx, "anything"); !errors.Is(err, context.Canceled) {
		t.Fatalf("BySlug error = %v", err)
	}
}

func seed(t *testing.T, db *sql.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO categories(id,name,slug,description,color) VALUES (101,'Synthetic AI','synthetic-ai','Synthetic artificial intelligence fixtures','#111111'),(102,'Synthetic Code','synthetic-code','Synthetic programming fixtures','#222222'),(103,'Synthetic Startups','synthetic-startups','Synthetic startup fixtures','#333333')`,
		`INSERT INTO authors(id,name,email,password_hash,avatar,bio,role) VALUES (201,'TechNews Editorial','editorial@example.invalid','fake','/uploads/synthetic-contract-image.png','Synthetic editorial contract fixture.','admin'),(202,'Synthetic Reporter','reporter@example.invalid','fake',NULL,NULL,'editor')`,
		`INSERT INTO articles(id,title,slug,excerpt,content,featured_image,category_id,author_id,status,published_at,meta_title,meta_description,source,source_url,view_count,created_at,updated_at) VALUES
(301,'Synthetic Published Newer','synthetic-published-newer','Newer synthetic excerpt','<p>Entirely synthetic contract article content.</p>','/uploads/synthetic-contract-image.png',101,201,'published','2026-09-19T12:00:00.000Z','Synthetic meta title','Synthetic meta description','Synthetic Wire','https://news.example.invalid/newer',42,'2026-09-19T10:00:00.000Z','2026-09-19T12:00:00.000Z'),
(302,'Synthetic Published Null Options','synthetic-published-null-options','Null option excerpt','Synthetic plain text.',NULL,102,202,'published','2026-09-18 09:00:00',NULL,NULL,NULL,NULL,0,'2026-09-18 08:00:00','2026-09-18 09:00:00'),
(303,'Synthetic Draft','synthetic-draft','Draft excerpt','<p>Synthetic draft.</p>',NULL,103,201,'draft',NULL,NULL,NULL,'Synthetic Wire','https://news.example.invalid/draft',3,'2026-09-17T00:00:00.000Z','2026-09-17T00:00:00.000Z')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}
