package migrate

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

type failingLedgerQueryer struct {
	err error
}

func (q failingLedgerQueryer) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, q.err
}

func (q failingLedgerQueryer) QueryRowContext(context.Context, string, ...any) *sql.Row {
	return &sql.Row{}
}

func TestValidateLedgerPreservesSentinelAndDatabaseCause(t *testing.T) {
	cause := errors.New("database inspection failed")
	err := validateLedger(context.Background(), failingLedgerQueryer{err: cause})
	if !errors.Is(err, ErrIncompatibleLedger) {
		t.Fatalf("validateLedger() error = %v, want ErrIncompatibleLedger", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("validateLedger() error = %v, want underlying database cause", err)
	}
}

func TestValidateDescriptorSQLTreatsUTF8BOMAsStatementWhitespace(t *testing.T) {
	for name, sqlText := range map[string]string{
		"file start":                 "\xef\xbb\xbfATTACH DATABASE 'aux.db' AS aux",
		"after semicolon whitespace": "; \n	\xef\xbb\xbfPRAGMA foreign_keys=OFF",
		"between comments":           "/* prefix */\xef\xbb\xbf-- line\nDETACH DATABASE aux",
	} {
		t.Run(name, func(t *testing.T) {
			err := validateDescriptorSQL(sqlText)
			if err == nil || !strings.Contains(err.Error(), "transaction-unsafe keyword") {
				t.Fatalf("validateDescriptorSQL() error = %v, want unsafe statement error", err)
			}
		})
	}
}

func TestValidateDescriptorSQLMalformedPartialBOMBoundaries(t *testing.T) {
	for name, partial := range map[string]string{
		"first byte":      "\xef",
		"first two bytes": "\xef\xbb",
	} {
		t.Run(name+" before safe SQL", func(t *testing.T) {
			if err := validateDescriptorSQL("; " + partial + "CREATE TABLE safe(id INTEGER)"); err != nil {
				t.Fatalf("validateDescriptorSQL() error = %v, want SQLite to diagnose malformed BOM", err)
			}
		})
		t.Run(name+" cannot hide unsafe SQL", func(t *testing.T) {
			err := validateDescriptorSQL("; " + partial + "ATTACH DATABASE 'aux.db' AS aux")
			if err == nil || !strings.Contains(err.Error(), "transaction-unsafe keyword ATTACH") {
				t.Fatalf("validateDescriptorSQL() error = %v, want unsafe ATTACH error", err)
			}
		})
	}
}
