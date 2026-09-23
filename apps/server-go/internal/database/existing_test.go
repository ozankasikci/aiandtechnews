package database_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
)

// rollbackJournalDatabase creates a database in rollback-journal (DELETE)
// mode with one row, using the driver directly so database.Open's WAL
// setting does not apply.
func rollbackJournalDatabase(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "existing db.db")
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{`PRAGMA journal_mode = DELETE`, `CREATE TABLE t (id INTEGER PRIMARY KEY)`, `INSERT INTO t VALUES (1)`} {
		if _, err := raw.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOpenExistingRefusesMissingPathsWithoutCreatingThem(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	path := filepath.Join(dir, "technews.db")
	for _, readOnly := range []bool{true, false} {
		if _, err := database.OpenExisting(context.Background(), path, readOnly); err == nil {
			t.Fatalf("OpenExisting(readOnly=%t) error = nil for a missing file", readOnly)
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("OpenExisting created %s: %v", dir, err)
	}
	if _, err := database.OpenExisting(context.Background(), t.TempDir(), true); err == nil {
		t.Fatal("OpenExisting(directory) error = nil")
	}
}

func TestOpenExistingReadOnlyRejectsWrites(t *testing.T) {
	path := rollbackJournalDatabase(t)
	db, err := database.OpenExisting(context.Background(), path, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM t`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("count = %d, %v", count, err)
	}
	if _, err := db.Exec(`INSERT INTO t VALUES (2)`); err == nil {
		t.Fatal("read-only connection accepted a write")
	}
}

func TestOpenExistingKeepsJournalModeAndEnforcesForeignKeys(t *testing.T) {
	path := rollbackJournalDatabase(t)
	db, err := database.OpenExisting(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for pragma, want := range map[string]string{"journal_mode": "delete", "foreign_keys": "1", "busy_timeout": "5000"} {
		var got string
		if err := db.QueryRow(`PRAGMA ` + pragma).Scan(&got); err != nil || got != want {
			t.Errorf("PRAGMA %s = %q, %v; want %q", pragma, got, err, want)
		}
	}
	if db.Stats().MaxOpenConnections != 1 {
		t.Errorf("MaxOpenConnections = %d, want 1", db.Stats().MaxOpenConnections)
	}
}
