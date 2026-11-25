package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

func TestOpenDatabaseCopyIsReadOnlyWithoutSidecarWrites(t *testing.T) {
	db, path := testutil.OpenDatabase(t)
	if _, err := db.Exec(`CREATE TABLE fixture (value INTEGER); INSERT INTO fixture VALUES (17)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	reader, err := openDatabaseCopy(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var value int
	if err := reader.QueryRowContext(ctx, `SELECT value FROM fixture`).Scan(&value); err != nil || value != 17 {
		t.Fatalf("read value = %d, err = %v", value, err)
	}
	if _, err := reader.ExecContext(ctx, `UPDATE fixture SET value = 18`); err == nil {
		t.Fatal("database copy accepted a write")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("database directory entries = %v", entries)
	}
}
