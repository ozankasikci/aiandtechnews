package config

import (
	"errors"
	"fmt"
	"net"
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
	if c.Mode != ModeProduction && portNumber == 4001 {
		return errors.New("port 4001 is reserved for production; set APP_ENV=production explicitly to use it")
	}
	if c.Mode != ModeProduction && filepath.Clean(c.DatabasePath) == filepath.Clean(ProductionDatabasePath) {
		return errors.New("production database path is forbidden outside APP_ENV=production")
	}
	return nil
}
