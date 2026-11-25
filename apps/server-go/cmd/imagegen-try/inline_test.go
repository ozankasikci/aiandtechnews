package main

import (
	"context"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

func TestInlineCommandsExplainMissingMigration17(t *testing.T) {
	ctx := context.Background()
	db, path := testutil.OpenDatabase(t)
	var previous []migrate.Descriptor
	for _, migration := range app.Migrations() {
		if migration.Version < 17 {
			previous = append(previous, migration)
		}
	}
	if err := migrate.Run(ctx, db, previous); err != nil {
		t.Fatal(err)
	}
	getenv := func(string) string { return "" }
	for _, mode := range []string{"-inline", "-inline-latest"} {
		t.Run(mode, func(t *testing.T) {
			var err error
			if mode == "-inline" {
				err = runInline(ctx, nil, nil, false, "article", path, t.TempDir(), nil, nil, getenv)
			} else {
				err = runInlineLatest(ctx, &realphoto.Finder{}, 1, path, t.TempDir(), getenv)
			}
			if err == nil || !strings.Contains(err.Error(), "no such table: article_featured_image_credits") ||
				!strings.Contains(err.Error(), "run cmd/migrate on this copy first: migration 17 is missing") {
				t.Fatalf("got %v, want migration 17 instructions", err)
			}
		})
	}
}
