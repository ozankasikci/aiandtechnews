package content_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

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
	after, err := store.ByID(context.Background(), "301")
	if err != nil {
		t.Fatal(err)
	}
	if after.ViewCount != 43 {
		t.Fatalf("view count = %d", after.ViewCount)
	}
	if _, err := store.PublishedBySlugAndIncrement(context.Background(), "synthetic-draft"); err != content.ErrNotFound {
		t.Fatalf("draft slug error = %v", err)
	}
	draft, err := store.ByID(context.Background(), "303")
	if err != nil || draft.Status != "draft" {
		t.Fatalf("draft ID = %#v, %v", draft, err)
	}
}

func TestSQLiteStoreByIDUsesBoundSQLiteCoercion(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	seed(t, db)
	store := content.NewSQLiteStore(db)
	for _, id := range []string{"301", "301.0", " 301 "} {
		article, err := store.ByID(context.Background(), id)
		if err != nil || article.ID != 301 {
			t.Errorf("ByID(%q) = %#v, %v", id, article, err)
		}
	}
	for _, id := range []string{"missing", "999", "301 OR 1=1"} {
		if _, err := store.ByID(context.Background(), id); err != content.ErrNotFound {
			t.Errorf("ByID(%q) error = %v", id, err)
		}
	}
}

func TestSQLiteStoreTrendingWindowRanksRecentReads(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	seed(t, db)
	if _, err := db.Exec(`INSERT INTO articles(id,title,slug,excerpt,content,category_id,author_id,status,published_at,view_count) VALUES (304,'Synthetic Fresh','synthetic-fresh','Fresh','<p>Fresh.</p>',101,201,'published',datetime('now','-1 hour'),0)`); err != nil {
		t.Fatal(err)
	}
	store := content.NewSQLiteStore(db)
	for range 2 {
		if _, err := store.PublishedBySlugAndIncrement(context.Background(), "synthetic-published-null-options"); err != nil {
			t.Fatal(err)
		}
	}
	var today int
	if err := db.QueryRow(`SELECT count FROM article_views WHERE article_id = 302 AND day = date('now')`).Scan(&today); err != nil || today != 2 {
		t.Fatalf("today's views for 302 = %d, %v", today, err)
	}

	allTime, err := store.Trending(context.Background(), 5, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if ids := articleIDs(allTime); len(ids) == 0 || ids[0] != 301 {
		t.Fatalf("all-time trending = %v, want 301 first", ids)
	}

	// 302 was read in the window, 304 was published in it with no reads yet,
	// and 301 has the most reads overall but none in the window.
	recent, err := store.Trending(context.Background(), 5, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if ids := articleIDs(recent); len(ids) != 2 || ids[0] != 302 || ids[1] != 304 {
		t.Fatalf("24h trending = %v, want [302 304]", ids)
	}
}

func articleIDs(articles []content.Article) []int64 {
	ids := make([]int64, len(articles))
	for i, article := range articles {
		ids[i] = article.ID
	}
	return ids
}

func TestSQLiteStoreReturnsSummaryWithArticleBySlug(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	seed(t, db)
	if _, err := db.Exec(`INSERT INTO article_summaries(article_id, tldr, why_it_matters) VALUES (301, '["One.","Two.","Three."]', 'It matters.')`); err != nil {
		t.Fatal(err)
	}
	store := content.NewSQLiteStore(db)
	with, err := store.PublishedBySlugAndIncrement(context.Background(), "synthetic-published-newer")
	if err != nil {
		t.Fatal(err)
	}
	if len(with.TLDR) != 3 || with.TLDR[2] != "Three." || with.WhyItMatters != "It matters." {
		t.Fatalf("summary = %q / %q", with.TLDR, with.WhyItMatters)
	}
	without, err := store.PublishedBySlugAndIncrement(context.Background(), "synthetic-published-null-options")
	if err != nil {
		t.Fatal(err)
	}
	if without.TLDR != nil || without.WhyItMatters != "" {
		t.Fatalf("summary without row = %q / %q", without.TLDR, without.WhyItMatters)
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

// "Today" ranks reads from the last 24 hours by hour; the week ranks by day.
func TestSQLiteStoreTrendingTodayCountsHoursNotCalendarDays(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	seed(t, db)
	store := content.NewSQLiteStore(db)
	if _, err := store.PublishedBySlugAndIncrement(context.Background(), "synthetic-published-null-options"); err != nil {
		t.Fatal(err)
	}
	var hourly int
	if err := db.QueryRow(`SELECT count FROM article_views_hourly WHERE article_id = 302 AND hour = strftime('%Y-%m-%d %H', 'now')`).Scan(&hourly); err != nil || hourly != 1 {
		t.Fatalf("this hour's views for 302 = %d, %v", hourly, err)
	}
	// 301 was read a lot 30 hours ago: inside the week, outside the last 24h,
	// though its calendar day may still be yesterday.
	old := time.Now().UTC().Add(-30 * time.Hour)
	if _, err := db.Exec(`INSERT INTO article_views_hourly(article_id, hour, count) VALUES (301, ?, 50)`, old.Format("2006-01-02 15")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO article_views(article_id, day, count) VALUES (301, ?, 50)`, old.Format(time.DateOnly)); err != nil {
		t.Fatal(err)
	}
	today, err := store.Trending(context.Background(), 5, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if ids := articleIDs(today); len(ids) != 1 || ids[0] != 302 {
		t.Fatalf("24h trending = %v, want [302]", ids)
	}
	week, err := store.Trending(context.Background(), 5, time.Now().Add(-7*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if ids := articleIDs(week); len(ids) < 2 || ids[0] != 301 || ids[1] != 302 {
		t.Fatalf("7d trending = %v, want 301 then 302", ids)
	}
}
