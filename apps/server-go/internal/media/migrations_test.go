package media_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

// nodeMediaDDL is apps/server/src/db.ts:58-65 verbatim.
const nodeMediaDDL = `CREATE TABLE IF NOT EXISTS media (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      filename TEXT NOT NULL,
      url TEXT NOT NULL,
      mime_type TEXT NOT NULL,
      size INTEGER NOT NULL,
      uploaded_at TEXT NOT NULL DEFAULT (datetime('now'))
    );`

func normalizedSQL(sql string) string {
	return strings.Join(strings.Fields(strings.TrimSuffix(strings.TrimSpace(sql), ";")), " ")
}

func TestMediaMigrationIsNodesMediaTable(t *testing.T) {
	descriptors := media.Migrations()
	if len(descriptors) != 1 || descriptors[0].Version != 5 || descriptors[0].Name != "media library" {
		t.Fatalf("descriptors = %#v", descriptors)
	}
	if got, want := normalizedSQL(descriptors[0].SQL), normalizedSQL(nodeMediaDDL); got != want {
		t.Fatalf("media DDL = %s\nwant %s", got, want)
	}
	db, _ := testutil.OpenDatabase(t)
	for i := 0; i < 2; i++ {
		if err := migrate.Run(context.Background(), db, media.Migrations()); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO media (filename, url, mime_type, size) VALUES ('a.png', '/uploads/a.png', 'image/png', 1)`); err != nil {
		t.Fatal(err)
	}
	var uploadedAt string
	if err := db.QueryRow(`SELECT uploaded_at FROM media`).Scan(&uploadedAt); err != nil || len(uploadedAt) != len("2006-01-02 15:04:05") {
		t.Fatalf("default uploaded_at = %q, %v", uploadedAt, err)
	}
	for _, statement := range []string{
		`INSERT INTO media (url, mime_type, size) VALUES ('/uploads/b.png', 'image/png', 1)`,
		`INSERT INTO media (filename, mime_type, size) VALUES ('b.png', 'image/png', 1)`,
		`INSERT INTO media (filename, url, size) VALUES ('b.png', '/uploads/b.png', 1)`,
		`INSERT INTO media (filename, url, mime_type) VALUES ('b.png', '/uploads/b.png', 'image/png')`,
	} {
		if _, err := db.Exec(statement); err == nil {
			t.Errorf("NOT NULL not enforced: %s", statement)
		}
	}
}

func TestMediaMigrationSQLKeepsATableNodeAlreadyCreated(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	if _, err := db.Exec(nodeMediaDDL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO media (id, filename, url, mime_type, size, uploaded_at) VALUES (401, 'old.png', '/uploads/old.png', 'image/png', 25, '2026-09-19T00:00:00.000Z')`); err != nil {
		t.Fatal(err)
	}
	// migrate.Run refuses unmanaged (Node-created) databases; the future
	// adoption command will stamp them. The SQL itself must be a no-op there.
	if _, err := db.Exec(media.Migrations()[0].SQL); err != nil {
		t.Fatal(err)
	}
	var filename string
	if err := db.QueryRow(`SELECT filename FROM media WHERE id = 401`).Scan(&filename); err != nil || filename != "old.png" {
		t.Fatalf("existing row = %q, %v", filename, err)
	}
	var next int64
	result, err := db.Exec(`INSERT INTO media (filename, url, mime_type, size) VALUES ('new.png', '/uploads/new.png', 'image/png', 1)`)
	if err == nil {
		next, err = result.LastInsertId()
	}
	if err != nil || next != 402 {
		t.Fatalf("next id = %d, %v", next, err)
	}
}
