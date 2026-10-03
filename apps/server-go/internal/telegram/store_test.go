package telegram_test

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/telegram"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

var storeNow = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

func openStore(t *testing.T) (*sql.DB, *telegram.SQLiteStore) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO categories(id,name,slug,description,color) VALUES (1,'AI','ai','AI','#111111')`,
		`INSERT INTO authors(id,name,email,password_hash,role) VALUES (1,'TechNews Editorial','editorial@example.invalid','fake','admin')`,
		// published_at mixes SQLite's UTC format, ISO with Z and ISO with an offset.
		`INSERT INTO articles(id,title,slug,excerpt,content,featured_image,category_id,author_id,status,published_at,created_at,updated_at) VALUES
(1,'Before start','before-start','e','c',NULL,1,1,'published','2026-10-02 23:59:59','x','x'),
(2,'Second','second','e2','c','/uploads/b.png',1,1,'published','2026-10-03T09:00:00.000Z','x','x'),
(3,'First','first','e3','c','https://example.com/a.webp',1,1,'published','2026-10-03 08:30:00','x','x'),
(4,'Offset','offset','e4','c',NULL,1,1,'published','2026-10-03T11:45:00+03:00','x','x'),
(5,'Draft','draft','e','c',NULL,1,1,'draft',NULL,'x','x'),
(6,'Future','future','e','c',NULL,1,1,'published','2026-10-03T13:00:00Z','x','x')`,
		`INSERT INTO article_summaries(article_id,tldr,why_it_matters) VALUES (3,'["One."," ","Two."]','Because.')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	return db, telegram.NewSQLiteStore(db, func() time.Time { return storeNow }, time.UTC)
}

var start = time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)

func nextSlug(t *testing.T, store *telegram.SQLiteStore) string {
	t.Helper()
	post, err := store.NextDue(context.Background(), start, telegram.MaxAttempts)
	if err != nil {
		t.Fatal(err)
	}
	if post == nil {
		return ""
	}
	return post.Slug
}

func TestNextDueIsTheOldestPublishedAfterTheStart(t *testing.T) {
	_, store := openStore(t)
	post, err := store.NextDue(context.Background(), start, telegram.MaxAttempts)
	if err != nil || post == nil {
		t.Fatalf("post = %v err = %v", post, err)
	}
	want := telegram.Post{
		ID: 3, Slug: "first", Title: "First", Excerpt: "e3", Category: "AI",
		FeaturedImage: "https://example.com/a.webp", WhyItMatters: "Because.", TLDR: []string{"One.", "Two."},
		PublishedAt: time.Date(2026, 10, 3, 8, 30, 0, 0, time.UTC),
	}
	if !reflect.DeepEqual(*post, want) {
		t.Fatalf("post = %+v\nwant %+v", *post, want)
	}
	// Article 4 is 08:45 UTC through its offset.
	ctx := context.Background()
	if err := store.MarkSent(ctx, 3, 100, storeNow); err != nil {
		t.Fatal(err)
	}
	if got := nextSlug(t, store); got != "offset" {
		t.Fatalf("next = %q, want offset", got)
	}
	if err := store.MarkSent(ctx, 4, 101, storeNow); err != nil {
		t.Fatal(err)
	}
	if got := nextSlug(t, store); got != "second" {
		t.Fatalf("next = %q, want second", got)
	}
	if err := store.MarkSent(ctx, 2, 102, storeNow); err != nil {
		t.Fatal(err)
	}
	// Nothing left: 1 is before the start, 5 a draft, 6 in the future.
	if got := nextSlug(t, store); got != "" {
		t.Fatalf("next = %q, want none", got)
	}
}

func TestMarkFailedCountsAttemptsUpToTheCap(t *testing.T) {
	db, store := openStore(t)
	ctx := context.Background()
	for want := 1; want <= telegram.MaxAttempts; want++ {
		if got := nextSlug(t, store); got != "first" {
			t.Fatalf("attempt %d: next = %q", want, got)
		}
		attempts, err := store.MarkFailed(ctx, 3, "boom", storeNow)
		if err != nil || attempts != want {
			t.Fatalf("attempts = %d err = %v, want %d", attempts, err, want)
		}
	}
	if got := nextSlug(t, store); got != "offset" {
		t.Fatalf("an exhausted article is still due; next = %q", got)
	}
	var status, lastError string
	if err := db.QueryRow(`SELECT status, last_error FROM telegram_posts WHERE article_id = 3`).Scan(&status, &lastError); err != nil || status != "failed" || lastError != "boom" {
		t.Fatalf("row = %q %q err = %v", status, lastError, err)
	}
	// A later success clears the error.
	if err := store.MarkSent(ctx, 3, 7, storeNow); err != nil {
		t.Fatal(err)
	}
	var messageID int64
	var postedAt string
	var clearedError sql.NullString
	if err := db.QueryRow(`SELECT status, message_id, posted_at, last_error FROM telegram_posts WHERE article_id = 3`).Scan(&status, &messageID, &postedAt, &clearedError); err != nil {
		t.Fatal(err)
	}
	if status != "sent" || messageID != 7 || postedAt != "2026-10-03 12:00:00" || clearedError.Valid {
		t.Fatalf("row = %s %d %s %v", status, messageID, postedAt, clearedError)
	}
}

func TestStartAfterIsSetOnceAndLastPostedAtSurvives(t *testing.T) {
	db, store := openStore(t)
	ctx := context.Background()
	first, err := store.StartAfter(ctx)
	if err != nil || !first.Equal(storeNow) {
		t.Fatalf("StartAfter() = %s, %v", first, err)
	}
	later := telegram.NewSQLiteStore(db, func() time.Time { return storeNow.Add(48 * time.Hour) }, time.UTC)
	again, err := later.StartAfter(ctx)
	if err != nil || !again.Equal(storeNow) {
		t.Fatalf("StartAfter() moved to %s, %v", again, err)
	}
	var value string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = ?`, telegram.StartAfterKey).Scan(&value); err != nil || value != "2026-10-03T12:00:00Z" {
		t.Fatalf("marker = %q err = %v", value, err)
	}

	if last, err := store.LastPostedAt(ctx); err != nil || !last.IsZero() {
		t.Fatalf("LastPostedAt() = %s, %v", last, err)
	}
	if _, err := store.MarkFailed(ctx, 2, "x", storeNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSent(ctx, 3, 1, storeNow); err != nil {
		t.Fatal(err)
	}
	if last, err := store.LastPostedAt(ctx); err != nil || !last.Equal(storeNow) {
		t.Fatalf("LastPostedAt() = %s, %v", last, err)
	}
}

func TestArticleBySlug(t *testing.T) {
	_, store := openStore(t)
	post, err := store.ArticleBySlug(context.Background(), "first")
	if err != nil || post.ID != 3 || post.WhyItMatters != "Because." || len(post.TLDR) != 2 {
		t.Fatalf("post = %+v err = %v", post, err)
	}
	if _, err := store.ArticleBySlug(context.Background(), "draft"); err == nil {
		t.Fatal("a draft was found")
	}
}
