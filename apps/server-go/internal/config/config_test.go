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

	if cfg.Mode != ModeDevelopment {
		t.Errorf("Mode = %q, want %q", cfg.Mode, ModeDevelopment)
	}
	if cfg.Address != "127.0.0.1:4401" {
		t.Errorf("Address = %q, want %q", cfg.Address, "127.0.0.1:4401")
	}
	wantDB := filepath.Join(worktreeRoot, "data", "technews.db")
	if cfg.DatabasePath != wantDB {
		t.Errorf("DatabasePath = %q, want worktree-local path %q", cfg.DatabasePath, wantDB)
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
