package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestLoadCollectorSettings(t *testing.T) {
	root := t.TempDir()
	env := map[string]string{"JWT_SECRET": "x", "COLLECTOR_ENABLED": "1", "COLLECTOR_INTERVAL": "10m"}
	cfg, err := Load(mapLookup(env), root)
	if err != nil || !cfg.CollectorEnabled || cfg.CollectorInterval != 10*time.Minute {
		t.Fatalf("cfg = %+v err=%v", cfg, err)
	}

	cfg, err = Load(mapLookup(map[string]string{"JWT_SECRET": "x"}), root)
	if err != nil || cfg.CollectorEnabled || cfg.CollectorInterval != 30*time.Minute {
		t.Fatalf("defaults = %+v err=%v", cfg, err)
	}

	for _, bad := range []string{"soon", "10s"} {
		env["COLLECTOR_INTERVAL"] = bad
		if _, err := Load(mapLookup(env), root); err == nil {
			t.Fatalf("COLLECTOR_INTERVAL=%q should fail", bad)
		}
	}
}

func TestLoadCollectorEnabledAcceptsKnownValuesCaseInsensitively(t *testing.T) {
	root := t.TempDir()
	for _, on := range []string{"1", "true", "True", "YES", "yes"} {
		cfg, err := Load(mapLookup(map[string]string{"JWT_SECRET": "x", "COLLECTOR_ENABLED": on}), root)
		if err != nil || !cfg.CollectorEnabled {
			t.Fatalf("COLLECTOR_ENABLED=%q: cfg = %+v err = %v", on, cfg, err)
		}
	}
	for _, off := range []string{"", "0", "false", "FALSE", "no", "No"} {
		cfg, err := Load(mapLookup(map[string]string{"JWT_SECRET": "x", "COLLECTOR_ENABLED": off}), root)
		if err != nil || cfg.CollectorEnabled {
			t.Fatalf("COLLECTOR_ENABLED=%q: cfg = %+v err = %v", off, cfg, err)
		}
	}
}

func TestLoadRejectsUnrecognizedCollectorEnabledValue(t *testing.T) {
	_, err := Load(mapLookup(map[string]string{"JWT_SECRET": "x", "COLLECTOR_ENABLED": "maybe"}), t.TempDir())
	if err == nil {
		t.Fatal("Load() error = nil, want invalid COLLECTOR_ENABLED error")
	}
}

func TestLoadPublisherDefaultsAreOff(t *testing.T) {
	cfg, err := Load(mapLookup(map[string]string{"JWT_SECRET": "x"}), t.TempDir())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.PublisherEnabled {
		t.Error("PublisherEnabled defaults to true, want false")
	}
	if cfg.PublisherInterval != time.Minute {
		t.Errorf("PublisherInterval = %s, want 1m", cfg.PublisherInterval)
	}
	if cfg.GeminiAPIKey != "" || cfg.GeminiTextModel != "" || cfg.GeminiImageModel != "" || cfg.GeminiVisionModel != "" {
		t.Errorf("Gemini fields not empty by default: %+v", cfg)
	}
	if cfg.S3Prefix != "features" {
		t.Errorf("S3Prefix = %q, want %q", cfg.S3Prefix, "features")
	}
}

func TestLoadPublisherEnabledAcceptsKnownValuesCaseInsensitively(t *testing.T) {
	root := t.TempDir()
	for _, on := range []string{"1", "true", "True", "YES", "yes"} {
		env := publisherEnv()
		env["PUBLISHER_ENABLED"] = on
		cfg, err := Load(mapLookup(env), root)
		if err != nil || !cfg.PublisherEnabled {
			t.Fatalf("PUBLISHER_ENABLED=%q: cfg = %+v err = %v", on, cfg, err)
		}
	}
	for _, off := range []string{"", "0", "false", "FALSE", "no", "No"} {
		env := publisherEnv()
		env["PUBLISHER_ENABLED"] = off
		cfg, err := Load(mapLookup(env), root)
		if err != nil || cfg.PublisherEnabled {
			t.Fatalf("PUBLISHER_ENABLED=%q: cfg = %+v err = %v", off, cfg, err)
		}
	}
}

func TestLoadRejectsUnrecognizedPublisherEnabledValue(t *testing.T) {
	_, err := Load(mapLookup(map[string]string{"JWT_SECRET": "x", "PUBLISHER_ENABLED": "maybe"}), t.TempDir())
	if err == nil {
		t.Fatal("Load() error = nil, want invalid PUBLISHER_ENABLED error")
	}
}

func TestLoadPublisherEnabledWithoutGeminiKeyFails(t *testing.T) {
	env := publisherEnv()
	delete(env, "GEMINI_API_KEY")
	_, err := Load(mapLookup(env), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "GEMINI_API_KEY") {
		t.Fatalf("Load() error = %v, want an error naming GEMINI_API_KEY", err)
	}
}

func TestLoadPublisherEnabledWithoutS3FieldsFails(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{name: "missing AWS_REGION", key: "AWS_REGION"},
		{name: "missing S3_FEATURE_IMAGE_BUCKET", key: "S3_FEATURE_IMAGE_BUCKET"},
		{name: "missing S3_FEATURE_IMAGE_PUBLIC_URL", key: "S3_FEATURE_IMAGE_PUBLIC_URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := publisherEnv()
			delete(env, tt.key)
			_, err := Load(mapLookup(env), t.TempDir())
			if err == nil {
				t.Fatalf("Load() error = nil, want error for missing %s", tt.key)
			}
		})
	}
}

func TestLoadPublisherEnabledRejectsNonHTTPSPublicURL(t *testing.T) {
	env := publisherEnv()
	env["S3_FEATURE_IMAGE_PUBLIC_URL"] = "http://example.invalid"
	_, err := Load(mapLookup(env), t.TempDir())
	if err == nil {
		t.Fatal("Load() error = nil, want error for non-https S3_FEATURE_IMAGE_PUBLIC_URL")
	}
}

func TestLoadPublisherEnabledRejectsShortInterval(t *testing.T) {
	env := publisherEnv()
	env["PUBLISHER_INTERVAL"] = "5s"
	_, err := Load(mapLookup(env), t.TempDir())
	if err == nil {
		t.Fatal("Load() error = nil, want error for PUBLISHER_INTERVAL below 10s")
	}
}

func TestLoadPublisherEnabledWithAllFieldsSucceeds(t *testing.T) {
	env := publisherEnv()
	cfg, err := Load(mapLookup(env), t.TempDir())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.PublisherEnabled {
		t.Fatal("PublisherEnabled = false, want true")
	}
	if cfg.GeminiAPIKey != env["GEMINI_API_KEY"] {
		t.Errorf("GeminiAPIKey = %q, want %q", cfg.GeminiAPIKey, env["GEMINI_API_KEY"])
	}
	if cfg.GeminiTextModel != env["GEMINI_TEXT_MODEL"] || cfg.GeminiImageModel != env["GEMINI_IMAGE_MODEL"] || cfg.GeminiVisionModel != env["GEMINI_VISION_MODEL"] {
		t.Errorf("Gemini model fields = %+v", cfg)
	}
	if cfg.AWSRegion != env["AWS_REGION"] || cfg.S3Bucket != env["S3_FEATURE_IMAGE_BUCKET"] ||
		cfg.S3Prefix != env["S3_FEATURE_IMAGE_PREFIX"] || cfg.S3PublicURL != env["S3_FEATURE_IMAGE_PUBLIC_URL"] {
		t.Errorf("S3 fields = %+v", cfg)
	}
}

func TestConfigFormattingRedactsGeminiAPIKey(t *testing.T) {
	const key = "secret-gemini-key-that-must-never-be-formatted"
	cfg := Config{Mode: ModeDevelopment, Address: DefaultAddress, DatabasePath: "/tmp/synthetic.db", GeminiAPIKey: key}
	for _, formatted := range []string{fmt.Sprint(cfg), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%#v", cfg)} {
		if strings.Contains(formatted, key) {
			t.Fatalf("formatted config leaked Gemini API key: %s", formatted)
		}
	}
}

// publisherEnv returns a full set of env values sufficient for
// PUBLISHER_ENABLED=1 to pass validation.
func publisherEnv() map[string]string {
	return map[string]string{
		"JWT_SECRET":                  "x",
		"PUBLISHER_ENABLED":           "1",
		"PUBLISHER_INTERVAL":          "1m",
		"GEMINI_API_KEY":              "synthetic-gemini-key",
		"GEMINI_TEXT_MODEL":           "text-model",
		"GEMINI_IMAGE_MODEL":          "image-model",
		"GEMINI_VISION_MODEL":         "vision-model",
		"AWS_REGION":                  "us-east-1",
		"S3_FEATURE_IMAGE_BUCKET":     "bucket",
		"S3_FEATURE_IMAGE_PREFIX":     "features",
		"S3_FEATURE_IMAGE_PUBLIC_URL": "https://images.example.invalid",
	}
}
