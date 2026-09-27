package inlineimage_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/inlineimage"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

var storeNow = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

func openStore(t *testing.T) (*sql.DB, *inlineimage.SQLiteStore) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO categories(id,name,slug,description,color) VALUES (1,'AI','ai','AI','#111111')`,
		`INSERT INTO authors(id,name,email,password_hash,role) VALUES (1,'TechNews Editorial','editorial@example.invalid','fake','admin')`,
		`INSERT INTO articles(id,title,slug,excerpt,content,featured_image,category_id,author_id,status,published_at,created_at,updated_at) VALUES
(1,'Newest','newest','e','` + body("p p p p p p") + `','https://img.test/features/a.webp',1,1,'published','2026-09-27 10:00:00','x','x'),
(2,'Older ISO','older-iso','e','` + body("p p p p p p") + `','https://img.test/features/b.webp',1,1,'published','2026-09-22T10:00:00.000Z','x','x'),
(3,'Too old','too-old','e','<p>x</p>','https://img.test/features/c.webp',1,1,'published','2026-09-19 10:00:00','x','x'),
(4,'Draft','draft','e','<p>x</p>','https://img.test/features/d.webp',1,1,'draft',NULL,'x','x'),
(5,'No image','no-image','e','<p>x</p>',NULL,1,1,'published','2026-09-26 10:00:00','x','x'),
(6,'Ready','ready','e','<p>x</p>','https://img.test/features/f.webp',1,1,'published','2026-09-26 11:00:00','x','x'),
(7,'Failed twice','failed-twice','e','<p>x</p>','https://img.test/features/g.webp',1,1,'published','2026-09-26 12:00:00','x','x'),
(8,'Failed once','failed-once','e','<p>x</p>','https://img.test/features/h.webp',1,1,'published','2026-09-25 12:00:00','x','x')`,
		`INSERT INTO candidates(id,source_url,source_name,feed_url,title,source_image_url,discovered_at,status,article_id,updated_at)
			VALUES (1,'https://news.example/1','Wire','https://news.example/feed','t','https://news.example/og.jpg','x','published',1,'x')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	store := inlineimage.NewSQLiteStore(db, func() time.Time { return storeNow })
	ctx := context.Background()
	if err := store.MarkReady(ctx, 6, "https://img.test/features/f-inline.webp", "Alt.", 4, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkFailed(ctx, 7, "boom", 2); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkFailed(ctx, 8, "boom", 1); err != nil {
		t.Fatal(err)
	}
	return db, store
}

func TestSQLiteStorePendingPicksRecentUnillustratedArticlesNewestFirst(t *testing.T) {
	_, store := openStore(t)
	articles, err := store.Pending(context.Background(), storeNow.Add(-inlineimage.Window), inlineimage.MaxAttempts)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, article := range articles {
		ids = append(ids, article.ID)
	}
	if len(ids) != 3 || ids[0] != 1 || ids[1] != 8 || ids[2] != 2 {
		t.Fatalf("pending = %v", ids)
	}
	first := articles[0]
	if first.Slug != "newest" || first.Title != "Newest" || first.FeaturedImage != "https://img.test/features/a.webp" || first.SourceImage != "https://news.example/og.jpg" || first.Attempts != 0 || !strings.Contains(first.Content, "Paragraph 6") {
		t.Fatalf("first = %+v", first)
	}
	if articles[1].Attempts != 1 || articles[2].SourceImage != "" {
		t.Fatalf("articles = %+v", articles)
	}
}

func TestSQLiteStoreMarkReadyReplacesAFailure(t *testing.T) {
	db, store := openStore(t)
	if err := store.MarkReady(context.Background(), 8, "https://img.test/x.webp", "An alt.", 5, 2); err != nil {
		t.Fatal(err)
	}
	var url, alt, status string
	var after, attempts int
	var lastError sql.NullString
	if err := db.QueryRow(`SELECT url, alt, after_paragraph, status, attempts, last_error FROM article_images WHERE article_id = 8`).
		Scan(&url, &alt, &after, &status, &attempts, &lastError); err != nil {
		t.Fatal(err)
	}
	if url != "https://img.test/x.webp" || alt != "An alt." || after != 5 || status != "ready" || attempts != 2 || lastError.Valid {
		t.Fatalf("row = %s %s %d %s %d %v", url, alt, after, status, attempts, lastError)
	}
	if _, err := db.Exec(`DELETE FROM articles WHERE id = 8`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM article_images WHERE article_id = 8`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rows after article delete = %d, %v", count, err)
	}
}

func TestSQLiteStoreBySlug(t *testing.T) {
	_, store := openStore(t)
	article, err := store.BySlug(context.Background(), "failed-once")
	if err != nil || article.ID != 8 || article.Attempts != 1 || article.FeaturedImage != "https://img.test/features/h.webp" {
		t.Fatalf("article = %+v, err = %v", article, err)
	}
	if _, err := store.BySlug(context.Background(), "draft"); !errors.Is(err, inlineimage.ErrNotFound) {
		t.Fatalf("draft err = %v", err)
	}
}
