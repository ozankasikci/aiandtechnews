// Package database provides the application's isolated database adapters.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const driverName = "sqlite"

// Open opens a file-backed SQLite database with the connection-level settings
// required by the application. The returned pool owns at most one connection,
// ensuring that every operation observes the same PRAGMA configuration.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if ctx == nil {
		return nil, errors.New("open sqlite: nil context")
	}
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("open sqlite: blank path")
	}
	lower := strings.ToLower(strings.TrimSpace(path))
	if lower == ":memory:" || strings.HasPrefix(lower, "file:") {
		return nil, fmt.Errorf("open sqlite: path %q is not a filesystem path", path)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: resolve path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o750); err != nil {
		return nil, fmt.Errorf("open sqlite: create parent: %w", err)
	}

	u := &url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}
	query := url.Values{}
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "foreign_keys(ON)")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "synchronous(NORMAL)")
	u.RawQuery = query.Encode()

	db, err := sql.Open(driverName, u.String())
	if err != nil {
		return nil, fmt.Errorf("open sqlite: create pool: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("open sqlite: ping: %w", err), db.Close())
	}
	return db, nil
}
