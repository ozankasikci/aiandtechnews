package adopt

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
)

// State says whether a migration's schema already exists in the database.
type State string

const (
	// StatePresent: every object the migration creates exists and matches.
	// Adoption records it in the ledger without running its SQL.
	StatePresent State = "present"
	// StatePending: at least one object is missing. Adoption runs its SQL.
	StatePending State = "pending"
)

// MigrationStatus is one migration's state in the verified database.
type MigrationStatus struct {
	Version  int64
	Name     string
	State    State
	Existing []string
	Missing  []string
}

// Report is the outcome of a verification. The database is compatible when
// Mismatches is empty.
type Report struct {
	Migrations []MigrationStatus
	// Mismatches make the database incompatible.
	Mismatches []string
	// Variants are the known Node schema variants that were accepted.
	Variants []string
	// Tolerated are objects Go does not know and leaves untouched.
	Tolerated []string
	// Warnings do not block adoption.
	Warnings []string
}

// Compatible reports whether the database may be adopted.
func (r Report) Compatible() bool { return len(r.Mismatches) == 0 }

// PresentVersions lists the versions whose schema already exists.
func (r Report) PresentVersions() []int64 {
	var versions []int64
	for _, status := range r.Migrations {
		if status.State == StatePresent {
			versions = append(versions, status.Version)
		}
	}
	return versions
}

func (r Report) String() string {
	var b strings.Builder
	b.WriteString("Migrations:\n")
	for _, status := range r.Migrations {
		switch {
		case status.State == StatePresent:
			fmt.Fprintf(&b, "  %d %s: present (recorded in the ledger; its SQL does not run)\n", status.Version, status.Name)
		case len(status.Existing) > 0:
			fmt.Fprintf(&b, "  %d %s: pending, partially present (its SQL runs over the existing objects; has: %s; missing: %s)\n",
				status.Version, status.Name, strings.Join(status.Existing, ", "), strings.Join(status.Missing, ", "))
		default:
			fmt.Fprintf(&b, "  %d %s: pending (its SQL runs; missing: %s)\n", status.Version, status.Name, strings.Join(status.Missing, ", "))
		}
	}
	section := func(title string, lines []string) {
		if len(lines) == 0 {
			return
		}
		b.WriteString(title + ":\n")
		for _, line := range lines {
			b.WriteString("  - " + line + "\n")
		}
	}
	section("Known Node variants accepted", r.Variants)
	section("Tolerated extras (not used by Go, left untouched)", r.Tolerated)
	section("Warnings", r.Warnings)
	section("Mismatches", r.Mismatches)
	if r.Compatible() {
		b.WriteString("Result: COMPATIBLE\n")
	} else {
		b.WriteString("Result: NOT COMPATIBLE (nothing was changed)\n")
	}
	return b.String()
}

// knownVariant is a column definition an older Node database may have
// instead of the Go migration's, and that Go handles identically.
type knownVariant struct {
	column     Column
	definition string // normalized column definition text
	reason     string
}

// knownVariants are Node's ALTER TABLE upgrades that add a column with a
// weaker definition than Node's own CREATE TABLE (apps/server/src/db.ts):
// SQLite cannot ALTER TABLE ADD COLUMN with a non-constant default, so Node
// adds created_at and updated_at as plain nullable TEXT. Go never relies on
// those defaults: internal/newsletter writes both columns on every INSERT.
var knownVariants = map[string]map[string]knownVariant{
	"subscribers": {
		"created_at": {Column{Name: "created_at", Type: "TEXT"}, "created_at text", "added by Node's ALTER TABLE; Go always writes it"},
		"updated_at": {Column{Name: "updated_at", Type: "TEXT"}, "updated_at text", "added by Node's ALTER TABLE; Go always writes it"},
	},
}

// Verify compares the database behind q with the reference. It reads only.
func Verify(ctx context.Context, q migrate.Queryer, reference *Reference) (Report, error) {
	target, err := Inspect(ctx, q)
	if err != nil {
		return Report{}, err
	}
	var report Report
	want := reference.Schema

	for _, migration := range reference.Migrations {
		status := MigrationStatus{Version: migration.Descriptor.Version, Name: migration.Descriptor.Name, State: StatePresent}
		for _, key := range migration.Objects {
			if _, ok := target.Objects[key]; ok {
				status.Existing = append(status.Existing, key)
			} else {
				status.Missing = append(status.Missing, key)
				status.State = StatePending
			}
		}
		report.Migrations = append(report.Migrations, status)
	}

	// Same name, different kind (a view where Go expects a table).
	targetKinds := map[string]string{}
	for _, object := range target.Objects {
		targetKinds[strings.ToLower(object.Name)] = object.Type
	}
	for key, object := range want.Objects {
		if _, ok := target.Objects[key]; ok {
			continue
		}
		if kind, ok := targetKinds[strings.ToLower(object.Name)]; ok {
			report.Mismatches = append(report.Mismatches, fmt.Sprintf("%s is a %s, want a %s", object.Name, kind, object.Type))
		}
	}

	for _, key := range sortedKeys(want.Objects) {
		object := want.Objects[key]
		got, ok := target.Objects[key]
		if !ok {
			continue
		}
		switch object.Type {
		case "table":
			report.compareTable(want.Tables[strings.ToLower(object.Name)], target.Tables[strings.ToLower(object.Name)])
		case "index":
			report.compareNamedIndex(object, got, want, target)
		default:
			if normalizeExpression(object.SQL) != normalizeExpression(got.SQL) {
				report.Mismatches = append(report.Mismatches, fmt.Sprintf("%s %s differs from the Go definition", object.Type, object.Name))
			}
		}
	}

	for _, key := range sortedKeys(target.Objects) {
		object := target.Objects[key]
		if _, ok := want.Objects[key]; ok {
			continue
		}
		if _, clash := want.Objects[objectKey(object.Type, object.Name)]; clash {
			continue
		}
		_, onGoTable := want.Tables[strings.ToLower(object.Table)]
		switch object.Type {
		case "table":
			if !hasObjectNamed(want, object.Name) {
				report.Tolerated = append(report.Tolerated, "table "+object.Name)
			}
		case "view":
			if !hasObjectNamed(want, object.Name) {
				report.Tolerated = append(report.Tolerated, "view "+object.Name)
			}
		case "index":
			if !onGoTable {
				continue // part of a tolerated table
			}
			index, found := findIndex(target.Tables[strings.ToLower(object.Table)], object.Name)
			if found && index.Unique {
				report.Mismatches = append(report.Mismatches, fmt.Sprintf("table %s: extra UNIQUE index %s (%s) can reject Go's writes", object.Table, object.Name, index.columnNames()))
			} else {
				report.Tolerated = append(report.Tolerated, fmt.Sprintf("index %s on %s (not unique)", object.Name, object.Table))
			}
		case "trigger":
			if onGoTable {
				report.Mismatches = append(report.Mismatches, fmt.Sprintf("trigger %s on %s changes what Go's writes do", object.Name, object.Table))
			} else {
				report.Tolerated = append(report.Tolerated, fmt.Sprintf("trigger %s on %s", object.Name, object.Table))
			}
		}
	}
	return report, nil
}

func hasObjectNamed(schema *Schema, name string) bool {
	for _, object := range schema.Objects {
		if strings.EqualFold(object.Name, name) {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func findIndex(table *Table, name string) (Index, bool) {
	if table == nil {
		return Index{}, false
	}
	for _, index := range table.Indexes {
		if strings.EqualFold(index.Name, name) {
			return index, true
		}
	}
	return Index{}, false
}

func (r *Report) mismatch(table, format string, args ...any) {
	r.Mismatches = append(r.Mismatches, "table "+table+": "+fmt.Sprintf(format, args...))
}

func (r *Report) compareTable(want, got *Table) {
	name := want.Name
	if want.WithoutRowID != got.WithoutRowID {
		r.mismatch(name, "WITHOUT ROWID is %s, want %s", onOff(got.WithoutRowID), onOff(want.WithoutRowID))
	}
	if want.Strict != got.Strict {
		r.mismatch(name, "STRICT is %s, want %s", onOff(got.Strict), onOff(want.Strict))
	}
	if want.Autoincrement != got.Autoincrement {
		r.mismatch(name, "AUTOINCREMENT is %s, want %s", onOff(got.Autoincrement), onOff(want.Autoincrement))
	}

	for _, columnName := range want.ColumnOrder {
		wantColumn := want.Columns[strings.ToLower(columnName)]
		gotColumn, ok := got.Columns[strings.ToLower(columnName)]
		if !ok {
			r.mismatch(name, "missing column %s %s", wantColumn.Name, wantColumn.describe())
			continue
		}
		key := strings.ToLower(columnName)
		wantDef, gotDef := want.ColumnDefs[key], got.ColumnDefs[key]
		if wantColumn.equal(gotColumn) && wantDef == gotDef {
			continue
		}
		if variant, ok := knownVariants[strings.ToLower(name)][key]; ok && variant.column.equal(gotColumn) && gotDef == variant.definition {
			r.Variants = append(r.Variants, fmt.Sprintf("%s.%s is %s (%s); the Go migration declares %s",
				name, wantColumn.Name, describeOrPlain(gotColumn), variant.reason, wantColumn.describe()))
			continue
		}
		// The full definition text catches what the PRAGMAs do not show
		// (COLLATE, ON CONFLICT, GENERATED, DEFERRABLE, ...). Formatting
		// is normalized; any other difference is a mismatch, even when it
		// might be harmless: a false reject is safe, a false accept is not.
		if wantDef != gotDef {
			r.mismatch(name, "column %s definition differs: have %q, want %q", wantColumn.Name, gotDef, wantDef)
		}
		if wantColumn.Type != gotColumn.Type {
			r.mismatch(name, "column %s: type %s, want %s", wantColumn.Name, orNone(gotColumn.Type), orNone(wantColumn.Type))
		}
		if wantColumn.NotNull != gotColumn.NotNull {
			r.mismatch(name, "column %s: %s, want %s", wantColumn.Name, nullability(gotColumn.NotNull), nullability(wantColumn.NotNull))
		}
		if wantColumn.Default != gotColumn.Default {
			r.mismatch(name, "column %s: default %s, want %s", wantColumn.Name, orNone(gotColumn.Default), orNone(wantColumn.Default))
		}
		if wantColumn.PK != gotColumn.PK {
			r.mismatch(name, "column %s: primary key position %d, want %d", wantColumn.Name, gotColumn.PK, wantColumn.PK)
		}
		if wantColumn.Hidden != gotColumn.Hidden {
			r.mismatch(name, "column %s: hidden kind %d, want %d", wantColumn.Name, gotColumn.Hidden, wantColumn.Hidden)
		}
	}
	for _, columnName := range got.ColumnOrder {
		column := got.Columns[strings.ToLower(columnName)]
		if _, ok := want.Columns[strings.ToLower(columnName)]; ok {
			continue
		}
		switch {
		case column.PK > 0:
			r.mismatch(name, "extra column %s is part of the primary key", column.Name)
		case column.NotNull && column.Default == "" && column.Hidden == 0:
			r.mismatch(name, "extra column %s is NOT NULL without a default, so Go's INSERTs would fail", column.Name)
		case column.Default != "":
			r.Tolerated = append(r.Tolerated, fmt.Sprintf("column %s.%s (%s; has a default)", name, column.Name, column.describe()))
		default:
			r.Tolerated = append(r.Tolerated, fmt.Sprintf("column %s.%s (%s; nullable)", name, column.Name, describeOrPlain(column)))
		}
	}

	if !slices.Equal(want.Constraints, got.Constraints) {
		r.mismatch(name, "table constraints differ: have %s, want %s", listOrNone(got.Constraints), listOrNone(want.Constraints))
	}
	if !slices.Equal(want.Checks, got.Checks) {
		r.mismatch(name, "CHECK constraints differ: have %s, want %s", listOrNone(got.Checks), listOrNone(want.Checks))
	}
	if !slices.Equal(want.ForeignKeys, got.ForeignKeys) {
		r.mismatch(name, "foreign keys differ: have %s, want %s", listOrNone(got.ForeignKeys), listOrNone(want.ForeignKeys))
	}

	// Automatic indexes (UNIQUE and non-rowid PRIMARY KEY constraints) are
	// compared by definition: their generated names depend on column order.
	wantAuto, gotAuto := automaticIndexes(want), automaticIndexes(got)
	for signature, index := range wantAuto {
		if _, ok := gotAuto[signature]; !ok {
			r.mismatch(name, "missing %s constraint (%s)", constraintKind(index), index.columnNames())
		}
	}
	for signature, index := range gotAuto {
		if _, ok := wantAuto[signature]; !ok {
			r.mismatch(name, "extra %s constraint (%s)", constraintKind(index), index.columnNames())
		}
	}
}

func (r *Report) compareNamedIndex(wantObject, gotObject Object, want, target *Schema) {
	if !strings.EqualFold(wantObject.Table, gotObject.Table) {
		r.Mismatches = append(r.Mismatches, fmt.Sprintf("index %s is on %s, want %s", wantObject.Name, gotObject.Table, wantObject.Table))
		return
	}
	wantIndex, _ := findIndex(want.Tables[strings.ToLower(wantObject.Table)], wantObject.Name)
	gotIndex, _ := findIndex(target.Tables[strings.ToLower(gotObject.Table)], gotObject.Name)
	if wantText, gotText := normalizeExpression(wantObject.SQL), normalizeExpression(gotObject.SQL); wantText != gotText {
		r.Mismatches = append(r.Mismatches, fmt.Sprintf("index %s on %s: definition differs: have %q, want %q", wantObject.Name, wantObject.Table, gotText, wantText))
	}
	if wantIndex.signature() != gotIndex.signature() {
		r.Mismatches = append(r.Mismatches, fmt.Sprintf("index %s on %s: have %s, want %s", wantObject.Name, wantObject.Table, gotIndex.signature(), wantIndex.signature()))
	}
}

func automaticIndexes(table *Table) map[string]Index {
	indexes := map[string]Index{}
	for _, index := range table.Indexes {
		if index.Origin == "c" {
			continue
		}
		indexes[index.Origin+" "+index.signature()] = index
	}
	return indexes
}

func constraintKind(index Index) string {
	if index.Origin == "pk" {
		return "PRIMARY KEY"
	}
	return "UNIQUE"
}

func (c Column) equal(other Column) bool {
	return c.Type == other.Type && c.NotNull == other.NotNull && c.Default == other.Default && c.PK == other.PK && c.Hidden == other.Hidden
}

func describeOrPlain(c Column) string {
	if description := c.describe(); description != "" {
		return description
	}
	return "untyped"
}

func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

func nullability(notNull bool) string {
	if notNull {
		return "NOT NULL"
	}
	return "nullable"
}

func orNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
}

func listOrNone(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return "[" + strings.Join(values, "; ") + "]"
}

// CheckHealth runs PRAGMA integrity_check (anything but "ok" is a mismatch)
// and PRAGMA foreign_key_check (violations are warnings: Go enforces
// foreign keys, so changing such a row can fail, but reading it works).
func CheckHealth(ctx context.Context, q migrate.Queryer) (mismatches, warnings []string, err error) {
	rows, err := q.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return nil, nil, fmt.Errorf("integrity_check: %w", err)
	}
	var problems []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("integrity_check: %w", err)
		}
		if line != "ok" {
			problems = append(problems, line)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, nil, fmt.Errorf("integrity_check: %w", err)
	}
	for _, problem := range problems {
		mismatches = append(mismatches, "integrity_check: "+problem)
	}

	rows, err = q.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return nil, nil, fmt.Errorf("foreign_key_check: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, parent string
		var rowID *int64
		var fkID int
		if err := rows.Scan(&table, &rowID, &parent, &fkID); err != nil {
			return nil, nil, fmt.Errorf("foreign_key_check: %w", err)
		}
		row := "?"
		if rowID != nil {
			row = fmt.Sprint(*rowID)
		}
		warnings = append(warnings, fmt.Sprintf("foreign key violation: %s row %s references %s, which has no matching row (Go enforces foreign keys: updating this row can fail)", table, row, parent))
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("foreign_key_check: %w", err)
	}
	return mismatches, warnings, nil
}
