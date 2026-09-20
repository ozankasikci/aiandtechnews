package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

type Mode string

const (
	ModeDevelopment Mode = "development"
	ModeProduction  Mode = "production"

	DefaultAddress         = "127.0.0.1:4401"
	ProductionDatabasePath = "/Users/ozan/Projects/technews/apps/server/data/technews.db"
)

type Config struct {
	Mode         Mode
	Address      string
	DatabasePath string
}

// Load builds configuration from environment values supplied by lookup.
// worktreeRoot is explicit so callers and tests control where development data lives.
func Load(lookup func(string) string, worktreeRoot string) (Config, error) {
	if lookup == nil {
		return Config{}, errors.New("environment lookup is required")
	}
	if worktreeRoot == "" {
		return Config{}, errors.New("worktree root is required")
	}

	cfg := Config{
		Mode:         ModeDevelopment,
		Address:      DefaultAddress,
		DatabasePath: filepath.Join(worktreeRoot, "data", "technews.db"),
	}
	if value := lookup("APP_ENV"); value != "" {
		cfg.Mode = Mode(value)
	}
	if value := lookup("SERVER_ADDR"); value != "" {
		cfg.Address = value
	}
	if value := lookup("DATABASE_PATH"); value != "" {
		cfg.DatabasePath = value
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.Mode != ModeDevelopment && c.Mode != ModeProduction {
		return fmt.Errorf("unsupported APP_ENV %q", c.Mode)
	}

	_, port, err := net.SplitHostPort(c.Address)
	if err != nil {
		return fmt.Errorf("invalid SERVER_ADDR %q: %w", c.Address, err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("invalid SERVER_ADDR port %q", port)
	}
	if portNumber == 3001 || portNumber == 3002 {
		return fmt.Errorf("port %d is reserved and cannot be used by the Go API", portNumber)
	}
	if c.Mode != ModeProduction && portNumber == 4001 {
		return errors.New("port 4001 is reserved for production; set APP_ENV=production explicitly to use it")
	}
	if c.Mode != ModeProduction {
		databasePath, err := canonicalPath(c.DatabasePath)
		if err != nil {
			return fmt.Errorf("canonicalize DATABASE_PATH: %w", err)
		}
		productionDatabasePath, err := canonicalPath(ProductionDatabasePath)
		if err != nil {
			return fmt.Errorf("canonicalize production database path: %w", err)
		}
		if databasePath == productionDatabasePath {
			return errors.New("production database path is forbidden outside APP_ENV=production")
		}
	}
	return nil
}

// canonicalPath resolves symlinks in the deepest existing ancestor, then
// reattaches any nonexistent suffix. This permits new development databases
// while ensuring aliases of existing protected paths compare equal.
func canonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	existing := filepath.Clean(absolute)
	var suffix []string
	for {
		_, err = os.Lstat(existing)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}

		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("no existing ancestor for %q", path)
		}
		suffix = append([]string{filepath.Base(existing)}, suffix...)
		existing = parent
	}

	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	parts := append([]string{resolved}, suffix...)
	return filepath.Clean(filepath.Join(parts...)), nil
}
