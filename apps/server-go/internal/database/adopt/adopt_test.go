package adopt_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/adopt"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
)

func run(t *testing.T, path string, apply bool) (adopt.Result, string, error) {
	t.Helper()
	var out bytes.Buffer
	result, err := adopt.Run(context.Background(), adopt.Options{
		DatabasePath: path,
		Apply:        apply,
		Descriptors:  app.Migrations(),
		Now:          fixedNow,
		Out:          &out,
	})
	return result, out.String(), err
}

func TestDryRunReportsAFreshNodeDatabaseAdoptableWithoutChangingIt(t *testing.T) {
	path := nodeDatabase(t, fixture{variant: "fresh"})
	before := fileHash(t, path)

	result, report, err := run(t, path, false)
	t.Log("\n" + report)
	if err != nil {
		t.Fatalf("Run() error = %v\n%s", err, report)
	}
	if result.Outcome != adopt.OutcomeAdoptable {
		t.Fatalf("Outcome = %v, want adoptable\n%s", result.Outcome, report)
	}
	if fileHash(t, path) != before {
		t.Fatal("dry run changed the database file")
	}
	if hasLedger(t, path) || len(backups(t, path)) != 0 {
		t.Fatal("dry run wrote a ledger or a backup")
	}
	for _, want := range []string{
		"1 editorial authors: present",
		"2 content categories and articles: present",
		"3 newsroom candidates: pending",
		"4 newsroom published index: pending",
		"5 media library: present",
		"6 newsletter: present",
		"Rehearsal on a copy: passed",
		"settings: 2 -> 4 rows",
		"Result: COMPATIBLE",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report is missing %q:\n%s", want, report)
		}
	}
}

func TestApplyAdoptsAFreshNodeDatabaseWithoutChangingItsData(t *testing.T) {
	path := nodeDatabase(t, fixture{variant: "fresh"})
	before := dumpTables(t, path, nodeTables...)

	result, report, err := run(t, path, true)
	if err != nil {
		t.Fatalf("Run() error = %v\n%s", err, report)
	}
	if result.Outcome != adopt.OutcomeAdopted {
		t.Fatalf("Outcome = %v, want adopted\n%s", result.Outcome, report)
	}
	if got, want := result.Adoption.Recorded, []int64{1, 2, 5, 6}; !equalVersions(got, want) {
		t.Errorf("Recorded = %v, want %v", got, want)
	}
	if got, want := result.Adoption.Applied, []int64{3, 4}; !equalVersions(got, want) {
		t.Errorf("Applied = %v, want %v", got, want)
	}

	after := dumpTables(t, path, nodeTables...)
	for _, table := range nodeTables {
		if table == "settings" {
			continue
		}
		if before[table] != after[table] {
			t.Errorf("table %s changed:\nbefore:\n%s\nafter:\n%s", table, before[table], after[table])
		}
	}
	// Migration 3 adds its two newsroom settings and nothing else.
	wantSettings := before["settings"] +
		"key=\"newsroom.publish_delay_max_minutes\" value=\"40\" \n" +
		"key=\"newsroom.publish_delay_min_minutes\" value=\"30\" \n"
	if after["settings"] != sortedLines(wantSettings) {
		t.Errorf("settings after adoption:\n%s\nwant:\n%s", after["settings"], sortedLines(wantSettings))
	}

	// The backup is the untouched original.
	if result.BackupPath != path+".pre-adopt-20260924T073015Z.db" {
		t.Errorf("BackupPath = %q", result.BackupPath)
	}
	if hasLedger(t, result.BackupPath) {
		t.Error("backup has a migration ledger")
	}
	backup := dumpTables(t, result.BackupPath, nodeTables...)
	for _, table := range nodeTables {
		if backup[table] != before[table] {
			t.Errorf("backup table %s differs from the original", table)
		}
	}

	assertManagedLikeAFreshGoDatabase(t, path)
}

func TestSecondRunOnAnAdoptedDatabaseIsANoOp(t *testing.T) {
	path := nodeDatabase(t, fixture{variant: "fresh"})
	if result, report, err := run(t, path, true); err != nil || result.Outcome != adopt.OutcomeAdopted {
		t.Fatalf("first run: %v %v\n%s", result.Outcome, err, report)
	}
	hash := fileHash(t, path)
	for _, apply := range []bool{false, true} {
		result, report, err := run(t, path, apply)
		if err != nil {
			t.Fatalf("apply=%t: Run() error = %v", apply, err)
		}
		if result.Outcome != adopt.OutcomeAlreadyManaged || !strings.Contains(report, "already managed") {
			t.Fatalf("apply=%t: Outcome = %v\n%s", apply, result.Outcome, report)
		}
		if fileHash(t, path) != hash {
			t.Fatalf("apply=%t: second run changed the database", apply)
		}
	}
	if got := backups(t, path); len(got) != 1 {
		t.Fatalf("backups = %v, want exactly the first run's", got)
	}
}

func TestLegacyAlterEvolvedDatabaseIsAdoptable(t *testing.T) {
	path := nodeDatabase(t, fixture{variant: "legacy"})
	before := dumpTables(t, path, "articles", "subscribers")

	result, report, err := run(t, path, true)
	t.Log("\n" + report)
	if err != nil || result.Outcome != adopt.OutcomeAdopted {
		t.Fatalf("Outcome = %v, err = %v\n%s", result.Outcome, err, report)
	}
	for _, want := range []string{"subscribers.created_at", "subscribers.updated_at", "Known Node variants accepted"} {
		if !strings.Contains(report, want) {
			t.Errorf("report is missing %q:\n%s", want, report)
		}
	}
	after := dumpTables(t, path, "articles", "subscribers")
	if before["articles"] != after["articles"] || before["subscribers"] != after["subscribers"] {
		t.Error("legacy data changed during adoption")
	}
	assertManagedLikeAFreshGoDatabase(t, path)
}

func TestMissingColumnFailsWithAPreciseReportAndNoChanges(t *testing.T) {
	path := nodeDatabase(t, fixture{
		variant: "fresh",
		replace: map[string][2]string{"articles": {"meta_title TEXT,\n", ""}},
	})
	hash := fileHash(t, path)
	for _, apply := range []bool{false, true} {
		result, report, err := run(t, path, apply)
		if err != nil {
			t.Fatalf("apply=%t: Run() error = %v", apply, err)
		}
		if result.Outcome != adopt.OutcomeIncompatible {
			t.Fatalf("apply=%t: Outcome = %v\n%s", apply, result.Outcome, report)
		}
		if !strings.Contains(report, "table articles: missing column meta_title TEXT") || !strings.Contains(report, "Result: NOT COMPATIBLE") {
			t.Errorf("apply=%t: report:\n%s", apply, report)
		}
		if fileHash(t, path) != hash || hasLedger(t, path) || len(backups(t, path)) != 0 {
			t.Fatalf("apply=%t: an incompatible database was changed", apply)
		}
	}
}

func TestUnknownExtraTableIsReportedAndTolerated(t *testing.T) {
	path := nodeDatabase(t, fixture{
		variant: "fresh",
		extra: []string{
			`CREATE TABLE page_views (id INTEGER PRIMARY KEY, path TEXT NOT NULL)`,
			`INSERT INTO page_views (id, path) VALUES (1, '/')`,
			`CREATE VIEW published AS SELECT id FROM articles WHERE status = 'published'`,
			`ALTER TABLE articles ADD COLUMN node_only_note TEXT`,
			`CREATE INDEX idx_articles_title ON articles(title)`,
		},
	})
	before := dumpTables(t, path, "page_views")
	result, report, err := run(t, path, true)
	t.Log("\n" + report)
	if err != nil || result.Outcome != adopt.OutcomeAdopted {
		t.Fatalf("Outcome = %v, err = %v\n%s", result.Outcome, err, report)
	}
	for _, want := range []string{"table page_views", "view published", "column articles.node_only_note", "index idx_articles_title"} {
		if !strings.Contains(report, want) {
			t.Errorf("report does not list %q:\n%s", want, report)
		}
	}
	if after := dumpTables(t, path, "page_views"); after["page_views"] != before["page_views"] {
		t.Error("extra table data changed")
	}
}

func TestIncompatibleSchemasAreRejected(t *testing.T) {
	for name, tc := range map[string]struct {
		f    fixture
		want string
	}{
		"changed check": {
			f:    fixture{variant: "fresh", replace: map[string][2]string{"articles": {"'draft', 'published', 'scheduled'", "'draft', 'published'"}}},
			want: "table articles: CHECK constraints differ",
		},
		"changed default": {
			f:    fixture{variant: "fresh", replace: map[string][2]string{"categories": {"DEFAULT '#6366f1'", "DEFAULT '#000000'"}}},
			want: "table categories: column color: default '#000000', want '#6366f1'",
		},
		"changed type": {
			f:    fixture{variant: "fresh", replace: map[string][2]string{"media": {"size INTEGER NOT NULL", "size TEXT NOT NULL"}}},
			want: "table media: column size: type TEXT, want INTEGER",
		},
		"dropped not null": {
			f:    fixture{variant: "fresh", replace: map[string][2]string{"authors": {"name TEXT NOT NULL", "name TEXT"}}},
			want: "table authors: column name: nullable, want NOT NULL",
		},
		"extra not null column without default": {
			f:    fixture{variant: "fresh", replace: map[string][2]string{"media": {"filename TEXT NOT NULL,", "filename TEXT NOT NULL,\n      checksum TEXT NOT NULL,"}}, noSeed: true},
			want: "table media: extra column checksum is NOT NULL without a default",
		},
		"extra unique index": {
			f:    fixture{variant: "fresh", extra: []string{`CREATE UNIQUE INDEX idx_articles_title ON articles(title)`}},
			want: "extra UNIQUE index idx_articles_title",
		},
		"trigger": {
			f:    fixture{variant: "fresh", extra: []string{`CREATE TRIGGER touch_articles AFTER UPDATE ON articles BEGIN SELECT 1; END`}},
			want: "trigger touch_articles on articles",
		},
		"changed foreign key": {
			f:    fixture{variant: "fresh", replace: map[string][2]string{"newsletter_deliveries": {" ON DELETE CASCADE", ""}}},
			want: "table newsletter_deliveries: foreign keys differ",
		},
		"missing unique constraint": {
			f:    fixture{variant: "fresh", replace: map[string][2]string{"authors": {"email TEXT NOT NULL UNIQUE", "email TEXT NOT NULL"}}},
			want: "table authors: missing UNIQUE constraint (email)",
		},
		"unique on conflict replace": {
			f:    fixture{variant: "fresh", replace: map[string][2]string{"authors": {"email TEXT NOT NULL UNIQUE", "email TEXT NOT NULL UNIQUE ON CONFLICT REPLACE"}}},
			want: "table authors: column email definition differs",
		},
		"not null on conflict ignore": {
			f:    fixture{variant: "fresh", replace: map[string][2]string{"authors": {"name TEXT NOT NULL", "name TEXT NOT NULL ON CONFLICT IGNORE"}}},
			want: "table authors: column name definition differs",
		},
		"collate nocase": {
			f:    fixture{variant: "fresh", replace: map[string][2]string{"authors": {"name TEXT NOT NULL", "name TEXT NOT NULL COLLATE NOCASE"}}},
			want: "table authors: column name definition differs",
		},
		"generated column": {
			f:    fixture{variant: "fresh", replace: map[string][2]string{"media": {"size INTEGER NOT NULL", "size INTEGER GENERATED ALWAYS AS (length(url)) STORED NOT NULL"}}, noSeed: true},
			want: "table media: column size definition differs",
		},
		"deferrable foreign key": {
			f:    fixture{variant: "fresh", replace: map[string][2]string{"newsletter_deliveries": {"ON DELETE CASCADE", "ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED"}}},
			want: "table newsletter_deliveries: table constraints differ",
		},
		"table unique on conflict": {
			f:    fixture{variant: "fresh", replace: map[string][2]string{"newsletter_deliveries": {"UNIQUE(subscriber_id, edition_key)", "UNIQUE(subscriber_id, edition_key) ON CONFLICT REPLACE"}}},
			want: "table newsletter_deliveries: table constraints differ",
		},
		"check moved to a table constraint": {
			f: fixture{variant: "fresh", replace: map[string][2]string{"authors": {
				"role TEXT NOT NULL DEFAULT 'editor' CHECK(role IN ('admin', 'editor'))",
				"role TEXT NOT NULL DEFAULT 'editor', CHECK(role IN ('admin', 'editor'))",
			}}},
			want: "table authors: column role definition differs",
		},
		"index collation": {
			f:    fixture{variant: "fresh", replace: map[string][2]string{"idx_subscribers_status": {"subscribers(status)", "subscribers(status COLLATE NOCASE)"}}},
			want: "index idx_subscribers_status on subscribers",
		},
		"partial index": {
			f:    fixture{variant: "fresh", replace: map[string][2]string{"idx_articles_source_url": {"articles(source_url)", "articles(source_url) WHERE source_url IS NOT NULL"}}},
			want: "index idx_articles_source_url on articles: definition differs",
		},
		"missing autoincrement": {
			f:    fixture{variant: "fresh", replace: map[string][2]string{"media": {"id INTEGER PRIMARY KEY AUTOINCREMENT", "id INTEGER PRIMARY KEY"}}},
			want: "table media: AUTOINCREMENT is off, want on",
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := nodeDatabase(t, tc.f)
			result, report, err := run(t, path, true)
			if err != nil {
				t.Fatalf("Run() error = %v\n%s", err, report)
			}
			if result.Outcome != adopt.OutcomeIncompatible {
				t.Fatalf("Outcome = %v, want incompatible\n%s", result.Outcome, report)
			}
			if !strings.Contains(report, tc.want) {
				t.Errorf("report is missing %q:\n%s", tc.want, report)
			}
			if hasLedger(t, path) || len(backups(t, path)) != 0 {
				t.Fatal("incompatible database was changed")
			}
		})
	}
}

func TestForeignKeyViolationsAreWarningsOnly(t *testing.T) {
	path := nodeDatabase(t, fixture{
		variant:  "fresh",
		fkOffSQL: []string{`INSERT INTO articles (id, title, slug, category_id, author_id) VALUES (9, 'Orphan', 'orphan', 1, 999)`},
	})
	result, report, err := run(t, path, false)
	if err != nil || result.Outcome != adopt.OutcomeAdoptable {
		t.Fatalf("Outcome = %v, err = %v\n%s", result.Outcome, err, report)
	}
	if !strings.Contains(report, "foreign key violation: articles row 9 references authors") {
		t.Errorf("report does not warn about the orphan:\n%s", report)
	}
}

func TestApplyRefusesAnExistingBackupPath(t *testing.T) {
	path := nodeDatabase(t, fixture{variant: "fresh"})
	occupied := path + ".pre-adopt-20260924T073015Z.db"
	if err := os.WriteFile(occupied, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, report, err := run(t, path, true)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("Run() error = %v, want an existing-backup error\n%s", err, report)
	}
	if hasLedger(t, path) {
		t.Fatal("adoption continued without a backup")
	}
	if content, _ := os.ReadFile(occupied); string(content) != "occupied" {
		t.Fatal("existing file at the backup path was overwritten")
	}
}

func TestRunRefusesEmptyAndMissingDatabases(t *testing.T) {
	missing := t.TempDir() + "/absent/technews.db"
	if _, _, err := run(t, missing, false); err == nil {
		t.Fatal("Run(missing) error = nil")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("Run created the missing database")
	}

	emptyPath := t.TempDir() + "/empty.db"
	db, err := database.Open(context.Background(), emptyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := run(t, emptyPath, false); !errors.Is(err, migrate.ErrNothingToAdopt) {
		t.Fatalf("Run(empty) error = %v, want %v", err, migrate.ErrNothingToAdopt)
	}
}

// assertManagedLikeAFreshGoDatabase checks that the adopted database passes
// migrate.Run with the application's descriptors and that its schema now
// matches the Go reference completely.
func assertManagedLikeAFreshGoDatabase(t *testing.T, path string) {
	t.Helper()
	ctx := context.Background()
	db, err := database.OpenExisting(ctx, path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrate.Run(ctx, db, app.Migrations()); err != nil {
		t.Fatalf("migrate.Run after adoption: %v", err)
	}
	reference, err := adopt.BuildReference(ctx, app.Migrations(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	report, err := adopt.Verify(ctx, db, reference)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Compatible() {
		t.Fatalf("adopted database does not match the Go schema:\n%s", report.String())
	}
	for _, status := range report.Migrations {
		if status.State != adopt.StatePresent {
			t.Errorf("migration %d is %s after adoption", status.Version, status.State)
		}
	}
}

func equalVersions(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func sortedLines(text string) string {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for i := 1; i < len(lines); i++ {
		for j := i; j > 0 && lines[j] < lines[j-1]; j-- {
			lines[j], lines[j-1] = lines[j-1], lines[j]
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func TestRehearsalFailureOfAPartiallyPresentMigrationChangesNothing(t *testing.T) {
	// candidates exists exactly as migration 3 creates it, but one of its
	// indexes is missing: migration 3 is pending, and its plain CREATE TABLE
	// cannot run over the existing table. The dry run must catch that.
	var candidates, indexes []string
	for _, descriptor := range app.Migrations() {
		if descriptor.Version != 3 {
			continue
		}
		for _, statement := range strings.Split(descriptor.SQL, ";") {
			switch {
			case strings.Contains(statement, "CREATE TABLE candidates"):
				candidates = append(candidates, statement)
			case strings.Contains(statement, "CREATE INDEX idx_candidates_status_scheduled"):
				indexes = append(indexes, statement)
			}
		}
	}
	path := nodeDatabase(t, fixture{variant: "fresh", extra: append(candidates, indexes...)})
	hash := fileHash(t, path)
	for _, apply := range []bool{false, true} {
		result, report, err := run(t, path, apply)
		if err != nil {
			t.Fatalf("apply=%t: Run() error = %v\n%s", apply, err, report)
		}
		if result.Outcome != adopt.OutcomeIncompatible || !strings.Contains(report, "Rehearsal on a copy: FAILED") {
			t.Fatalf("apply=%t: Outcome = %v\n%s", apply, result.Outcome, report)
		}
		if !strings.Contains(report, "3 newsroom candidates: pending, partially present") {
			t.Errorf("report does not explain the partial migration:\n%s", report)
		}
		if fileHash(t, path) != hash || hasLedger(t, path) || len(backups(t, path)) != 0 {
			t.Fatalf("apply=%t: database changed after a failed rehearsal", apply)
		}
	}
}

func TestAnObjectOfTheWrongKindIsAMismatch(t *testing.T) {
	path := nodeDatabase(t, fixture{variant: "fresh", extra: []string{`CREATE VIEW candidates AS SELECT 1 AS id`}})
	result, report, err := run(t, path, false)
	if err != nil || result.Outcome != adopt.OutcomeIncompatible {
		t.Fatalf("Outcome = %v, err = %v\n%s", result.Outcome, err, report)
	}
	if !strings.Contains(report, "candidates is a view, want a table") {
		t.Errorf("report:\n%s", report)
	}
}

func TestFormattingOnlyDifferencesAreCompatible(t *testing.T) {
	// Whitespace, keyword case, identifier quoting, column order, and a
	// comment change nothing. Anything else in a definition is a mismatch.
	path := nodeDatabase(t, fixture{
		variant: "fresh",
		replace: map[string][2]string{
			"authors": {
				"role TEXT NOT NULL DEFAULT 'editor' CHECK(role IN ('admin', 'editor'))",
				"\"ROLE\"   text not null default 'editor' -- the editorial role\n check ( \"role\" in ('admin' , 'editor') )",
			},
			"media": {
				"filename TEXT NOT NULL,\n      url TEXT NOT NULL,",
				"url TEXT NOT NULL,\n      [filename] TEXT NOT NULL,",
			},
		},
	})
	result, report, err := run(t, path, false)
	if err != nil || result.Outcome != adopt.OutcomeAdoptable {
		t.Fatalf("Outcome = %v, err = %v\n%s", result.Outcome, err, report)
	}
}
