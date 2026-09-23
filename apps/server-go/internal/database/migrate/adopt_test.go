package migrate_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
)

// adoptionDescriptors mimics the production shape: versions 1 and 3 already
// exist in an unmanaged database, version 2 does not.
func adoptionDescriptors() []migrate.Descriptor {
	return []migrate.Descriptor{
		{Version: 1, Name: "users", SQL: `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);`},
		{Version: 2, Name: "tags", SQL: `CREATE TABLE tags (id INTEGER PRIMARY KEY);`},
		{Version: 3, Name: "notes", SQL: `CREATE TABLE IF NOT EXISTS notes (id INTEGER PRIMARY KEY, body TEXT);`},
	}
}

func unmanagedDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db, _ := openDB(t)
	for _, statement := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`,
		`CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT)`,
		`INSERT INTO users (id, name) VALUES (7, 'kept')`,
		`INSERT INTO notes (id, body) VALUES (9, 'kept')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func present(versions ...int64) func(context.Context, migrate.Queryer) ([]int64, error) {
	return func(context.Context, migrate.Queryer) ([]int64, error) { return versions, nil }
}

func TestAdoptRecordsPresentVersionsAndAppliesTheRestAtomically(t *testing.T) {
	db := unmanagedDatabase(t)
	result, err := migrate.Adopt(context.Background(), db, adoptionDescriptors(), present(1, 3))
	if err != nil {
		t.Fatalf("Adopt() error = %v", err)
	}
	if len(result.Recorded) != 2 || result.Recorded[0] != 1 || result.Recorded[1] != 3 {
		t.Errorf("Recorded = %v, want [1 3]", result.Recorded)
	}
	if len(result.Applied) != 1 || result.Applied[0] != 2 {
		t.Errorf("Applied = %v, want [2]", result.Applied)
	}

	rows, err := db.Query(`SELECT version, name, checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	descriptors := adoptionDescriptors()
	index := 0
	for rows.Next() {
		var version int64
		var name, checksum string
		if err := rows.Scan(&version, &name, &checksum); err != nil {
			t.Fatal(err)
		}
		want := descriptors[index]
		if version != want.Version || name != want.Name || checksum != want.Checksum() {
			t.Errorf("ledger row %d = (%d, %q, %q), want (%d, %q, %q)", index, version, name, checksum, want.Version, want.Name, want.Checksum())
		}
		index++
	}
	if index != 3 {
		t.Fatalf("ledger rows = %d, want 3", index)
	}
	var kept string
	if err := db.QueryRow(`SELECT name FROM users WHERE id = 7`).Scan(&kept); err != nil || kept != "kept" {
		t.Fatalf("existing row = %q, %v", kept, err)
	}
	if err := migrate.Run(context.Background(), db, adoptionDescriptors()); err != nil {
		t.Fatalf("Run() after Adopt error = %v", err)
	}
}

func TestAdoptRollsBackEverythingWhenAPendingMigrationFails(t *testing.T) {
	db := unmanagedDatabase(t)
	descriptors := adoptionDescriptors()
	descriptors[1].SQL = `CREATE TABLE users (id INTEGER PRIMARY KEY);`
	if _, err := migrate.Adopt(context.Background(), db, descriptors, present(1, 3)); err == nil {
		t.Fatal("Adopt() error = nil, want failure from the conflicting pending migration")
	}
	var ledgers int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name = 'schema_migrations'`).Scan(&ledgers); err != nil {
		t.Fatal(err)
	}
	if ledgers != 0 {
		t.Fatal("failed adoption left a migration ledger behind")
	}
}

func TestAdoptRollsBackWhenVerificationFails(t *testing.T) {
	db := unmanagedDatabase(t)
	boom := errors.New("schema mismatch")
	_, err := migrate.Adopt(context.Background(), db, adoptionDescriptors(), func(context.Context, migrate.Queryer) ([]int64, error) {
		return nil, boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Adopt() error = %v, want %v", err, boom)
	}
	var ledgers int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name = 'schema_migrations'`).Scan(&ledgers); err != nil || ledgers != 0 {
		t.Fatalf("ledger count = %d, %v", ledgers, err)
	}
}

func TestAdoptVerifiesInsideTheWriteTransaction(t *testing.T) {
	db := unmanagedDatabase(t)
	_, err := migrate.Adopt(context.Background(), db, adoptionDescriptors(), func(ctx context.Context, q migrate.Queryer) ([]int64, error) {
		var count int
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
			return nil, err
		}
		if count != 1 {
			t.Errorf("verify saw %d users, want 1", count)
		}
		return []int64{1, 3}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAdoptRefusesManagedAndEmptyDatabases(t *testing.T) {
	managed, _ := openDB(t)
	if err := migrate.Run(context.Background(), managed, adoptionDescriptors()); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Adopt(context.Background(), managed, adoptionDescriptors(), present()); !errors.Is(err, migrate.ErrAlreadyManaged) {
		t.Fatalf("Adopt(managed) error = %v, want %v", err, migrate.ErrAlreadyManaged)
	}

	empty, _ := openDB(t)
	if _, err := migrate.Adopt(context.Background(), empty, adoptionDescriptors(), present()); !errors.Is(err, migrate.ErrNothingToAdopt) {
		t.Fatalf("Adopt(empty) error = %v, want %v", err, migrate.ErrNothingToAdopt)
	}
}

func TestAdoptRejectsUnknownOrDuplicatePresentVersions(t *testing.T) {
	for name, versions := range map[string][]int64{"unknown": {1, 4}, "duplicate": {1, 1}} {
		t.Run(name, func(t *testing.T) {
			db := unmanagedDatabase(t)
			if _, err := migrate.Adopt(context.Background(), db, adoptionDescriptors(), present(versions...)); err == nil {
				t.Fatal("Adopt() error = nil")
			}
		})
	}
}

func TestAdoptRejectsInvalidDescriptorsAndNilVerify(t *testing.T) {
	db := unmanagedDatabase(t)
	bad := []migrate.Descriptor{{Version: 1, Name: "bad", SQL: `BEGIN; CREATE TABLE x (id INTEGER);`}}
	if _, err := migrate.Adopt(context.Background(), db, bad, present()); !errors.Is(err, migrate.ErrInvalidDescriptors) {
		t.Fatalf("Adopt(bad descriptors) error = %v", err)
	}
	if _, err := migrate.Adopt(context.Background(), db, adoptionDescriptors(), nil); err == nil {
		t.Fatal("Adopt(nil verify) error = nil")
	}
}
