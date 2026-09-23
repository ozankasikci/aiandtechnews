// Command devseed migrates the development database and seeds a login plus
// newsroom candidates. It refuses to run in production mode.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/devseed"
)

const defaultPassword = "dev-password"

func main() {
	if err := run(context.Background()); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "devseed: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) (err error) {
	// Production has no path defaults and needs no repository checkout.
	var root string
	if os.Getenv("APP_ENV") != string(config.ModeProduction) {
		if root, err = worktreeRoot(); err != nil {
			return err
		}
	}
	cfg, err := config.Load(os.Getenv, root)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	if cfg.Mode == config.ModeProduction {
		return errors.New("refusing to seed development fixtures with APP_ENV=production")
	}
	db, err := database.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	if err := migrate.Run(ctx, db, app.Migrations()); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	password := os.Getenv("DEV_PASSWORD")
	if password == "" {
		password = defaultPassword
	}
	summary, err := devseed.Seed(ctx, db, time.Now(), password)
	if err != nil {
		return err
	}
	fmt.Printf("Seeded %s (%d new candidates)\nLogin: %s / %s\n", cfg.DatabasePath, summary.Inserted, summary.Email, password)
	return nil
}

func worktreeRoot() (string, error) {
	current, err := os.Getwd()
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
