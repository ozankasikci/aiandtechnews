package migrate_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

func openDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	return testutil.OpenDatabase(t)
}

func descriptors() []migrate.Descriptor {
	return []migrate.Descriptor{
		{Version: 1, Name: "users", SQL: `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);`},
		{Version: 3, Name: "index", SQL: `CREATE INDEX users_name_idx ON users(name);`},
	}
}

func TestChecksumHashesExactSQLBytes(t *testing.T) {
	sqlText := " SELECT 1;\n"
	want := fmt.Sprintf("%x", sha256.Sum256([]byte(sqlText)))
	descriptor := migrate.Descriptor{SQL: sqlText}
	if got := descriptor.Checksum(); got != want {
		t.Fatalf("Checksum() = %q, want %q", got, want)
	}
	trimmed := migrate.Descriptor{SQL: strings.TrimSpace(sqlText)}
	if descriptor.Checksum() == trimmed.Checksum() {
		t.Fatal("Checksum normalized SQL instead of hashing exact bytes")
	}
}

func TestRunAppliesInOrderRecordsLedgerAndIsIdempotent(t *testing.T) {
	db, _ := openDB(t)
	migrations := descriptors()
	if err := migrate.Run(context.Background(), db, migrations); err != nil {
		t.Fatal(err)
	}
	if err := migrate.Run(context.Background(), db, migrations); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	rows, err := db.Query(`SELECT version, name, checksum, applied_at FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for i, want := range migrations {
		if !rows.Next() {
			t.Fatalf("missing ledger row %d", i)
		}
		var version int64
		var name, checksum, applied string
		if err := rows.Scan(&version, &name, &checksum, &applied); err != nil {
			t.Fatal(err)
		}
		if version != want.Version || name != want.Name || checksum != want.Checksum() {
			t.Errorf("row = (%d,%q,%q), want (%d,%q,%q)", version, name, checksum, want.Version, want.Name, want.Checksum())
		}
		if _, err := time.Parse("2006-01-02T15:04:05.000Z", applied); err != nil {
			t.Errorf("applied_at %q: %v", applied, err)
		}
	}
	if rows.Next() {
		t.Fatal("unexpected extra ledger row")
	}
}

func TestRunRollsBackAllMigrationsOnFailure(t *testing.T) {
	db, _ := openDB(t)
	err := migrate.Run(context.Background(), db, []migrate.Descriptor{
		{Version: 1, Name: "one", SQL: `CREATE TABLE first_table (id INTEGER);`},
		{Version: 2, Name: "broken", SQL: `CREATE TABLE second_table (id INTEGER); INSERT INTO missing_table VALUES (1);`},
	})
	if err == nil {
		t.Fatal("Run() error = nil")
	}
	assertObjectAbsent(t, db, "first_table")
	assertObjectAbsent(t, db, "schema_migrations")
}

func TestRunRejectsAttachBeforeItCanLeakOutsideRollback(t *testing.T) {
	db, _ := openDB(t)
	auxPath := filepath.Join(t.TempDir(), "auxiliary.db")
	descriptor := migrate.Descriptor{
		Version: 1,
		Name:    "attach escape",
		SQL:     fmt.Sprintf("ATTACH DATABASE '%s' AS aux; CREATE TABLE leaked(id INTEGER); INSERT INTO missing_table VALUES (1);", strings.ReplaceAll(auxPath, "'", "''")),
	}

	err := migrate.Run(context.Background(), db, []migrate.Descriptor{descriptor})
	if !errors.Is(err, migrate.ErrInvalidDescriptors) {
		t.Errorf("Run() error = %v, want ErrInvalidDescriptors", err)
	}
	if _, err := os.Stat(auxPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("auxiliary database Stat() error = %v, want not exist", err)
	}
	assertDatabaseNames(t, db, "main")
	assertObjectAbsent(t, db, "schema_migrations")
	assertObjectAbsent(t, db, "leaked")
}

func TestRunRejectsPragmaBeforeItCanLeakOutsideRollback(t *testing.T) {
	db, _ := openDB(t)
	descriptor := migrate.Descriptor{
		Version: 1,
		Name:    "pragma escape",
		SQL:     `PRAGMA ignore_check_constraints=ON; CREATE TABLE leaked(id INTEGER); INSERT INTO missing_table VALUES (1);`,
	}

	err := migrate.Run(context.Background(), db, []migrate.Descriptor{descriptor})
	if !errors.Is(err, migrate.ErrInvalidDescriptors) {
		t.Errorf("Run() error = %v, want ErrInvalidDescriptors", err)
	}
	var ignoreCheckConstraints int
	if err := db.QueryRow(`PRAGMA ignore_check_constraints`).Scan(&ignoreCheckConstraints); err != nil {
		t.Fatalf("read ignore_check_constraints: %v", err)
	}
	if ignoreCheckConstraints != 0 {
		t.Errorf("ignore_check_constraints = %d, want 0", ignoreCheckConstraints)
	}
	assertObjectAbsent(t, db, "schema_migrations")
	assertObjectAbsent(t, db, "leaked")
}

func TestRunAppliesTriggerMigrationAndRecordsLedger(t *testing.T) {
	db, _ := openDB(t)
	descriptor := migrate.Descriptor{
		Version: 1,
		Name:    "audit source inserts",
		SQL: `CREATE TABLE source (id INTEGER PRIMARY KEY, value TEXT NOT NULL);
		CREATE TABLE audit (source_id INTEGER NOT NULL, detail TEXT NOT NULL);
		CREATE TRIGGER audit_source_insert
		AFTER INSERT ON source
		BEGIN
			INSERT INTO audit(source_id, detail) VALUES (NEW.id, 'created; safely');
			INSERT INTO audit(source_id, detail) VALUES (NEW.id, 'value=' || NEW.value);
		END;`,
	}

	if err := migrate.Run(context.Background(), db, []migrate.Descriptor{descriptor}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if _, err := db.Exec(`INSERT INTO source(value) VALUES ('example')`); err != nil {
		t.Fatalf("insert source row: %v", err)
	}

	rows, err := db.Query(`SELECT source_id, detail FROM audit ORDER BY rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	wantDetails := []string{"created; safely", "value=example"}
	for i, wantDetail := range wantDetails {
		if !rows.Next() {
			t.Fatalf("missing audit row %d", i)
		}
		var sourceID int64
		var detail string
		if err := rows.Scan(&sourceID, &detail); err != nil {
			t.Fatal(err)
		}
		if sourceID != 1 || detail != wantDetail {
			t.Errorf("audit row %d = (%d, %q), want (1, %q)", i, sourceID, detail, wantDetail)
		}
	}
	if rows.Next() {
		t.Fatal("unexpected extra audit row")
	}

	var name, checksum string
	if err := db.QueryRow(`SELECT name, checksum FROM schema_migrations WHERE version = 1`).Scan(&name, &checksum); err != nil {
		t.Fatalf("query ledger: %v", err)
	}
	if name != descriptor.Name || checksum != descriptor.Checksum() {
		t.Fatalf("ledger row = (%q, %q), want (%q, %q)", name, checksum, descriptor.Name, descriptor.Checksum())
	}
}

func TestRunRejectsTransactionControlBeforeDatabaseTouch(t *testing.T) {
	descriptor := migrate.Descriptor{
		Version: 7,
		Name:    "atomicity escape",
		SQL:     `CREATE TABLE leaked(id INTEGER); COMMIT; INSERT INTO leaked VALUES (1); INSERT INTO missing_table VALUES (1);`,
	}

	err := migrate.Run(nil, nil, []migrate.Descriptor{descriptor})
	if !errors.Is(err, migrate.ErrInvalidDescriptors) {
		t.Fatalf("Run(nil, nil) error = %v, want descriptor validation before dependencies", err)
	}
	if !strings.Contains(err.Error(), "version 7") || !strings.Contains(err.Error(), `"atomicity escape"`) {
		t.Fatalf("Run(nil, nil) error = %q, want descriptor version and name", err)
	}

	db, _ := openDB(t)
	err = migrate.Run(context.Background(), db, []migrate.Descriptor{descriptor})
	if !errors.Is(err, migrate.ErrInvalidDescriptors) {
		t.Fatalf("Run() error = %v, want ErrInvalidDescriptors", err)
	}
	assertObjectAbsent(t, db, "schema_migrations")
	assertObjectAbsent(t, db, "leaked")
}

func TestRunValidatesDescriptorSQLStatementStarts(t *testing.T) {
	rejected := map[string]string{
		"attach mixed case":       `aTtAcH DATABASE 'aux.db' AS aux`,
		"detach after comments":   "-- prefix\n/* more */ DeTaCh DATABASE aux",
		"pragma after semicolons": `;;; pRaGmA ignore_check_constraints=ON`,
		"begin":                   `BEGIN`,
		"commit mixed case":       `cOmMiT TRANSACTION`,
		"rollback after SQL":      `CREATE TABLE ok(id INTEGER); ROLLBACK`,
		"savepoint comments":      "CREATE TABLE ok(id INTEGER); -- explain\n /* more */ SAVEPOINT x",
		"release semicolons":      `;;; RELEASE x`,
		"top-level end":           `END`,
		"commit after trigger":    `CREATE TRIGGER safe_insert AFTER INSERT ON safe BEGIN SELECT 1; END; COMMIT`,
		"commit in trigger body":  `CREATE TRIGGER unsafe_insert AFTER INSERT ON safe BEGIN SELECT 1; COMMIT; END;`,
		"vacuum":                  ` VACUUM main`,
		"unterminated comment":    `CREATE TABLE ok(id INTEGER); /* never closed`,
		"unterminated string":     `INSERT INTO ok VALUES ('never closed)`,
		"unterminated double":     `CREATE TABLE "never closed (id INTEGER)`,
		"unterminated backtick":   "CREATE TABLE `never closed (id INTEGER)",
		"unterminated bracket":    `CREATE TABLE [never closed (id INTEGER)`,
	}
	for name, sqlText := range rejected {
		t.Run("reject "+name, func(t *testing.T) {
			descriptor := migrate.Descriptor{Version: 42, Name: "scanner case", SQL: sqlText}
			err := migrate.Run(nil, nil, []migrate.Descriptor{descriptor})
			if !errors.Is(err, migrate.ErrInvalidDescriptors) {
				t.Fatalf("Run(nil, nil) error = %v, want ErrInvalidDescriptors", err)
			}
			if !strings.Contains(err.Error(), "version 42") || !strings.Contains(err.Error(), `"scanner case"`) {
				t.Fatalf("Run(nil, nil) error = %q, want descriptor version and name", err)
			}
		})
	}

	accepted := map[string]string{
		"ordinary multi statement":  `CREATE TABLE ordinary(id INTEGER); INSERT INTO ordinary VALUES (1);`,
		"new unsafe words literals": `SELECT 'ATTACH; DETACH; PRAGMA';`,
		"new unsafe words comments": "-- ATTACH; DETACH; PRAGMA\nSELECT 1;",
		"new unsafe identifiers":    `CREATE TABLE pragma_log(attach_value TEXT, detach_reason TEXT);`,
		"keywords in literals":      `SELECT 'COMMIT; ROLLBACK', 'it''s BEGIN'; SELECT 1;`,
		"keywords in comments":      "-- COMMIT;\n/* ROLLBACK; SAVEPOINT */ SELECT 1;",
		"keywords as identifiers":   `CREATE TABLE commit_log(begin_value TEXT, rollback_reason TEXT);`,
		"quoted identifiers":        "CREATE TABLE \"COMMIT\" (`ROLLBACK` TEXT, [BEGIN] TEXT);",
		"escaped quoted content":    "SELECT \"a\"\"; COMMIT\", `b``; ROLLBACK`, [c]]; VACUUM]; SELECT 1;",
		"create trigger":            `CREATE /* prefix comment */ TRIGGER audit_insert AFTER INSERT ON ordinary BEGIN INSERT INTO ordinary VALUES (NEW.id); UPDATE ordinary SET id = id; END;`,
		"create temp trigger":       "CREATE -- prefix comment\n TEMP TRIGGER audit_insert AFTER INSERT ON ordinary BEGIN SELECT 'END; COMMIT'; /* body ; */ SELECT 1; END;",
		"create temporary trigger":  `CREATE TEMPORARY TRIGGER audit_insert AFTER INSERT ON ordinary BEGIN SELECT 1; END;`,
	}
	for name, sqlText := range accepted {
		t.Run("accept "+name, func(t *testing.T) {
			err := migrate.Run(nil, nil, []migrate.Descriptor{{Version: 1, Name: "safe", SQL: sqlText}})
			if errors.Is(err, migrate.ErrInvalidDescriptors) {
				t.Fatalf("Run(nil, nil) error = %v, did not want ErrInvalidDescriptors", err)
			}
			if err == nil || !strings.Contains(err.Error(), "nil context") {
				t.Fatalf("Run(nil, nil) error = %v, want validation to pass before nil context error", err)
			}
		})
	}
}

func TestRunValidatesAllDescriptorsBeforeDatabaseTouch(t *testing.T) {
	cases := map[string][]migrate.Descriptor{
		"negative version":  {{Version: -1, Name: "a", SQL: "SELECT 1"}},
		"zero version":      {{Version: 0, Name: "a", SQL: "SELECT 1"}},
		"blank name":        {{Version: 1, Name: " ", SQL: "SELECT 1"}},
		"blank SQL":         {{Version: 1, Name: "a", SQL: "\n"}},
		"duplicate version": {{Version: 1, Name: "a", SQL: "SELECT 1"}, {Version: 1, Name: "b", SQL: "SELECT 2"}},
		"duplicate name":    {{Version: 1, Name: "a", SQL: "SELECT 1"}, {Version: 2, Name: "a", SQL: "SELECT 2"}},
		"out of order":      {{Version: 2, Name: "a", SQL: "SELECT 1"}, {Version: 1, Name: "b", SQL: "SELECT 2"}},
	}
	for name, migrations := range cases {
		t.Run(name, func(t *testing.T) {
			if err := migrate.Run(nil, nil, migrations); !errors.Is(err, migrate.ErrInvalidDescriptors) {
				t.Fatalf("Run(nil database) error = %v, want descriptor validation first", err)
			}
			db, _ := openDB(t)
			if err := migrate.Run(context.Background(), db, migrations); !errors.Is(err, migrate.ErrInvalidDescriptors) {
				t.Fatalf("Run() error = %v, want ErrInvalidDescriptors", err)
			}
			assertObjectAbsent(t, db, "schema_migrations")
		})
	}
}

func TestRunRejectsNilDependenciesForValidDescriptors(t *testing.T) {
	valid := []migrate.Descriptor{{Version: 1, Name: "one", SQL: "SELECT 1"}}
	db, _ := openDB(t)

	if err := migrate.Run(nil, db, valid); err == nil || !strings.Contains(err.Error(), "nil context") {
		t.Fatalf("Run(nil context) error = %v, want nil context error", err)
	}
	if err := migrate.Run(context.Background(), nil, valid); err == nil || !strings.Contains(err.Error(), "nil database") {
		t.Fatalf("Run(nil database) error = %v, want nil database error", err)
	}
	assertObjectAbsent(t, db, "schema_migrations")
}

func TestRunDetectsDriftUnknownAndHistoryGap(t *testing.T) {
	t.Run("name drift", func(t *testing.T) {
		db, _ := openDB(t)
		m := descriptors()
		if err := migrate.Run(context.Background(), db, m); err != nil {
			t.Fatal(err)
		}
		m[0].Name = "renamed"
		if err := migrate.Run(context.Background(), db, m); !errors.Is(err, migrate.ErrMigrationDrift) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("SQL drift", func(t *testing.T) {
		db, _ := openDB(t)
		m := descriptors()
		if err := migrate.Run(context.Background(), db, m); err != nil {
			t.Fatal(err)
		}
		m[0].SQL += "\n"
		if err := migrate.Run(context.Background(), db, m); !errors.Is(err, migrate.ErrMigrationDrift) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("unknown applied version", func(t *testing.T) {
		db, _ := openDB(t)
		m := descriptors()
		if err := migrate.Run(context.Background(), db, m); err != nil {
			t.Fatal(err)
		}
		if err := migrate.Run(context.Background(), db, m[:1]); !errors.Is(err, migrate.ErrUnknownMigration) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("history gap", func(t *testing.T) {
		db, _ := openDB(t)
		if err := migrate.Run(context.Background(), db, []migrate.Descriptor{{Version: 3, Name: "three", SQL: `CREATE TABLE three(id INTEGER);`}}); err != nil {
			t.Fatal(err)
		}
		all := []migrate.Descriptor{{Version: 1, Name: "one", SQL: `CREATE TABLE one(id INTEGER);`}, {Version: 3, Name: "three", SQL: `CREATE TABLE three(id INTEGER);`}}
		if err := migrate.Run(context.Background(), db, all); !errors.Is(err, migrate.ErrHistoryGap) {
			t.Fatalf("error = %v", err)
		}
		assertObjectAbsent(t, db, "one")
	})
}

func TestRunRejectsUnmanagedDatabaseWithoutCreatingLedger(t *testing.T) {
	db, _ := openDB(t)
	if _, err := db.Exec(`CREATE TABLE legacy(id INTEGER); CREATE VIEW legacy_view AS SELECT * FROM legacy;`); err != nil {
		t.Fatal(err)
	}
	if err := migrate.Run(context.Background(), db, descriptors()); !errors.Is(err, migrate.ErrUnmanagedDatabase) {
		t.Fatalf("error = %v", err)
	}
	assertObjectAbsent(t, db, "schema_migrations")
}

func TestRunRejectsIncompatibleLedgerSchemas(t *testing.T) {
	variants := map[string]string{
		"view instead of table":   `CREATE VIEW schema_migrations AS SELECT 1 AS version;`,
		"missing column":          `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, checksum TEXT NOT NULL);`,
		"wrong version type":      `CREATE TABLE schema_migrations(version TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, checksum TEXT NOT NULL, applied_at TEXT NOT NULL);`,
		"version not primary key": `CREATE TABLE schema_migrations(version INTEGER NOT NULL, name TEXT NOT NULL UNIQUE, checksum TEXT NOT NULL, applied_at TEXT NOT NULL);`,
		"nullable checksum":       `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, checksum TEXT, applied_at TEXT NOT NULL);`,
		"name not unique":         `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, name TEXT NOT NULL, checksum TEXT NOT NULL, applied_at TEXT NOT NULL);`,
		"partial name uniqueness": `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, name TEXT NOT NULL, checksum TEXT NOT NULL, applied_at TEXT NOT NULL); CREATE UNIQUE INDEX partial_name ON schema_migrations(name) WHERE version > 0;`,
	}
	for name, ddl := range variants {
		t.Run(name, func(t *testing.T) {
			db, _ := openDB(t)
			if _, err := db.Exec(ddl); err != nil {
				t.Fatal(err)
			}
			if err := migrate.Run(context.Background(), db, descriptors()); !errors.Is(err, migrate.ErrIncompatibleLedger) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestRunRejectsMalformedAppliedAtBeforePendingMigrations(t *testing.T) {
	db, _ := openDB(t)
	applied := migrate.Descriptor{Version: 1, Name: "applied", SQL: `CREATE TABLE applied(id INTEGER);`}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		checksum TEXT NOT NULL,
		applied_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations(version, name, checksum, applied_at) VALUES (?, ?, ?, ?)`,
		applied.Version, applied.Name, applied.Checksum(), "not-a-timestamp"); err != nil {
		t.Fatal(err)
	}
	pending := migrate.Descriptor{Version: 2, Name: "pending", SQL: `CREATE TABLE pending(id INTEGER);`}

	err := migrate.Run(context.Background(), db, []migrate.Descriptor{applied, pending})
	if !errors.Is(err, migrate.ErrIncompatibleLedger) {
		t.Fatalf("Run() error = %v, want ErrIncompatibleLedger", err)
	}
	var parseError *time.ParseError
	if !errors.As(err, &parseError) {
		t.Fatalf("Run() error = %v, want underlying time.ParseError", err)
	}
	assertObjectAbsent(t, db, "pending")
	var rows int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("ledger rows = %d, want 1", rows)
	}
}

func TestConcurrentRunnersApplyExactlyOnce(t *testing.T) {
	_, path := openDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db1, err := database.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db1.Close()
	db2, err := database.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	m := []migrate.Descriptor{{Version: 1, Name: "once", SQL: `CREATE TABLE once(id INTEGER); INSERT INTO once VALUES(1);`}}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, db := range []*sql.DB{db1, db2} {
		wg.Add(1)
		go func(db *sql.DB) { defer wg.Done(); <-start; errs <- migrate.Run(ctx, db, m) }(db)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("Run() error = %v", err)
		}
	}
	var count int
	if err := db1.QueryRow(`SELECT count(*) FROM once`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}

func TestCancellationLeavesNoPartialState(t *testing.T) {
	db, _ := openDB(t)
	base := []migrate.Descriptor{{Version: 1, Name: "base", SQL: `CREATE TABLE base(id INTEGER); INSERT INTO base VALUES (1);`}}
	if err := migrate.Run(context.Background(), db, base); err != nil {
		t.Fatalf("apply base migration: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := migrate.Run(ctx, db, append(base, migrate.Descriptor{Version: 2, Name: "canceled", SQL: `
		CREATE TABLE canceled(id INTEGER);
		WITH RECURSIVE counter(value) AS (
			SELECT 1 UNION ALL SELECT value + 1 FROM counter WHERE value < 1000000000
		) INSERT INTO canceled SELECT value FROM counter;`}))
	if err == nil {
		t.Fatal("Run() error = nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want context.DeadlineExceeded identity", err)
	}
	assertObjectAbsent(t, db, "canceled")
	var ledgerRows, baseRows int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&ledgerRows); err != nil {
		t.Fatalf("query ledger after cancellation: %v", err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM base`).Scan(&baseRows); err != nil {
		t.Fatalf("query base after cancellation: %v", err)
	}
	if ledgerRows != 1 || baseRows != 1 {
		t.Fatalf("state after cancellation = ledger rows %d, base rows %d; want 1, 1", ledgerRows, baseRows)
	}
}

func assertDatabaseNames(t *testing.T, db *sql.DB, want ...string) {
	t.Helper()
	rows, err := db.Query(`PRAGMA database_list`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var sequence int
		var name, file string
		if err := rows.Scan(&sequence, &name, &file); err != nil {
			t.Fatal(err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("database names = %v, want %v", got, want)
	}
}

func assertObjectAbsent(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name = ?`, name).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("object %q exists", name)
	}
}
