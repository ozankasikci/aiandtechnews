package migrate

import (
	"context"
	"database/sql"
	"errors"
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
