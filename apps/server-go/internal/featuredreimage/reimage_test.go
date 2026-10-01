package featuredreimage_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/featuredreimage"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

const ours = "https://aiandtech-feature-images-106111531869.s3.eu-west-1.amazonaws.com/features/a.webp"

func openStore(t *testing.T) (*sql.DB, *featuredreimage.SQLiteStore) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO categories(id,name,slug,description,color) VALUES (1,'AI','ai','AI','#111111')`,
		`INSERT INTO authors(id,name,email,password_hash,role) VALUES (1,'TechNews Editorial','editorial@example.invalid','fake','admin')`,
		`INSERT INTO articles(id,title,slug,excerpt,content,featured_image,category_id,author_id,status,published_at,created_at,updated_at) VALUES
(1,'Source newest','source-newest','e','c','https://techcrunch.com/wp/a.jpg',1,1,'published','2026-09-20 10:00:00','x','x'),
(2,'Ours','ours','e','c','` + ours + `',1,1,'published','2026-09-19 10:00:00','x','x'),
(3,'Source older','source-older','e','c','https://cdn.arstechnica.net/b.jpg',1,1,'published','2026-09-10 10:00:00','x','x'),
(4,'Draft source','draft-source','e','c','https://theverge.com/c.jpg',1,1,'draft',NULL,'x','x'),
(5,'No image','no-image','e','c',NULL,1,1,'published','2026-09-15 10:00:00','x','x'),
(6,'Relative','relative','e','c','/images/default-article.jpg',1,1,'published','2026-09-14 10:00:00','x','x'),
(7,'Exhausted','exhausted','e','c','https://theverge.com/d.jpg',1,1,'published','2026-09-18 10:00:00','x','x')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	store := featuredreimage.NewSQLiteStore(db, func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) })
	if err := store.MarkFailed(context.Background(), 7, "boom", featuredreimage.MaxAttempts); err != nil {
		t.Fatal(err)
	}
	return db, store
}

func TestPendingIsNewestFirstAndSkipsOurOwnDraftsAndExhausted(t *testing.T) {
	_, store := openStore(t)
	articles, err := store.Pending(context.Background(), featuredreimage.MaxAttempts)
	if err != nil {
		t.Fatal(err)
	}
	var slugs []string
	for _, article := range articles {
		slugs = append(slugs, article.Slug)
	}
	want := []string{"source-newest", "no-image", "source-older"}
	if len(slugs) != len(want) {
		t.Fatalf("slugs = %v, want %v", slugs, want)
	}
	for i := range want {
		if slugs[i] != want[i] {
			t.Fatalf("slugs = %v, want %v", slugs, want)
		}
	}
}

func TestReplaceIsGuardedByTheOldValueAndTouchesOnlyThatArticle(t *testing.T) {
	db, store := openStore(t)
	ctx := context.Background()
	if changed, err := store.Replace(ctx, 1, "https://other.test/x.jpg", ours); err != nil || changed {
		t.Fatalf("stale replace = %t, %v", changed, err)
	}
	if changed, err := store.Replace(ctx, 1, "https://techcrunch.com/wp/a.jpg", ours); err != nil || !changed {
		t.Fatalf("replace = %t, %v", changed, err)
	}
	if changed, err := store.Replace(ctx, 5, "", ours); err != nil || !changed {
		t.Fatalf("replace of NULL image = %t, %v", changed, err)
	}
	var image, updated, other string
	if err := db.QueryRow(`SELECT featured_image, updated_at FROM articles WHERE id = 1`).Scan(&image, &updated); err != nil || image != ours || updated != "2026-10-01 12:00:00" {
		t.Fatalf("article 1 = %q %q, %v", image, updated, err)
	}
	if err := db.QueryRow(`SELECT featured_image FROM articles WHERE id = 3`).Scan(&other); err != nil || other != "https://cdn.arstechnica.net/b.jpg" {
		t.Fatalf("article 3 = %q, %v", other, err)
	}
}

type fakeIllustrator struct {
	requests []publisher.IllustrationRequest
	err      error
}

func (f *fakeIllustrator) IllustrateGenerated(_ context.Context, request publisher.IllustrationRequest) (publisher.Illustration, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return publisher.Illustration{}, f.err
	}
	return publisher.Illustration{URL: ours}, nil
}

type fakeRevalidator struct{ slugs [][]string }

func (f *fakeRevalidator) Notify(slugs []string) { f.slugs = append(f.slugs, slugs) }

func newWorker(store *featuredreimage.SQLiteStore, ill *fakeIllustrator, rev *fakeRevalidator) *featuredreimage.Worker {
	return featuredreimage.NewWorker(store, ill, rev, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestRunOnceReplacesTheNewestAndRevalidatesWithoutAReferenceImage(t *testing.T) {
	db, store := openStore(t)
	ill, rev := &fakeIllustrator{}, &fakeRevalidator{}
	tried, err := newWorker(store, ill, rev).RunOnce(context.Background())
	if err != nil || !tried {
		t.Fatalf("RunOnce = %t, %v", tried, err)
	}
	if len(ill.requests) != 1 || ill.requests[0].Slug != "source-newest" || ill.requests[0].ReferenceImageURL != "" {
		t.Fatalf("requests = %#v", ill.requests)
	}
	if len(rev.slugs) != 1 || rev.slugs[0][0] != "source-newest" {
		t.Fatalf("revalidated = %v", rev.slugs)
	}
	var image string
	if err := db.QueryRow(`SELECT featured_image FROM articles WHERE id = 1`).Scan(&image); err != nil || image != ours {
		t.Fatalf("image = %q, %v", image, err)
	}
}

func TestRunOnceCountsFailuresAndGivesUpAfterMaxAttempts(t *testing.T) {
	db, store := openStore(t)
	ill, rev := &fakeIllustrator{err: errors.New("codex timed out")}, &fakeRevalidator{}
	worker := newWorker(store, ill, rev)
	for i := 0; i < featuredreimage.MaxAttempts; i++ {
		if _, err := worker.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	var attempts int
	var message string
	if err := db.QueryRow(`SELECT attempts, last_error FROM featured_reimage WHERE article_id = 1`).Scan(&attempts, &message); err != nil || attempts != featuredreimage.MaxAttempts || message != "codex timed out" {
		t.Fatalf("attempts = %d %q, %v", attempts, message, err)
	}
	// The next run moves on to the next article.
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := ill.requests[len(ill.requests)-1].Slug; got != "no-image" {
		t.Fatalf("next article = %q", got)
	}
	if len(rev.slugs) != 0 {
		t.Fatalf("revalidated after failures: %v", rev.slugs)
	}
}

func TestRunOnceKeepsAttemptsOnASystemFaultAndFailsPermanentErrorsForGood(t *testing.T) {
	db, store := openStore(t)
	ill := &fakeIllustrator{err: publisher.SystemFault(errors.New("codex login"))}
	worker := newWorker(store, ill, nil)
	if _, err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("system fault should surface")
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM featured_reimage WHERE article_id = 1`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("system fault recorded %d rows, %v", rows, err)
	}
	ill.err = publisher.Permanent(errors.New("blocked"))
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var attempts int
	if err := db.QueryRow(`SELECT attempts FROM featured_reimage WHERE article_id = 1`).Scan(&attempts); err != nil || attempts != featuredreimage.MaxAttempts {
		t.Fatalf("attempts = %d, %v", attempts, err)
	}
}

func TestRunOnceLeavesAnArticleEditedMeanwhileAlone(t *testing.T) {
	db, store := openStore(t)
	ill := &editingIllustrator{db: db}
	rev := &fakeRevalidator{}
	if _, err := newWorker2(store, ill, rev).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var image string
	if err := db.QueryRow(`SELECT featured_image FROM articles WHERE id = 1`).Scan(&image); err != nil || image != "https://edited.test/x.jpg" {
		t.Fatalf("image = %q, %v", image, err)
	}
	if len(rev.slugs) != 0 {
		t.Fatalf("revalidated = %v", rev.slugs)
	}
}

type editingIllustrator struct{ db *sql.DB }

func (e *editingIllustrator) IllustrateGenerated(context.Context, publisher.IllustrationRequest) (publisher.Illustration, error) {
	_, err := e.db.Exec(`UPDATE articles SET featured_image = 'https://edited.test/x.jpg' WHERE id = 1`)
	return publisher.Illustration{URL: ours}, err
}

func newWorker2(store *featuredreimage.SQLiteStore, ill featuredreimage.Illustrator, rev featuredreimage.Revalidator) *featuredreimage.Worker {
	return featuredreimage.NewWorker(store, ill, rev, slog.New(slog.NewTextHandler(io.Discard, nil)))
}
