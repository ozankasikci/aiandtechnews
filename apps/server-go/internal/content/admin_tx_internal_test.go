package content

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

// openTxStore builds the v1 authors + v2 content schema without importing the
// editorial capability (the authors DDL mirrors editorial migration 1).
func openTxStore(t *testing.T) (*SQLiteStore, *sql.DB) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	descriptors := append([]migrate.Descriptor{{Version: 1, Name: "authors", SQL: `CREATE TABLE authors (
		id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, email TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL, avatar TEXT, bio TEXT,
		role TEXT NOT NULL DEFAULT 'editor' CHECK(role IN ('admin', 'editor')))`}}, Migrations()...)
	if err := migrate.Run(context.Background(), db, descriptors); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO categories (id, name, slug) VALUES (101, 'AI', 'ai'), (104, 'Deals', 'deals')`,
		`INSERT INTO authors (id, name, email, password_hash, role) VALUES (201, 'TechNews Editorial', 'e@example.invalid', 'x', 'admin')`,
		`INSERT INTO articles (id, title, slug, category_id, author_id, status, source, source_url) VALUES
			(301, 'A', 'a', 101, 201, 'published', 'TechCrunch', 'https://techcrunch.com/a'),
			(302, 'B', 'b', 101, 201, 'draft', NULL, NULL)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	return NewSQLiteStore(db), db
}

func TestAdminTxLookupsUseNodeSQLSemantics(t *testing.T) {
	store, _ := openTxStore(t)
	ctx := context.Background()
	url := "https://techcrunch.com/a"
	err := store.inAdminTx(ctx, func(tx adminTx) error {
		for _, id := range []any{"101", float64(101), " 101", int64(101)} {
			if slug, found, err := tx.categorySlug(ctx, id); err != nil || !found || slug != "ai" {
				t.Errorf("categorySlug(%#v) = %q %v %v", id, slug, found, err)
			}
		}
		if _, found, _ := tx.categorySlug(ctx, "101abc"); found {
			t.Error("categorySlug matched a non-numeric id")
		}
		for name, tt := range map[string]struct {
			exclude *string
			slug    any
			url     *string
			want    bool
		}{
			"slug":                        {nil, "a", nil, true},
			"source url":                  {nil, "new", &url, true},
			"null url never matches null": {nil, "new", nil, false},
			"own row excluded":            {ptr("301"), "a", &url, false},
			"other row not excluded":      {ptr("302"), "a", nil, true},
		} {
			if got, err := tx.articleConflict(ctx, tt.exclude, tt.slug, tt.url); err != nil || got != tt.want {
				t.Errorf("%s: articleConflict = %v %v, want %v", name, got, err, tt.want)
			}
		}
		if id, found, err := tx.editorialAuthorID(ctx); err != nil || !found || id != 201 {
			t.Errorf("editorialAuthorID = %d %v %v", id, found, err)
		}
		if _, err := tx.storedArticle(ctx, "999"); !errors.Is(err, ErrNotFound) {
			t.Errorf("storedArticle(999) error = %v", err)
		}
		stored, err := tx.storedArticle(ctx, "302")
		if err != nil || stored.Slug != "b" || stored.Source != nil || stored.SourceURL != nil || stored.CategoryID != 101 {
			t.Errorf("storedArticle(302) = %+v %v", stored, err)
		}
		if deleted, err := tx.deleteRow(ctx, "articles", "999"); err != nil || deleted {
			t.Errorf("deleteRow(999) = %v %v", deleted, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAdminTxRollsBackWhenTheMutationFails(t *testing.T) {
	store, db := openTxStore(t)
	ctx := context.Background()
	failure := errors.New("later check failed")
	err := store.inAdminTx(ctx, func(tx adminTx) error {
		if err := tx.updateRow(ctx, "articles", "302", []assignment{{"title", "Changed"}}); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("inAdminTx error = %v", err)
	}
	var title string
	if err := db.QueryRow(`SELECT title FROM articles WHERE id = 302`).Scan(&title); err != nil || title != "B" {
		t.Fatalf("title after rollback = %q %v", title, err)
	}
}

// TestInAdminTxReleasesConnectionOnPanic guards the unconditional
// `defer tx.Rollback()` right after BeginTx: the store's pool holds a single
// connection (see database.Open), so if a panicking callback ever left the
// transaction open, every later query would block forever waiting for that
// connection instead of failing fast.
func TestInAdminTxReleasesConnectionOnPanic(t *testing.T) {
	store, db := openTxStore(t)
	ctx := context.Background()

	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("expected inAdminTx callback panic to propagate")
			}
		}()
		_ = store.inAdminTx(ctx, func(tx adminTx) error {
			if err := tx.updateRow(ctx, "articles", "302", []assignment{{"title", "Changed"}}); err != nil {
				t.Fatal(err)
			}
			panic("boom")
		})
	}()

	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var one int
	if err := db.QueryRowContext(queryCtx, `SELECT 1`).Scan(&one); err != nil {
		t.Fatalf("connection was not released after panic: %v", err)
	}
	if one != 1 {
		t.Fatalf("SELECT 1 = %d", one)
	}

	var title string
	if err := db.QueryRow(`SELECT title FROM articles WHERE id = 302`).Scan(&title); err != nil || title != "B" {
		t.Fatalf("title after panicking rollback = %q %v", title, err)
	}
}

func ptr(value string) *string { return &value }
