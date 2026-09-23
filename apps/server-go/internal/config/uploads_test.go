package config

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaultsUploadsDirUnderWorktreeData(t *testing.T) {
	root := t.TempDir()
	cfg, err := Load(func(string) string { return "" }, root)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "data", "uploads"); cfg.UploadsDir != want {
		t.Fatalf("UploadsDir = %q, want %q", cfg.UploadsDir, want)
	}
}

func TestLoadReadsExplicitUploadsDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "media")
	cfg, err := Load(mapLookup(map[string]string{"UPLOADS_DIR": dir}), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UploadsDir != dir {
		t.Fatalf("UploadsDir = %q, want %q", cfg.UploadsDir, dir)
	}
}

func TestLoadHasNoDefaultUploadsDirInProduction(t *testing.T) {
	_, databasePath, uploads := productionTree(t)
	_, err := Load(mapLookup(map[string]string{"APP_ENV": string(ModeProduction), "DATABASE_PATH": databasePath}), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "UPLOADS_DIR") {
		t.Fatalf("Load() error = %v, want UPLOADS_DIR to be required", err)
	}
	cfg, err := Load(mapLookup(productionEnv(databasePath, uploads)), t.TempDir())
	if err != nil || cfg.UploadsDir != uploads {
		t.Fatalf("cfg = %+v, err = %v", cfg, err)
	}
}

func TestLoadRejectsRelativeUploadsDir(t *testing.T) {
	_, err := Load(mapLookup(map[string]string{"UPLOADS_DIR": "data/uploads"}), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "UPLOADS_DIR must be an absolute path") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadRejectsLegacyNodeUploadsDirOutsideProduction(t *testing.T) {
	_, err := Load(mapLookup(map[string]string{"UPLOADS_DIR": LegacyNodeUploadsDir}), t.TempDir())
	if !errors.Is(err, ErrProductionUploadsAlias) {
		t.Fatalf("Load() error = %v, want %v", err, ErrProductionUploadsAlias)
	}
	_, err = Load(mapLookup(map[string]string{"UPLOADS_DIR": LegacyNodeUploadsDir + "/"}), t.TempDir())
	if !errors.Is(err, ErrProductionUploadsAlias) {
		t.Fatalf("trailing slash alias error = %v", err)
	}
}

func TestValidateAllowsEmptyUploadsDirForHealthOnlyComposition(t *testing.T) {
	cfg := Config{Mode: ModeDevelopment, Address: DefaultAddress, DatabasePath: filepath.Join(t.TempDir(), "dev.db")}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestConfigFormattingIncludesUploadsDir(t *testing.T) {
	cfg := Config{Mode: ModeDevelopment, Address: DefaultAddress, DatabasePath: "/tmp/synthetic.db", UploadsDir: "/tmp/synthetic-uploads"}
	if !strings.Contains(cfg.String(), `UploadsDir:"/tmp/synthetic-uploads"`) {
		t.Fatalf("String() = %s", cfg.String())
	}
}

func TestValidateRejectsAnUploadsDirThatContainsTheDatabase(t *testing.T) {
	root := t.TempDir()
	for name, tc := range map[string]struct{ database, uploads string }{
		"database directory":   {filepath.Join(root, "data", "technews.db"), filepath.Join(root, "data")},
		"ancestor of database": {filepath.Join(root, "data", "db", "technews.db"), filepath.Join(root, "data")},
		"trailing slash":       {filepath.Join(root, "data", "technews.db"), filepath.Join(root, "data") + "/"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := Config{Mode: ModeDevelopment, Address: DefaultAddress, DatabasePath: tc.database, UploadsDir: tc.uploads}
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "UPLOADS_DIR must not contain DATABASE_PATH") {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
	sibling := Config{Mode: ModeDevelopment, Address: DefaultAddress, DatabasePath: filepath.Join(root, "data", "technews.db"), UploadsDir: filepath.Join(root, "data", "uploads")}
	if err := sibling.Validate(); err != nil {
		t.Fatalf("sibling uploads directory rejected: %v", err)
	}
	prefixOnly := Config{Mode: ModeDevelopment, Address: DefaultAddress, DatabasePath: filepath.Join(root, "database", "technews.db"), UploadsDir: filepath.Join(root, "data")}
	if err := prefixOnly.Validate(); err != nil {
		t.Fatalf("name-prefix sibling rejected: %v", err)
	}
}

func TestValidateRejectsOverlapWithTheLegacyNodeUploadsDirOutsideProduction(t *testing.T) {
	for _, dir := range []string{
		LegacyNodeUploadsDir,
		filepath.Dir(LegacyNodeUploadsDir),
		"/Users/ozan/Projects/technews",
		LegacyNodeUploadsDir + "/nested",
	} {
		cfg := Config{Mode: ModeDevelopment, Address: DefaultAddress, DatabasePath: filepath.Join(t.TempDir(), "dev.db"), UploadsDir: dir}
		if err := cfg.Validate(); !errors.Is(err, ErrProductionUploadsAlias) {
			t.Errorf("UploadsDir %q error = %v, want %v", dir, err, ErrProductionUploadsAlias)
		}
	}
	sibling := Config{Mode: ModeDevelopment, Address: DefaultAddress, DatabasePath: filepath.Join(t.TempDir(), "dev.db"), UploadsDir: LegacyNodeUploadsDir + "-copy"}
	if err := sibling.Validate(); err != nil {
		t.Fatalf("name-prefix sibling of the production directory rejected: %v", err)
	}
}
