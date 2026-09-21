package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	tests := []struct {
		name string
		path string
	}{
		{name: "canonical path", path: ProductionDatabasePath},
		{name: "relative alias", path: relativeAlias},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(mapLookup(map[string]string{"DATABASE_PATH": tt.path}), t.TempDir())
			if !errors.Is(err, ErrProductionDatabaseAlias) {
				t.Fatalf("Load() error = %v, want errors.Is(err, ErrProductionDatabaseAlias)", err)
			}
		})
	}

	t.Run("symlink alias", func(t *testing.T) {
		if _, err := os.Stat(ProductionDatabasePath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				t.Skip("production database does not exist")
			}
			t.Fatalf("Stat(production database) error = %v", err)
		}

		symlinkAlias := filepath.Join(t.TempDir(), "production.db")
		if err := os.Symlink(ProductionDatabasePath, symlinkAlias); err != nil {
			t.Skipf("symlink creation is unsupported: %v", err)
		}

		_, err := Load(mapLookup(map[string]string{"DATABASE_PATH": symlinkAlias}), t.TempDir())
		if !errors.Is(err, ErrProductionDatabaseAlias) {
			t.Fatalf("Load() error = %v, want errors.Is(err, ErrProductionDatabaseAlias)", err)
		}
	})
}

func TestLoadRejectsCaseVariantOfProductionDatabaseOnCaseInsensitiveFilesystem(t *testing.T) {
	caseVariant := strings.Replace(ProductionDatabasePath, "/Users/", "/users/", 1)
	if caseVariant == ProductionDatabasePath {
		t.Fatal("test setup did not create a case-variant path")
	}

	productionInfo, err := os.Stat(ProductionDatabasePath)
	if err != nil {
		t.Skipf("production database is unavailable: %v", err)
	}
	variantInfo, err := os.Stat(caseVariant)
	if err != nil {
		t.Skipf("filesystem does not resolve the case variant: %v", err)
	}
	if !os.SameFile(productionInfo, variantInfo) {
		t.Skip("case-variant path does not resolve to the production database")
	}

	_, err = Load(mapLookup(map[string]string{"DATABASE_PATH": caseVariant}), t.TempDir())
	if !errors.Is(err, ErrProductionDatabaseAlias) {
		t.Fatalf("Load() error = %v, want errors.Is(err, ErrProductionDatabaseAlias)", err)
	}
}

func TestPathsEquivalentDetectsHardLinks(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "original.db")
	if err := os.WriteFile(original, []byte("temporary test database"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	hardLink := filepath.Join(root, "hard-link.db")
	if err := os.Link(original, hardLink); err != nil {
		t.Skipf("hard-link creation is unsupported: %v", err)
	}

	equivalent, err := pathsEquivalent(original, hardLink)
	if err != nil {
		t.Fatalf("pathsEquivalent() error = %v", err)
	}
	if !equivalent {
		t.Fatal("pathsEquivalent() = false, want true for hard links to the same temporary file")
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
		t.Skipf("symlink creation is unsupported: %v", err)
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

func TestLoadReadsJWTSecretWithoutRequiringIt(t *testing.T) {
	const secret = "synthetic-config-secret"
	cfg, err := Load(mapLookup(map[string]string{"JWT_SECRET": secret}), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.JWTSecret != secret {
		t.Fatal("JWT_SECRET was not loaded")
	}
	without, err := Load(func(string) string { return "" }, t.TempDir())
	if err != nil {
		t.Fatalf("migration-compatible config rejected empty JWT secret: %v", err)
	}
	if without.JWTSecret != "" {
		t.Fatal("empty JWT secret was not preserved")
	}
}

func TestConfigFormattingRedactsJWTSecret(t *testing.T) {
	const secret = "secret-that-must-never-be-formatted"
	cfg := Config{Mode: ModeDevelopment, Address: DefaultAddress, DatabasePath: "/tmp/synthetic.db", JWTSecret: secret}
	for _, formatted := range []string{fmt.Sprint(cfg), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%#v", cfg)} {
		if strings.Contains(formatted, secret) {
			t.Fatalf("formatted config leaked secret: %s", formatted)
		}
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
