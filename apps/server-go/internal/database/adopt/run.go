package adopt

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
)

// Outcome is what Run concluded.
type Outcome int

const (
	// OutcomeIncompatible: verification or the rehearsal failed; nothing was changed.
	OutcomeIncompatible Outcome = iota
	// OutcomeAdoptable: a dry run found the database compatible and the
	// rehearsal on a copy passed; nothing was changed.
	OutcomeAdoptable
	// OutcomeAdopted: --apply backed the database up and adopted it.
	OutcomeAdopted
	// OutcomeAlreadyManaged: the database has a migration ledger; nothing was changed.
	OutcomeAlreadyManaged
)

func (o Outcome) String() string {
	switch o {
	case OutcomeAdoptable:
		return "adoptable"
	case OutcomeAdopted:
		return "adopted"
	case OutcomeAlreadyManaged:
		return "already managed"
	default:
		return "incompatible"
	}
}

// Options configure Run.
type Options struct {
	DatabasePath string
	// Apply performs the adoption; otherwise Run is a read-only dry run.
	Apply       bool
	Descriptors []migrate.Descriptor
	// Now names the backup (UTC); defaults to time.Now.
	Now func() time.Time
	Out io.Writer
}

// Result is what Run did.
type Result struct {
	Outcome    Outcome
	Report     Report
	BackupPath string
	Adoption   migrate.AdoptResult
}

// Run verifies an existing database against the Go migrations and, with
// Apply, adopts it:
//
//  1. refuse a missing or empty database; report "already managed" for one
//     with a ledger;
//  2. verify the schema and the database's health on a read-only
//     connection (a mismatch stops here: nothing is written);
//  3. rehearse the whole adoption on a VACUUM INTO copy in a temporary
//     directory, then run migrate.Run and verify again on the copy;
//  4. with Apply only: VACUUM INTO '<path>.pre-adopt-<UTC timestamp>.db'
//     (refused when that file exists), check the backup, run migrate.Adopt
//     (which verifies again inside its write transaction), run migrate.Run
//     (a no-op), and verify the result.
//
// Operational failures return an error; an incompatible schema returns
// OutcomeIncompatible and a nil error.
func Run(ctx context.Context, options Options) (result Result, err error) {
	out := options.Out
	if out == nil {
		out = io.Discard
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	path, err := filepath.Abs(options.DatabasePath)
	if err != nil {
		return Result{}, fmt.Errorf("resolve database path: %w", err)
	}

	source, err := database.OpenExisting(ctx, path, true)
	if err != nil {
		return Result{}, err
	}
	defer func() {
		if source != nil {
			err = errors.Join(err, source.Close())
		}
	}()

	mode := "dry run (read-only; pass --apply to adopt)"
	if options.Apply {
		mode = "apply"
	}
	fmt.Fprintf(out, "Database: %s\nMode: %s\n\n", path, mode)

	managed, err := hasLedger(ctx, source)
	if err != nil {
		return Result{}, err
	}
	if managed {
		if err := reportManaged(ctx, source, options.Descriptors, out); err != nil {
			return Result{}, err
		}
		return Result{Outcome: OutcomeAlreadyManaged}, nil
	}
	empty, err := isEmpty(ctx, source)
	if err != nil {
		return Result{}, err
	}
	if empty {
		return Result{}, fmt.Errorf("%s: %w", path, migrate.ErrNothingToAdopt)
	}

	scratch, err := os.MkdirTemp("", "technews-adopt-")
	if err != nil {
		return Result{}, fmt.Errorf("create scratch directory: %w", err)
	}
	defer func() { err = errors.Join(err, os.RemoveAll(scratch)) }()

	reference, err := BuildReference(ctx, options.Descriptors, filepath.Join(scratch))
	if err != nil {
		return Result{}, err
	}
	report, err := Verify(ctx, source, reference)
	if err != nil {
		return Result{}, err
	}
	healthMismatches, warnings, err := CheckHealth(ctx, source)
	if err != nil {
		return Result{}, err
	}
	report.Mismatches = append(report.Mismatches, healthMismatches...)
	report.Warnings = append(report.Warnings, warnings...)
	result.Report = report
	fmt.Fprint(out, report.String())
	if !report.Compatible() {
		return result, nil
	}

	counts, err := rowCounts(ctx, source)
	if err != nil {
		return Result{}, err
	}

	// Rehearse on a copy, so the dry run also proves that every pending
	// migration's SQL runs over the existing objects.
	rehearsalPath := filepath.Join(scratch, "rehearsal.db")
	if err := vacuumInto(ctx, source, rehearsalPath); err != nil {
		return Result{}, fmt.Errorf("copy the database for the rehearsal: %w", err)
	}
	rehearsal, rehearsalErr := adoptAndCheck(ctx, rehearsalPath, options.Descriptors, reference)
	fmt.Fprintln(out)
	if rehearsalErr != nil {
		fmt.Fprintf(out, "Rehearsal on a copy: FAILED: %v\nResult: NOT COMPATIBLE (nothing was changed)\n", rehearsalErr)
		result.Report.Mismatches = append(result.Report.Mismatches, "rehearsal: "+rehearsalErr.Error())
		return result, nil
	}
	fmt.Fprintf(out, "Rehearsal on a copy: passed (recorded %s; ran %s; migrate.Run is a no-op afterwards)\n",
		versionList(rehearsal.adoption.Recorded), versionList(rehearsal.adoption.Applied))
	writeCountChanges(out, counts, rehearsal.counts)

	if !options.Apply {
		fmt.Fprintln(out, "\nDry run: nothing was changed. Run again with --apply to adopt.")
		result.Outcome = OutcomeAdoptable
		return result, nil
	}

	// Apply: back up first, from the read-only connection.
	backupPath := fmt.Sprintf("%s.pre-adopt-%s.db", path, now().UTC().Format("20060102T150405Z"))
	if _, err := os.Lstat(backupPath); err == nil {
		return result, fmt.Errorf("backup %s already exists; refusing to overwrite it", backupPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, fmt.Errorf("check backup path: %w", err)
	}
	if err := vacuumInto(ctx, source, backupPath); err != nil {
		return result, fmt.Errorf("write backup %s: %w", backupPath, err)
	}
	if err := checkBackup(ctx, backupPath, counts); err != nil {
		return result, fmt.Errorf("backup %s: %w", backupPath, err)
	}
	result.BackupPath = backupPath
	fmt.Fprintf(out, "\nBackup: %s (integrity ok, row counts match)\n", backupPath)

	if err := source.Close(); err != nil {
		return result, fmt.Errorf("close read-only connection: %w", err)
	}
	source = nil

	adopted, err := adoptAndCheck(ctx, path, options.Descriptors, reference)
	if err != nil {
		return result, fmt.Errorf("adopt %s (restore from %s if needed): %w", path, backupPath, err)
	}
	result.Adoption = adopted.adoption
	result.Outcome = OutcomeAdopted
	fmt.Fprintf(out, "Adopted: recorded %s; ran %s; migrate.Run is a no-op.\n", versionList(adopted.adoption.Recorded), versionList(adopted.adoption.Applied))
	writeCountChanges(out, counts, adopted.counts)
	return result, nil
}

type adoption struct {
	adoption migrate.AdoptResult
	counts   map[string]int64
}

// adoptAndCheck adopts the database at path, runs migrate.Run, and checks
// that the result matches the reference completely.
func adoptAndCheck(ctx context.Context, path string, descriptors []migrate.Descriptor, reference *Reference) (result adoption, err error) {
	db, err := database.OpenExisting(ctx, path, false)
	if err != nil {
		return adoption{}, err
	}
	defer func() { err = errors.Join(err, db.Close()) }()

	adopted, err := migrate.Adopt(ctx, db, descriptors, func(ctx context.Context, q migrate.Queryer) ([]int64, error) {
		report, err := Verify(ctx, q, reference)
		if err != nil {
			return nil, err
		}
		if !report.Compatible() {
			return nil, fmt.Errorf("schema changed since verification: %s", strings.Join(report.Mismatches, "; "))
		}
		return report.PresentVersions(), nil
	})
	if err != nil {
		return adoption{}, err
	}
	if err := migrate.Run(ctx, db, descriptors); err != nil {
		return adoption{}, fmt.Errorf("migrate.Run after adoption: %w", err)
	}
	final, err := Verify(ctx, db, reference)
	if err != nil {
		return adoption{}, err
	}
	if !final.Compatible() {
		return adoption{}, fmt.Errorf("schema after adoption does not match: %s", strings.Join(final.Mismatches, "; "))
	}
	for _, status := range final.Migrations {
		if status.State != StatePresent {
			return adoption{}, fmt.Errorf("migration %d is still %s after adoption", status.Version, status.State)
		}
	}
	counts, err := rowCounts(ctx, db)
	if err != nil {
		return adoption{}, err
	}
	return adoption{adoption: adopted, counts: counts}, nil
}

func vacuumInto(ctx context.Context, db *sql.DB, path string) error {
	_, err := db.ExecContext(ctx, `VACUUM INTO ?`, path)
	return err
}

func checkBackup(ctx context.Context, path string, want map[string]int64) (err error) {
	db, err := database.OpenExisting(ctx, path, true)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	var integrity string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return fmt.Errorf("integrity_check: %w", err)
	}
	if integrity != "ok" {
		return fmt.Errorf("integrity_check: %s", integrity)
	}
	got, err := rowCounts(ctx, db)
	if err != nil {
		return err
	}
	for table, count := range want {
		if got[table] != count {
			return fmt.Errorf("table %s has %d rows, the original %d", table, got[table], count)
		}
	}
	if len(got) != len(want) {
		return fmt.Errorf("backup has %d tables, the original %d", len(got), len(want))
	}
	return nil
}

func hasLedger(ctx context.Context, q migrate.Queryer) (bool, error) {
	var count int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE name = 'schema_migrations'`).Scan(&count); err != nil {
		return false, fmt.Errorf("look for the migration ledger: %w", err)
	}
	return count > 0, nil
}

func isEmpty(ctx context.Context, q migrate.Queryer) (bool, error) {
	var count int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type IN ('table', 'view', 'trigger') AND name NOT LIKE 'sqlite\_%' ESCAPE '\'`).Scan(&count); err != nil {
		return false, fmt.Errorf("inspect database: %w", err)
	}
	return count == 0, nil
}

func reportManaged(ctx context.Context, q migrate.Queryer, descriptors []migrate.Descriptor, out io.Writer) error {
	rows, err := q.QueryContext(ctx, `SELECT version, name FROM schema_migrations ORDER BY version`)
	if err != nil {
		return fmt.Errorf("read migration ledger: %w", err)
	}
	defer rows.Close()
	applied := map[int64]bool{}
	fmt.Fprintln(out, "The database is already managed by the migration ledger; nothing to adopt, nothing was changed.")
	for rows.Next() {
		var version int64
		var name string
		if err := rows.Scan(&version, &name); err != nil {
			return fmt.Errorf("read migration ledger: %w", err)
		}
		applied[version] = true
		fmt.Fprintf(out, "  applied %d %s\n", version, name)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read migration ledger: %w", err)
	}
	for _, descriptor := range descriptors {
		if !applied[descriptor.Version] {
			fmt.Fprintf(out, "  pending %d %s (apply with cmd/migrate)\n", descriptor.Version, descriptor.Name)
		}
	}
	return nil
}

func rowCounts(ctx context.Context, q migrate.Queryer) (map[string]int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("list tables: %w", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	counts := map[string]int64{}
	for _, table := range tables {
		var count int64
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM "`+strings.ReplaceAll(table, `"`, `""`)+`"`).Scan(&count); err != nil {
			return nil, fmt.Errorf("count rows of %s: %w", table, err)
		}
		counts[table] = count
	}
	return counts, nil
}

func writeCountChanges(out io.Writer, before, after map[string]int64) {
	var names []string
	for name := range after {
		names = append(names, name)
	}
	sort.Strings(names)
	var changes []string
	for _, name := range names {
		previous, existed := before[name]
		switch {
		case name == "schema_migrations":
			continue
		case !existed:
			changes = append(changes, fmt.Sprintf("%s: new table, %d rows", name, after[name]))
		case previous != after[name]:
			changes = append(changes, fmt.Sprintf("%s: %d -> %d rows", name, previous, after[name]))
		}
	}
	for name := range before {
		if _, ok := after[name]; !ok {
			changes = append(changes, fmt.Sprintf("%s: MISSING after adoption", name))
		}
	}
	if len(changes) == 0 {
		fmt.Fprintln(out, "Row counts: unchanged in every existing table.")
		return
	}
	fmt.Fprintln(out, "Row count changes (every other existing table is unchanged):")
	for _, change := range changes {
		fmt.Fprintln(out, "  - "+change)
	}
}

func versionList(versions []int64) string {
	if len(versions) == 0 {
		return "none"
	}
	parts := make([]string, len(versions))
	for i, version := range versions {
		parts[i] = fmt.Sprint(version)
	}
	return strings.Join(parts, ", ")
}
