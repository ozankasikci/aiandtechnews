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
	cfg, err := Load(mapLookup(map[string]string{"APP_ENV": string(ModeProduction)}), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UploadsDir != "" {
		t.Fatalf("production UploadsDir = %q, want no default", cfg.UploadsDir)
	}
	dir := filepath.Join(t.TempDir(), "uploads")
	cfg, err = Load(mapLookup(map[string]string{"APP_ENV": string(ModeProduction), "UPLOADS_DIR": dir}), t.TempDir())
	if err != nil || cfg.UploadsDir != dir {
		t.Fatalf("cfg = %+v, err = %v", cfg, err)
	}
}

func TestLoadRejectsRelativeUploadsDir(t *testing.T) {
	_, err := Load(mapLookup(map[string]string{"UPLOADS_DIR": "data/uploads"}), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "UPLOADS_DIR must be an absolute path") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadRejectsProductionUploadsDirOutsideProduction(t *testing.T) {
	_, err := Load(mapLookup(map[string]string{"UPLOADS_DIR": ProductionUploadsDir}), t.TempDir())
	if !errors.Is(err, ErrProductionUploadsAlias) {
		t.Fatalf("Load() error = %v, want %v", err, ErrProductionUploadsAlias)
	}
	_, err = Load(mapLookup(map[string]string{"UPLOADS_DIR": ProductionUploadsDir + "/"}), t.TempDir())
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
