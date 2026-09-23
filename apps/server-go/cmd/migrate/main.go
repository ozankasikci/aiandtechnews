package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
)

func main() {
	if err := run(context.Background(), os.Getenv, worktreeRoot); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv func(string) string, findRoot func() (string, error)) (err error) {
	// Production has no path defaults and needs no repository checkout.
	var root string
	if getenv("APP_ENV") != string(config.ModeProduction) {
		if root, err = findRoot(); err != nil {
			return err
		}
	}
	cfg, err := config.Load(getenv, root)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	if cfg.Mode == config.ModeProduction {
		return migrateProduction(ctx, cfg.DatabasePath)
	}
	db, err := database.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	if err := migrate.Run(ctx, db, app.Migrations()); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// migrateProduction only migrates an existing, adopted database: it never
// creates the file or its directory (a wrong DATABASE_PATH would otherwise
// yield a fresh, fully migrated, empty database that the API would serve),
// and it refuses a database without the migration ledger, which only
// cmd/adopt may bring under the ledger.
func migrateProduction(ctx context.Context, path string) (err error) {
	// Check the ledger on a read-only connection first, so a refused
	// database is not touched at all (not even its journal mode).
	if err := checkManaged(ctx, path); err != nil {
		return err
	}
	db, err := database.OpenForServing(ctx, path)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	if err := migrate.Run(ctx, db, app.Migrations()); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

func checkManaged(ctx context.Context, path string) (err error) {
	db, err := database.OpenExisting(ctx, path, true)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	if _, err := migrate.Status(ctx, db, app.Migrations()); err != nil {
		if errors.Is(err, migrate.ErrUnmanagedDatabase) {
			return fmt.Errorf("%s: %w: it has no migration ledger; adopt it with bin/adopt (dry run, then --apply) instead of migrating it", path, err)
		}
		return fmt.Errorf("check migration ledger: %w", err)
	}
	return nil
}

func worktreeRoot() (string, error) {
	current, err := os.Getwd()
	if err != nil {
		return "", err
	}
	current, err = filepath.Abs(current)
	if err != nil {
		return "", err
	}
	for {
		if regular(filepath.Join(current, "pnpm-workspace.yaml")) && regular(filepath.Join(current, "apps", "server-go", "go.mod")) {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("locate worktree root")
		}
		current = parent
	}
}

func regular(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
