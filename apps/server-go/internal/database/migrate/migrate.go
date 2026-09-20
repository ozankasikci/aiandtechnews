// Package migrate applies a deterministic, append-only SQLite migration history.
//
// Migration SQL is immutable: checksums cover its exact bytes. A migration must
// not contain transaction-control statements (BEGIN, COMMIT, ROLLBACK,
// SAVEPOINT, RELEASE, or top-level END) or operations SQLite forbids in a
// transaction, such as VACUUM. END is permitted only as the closing keyword of
// a recognized CREATE TRIGGER body. The runner owns the single transaction
// around the complete migration batch.
package migrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const rollbackTimeout = 2 * time.Second

var (
	ErrInvalidDescriptors = errors.New("invalid migration descriptors")
	ErrIncompatibleLedger = errors.New("incompatible migration ledger")
	ErrUnmanagedDatabase  = errors.New("unmanaged database")
	ErrUnknownMigration   = errors.New("unknown applied migration")
	ErrMigrationDrift     = errors.New("migration drift")
	ErrHistoryGap         = errors.New("migration history gap")
)

// Descriptor is one immutable migration. Versions need only be strictly
// increasing; gaps are allowed.
type Descriptor struct {
	Version int64
	Name    string
	SQL     string
}

// Checksum returns the lowercase hexadecimal SHA-256 of the descriptor's exact
// SQL bytes. Whitespace and line endings are intentionally significant.
func (d Descriptor) Checksum() string {
	sum := sha256.Sum256([]byte(d.SQL))
	return fmt.Sprintf("%x", sum)
}

// Run verifies the existing ledger and atomically applies pending migrations.
func Run(ctx context.Context, db *sql.DB, descriptors []Descriptor) (err error) {
	if err := validateDescriptors(descriptors); err != nil {
		return err
	}
	if ctx == nil {
		return fmt.Errorf("run migrations: nil context")
	}
	if db == nil {
		return errors.New("run migrations: nil database")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("run migrations: acquire connection: %w", err)
	}
	defer func() { err = errors.Join(err, conn.Close()) }()

	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("run migrations: begin immediate: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			rollbackCtx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
			defer cancel()
			_, rollbackErr := conn.ExecContext(rollbackCtx, "ROLLBACK")
			if rollbackErr != nil {
				err = errors.Join(err, fmt.Errorf("rollback migrations: %w", rollbackErr))
			}
		}
	}()

	exists, err := ledgerExists(ctx, conn)
	if err != nil {
		return err
	}
	if !exists {
		managed, err := databaseIsEmpty(ctx, conn)
		if err != nil {
			return err
		}
		if !managed {
			return ErrUnmanagedDatabase
		}
		if _, err := conn.ExecContext(ctx, `CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			checksum TEXT NOT NULL,
			applied_at TEXT NOT NULL
		)`); err != nil {
			return fmt.Errorf("create migration ledger: %w", err)
		}
	} else if err := validateLedger(ctx, conn); err != nil {
		return err
	}

	applied, appliedVersions, highest, err := readApplied(ctx, conn)
	if err != nil {
		return err
	}
	byVersion := make(map[int64]Descriptor, len(descriptors))
	for _, descriptor := range descriptors {
		byVersion[descriptor.Version] = descriptor
	}
	for _, version := range appliedVersions {
		record := applied[version]
		descriptor, ok := byVersion[version]
		if !ok {
			return fmt.Errorf("%w: version %d", ErrUnknownMigration, version)
		}
		if descriptor.Name != record.name || descriptor.Checksum() != record.checksum {
			return fmt.Errorf("%w: version %d", ErrMigrationDrift, version)
		}
	}
	for _, descriptor := range descriptors {
		if descriptor.Version < highest {
			if _, ok := applied[descriptor.Version]; !ok {
				return fmt.Errorf("%w: unapplied version %d precedes applied version %d", ErrHistoryGap, descriptor.Version, highest)
			}
		}
	}

	for _, descriptor := range descriptors {
		if _, ok := applied[descriptor.Version]; ok {
			continue
		}
		if _, err := conn.ExecContext(ctx, descriptor.SQL); err != nil {
			return fmt.Errorf("apply migration %d (%s): %w", descriptor.Version, descriptor.Name, err)
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO schema_migrations(version, name, checksum, applied_at)
			VALUES (?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, descriptor.Version, descriptor.Name, descriptor.Checksum()); err != nil {
			return fmt.Errorf("record migration %d (%s): %w", descriptor.Version, descriptor.Name, err)
		}
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	committed = true
	return nil
}

func validateDescriptors(descriptors []Descriptor) error {
	versions := make(map[int64]struct{}, len(descriptors))
	names := make(map[string]struct{}, len(descriptors))
	var previous int64
	for i, descriptor := range descriptors {
		if descriptor.Version <= 0 || strings.TrimSpace(descriptor.Name) == "" || strings.TrimSpace(descriptor.SQL) == "" {
			return fmt.Errorf("%w: descriptor %d has invalid fields", ErrInvalidDescriptors, i)
		}
		if _, ok := versions[descriptor.Version]; ok {
			return fmt.Errorf("%w: duplicate version %d", ErrInvalidDescriptors, descriptor.Version)
		}
		if _, ok := names[descriptor.Name]; ok {
			return fmt.Errorf("%w: duplicate name %q", ErrInvalidDescriptors, descriptor.Name)
		}
		if i > 0 && descriptor.Version <= previous {
			return fmt.Errorf("%w: version %d is not strictly after %d", ErrInvalidDescriptors, descriptor.Version, previous)
		}
		if err := validateDescriptorSQL(descriptor.SQL); err != nil {
			return fmt.Errorf("%w: descriptor version %d (%q): %v", ErrInvalidDescriptors, descriptor.Version, descriptor.Name, err)
		}
		versions[descriptor.Version] = struct{}{}
		names[descriptor.Name] = struct{}{}
		previous = descriptor.Version
	}
	return nil
}

var transactionUnsafeStatements = map[string]struct{}{
	"BEGIN":     {},
	"COMMIT":    {},
	"ROLLBACK":  {},
	"SAVEPOINT": {},
	"RELEASE":   {},
	"END":       {},
	"VACUUM":    {},
}

type triggerScanState uint8

const (
	triggerNone triggerScanState = iota
	triggerSawCreate
	triggerSawCreateTemp
	triggerAwaitingBody
	triggerBody
	triggerEnded
)

// validateDescriptorSQL identifies statement-leading tokens without
// interpreting tokens inside comments or SQLite's quoted strings and
// identifiers. It has deliberately conservative, limited awareness of CREATE
// [TEMP|TEMPORARY] TRIGGER ... BEGIN ...; ...; END so body semicolons do not
// hide transaction control and the trigger-closing END is not mistaken for a
// transaction statement. It is not a general SQL parser. The migration runner,
// rather than descriptors, owns the transaction, so transaction control and
// statements that cannot run in a transaction are rejected.
func validateDescriptorSQL(sqlText string) error {
	statementStart := true
	triggerState := triggerNone
	for i := 0; i < len(sqlText); {
		switch sqlText[i] {
		case ' ', '	', '\n', '\r', '\v', '\f':
			i++
		case ';':
			statementStart = true
			if triggerState != triggerBody {
				triggerState = triggerNone
			}
			i++
		case '-':
			if i+1 < len(sqlText) && sqlText[i+1] == '-' {
				i += 2
				for i < len(sqlText) && sqlText[i] != '\n' && sqlText[i] != '\r' {
					i++
				}
				continue
			}
			statementStart = false
			i++
		case '/':
			if i+1 < len(sqlText) && sqlText[i+1] == '*' {
				end := strings.Index(sqlText[i+2:], "*/")
				if end < 0 {
					return errors.New("unterminated block comment")
				}
				i += end + 4
				continue
			}
			statementStart = false
			i++
		case '\'', '"', '`', '[':
			close := sqlText[i]
			if close == '[' {
				close = ']'
			}
			var err error
			i, err = scanQuotedSQL(sqlText, i+1, close)
			if err != nil {
				return err
			}
			if triggerState == triggerSawCreate || triggerState == triggerSawCreateTemp {
				triggerState = triggerNone
			}
			statementStart = false
		default:
			if !isSQLWordByte(sqlText[i]) {
				if triggerState == triggerSawCreate || triggerState == triggerSawCreateTemp {
					triggerState = triggerNone
				}
				statementStart = false
				i++
				continue
			}
			start := i
			for i < len(sqlText) && isSQLWordByte(sqlText[i]) {
				i++
			}
			keyword := strings.ToUpper(sqlText[start:i])
			atStatementStart := statementStart
			if triggerState == triggerBody && atStatementStart && keyword == "END" {
				triggerState = triggerEnded
				statementStart = false
				continue
			}
			if atStatementStart {
				if _, unsafe := transactionUnsafeStatements[keyword]; unsafe {
					return fmt.Errorf("statement starts with transaction-unsafe keyword %s", keyword)
				}
			}
			switch triggerState {
			case triggerNone:
				if atStatementStart && keyword == "CREATE" {
					triggerState = triggerSawCreate
				}
			case triggerSawCreate:
				switch keyword {
				case "TRIGGER":
					triggerState = triggerAwaitingBody
				case "TEMP", "TEMPORARY":
					triggerState = triggerSawCreateTemp
				default:
					triggerState = triggerNone
				}
			case triggerSawCreateTemp:
				if keyword == "TRIGGER" {
					triggerState = triggerAwaitingBody
				} else {
					triggerState = triggerNone
				}
			case triggerAwaitingBody:
				if keyword == "BEGIN" {
					triggerState = triggerBody
					statementStart = true
					continue
				}
			}
			statementStart = false
		}
	}
	return nil
}

func scanQuotedSQL(sqlText string, start int, close byte) (int, error) {
	for i := start; i < len(sqlText); i++ {
		if sqlText[i] != close {
			continue
		}
		if i+1 < len(sqlText) && sqlText[i+1] == close {
			i++
			continue
		}
		return i + 1, nil
	}
	return 0, fmt.Errorf("unterminated %q quoted content", close)
}

func isSQLWordByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' || value == '_'
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func ledgerExists(ctx context.Context, q queryer) (bool, error) {
	var kind string
	err := q.QueryRowContext(ctx, `SELECT type FROM sqlite_schema WHERE name = 'schema_migrations'`).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect migration ledger: %w", err)
	}
	if kind != "table" {
		return true, fmt.Errorf("%w: schema_migrations is a %s", ErrIncompatibleLedger, kind)
	}
	return true, nil
}

func databaseIsEmpty(ctx context.Context, q queryer) (bool, error) {
	var count int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema
		WHERE type IN ('table', 'view', 'trigger')
		AND name NOT LIKE 'sqlite_%'
		AND name <> 'schema_migrations'`).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("inspect unmanaged database objects: %w", err)
	}
	return count == 0, nil
}

type columnInfo struct {
	name     string
	typeName string
	notNull  int
	pk       int
}

func validateLedger(ctx context.Context, q queryer) error {
	rows, err := q.QueryContext(ctx, `PRAGMA table_info('schema_migrations')`)
	if err != nil {
		return fmt.Errorf("%w: inspect columns: %v", ErrIncompatibleLedger, err)
	}
	defer rows.Close()
	columns := map[string]columnInfo{}
	for rows.Next() {
		var cid int
		var column columnInfo
		var defaultValue any
		if err := rows.Scan(&cid, &column.name, &column.typeName, &column.notNull, &defaultValue, &column.pk); err != nil {
			return fmt.Errorf("%w: scan columns: %v", ErrIncompatibleLedger, err)
		}
		columns[column.name] = column
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("%w: inspect columns: %v", ErrIncompatibleLedger, err)
	}
	if len(columns) != 4 || !validColumn(columns["version"], "INTEGER", false, 1) ||
		!validColumn(columns["name"], "TEXT", true, 0) ||
		!validColumn(columns["checksum"], "TEXT", true, 0) ||
		!validColumn(columns["applied_at"], "TEXT", true, 0) {
		return fmt.Errorf("%w: unexpected columns", ErrIncompatibleLedger)
	}
	unique, err := hasUniqueNameIndex(ctx, q)
	if err != nil {
		return err
	}
	if !unique {
		return fmt.Errorf("%w: name is not uniquely constrained", ErrIncompatibleLedger)
	}
	return nil
}

func validColumn(column columnInfo, typeName string, notNull bool, pk int) bool {
	return column.name != "" && strings.EqualFold(column.typeName, typeName) && column.notNull == boolInt(notNull) && column.pk == pk
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func hasUniqueNameIndex(ctx context.Context, q queryer) (bool, error) {
	rows, err := q.QueryContext(ctx, `PRAGMA index_list('schema_migrations')`)
	if err != nil {
		return false, fmt.Errorf("%w: inspect indexes: %v", ErrIncompatibleLedger, err)
	}
	type index struct {
		name    string
		unique  int
		partial int
	}
	var indexes []index
	for rows.Next() {
		var seq int
		var idx index
		var origin string
		if err := rows.Scan(&seq, &idx.name, &idx.unique, &origin, &idx.partial); err != nil {
			rows.Close()
			return false, fmt.Errorf("%w: scan indexes: %v", ErrIncompatibleLedger, err)
		}
		indexes = append(indexes, idx)
	}
	if err := rows.Close(); err != nil {
		return false, fmt.Errorf("%w: close indexes: %v", ErrIncompatibleLedger, err)
	}
	for _, idx := range indexes {
		if idx.unique != 1 || idx.partial != 0 {
			continue
		}
		info, err := q.QueryContext(ctx, `PRAGMA index_info('`+strings.ReplaceAll(idx.name, "'", "''")+`')`)
		if err != nil {
			return false, fmt.Errorf("%w: inspect index: %v", ErrIncompatibleLedger, err)
		}
		var names []string
		for info.Next() {
			var seq, cid int
			var name string
			if err := info.Scan(&seq, &cid, &name); err != nil {
				info.Close()
				return false, fmt.Errorf("%w: scan index: %v", ErrIncompatibleLedger, err)
			}
			names = append(names, name)
		}
		if err := info.Close(); err != nil {
			return false, fmt.Errorf("%w: close index: %v", ErrIncompatibleLedger, err)
		}
		if len(names) == 1 && names[0] == "name" {
			return true, nil
		}
	}
	return false, nil
}

type appliedRecord struct{ name, checksum string }

func readApplied(ctx context.Context, q queryer) (map[int64]appliedRecord, []int64, int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT version, name, checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("read migration ledger: %w", err)
	}
	defer rows.Close()
	applied := map[int64]appliedRecord{}
	var versions []int64
	var highest int64
	for rows.Next() {
		var version int64
		var record appliedRecord
		if err := rows.Scan(&version, &record.name, &record.checksum); err != nil {
			return nil, nil, 0, fmt.Errorf("scan migration ledger: %w", err)
		}
		applied[version] = record
		versions = append(versions, version)
		if version > highest {
			highest = version
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, 0, fmt.Errorf("read migration ledger: %w", err)
	}
	return applied, versions, highest, nil
}
