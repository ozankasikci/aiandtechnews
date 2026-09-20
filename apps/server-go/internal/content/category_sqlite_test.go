package content_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

func TestSQLiteStoreListsCategoriesInSQLiteNameOrder(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(`INSERT INTO categories(id,name,slug,description,color) VALUES
		(1,'lowercase','lowercase','lower','#111111'),
		(2,'Zulu','zulu','last','#222222'),
		(3,'Alpha','alpha','first','#333333')`)
	if err != nil {
		t.Fatal(err)
	}

	got, err := content.NewSQLiteStore(db).ListCategories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != (content.Category{ID: 3, Name: "Alpha", Slug: "alpha", Description: "first", Color: "#333333"}) || got[1].Name != "Zulu" || got[2].Name != "lowercase" {
		t.Fatalf("categories = %#v", got)
	}
}

func TestSQLiteStoreListCategoriesReturnsNonNilEmptyAndErrors(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	store := content.NewSQLiteStore(db)
	got, err := store.ListCategories(context.Background())
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty categories = %#v, %v", got, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.ListCategories(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListCategories(context.Background()); err == nil {
		t.Fatal("closed database error = nil")
	}
}
