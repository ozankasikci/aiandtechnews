package config

import (
	"path/filepath"
	"testing"
)

func TestLoadDefaultsAreDevelopmentSafe(t *testing.T) {
	worktreeRoot := t.TempDir()

	cfg, err := Load(func(string) string { return "" }, worktreeRoot)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	wantDB := filepath.Join(worktreeRoot, "data", "technews.db")
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "mode", got: string(cfg.Mode), want: string(ModeDevelopment)},
		{name: "address", got: cfg.Address, want: DefaultAddress},
		{name: "database path", got: cfg.DatabasePath, want: wantDB},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("default = %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestLoadRejectsInvalidServerAddress(t *testing.T) {
	tests := []struct {
		name    string
		address string
	}{
		{name: "malformed address", address: "not-an-address"},
		{name: "missing port", address: "127.0.0.1"},
		{name: "non-numeric port", address: "127.0.0.1:http"},
		{name: "negative port", address: "127.0.0.1:-1"},
		{name: "zero port", address: "127.0.0.1:0"},
		{name: "out-of-range port", address: "127.0.0.1:65536"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(mapLookup(map[string]string{"SERVER_ADDR": tt.address}), t.TempDir())
			if err == nil {
				t.Fatal("Load() error = nil, want invalid SERVER_ADDR error")
			}
		})
	}
}

func TestLoadRejectsProductionResourcesOutsideProductionMode(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{
			name: "production port",
			env:  map[string]string{"SERVER_ADDR": "127.0.0.1:4001"},
		},
		{
			name: "production port on another interface",
			env:  map[string]string{"SERVER_ADDR": "0.0.0.0:4001"},
		},
		{
			name: "production port with leading zeroes",
			env:  map[string]string{"SERVER_ADDR": "127.0.0.1:04001"},
		},
		{
			name: "production database",
			env:  map[string]string{"DATABASE_PATH": ProductionDatabasePath},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(mapLookup(tt.env), t.TempDir())
			if err == nil {
				t.Fatal("Load() error = nil, want safety validation error")
			}
		})
	}
}

func TestLoadAllowsProductionResourcesOnlyInExplicitProductionMode(t *testing.T) {
	cfg, err := Load(mapLookup(map[string]string{
		"APP_ENV":       string(ModeProduction),
		"SERVER_ADDR":   "0.0.0.0:4001",
		"DATABASE_PATH": ProductionDatabasePath,
	}), t.TempDir())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Mode != ModeProduction {
		t.Errorf("Mode = %q, want %q", cfg.Mode, ModeProduction)
	}
}

func mapLookup(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}
