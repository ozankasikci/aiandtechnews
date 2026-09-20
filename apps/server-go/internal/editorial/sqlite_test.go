package editorial_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/editorial"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

func TestSQLiteStoreListsAuthorsInSQLiteNameOrderWithNulls(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(`INSERT INTO authors(id,name,email,password_hash,avatar,bio,role) VALUES
		(1,'lowercase','lower@example.invalid','secret',NULL,NULL,'editor'),
		(2,'Zulu','zulu@example.invalid','do-not-expose','/z.png','Biography','admin'),
		(3,'Alpha','alpha@example.invalid','also-secret',NULL,'', 'editor')`)
	if err != nil {
		t.Fatal(err)
	}

	got, err := editorial.NewSQLiteStore(db).ListAuthors(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != 3 || got[1].Name != "Zulu" || got[2].Name != "lowercase" {
		t.Fatalf("authors = %#v", got)
	}
	if got[1].Avatar == nil || *got[1].Avatar != "/z.png" || got[1].Bio == nil || *got[1].Bio != "Biography" || got[2].Avatar != nil || got[2].Bio != nil {
		t.Fatalf("author nullable fields = %#v", got)
	}
}

func TestSQLiteStoreListAuthorsReturnsNonNilEmptyAndErrors(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	store := editorial.NewSQLiteStore(db)
	got, err := store.ListAuthors(context.Background())
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty authors = %#v, %v", got, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.ListAuthors(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListAuthors(context.Background()); err == nil {
		t.Fatal("closed database error = nil")
	}
}
