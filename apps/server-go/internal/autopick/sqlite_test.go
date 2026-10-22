package autopick

import (
	"cmp"
	"context"
	"database/sql"
	"slices"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/editorial"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
	_ "modernc.org/sqlite"
)

func TestPendingMarksOriginalsByFeedURL(t *testing.T) {
	// Keep the fixture in memory so this test never opens a database file.
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	descriptors := editorial.Migrations()
	descriptors = append(descriptors, content.Migrations()...)
	descriptors = append(descriptors, newsroom.Migrations()...)
	slices.SortStableFunc(descriptors, func(a, b migrate.Descriptor) int { return cmp.Compare(a.Version, b.Version) })
	if err := migrate.Run(ctx, db, descriptors); err != nil {
		t.Fatal(err)
	}
	queue := newsroom.NewSQLiteStore(db)
	for _, tt := range []struct {
		feed     string
		source   string
		original bool
	}{
		{feed: "https://openai.com/news/rss.xml", source: "Company newsroom", original: true},
		{feed: "https://techcrunch.com/feed/", source: "OpenAI", original: false},
		{feed: "https://example.invalid/feed", source: "OpenAI", original: false},
	} {
		t.Run(tt.feed, func(t *testing.T) {
			id, inserted, err := queue.Insert(ctx, newsroom.NewCandidate{
				SourceURL: tt.feed + "story", SourceName: tt.source, FeedURL: tt.feed,
				Title: "A new model", FeedSummary: "Model details.",
			}, now)
			if err != nil || !inserted {
				t.Fatalf("inserted=%v err=%v", inserted, err)
			}
			pending, err := NewSQLiteStore(db).Pending(ctx, 1)
			if err != nil || len(pending) != 1 {
				t.Fatalf("pending=%+v err=%v", pending, err)
			}
			got := pending[0]
			if got.ID != id || got.Original != tt.original || got.Source != tt.source || got.Title != "A new model" || got.Summary != "Model details." || got.PublishedAt != "" {
				t.Fatalf("candidate = %+v, want id=%d original=%v source=%q", got, id, tt.original, tt.source)
			}
		})
	}
}
