package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "api: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	root, err := runtimeWorktreeRoot()
	if err != nil {
		return err
	}
	cfg, err := config.Load(os.Getenv, root)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	application, err := app.New(cfg, logger)
	if err != nil {
		return fmt.Errorf("compose application: %w", err)
	}
	logger.Info("starting HTTP server", "address", application.Address(), "mode", cfg.Mode)
	if err := application.Run(ctx); err != nil {
		return fmt.Errorf("run application: %w", err)
	}
	logger.Info("HTTP server stopped")
	return nil
}

func runtimeWorktreeRoot() (string, error) {
	cwd, cwdErr := os.Getwd()
	if cwdErr == nil {
		if root, err := findWorktreeRoot(cwd); err == nil {
			return root, nil
		}
	}

	executable, executableErr := os.Executable()
	if executableErr == nil {
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
		if root, err := findWorktreeRoot(filepath.Dir(executable)); err == nil {
			return root, nil
		}
	}
	return "", errors.New("locate worktree root: expected pnpm-workspace.yaml and apps/server-go/go.mod in an ancestor")
}

func findWorktreeRoot(start string) (string, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("absolute start path: %w", err)
	}
	if info, statErr := os.Stat(current); statErr == nil && !info.IsDir() {
		current = filepath.Dir(current)
	}

	for {
		workspace := filepath.Join(current, "pnpm-workspace.yaml")
		module := filepath.Join(current, "apps", "server-go", "go.mod")
		if regularFile(workspace) && regularFile(module) {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return "", fmt.Errorf("worktree root not found from %q", start)
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
