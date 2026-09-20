// Package testutil provides isolated test infrastructure.
package testutil

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
)

// OpenDatabase opens a fresh file-backed SQLite database under t.TempDir.
func OpenDatabase(t testing.TB) (*sql.DB, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := database.Open(ctx, path)
	if err != nil {
		t.Fatalf("open temporary database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close temporary database: %v", err)
		}
	})
	return db, path
}
