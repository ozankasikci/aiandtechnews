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

var ErrProductionDatabaseAlias = errors.New("production database path is forbidden outside APP_ENV=production")

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
		equivalent, err := pathsEquivalent(c.DatabasePath, ProductionDatabasePath)
		if err != nil {
			return fmt.Errorf("compare DATABASE_PATH with production database: %w", err)
		}
		if equivalent {
			return fmt.Errorf("%w: %q", ErrProductionDatabaseAlias, c.DatabasePath)
		}
	}
	return nil
}

// pathsEquivalent compares canonical names first, then file identity when both
// targets exist. The identity check catches aliases that canonical names do not,
// including hard links and case variants on case-insensitive filesystems.
func pathsEquivalent(first, second string) (bool, error) {
	canonicalFirst, err := canonicalPath(first)
	if err != nil {
		return false, fmt.Errorf("canonicalize first path: %w", err)
	}
	canonicalSecond, err := canonicalPath(second)
	if err != nil {
		return false, fmt.Errorf("canonicalize second path: %w", err)
	}
	if canonicalFirst == canonicalSecond {
		return true, nil
	}

	firstInfo, firstErr := os.Stat(canonicalFirst)
	secondInfo, secondErr := os.Stat(canonicalSecond)
	if firstErr != nil && !errors.Is(firstErr, os.ErrNotExist) {
		return false, fmt.Errorf("stat first path: %w", firstErr)
	}
	if secondErr != nil && !errors.Is(secondErr, os.ErrNotExist) {
		return false, fmt.Errorf("stat second path: %w", secondErr)
	}
	if firstErr != nil || secondErr != nil {
		return false, nil
	}
	return os.SameFile(firstInfo, secondInfo), nil
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
