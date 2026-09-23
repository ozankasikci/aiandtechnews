// Command adopt verifies a database created by the Node server against the Go
// migrations and, with --apply, brings it under the migration ledger.
//
// It reads DATABASE_PATH (and the rest of the configuration) like cmd/api,
// so the same guards apply: a development run refuses a production-marked
// database, and APP_ENV=production requires the marked explicit paths.
//
//	adopt           read-only dry run: report, then rehearse on a copy
//	adopt --apply   back up with VACUUM INTO, then adopt
//
// Exit status: 0 when the database is adoptable, adopted, or already
// managed; 1 when it is incompatible or an operation failed; 2 for usage
// errors. See docs/cutover.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/adopt"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, worktreeRoot, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, getenv func(string) string, findRoot func() (string, error), stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("adopt", flag.ContinueOnError)
	flags.SetOutput(stderr)
	apply := flags.Bool("apply", false, "back up the database with VACUUM INTO and adopt it (default: read-only dry run)")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: adopt [--apply]\n\nVerifies DATABASE_PATH against the Go migrations. Without --apply nothing is written.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		flags.Usage()
		return 2
	}

	var root string
	if getenv("APP_ENV") != string(config.ModeProduction) {
		var err error
		if root, err = findRoot(); err != nil {
			fmt.Fprintf(stderr, "adopt: %v\n", err)
			return 1
		}
	}
	cfg, err := config.Load(getenv, root)
	if err != nil {
		fmt.Fprintf(stderr, "adopt: load configuration: %v\n", err)
		return 1
	}

	result, err := adopt.Run(ctx, adopt.Options{
		DatabasePath: cfg.DatabasePath,
		Apply:        *apply,
		Descriptors:  app.Migrations(),
		Out:          stdout,
	})
	if err != nil {
		fmt.Fprintf(stderr, "adopt: %v\n", err)
		return 1
	}
	if result.Outcome == adopt.OutcomeIncompatible {
		fmt.Fprintln(stderr, "adopt: the database is not compatible with the Go migrations; nothing was changed")
		return 1
	}
	return 0
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
			return "", errors.New("locate worktree root (set APP_ENV=production to run without a checkout)")
		}
		current = parent
	}
}

func regular(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
