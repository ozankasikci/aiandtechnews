package newsroom_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/editorial"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

var t0 = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

func openStore(t *testing.T) (*newsroom.SQLiteStore, *sql.DB) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	descriptors := editorial.Migrations()
	descriptors = append(descriptors, content.Migrations()...)
	descriptors = append(descriptors, newsroom.Migrations()...)
	if err := migrate.Run(context.Background(), db, descriptors); err != nil {
		t.Fatal(err)
	}
	return newsroom.NewSQLiteStore(db), db
}

func insert(t *testing.T, store *newsroom.SQLiteStore, url string, at time.Time) int64 {
	t.Helper()
	id, inserted, err := store.Insert(context.Background(), newsroom.NewCandidate{
		SourceURL: url, SourceName: "The Verge", FeedURL: "https://www.theverge.com/rss/index.xml",
		Title: "Title " + url, FeedSummary: "Summary",
	}, at)
	if err != nil || !inserted {
		t.Fatalf("insert %s: inserted=%v err=%v", url, inserted, err)
	}
	return id
}

// setStatus forces a status the phase-1 API cannot produce (processing, published, failed).
func setStatus(t *testing.T, db *sql.DB, id int64, status string, updatedAt time.Time) {
	t.Helper()
	if _, err := db.Exec(`UPDATE candidates SET status = ?, updated_at = ? WHERE id = ?`,
		status, updatedAt.UTC().Format(time.RFC3339), id); err != nil {
		t.Fatal(err)
	}
}

func mustGet(t *testing.T, store *newsroom.SQLiteStore, id int64) newsroom.Candidate {
	t.Helper()
	candidate, err := store.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return candidate
}
