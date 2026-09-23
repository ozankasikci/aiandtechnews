package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// productionTree creates a temporary production data root with the marker,
// a database directory and an uploads directory, and returns the root, the
// database path and the uploads directory.
func productionTree(t *testing.T) (root, databasePath, uploads string) {
	t.Helper()
	root = t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ProductionMarker), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	databasePath = filepath.Join(root, "data", "technews.db")
	uploads = filepath.Join(root, "uploads")
	for _, dir := range []string{filepath.Dir(databasePath), uploads} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root, databasePath, uploads
}

func productionEnv(databasePath, uploads string) map[string]string {
	return map[string]string{
		"APP_ENV":       string(ModeProduction),
		"DATABASE_PATH": databasePath,
		"UPLOADS_DIR":   uploads,
	}
}

func TestProductionLoadsExplicitMarkedPathsWithoutAWorktree(t *testing.T) {
	_, databasePath, uploads := productionTree(t)
	cfg, err := Load(mapLookup(productionEnv(databasePath, uploads)), "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Mode != ModeProduction || cfg.DatabasePath != databasePath || cfg.UploadsDir != uploads {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.Address != DefaultAddress {
		t.Fatalf("Address = %q, want default %q", cfg.Address, DefaultAddress)
	}
}

func TestDevelopmentStillRequiresAWorktreeRoot(t *testing.T) {
	if _, err := Load(mapLookup(map[string]string{}), ""); err == nil {
		t.Fatal("Load() error = nil, want worktree root error in development")
	}
}

func TestProductionRequiresExplicitAbsoluteDatabasePath(t *testing.T) {
	_, databasePath, uploads := productionTree(t)
	for name, value := range map[string]string{
		"missing":  "",
		"relative": "data/technews.db",
	} {
		t.Run(name, func(t *testing.T) {
			env := productionEnv(value, uploads)
			_, err := Load(mapLookup(env), t.TempDir())
			if err == nil || !strings.Contains(err.Error(), "DATABASE_PATH") {
				t.Fatalf("Load() error = %v, want a DATABASE_PATH error", err)
			}
		})
	}
	if _, err := Load(mapLookup(productionEnv(databasePath, uploads)), t.TempDir()); err != nil {
		t.Fatalf("explicit absolute DATABASE_PATH rejected: %v", err)
	}
}

func TestProductionRequiresExplicitAbsoluteUploadsDir(t *testing.T) {
	_, databasePath, _ := productionTree(t)
	for name, value := range map[string]string{
		"missing":  "",
		"relative": "uploads",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(mapLookup(productionEnv(databasePath, value)), t.TempDir())
			if err == nil || !strings.Contains(err.Error(), "UPLOADS_DIR") {
				t.Fatalf("Load() error = %v, want an UPLOADS_DIR error", err)
			}
		})
	}
}

func TestProductionRequiresTheMarkerAboveBothPaths(t *testing.T) {
	t.Run("no marker anywhere", func(t *testing.T) {
		root := t.TempDir()
		env := productionEnv(filepath.Join(root, "data", "technews.db"), filepath.Join(root, "uploads"))
		_, err := Load(mapLookup(env), "")
		if !errors.Is(err, ErrProductionMarkerMissing) {
			t.Fatalf("Load() error = %v, want %v", err, ErrProductionMarkerMissing)
		}
	})
	t.Run("marker only covers the database", func(t *testing.T) {
		root := t.TempDir()
		data := filepath.Join(root, "data")
		if err := os.MkdirAll(data, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(data, ProductionMarker), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		env := productionEnv(filepath.Join(data, "technews.db"), filepath.Join(root, "uploads"))
		_, err := Load(mapLookup(env), "")
		if !errors.Is(err, ErrProductionMarkerMissing) || !strings.Contains(err.Error(), "UPLOADS_DIR") {
			t.Fatalf("Load() error = %v, want a missing marker error for UPLOADS_DIR", err)
		}
	})
	t.Run("marker in a sibling directory", func(t *testing.T) {
		root := t.TempDir()
		sibling := filepath.Join(root, "other")
		if err := os.MkdirAll(sibling, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sibling, ProductionMarker), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		env := productionEnv(filepath.Join(root, "data", "technews.db"), filepath.Join(root, "uploads"))
		if _, err := Load(mapLookup(env), ""); !errors.Is(err, ErrProductionMarkerMissing) {
			t.Fatalf("Load() error = %v, want %v", err, ErrProductionMarkerMissing)
		}
	})
}

func TestProductionAllowsAnyPort(t *testing.T) {
	_, databasePath, uploads := productionTree(t)
	for _, address := range []string{"127.0.0.1:3001", "127.0.0.1:3002", "127.0.0.1:4001", "0.0.0.0:8080", "127.0.0.1:4402"} {
		env := productionEnv(databasePath, uploads)
		env["SERVER_ADDR"] = address
		cfg, err := Load(mapLookup(env), "")
		if err != nil {
			t.Errorf("SERVER_ADDR %q rejected in production: %v", address, err)
			continue
		}
		if cfg.Address != address {
			t.Errorf("Address = %q, want %q", cfg.Address, address)
		}
	}
}

func TestDevelopmentRefusesPathsUnderAProductionMarker(t *testing.T) {
	root, databasePath, uploads := productionTree(t)
	devUploads := filepath.Join(t.TempDir(), "uploads")
	devDatabase := filepath.Join(t.TempDir(), "dev.db")

	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	tests := []struct {
		name     string
		database string
		uploads  string
		want     error
	}{
		{name: "database in the marked root", database: filepath.Join(root, "technews.db"), uploads: devUploads, want: ErrProductionDatabaseAlias},
		{name: "database below the marked root", database: databasePath, uploads: devUploads, want: ErrProductionDatabaseAlias},
		{name: "nonexistent database below the marked root", database: filepath.Join(root, "new", "deep", "technews.db"), uploads: devUploads, want: ErrProductionDatabaseAlias},
		{name: "database through a symlinked alias", database: filepath.Join(alias, "data", "technews.db"), uploads: devUploads, want: ErrProductionDatabaseAlias},
		{name: "uploads below the marked root", database: devDatabase, uploads: uploads, want: ErrProductionUploadsAlias},
		{name: "uploads is the marked root", database: devDatabase, uploads: root, want: ErrProductionUploadsAlias},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(mapLookup(map[string]string{"DATABASE_PATH": tt.database, "UPLOADS_DIR": tt.uploads}), t.TempDir())
			if !errors.Is(err, tt.want) {
				t.Fatalf("Load() error = %v, want %v", err, tt.want)
			}
		})
	}

	if _, err := Load(mapLookup(map[string]string{"DATABASE_PATH": devDatabase, "UPLOADS_DIR": devUploads}), t.TempDir()); err != nil {
		t.Fatalf("unmarked development paths rejected: %v", err)
	}
}

func TestDevelopmentRefusesTheEnvironmentProductionDatabaseGuard(t *testing.T) {
	root := t.TempDir()
	production := filepath.Join(root, "prod", "technews.db")
	if err := os.MkdirAll(filepath.Dir(production), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(production, []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(root, "link.db")
	if err := os.Symlink(production, symlink); err != nil {
		t.Fatal(err)
	}
	hardLink := filepath.Join(root, "hard.db")
	if err := os.Link(production, hardLink); err != nil {
		t.Skipf("hard links unsupported: %v", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, production)
	if err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{"same path": production, "relative": relative, "symlink": symlink, "hard link": hardLink} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(mapLookup(map[string]string{"DATABASE_PATH": path, "PRODUCTION_DATABASE_PATH": production}), t.TempDir())
			if !errors.Is(err, ErrProductionDatabaseAlias) {
				t.Fatalf("Load() error = %v, want %v", err, ErrProductionDatabaseAlias)
			}
		})
	}
	other := filepath.Join(root, "dev.db")
	if _, err := Load(mapLookup(map[string]string{"DATABASE_PATH": other, "PRODUCTION_DATABASE_PATH": production}), t.TempDir()); err != nil {
		t.Fatalf("unrelated development database rejected: %v", err)
	}
}

func TestDevelopmentRefusesTheEnvironmentProductionUploadsGuard(t *testing.T) {
	root := t.TempDir()
	production := filepath.Join(root, "prod", "uploads")
	for name, dir := range map[string]string{
		"same directory": production,
		"nested":         filepath.Join(production, "nested"),
		"ancestor":       filepath.Dir(production),
		"trailing slash": production + "/",
	} {
		t.Run(name, func(t *testing.T) {
			env := map[string]string{"DATABASE_PATH": filepath.Join(t.TempDir(), "dev.db"), "UPLOADS_DIR": dir, "PRODUCTION_UPLOADS_DIR": production}
			if _, err := Load(mapLookup(env), t.TempDir()); !errors.Is(err, ErrProductionUploadsAlias) {
				t.Fatalf("Load() error = %v, want %v", err, ErrProductionUploadsAlias)
			}
		})
	}
	sibling := map[string]string{"DATABASE_PATH": filepath.Join(t.TempDir(), "dev.db"), "UPLOADS_DIR": production + "-copy", "PRODUCTION_UPLOADS_DIR": production}
	if _, err := Load(mapLookup(sibling), t.TempDir()); err != nil {
		t.Fatalf("name-prefix sibling rejected: %v", err)
	}
}

func TestProductionIgnoresTheDevelopmentGuards(t *testing.T) {
	_, databasePath, uploads := productionTree(t)
	env := productionEnv(databasePath, uploads)
	env["PRODUCTION_DATABASE_PATH"] = databasePath
	env["PRODUCTION_UPLOADS_DIR"] = uploads
	if _, err := Load(mapLookup(env), ""); err != nil {
		t.Fatalf("production rejected its own guarded paths: %v", err)
	}
}
