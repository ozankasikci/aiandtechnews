package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
)

// nodeDatabase creates a database with the recorded fresh Node schema
// (internal/database/adopt/testdata/node-schema.json) and one article.
func nodeDatabase(t *testing.T, dir string, mutate func(string) string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "internal", "database", "adopt", "testdata", "node-schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Fresh []struct{ SQL string } `json:"fresh"`
	}
	if err := json.Unmarshal(content, &schema); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "technews.db")
	db, err := database.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range schema.Fresh {
		statement := object.SQL
		if mutate != nil {
			statement = mutate(statement)
		}
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		`INSERT INTO categories (id, name, slug) VALUES (1, 'AI', 'ai')`,
		`INSERT INTO authors (id, name, email, password_hash) VALUES (1, 'E', 'e@example.invalid', 'x')`,
		`INSERT INTO articles (id, title, slug, category_id, author_id, status) VALUES (1, 'T', 't', 1, 1, 'published')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

const testSecret = "0123456789abcdef0123456789abcdef"

func runAdopt(t *testing.T, env map[string]string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), args, func(key string) string { return env[key] }, func() (string, error) { return t.TempDir(), nil }, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func devEnv(databasePath string) map[string]string {
	return map[string]string{"DATABASE_PATH": databasePath, "UPLOADS_DIR": filepath.Join(filepath.Dir(databasePath), "uploads")}
}

func TestDryRunThenApplyThenNoOp(t *testing.T) {
	path := nodeDatabase(t, t.TempDir(), nil)
	env := devEnv(path)

	code, stdout, stderr := runAdopt(t, env)
	if code != 0 || !strings.Contains(stdout, "Result: COMPATIBLE") || !strings.Contains(stdout, "Dry run: nothing was changed") {
		t.Fatalf("dry run exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	code, stdout, stderr = runAdopt(t, env, "--apply")
	if code != 0 || !strings.Contains(stdout, "Adopted: recorded 1, 2, 5, 6; ran 3, 4") {
		t.Fatalf("apply exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	backups, _ := filepath.Glob(path + ".pre-adopt-*.db")
	if len(backups) != 1 {
		t.Fatalf("backups = %v", backups)
	}

	for _, args := range [][]string{nil, {"--apply"}} {
		code, stdout, stderr = runAdopt(t, env, args...)
		if code != 0 || !strings.Contains(stdout, "already managed") {
			t.Fatalf("rerun %v exit %d\nstdout:\n%s\nstderr:\n%s", args, code, stdout, stderr)
		}
	}
}

func TestIncompatibleDatabaseExitsNonZero(t *testing.T) {
	path := nodeDatabase(t, t.TempDir(), func(statement string) string {
		return strings.Replace(statement, "meta_title TEXT,\n", "", 1)
	})
	code, stdout, _ := runAdopt(t, devEnv(path), "--apply")
	if code != 1 || !strings.Contains(stdout, "missing column meta_title") {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
}

func TestDevelopmentRefusesAProductionMarkedDatabase(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, config.ProductionMarker), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := nodeDatabase(t, dataDir, nil)
	before, _ := os.ReadFile(path)

	code, _, stderr := runAdopt(t, map[string]string{"DATABASE_PATH": path, "UPLOADS_DIR": filepath.Join(t.TempDir(), "uploads")}, "--apply")
	if code != 1 || !strings.Contains(stderr, "production database path is forbidden outside APP_ENV=production") {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("refused run changed the database")
	}

	// The same database in production mode is adoptable.
	env := map[string]string{"APP_ENV": "production", "DATABASE_PATH": path, "UPLOADS_DIR": filepath.Join(root, "uploads"), "JWT_SECRET": testSecret, "TZ": "Europe/Istanbul"}
	code, stdout, stderr := runAdopt(t, env)
	if code != 0 {
		t.Fatalf("production dry run exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}

func TestProductionDoesNotLookForAWorktree(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, config.ProductionMarker), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	path := nodeDatabase(t, root, nil)
	var stdout, stderr bytes.Buffer
	env := map[string]string{"APP_ENV": "production", "DATABASE_PATH": path, "UPLOADS_DIR": filepath.Join(root, "uploads"), "JWT_SECRET": testSecret, "TZ": "Europe/Istanbul"}
	code := run(context.Background(), nil, func(key string) string { return env[key] }, func() (string, error) {
		t.Fatal("worktree lookup in production")
		return "", nil
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
}

func TestUsageErrorsAndMissingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent", "technews.db")
	if code, _, _ := runAdopt(t, devEnv(path), "--bogus"); code != 2 {
		t.Fatalf("unknown flag exit %d, want 2", code)
	}
	if code, _, _ := runAdopt(t, devEnv(path), "extra-argument"); code != 2 {
		t.Fatalf("positional argument exit %d, want 2", code)
	}
	code, _, stderr := runAdopt(t, devEnv(path))
	if code != 1 || !strings.Contains(stderr, "no such file") {
		t.Fatalf("missing database exit %d, stderr:\n%s", code, stderr)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("adopt created the missing database directory")
	}
}
