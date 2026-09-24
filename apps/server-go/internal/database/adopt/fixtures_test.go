package adopt_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
)

//go:embed testdata/node-schema.json
var nodeSchemaJSON []byte

// legacyOriginalSubscribersSQL is the production database's subscribers
// table, exactly as its sqlite_schema row reads: the original Node table
// (id, email, created_at DATETIME) with every later column added by Node's
// ALTER TABLE upgrade (apps/server/src/db.ts:116-125).
//
//go:embed testdata/legacy-original-subscribers.sql
var legacyOriginalSubscribersSQL string

type nodeObject struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	TblName string `json:"tbl_name"`
	SQL     string `json:"sql"`
}

type nodeSchema struct {
	Fresh  []nodeObject `json:"fresh"`
	Legacy []nodeObject `json:"legacy"`
}

func recordedNodeSchema(t *testing.T, variant string) []nodeObject {
	t.Helper()
	var schema nodeSchema
	if err := json.Unmarshal(nodeSchemaJSON, &schema); err != nil {
		t.Fatalf("parse node-schema.json: %v", err)
	}
	switch variant {
	case "fresh":
		return schema.Fresh
	case "legacy":
		return schema.Legacy
	}
	t.Fatalf("unknown variant %q", variant)
	return nil
}

// fixture describes a Node-shaped database: a recorded variant, optional
// whole-statement overrides and replacements applied to the recorded SQL of
// named objects, and extra statements run after the schema and seed data
// exist.
type fixture struct {
	variant  string
	override map[string]string    // object name -> statement used instead of the recorded one
	replace  map[string][2]string // object name -> {old, new}, applied after override
	extra    []string
	noSeed   bool
	fkOffSQL []string // statements run with foreign keys off (orphans)
}

// nodeDatabase builds the fixture in a temporary directory, closes it, and
// returns its path.
func nodeDatabase(t *testing.T, f fixture) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "technews.db")
	ctx := context.Background()
	db, err := database.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range recordedNodeSchema(t, f.variant) {
		statement := object.SQL
		if sql, ok := f.override[object.Name]; ok {
			statement = sql
		}
		if change, ok := f.replace[object.Name]; ok {
			if !strings.Contains(statement, change[0]) {
				t.Fatalf("fixture replacement %q not found in %s", change[0], object.Name)
			}
			statement = strings.Replace(statement, change[0], change[1], 1)
		}
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("create %s: %v", object.Name, err)
		}
	}
	if !f.noSeed {
		seedNodeData(t, db)
	}
	for _, statement := range f.extra {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("fixture statement %q: %v", statement, err)
		}
	}
	if len(f.fkOffSQL) > 0 {
		if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
			t.Fatal(err)
		}
		for _, statement := range f.fkOffSQL {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				t.Fatalf("fixture statement %q: %v", statement, err)
			}
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func seedNodeData(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		`INSERT INTO categories (id, name, slug, description, color) VALUES (1, 'AI', 'ai', 'Artificial intelligence', '#8b5cf6'), (2, 'Programming', 'programming', '', '#3b82f6')`,
		`INSERT INTO authors (id, name, email, password_hash, avatar, bio, role) VALUES (1, 'Editorial', 'editor@example.invalid', '$2a$10$synthetic', '/uploads/a.png', 'Bio', 'admin')`,
		`INSERT INTO articles (id, title, slug, excerpt, content, featured_image, category_id, author_id, status, published_at, source, source_url, view_count, created_at, updated_at)
			VALUES (1, 'First', 'first', 'e1', '<p>c1</p>', '/uploads/f.png', 1, 1, 'published', '2026-09-20 10:00:00', 'Source', 'https://example.invalid/1', 42, '2026-09-20 09:00:00', '2026-09-20 09:30:00'),
			       (2, 'Draft', 'draft', '', '', NULL, 2, 1, 'draft', NULL, NULL, NULL, 0, '2026-09-21 09:00:00', '2026-09-21 09:00:00')`,
		`INSERT INTO media (id, filename, url, mime_type, size, uploaded_at) VALUES (1, 'f.png', '/uploads/f.png', 'image/png', 1234, '2026-09-20 08:00:00')`,
		`INSERT INTO settings (key, value) VALUES ('site_name', 'TechNews'), ('newsletter_enabled', 'false')`,
		`INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (501, 'a@example.invalid', 'active', '2026-09-01 00:00:00', '2026-09-01 00:00:00'), (502, 'b@example.invalid', 'unsubscribed', '2026-09-02 00:00:00', '2026-09-03 00:00:00')`,
		`INSERT INTO newsletter_editions (id, edition_key, subject, articles, created_at) VALUES (601, '2026-09-20', 'Subject', '[]', '2026-09-20 05:00:00')`,
		`INSERT INTO newsletter_deliveries (id, subscriber_id, edition_key, status, provider_message_id, created_at, sent_at) VALUES (701, 501, '2026-09-20', 'sent', 'msg-1', '2026-09-20 05:00:01', '2026-09-20 05:00:02')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

// legacyOriginalFixture is the legacy (ALTER TABLE-evolved) Node schema
// with the production subscribers table, and subscribers rows shaped like
// production's: created_at in both Node formats ('YYYY-MM-DD HH:MM:SS' from
// CURRENT_TIMESTAMP, ISO from Node's later INSERTs), never NULL.
func legacyOriginalFixture() fixture {
	return fixture{
		variant:  "legacy",
		override: map[string]string{"subscribers": legacyOriginalSubscribersSQL},
		extra: []string{
			// Node's original signup inserted only the email.
			`INSERT INTO subscribers (id, email) VALUES (503, 'default@example.invalid')`,
			`UPDATE subscribers SET updated_at = created_at WHERE id = 503`,
			`INSERT INTO subscribers (id, email, status, source_placement, confirmation_sent_at, created_at, updated_at)
				VALUES (504, 'pending@example.invalid', 'pending', 'footer', '2026-09-18T00:00:00.000Z', '2026-09-18T00:00:00.000Z', '2026-09-18T00:00:00.000Z')`,
		},
	}
}

var nodeTables = []string{"authors", "categories", "articles", "media", "settings", "subscribers", "newsletter_deliveries", "newsletter_editions"}

// dumpTables returns every row of every named table, in rowid order (or key
// order for tables without a rowid alias), as text.
func dumpTables(t *testing.T, path string, tables ...string) map[string]string {
	t.Helper()
	db, err := database.OpenExisting(context.Background(), path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dump := map[string]string{}
	for _, table := range tables {
		rows, err := db.Query(`SELECT * FROM "` + table + `" ORDER BY 1`)
		if err != nil {
			t.Fatalf("dump %s: %v", table, err)
		}
		columns, _ := rows.Columns()
		var b strings.Builder
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			for i, value := range values {
				fmt.Fprintf(&b, "%s=%#v ", columns[i], value)
			}
			b.WriteString("\n")
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		dump[table] = b.String()
	}
	return dump
}

func fileHash(t *testing.T, path string) [32]byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(content)
}

func hasLedger(t *testing.T, path string) bool {
	t.Helper()
	db, err := database.OpenExisting(context.Background(), path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name = 'schema_migrations'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count == 1
}

func backups(t *testing.T, path string) []string {
	t.Helper()
	matches, err := filepath.Glob(path + ".pre-adopt-*.db")
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

var fixedNow = func() time.Time { return time.Date(2026, 9, 24, 7, 30, 15, 0, time.UTC) }
