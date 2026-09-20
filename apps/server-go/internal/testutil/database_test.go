package testutil_test

import (
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

func TestOpenDatabaseReturnsUsableIsolatedDatabase(t *testing.T) {
	db, path := testutil.OpenDatabase(t)
	if db == nil || path == "" {
		t.Fatalf("OpenDatabase() = (%v, %q)", db, path)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}
