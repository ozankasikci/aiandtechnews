package migrate_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
)

func TestStatusReportsPendingMigrationsWithoutWriting(t *testing.T) {
	db, _ := openDB(t)
	all := descriptors()
	if err := migrate.Run(context.Background(), db, all[:1]); err != nil {
		t.Fatal(err)
	}
	pending, err := migrate.Status(context.Background(), db, all)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if len(pending) != 1 || pending[0].Version != 3 {
		t.Fatalf("pending = %+v, want version 3", pending)
	}
	var indexes int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name = 'users_name_idx'`).Scan(&indexes); err != nil || indexes != 0 {
		t.Fatalf("Status applied a migration: %d, %v", indexes, err)
	}

	if err := migrate.Run(context.Background(), db, all); err != nil {
		t.Fatal(err)
	}
	pending, err = migrate.Status(context.Background(), db, all)
	if err != nil || len(pending) != 0 {
		t.Fatalf("Status() after Run = %+v, %v", pending, err)
	}
}

func TestStatusRefusesUnmanagedDriftedAndUnknownLedgers(t *testing.T) {
	ctx := context.Background()

	empty, _ := openDB(t)
	if _, err := migrate.Status(ctx, empty, descriptors()); !errors.Is(err, migrate.ErrUnmanagedDatabase) {
		t.Fatalf("empty database: %v, want %v", err, migrate.ErrUnmanagedDatabase)
	}

	unmanaged, _ := openDB(t)
	if _, err := unmanaged.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Status(ctx, unmanaged, descriptors()); !errors.Is(err, migrate.ErrUnmanagedDatabase) {
		t.Fatalf("unmanaged database: %v, want %v", err, migrate.ErrUnmanagedDatabase)
	}

	drifted, _ := openDB(t)
	if err := migrate.Run(ctx, drifted, descriptors()); err != nil {
		t.Fatal(err)
	}
	changed := descriptors()
	changed[0].SQL += " "
	if _, err := migrate.Status(ctx, drifted, changed); !errors.Is(err, migrate.ErrMigrationDrift) {
		t.Fatalf("drifted ledger: %v, want %v", err, migrate.ErrMigrationDrift)
	}
	if _, err := migrate.Status(ctx, drifted, descriptors()[:1]); !errors.Is(err, migrate.ErrUnknownMigration) {
		t.Fatalf("unknown applied version: %v, want %v", err, migrate.ErrUnknownMigration)
	}
}
