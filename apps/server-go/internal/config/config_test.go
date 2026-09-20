package config

import (
	"os"
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

func TestLoadRejectsReservedPorts(t *testing.T) {
	tests := []struct {
		name    string
		mode    Mode
		address string
	}{
		{name: "dashboard port in development", mode: ModeDevelopment, address: "127.0.0.1:3001"},
		{name: "dashboard port in production", mode: ModeProduction, address: "0.0.0.0:3001"},
		{name: "dashboard port alias", mode: ModeDevelopment, address: ":03001"},
		{name: "other reserved port in development", mode: ModeDevelopment, address: "localhost:3002"},
		{name: "other reserved port in production", mode: ModeProduction, address: "[::1]:3002"},
		{name: "other reserved port with leading zeroes", mode: ModeProduction, address: "127.0.0.1:03002"},
		{name: "production port in development", mode: ModeDevelopment, address: "127.0.0.1:4001"},
		{name: "production port alias in development", mode: ModeDevelopment, address: "0.0.0.0:04001"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(mapLookup(map[string]string{
				"APP_ENV":     string(tt.mode),
				"SERVER_ADDR": tt.address,
			}), t.TempDir())
			if err == nil {
				t.Fatal("Load() error = nil, want reserved port error")
			}
		})
	}
}

func TestLoadRejectsProductionDatabaseAliasesOutsideProductionMode(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	relativeAlias, err := filepath.Rel(cwd, ProductionDatabasePath)
	if err != nil {
		t.Fatalf("Rel() error = %v", err)
	}

	symlinkAlias := filepath.Join(t.TempDir(), "production.db")
	if err := os.Symlink(ProductionDatabasePath, symlinkAlias); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	tests := []struct {
		name string
		path string
	}{
		{name: "canonical path", path: ProductionDatabasePath},
		{name: "relative alias", path: relativeAlias},
		{name: "symlink alias", path: symlinkAlias},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(mapLookup(map[string]string{"DATABASE_PATH": tt.path}), t.TempDir())
			if err == nil {
				t.Fatal("Load() error = nil, want production database safety error")
			}
		})
	}
}

func TestLoadCanonicalizesNonexistentDevelopmentPathFromExistingAncestor(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "not", "created", "development.db")

	cfg, err := Load(mapLookup(map[string]string{"DATABASE_PATH": path}), root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DatabasePath != path {
		t.Errorf("DatabasePath = %q, want %q", cfg.DatabasePath, path)
	}
}

func TestLoadFailsClosedWhenDatabasePathCannotBeCanonicalized(t *testing.T) {
	root := t.TempDir()
	loop := filepath.Join(root, "loop")
	if err := os.Symlink("loop", loop); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	_, err := Load(mapLookup(map[string]string{
		"DATABASE_PATH": filepath.Join(loop, "development.db"),
	}), root)
	if err == nil {
		t.Fatal("Load() error = nil, want canonicalization error")
	}
}

func TestLoadAllowsProductionResourcesOnlyInExplicitProductionMode(t *testing.T) {
	wantAddress := "0.0.0.0:4001"
	cfg, err := Load(mapLookup(map[string]string{
		"APP_ENV":       string(ModeProduction),
		"SERVER_ADDR":   wantAddress,
		"DATABASE_PATH": ProductionDatabasePath,
	}), t.TempDir())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Mode != ModeProduction {
		t.Errorf("Mode = %q, want %q", cfg.Mode, ModeProduction)
	}
	if cfg.Address != wantAddress {
		t.Errorf("Address = %q, want %q", cfg.Address, wantAddress)
	}
	if cfg.DatabasePath != ProductionDatabasePath {
		t.Errorf("DatabasePath = %q, want %q", cfg.DatabasePath, ProductionDatabasePath)
	}
}

func TestLoadRejectsInvalidMode(t *testing.T) {
	_, err := Load(mapLookup(map[string]string{"APP_ENV": "staging"}), t.TempDir())
	if err == nil {
		t.Fatal("Load() error = nil, want unsupported APP_ENV error")
	}
}

func TestLoadRejectsNilLookup(t *testing.T) {
	_, err := Load(nil, t.TempDir())
	if err == nil {
		t.Fatal("Load() error = nil, want lookup error")
	}
}

func TestLoadRejectsEmptyWorktreeRoot(t *testing.T) {
	_, err := Load(func(string) string { return "" }, "")
	if err == nil {
		t.Fatal("Load() error = nil, want worktree root error")
	}
}

func mapLookup(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}
