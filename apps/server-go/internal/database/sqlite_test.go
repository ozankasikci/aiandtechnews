package database_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
)

func TestOpenConfiguresSQLiteAndEscapesFilePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested dir", "news #1 ? 日本.db")
	db, err := database.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if got, want := db.Stats().MaxOpenConnections, 1; got != want {
		t.Errorf("MaxOpenConnections = %d, want %d", got, want)
	}
	for pragma, want := range map[string]string{
		"journal_mode": "wal",
		"foreign_keys": "1",
		"busy_timeout": "5000",
		"synchronous":  "1",
	} {
		var got string
		if err := db.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil {
			t.Fatalf("PRAGMA %s: %v", pragma, err)
		}
		if got != want {
			t.Errorf("PRAGMA %s = %q, want %q", pragma, got, want)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database file was not created at exact path: %v", err)
	}
	if info, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("parent directory: %v", err)
	} else if info.Mode().Perm() != 0o750 {
		t.Errorf("parent mode = %#o, want %#o", info.Mode().Perm(), 0o750)
	}

	if _, err := db.Exec(`CREATE TABLE parent (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE child (parent_id INTEGER REFERENCES parent(id))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO child(parent_id) VALUES (999)`); err == nil {
		t.Fatal("foreign key violating insert succeeded")
	}
}

func TestOpenRejectsInvalidInputWithoutCreatingDatabase(t *testing.T) {
	t.Run("nil context", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "nil.db")
		if db, err := database.Open(nil, path); err == nil || db != nil {
			t.Fatalf("Open(nil) = (%v, %v), want (nil, error)", db, err)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Stat() error = %v, want not exist", err)
		}
	})
	t.Run("blank path", func(t *testing.T) {
		if db, err := database.Open(context.Background(), " \t"); err == nil || db != nil {
			t.Fatalf("Open(blank) = (%v, %v), want (nil, error)", db, err)
		}
	})
	t.Run("canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if db, err := database.Open(ctx, filepath.Join(t.TempDir(), "canceled.db")); err == nil || db != nil {
			t.Fatalf("Open(canceled) = (%v, %v), want (nil, error)", db, err)
		}
	})
	t.Run("directory is unusable as database", func(t *testing.T) {
		path := t.TempDir()
		if db, err := database.Open(context.Background(), path); err == nil || db != nil {
			t.Fatalf("Open(directory) = (%v, %v), want (nil, error)", db, err)
		}
	})
}

func TestOpenUsesAFileBackedDatabase(t *testing.T) {
	for _, path := range []string{":memory:", "file::memory:", "file:test.db?mode=memory"} {
		t.Run(path, func(t *testing.T) {
			db, err := database.Open(context.Background(), path)
			if err == nil {
				_ = db.Close()
				t.Fatalf("Open(%q) succeeded, want file-backed-only rejection", path)
			}
		})
	}
}
