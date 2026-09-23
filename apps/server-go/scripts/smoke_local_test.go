package scripts_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
)

// smokeDatabase builds a Node-shaped database (the recorded Node schema)
// with a login, a published article, and a newsletter edition.
func smokeDatabase(t *testing.T, password string, mutate func(string) string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "internal", "database", "adopt", "testdata", "node-schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Fresh []struct{ SQL string } `json:"fresh"`
	}
	if err := json.Unmarshal(content, &schema); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "source.db")
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
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO categories (id, name, slug, description, color) VALUES (1, 'AI', 'ai', 'AI news', '#8b5cf6')`,
		`INSERT INTO authors (id, name, email, password_hash, role) VALUES (1, 'Smoke Editor', 'smoke@example.invalid', '` + string(hash) + `', 'admin')`,
		`INSERT INTO articles (id, title, slug, excerpt, content, featured_image, category_id, author_id, status, published_at)
			VALUES (7, 'Smoke article', 'smoke-article', 'Excerpt', '<p>Body</p>', '/uploads/sample.png', 1, 1, 'published', '2026-09-20 10:00:00')`,
		`INSERT INTO newsletter_editions (edition_key, subject, articles) VALUES ('2026-09-20', 'Subject', '[]')`,
		`INSERT INTO settings (key, value) VALUES ('site_name', 'TechNews')`,
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

func requireTools(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds and runs the API; skipped with -short")
	}
	for _, tool := range []string{"bash", "curl", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available: %v", tool, err)
		}
	}
}

func runSmoke(t *testing.T, tmp string, env []string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "./smoke-local.sh", args...)
	command.Env = append(os.Environ(), append([]string{"TMPDIR=" + tmp}, env...)...)
	output, err := command.CombinedOutput()
	return string(output), err
}

func assertCleanedUp(t *testing.T, tmp, output string) {
	t.Helper()
	leftovers, err := filepath.Glob(filepath.Join(tmp, "technews-smoke.*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Errorf("temporary directories left behind: %v", leftovers)
	}
	if match := regexp.MustCompile(`starting the API on http://(127\.0\.0\.1:\d+)`).FindStringSubmatch(output); match != nil {
		if conn, err := net.DialTimeout("tcp", match[1], time.Second); err == nil {
			conn.Close()
			t.Errorf("the API is still listening on %s after the script exited", match[1])
		}
	}
}

func TestSmokeLocalPassesAgainstACopyAndCleansUp(t *testing.T) {
	requireTools(t)
	const password = "smoke-password"
	source := smokeDatabase(t, password, nil)
	uploads := t.TempDir()
	if err := os.WriteFile(filepath.Join(uploads, "sample.png"), []byte("\x89PNG\r\n\x1a\nsynthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()

	output, err := runSmoke(t, tmp, []string{"SMOKE_EMAIL=smoke@example.invalid", "SMOKE_PASSWORD=" + password}, source, uploads)
	if err != nil {
		t.Fatalf("smoke-local.sh: %v\n%s", err, output)
	}
	for _, want := range []string{
		"ok   200 /api/articles/smoke-article",
		"ok   200 /api/articles/id/7",
		"ok   200 /api/newsletter/editions/2026-09-20",
		"ok   200 /uploads/sample.png",
		"ok   200 POST /api/auth/login",
		"ok   200 /api/newsroom/candidates",
		"smoke: PASS",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output lacks %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, password) {
		t.Error("the password appears in the output")
	}
	after, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("the smoke test changed the source database")
	}
	if backups, _ := filepath.Glob(source + ".pre-adopt-*"); len(backups) != 0 {
		t.Errorf("backups written next to the source: %v", backups)
	}
	assertCleanedUp(t, tmp, output)
}

func TestSmokeLocalFailsOnAnIncompatibleDatabase(t *testing.T) {
	requireTools(t)
	source := smokeDatabase(t, "x", func(statement string) string {
		return strings.Replace(statement, "meta_title TEXT,\n", "", 1)
	})
	tmp := t.TempDir()
	output, err := runSmoke(t, tmp, nil, source)
	if err == nil {
		t.Fatalf("smoke-local.sh succeeded on an incompatible database:\n%s", output)
	}
	if !strings.Contains(output, "cmd/adopt failed on the copy") {
		t.Errorf("output:\n%s", output)
	}
	assertCleanedUp(t, tmp, output)
}

func TestSmokeLocalUsage(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	command := exec.Command("./smoke-local.sh")
	output, err := command.CombinedOutput()
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 2 || !strings.Contains(string(output), "usage:") {
		t.Fatalf("err = %v, output:\n%s", err, output)
	}
}
