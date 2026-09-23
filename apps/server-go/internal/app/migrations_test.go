package app_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

func TestApplicationMigrationsHaveStableGlobalOrderAndAreIdempotent(t *testing.T) {
	descriptors := app.Migrations()
	if len(descriptors) != 4 ||
		descriptors[0].Version != 1 || descriptors[0].Name != "editorial authors" ||
		descriptors[1].Version != 2 || descriptors[1].Name != "content categories and articles" ||
		descriptors[2].Version != 3 || descriptors[2].Name != "newsroom candidates" ||
		descriptors[3].Version != 4 || descriptors[3].Name != "newsroom published index" {
		t.Fatalf("descriptors = %#v", descriptors)
	}
	firstChecksum, secondChecksum := descriptors[0].Checksum(), descriptors[1].Checksum()
	descriptors[0].Name = "mutated copy"
	fresh := app.Migrations()
	if fresh[0].Name != "editorial authors" || fresh[0].Checksum() != firstChecksum || fresh[1].Checksum() != secondChecksum {
		t.Fatal("migration descriptors are mutable across calls")
	}
	db, _ := testutil.OpenDatabase(t)
	for i := 0; i < 2; i++ {
		if err := migrate.Run(context.Background(), db, fresh); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	assertSchemaCompatibility(t, db)
}

func assertSchemaCompatibility(t *testing.T, db *sql.DB) {
	t.Helper()
	var foreignKeys int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, %v", foreignKeys, err)
	}
	var articleSQL, authorSQL string
	if err := db.QueryRow(`SELECT sql FROM sqlite_schema WHERE type='table' AND name='articles'`).Scan(&articleSQL); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT sql FROM sqlite_schema WHERE type='table' AND name='authors'`).Scan(&authorSQL); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft', 'published', 'scheduled'))", "view_count INTEGER NOT NULL DEFAULT 0", "created_at TEXT NOT NULL DEFAULT (datetime('now'))", "FOREIGN KEY (author_id) REFERENCES authors(id)"} {
		if !containsSQL(articleSQL, fragment) {
			t.Errorf("articles schema missing %q: %s", fragment, articleSQL)
		}
	}
	if !containsSQL(authorSQL, "role TEXT NOT NULL DEFAULT 'editor' CHECK(role IN ('admin', 'editor'))") {
		t.Errorf("authors schema = %s", authorSQL)
	}
	var fkCount int
	rows, err := db.Query(`PRAGMA foreign_key_list('articles')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		fkCount++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if fkCount != 2 {
		t.Errorf("article foreign keys = %d", fkCount)
	}

	var indexSQL string
	if err := db.QueryRow(`SELECT sql FROM sqlite_schema WHERE type='index' AND name='idx_articles_source_url'`).Scan(&indexSQL); err != nil {
		t.Fatalf("source URL index: %v", err)
	}
	if !containsSQL(indexSQL, "CREATE INDEX idx_articles_source_url ON articles(source_url)") {
		t.Errorf("source URL index = %s", indexSQL)
	}
	if err := db.QueryRow(`SELECT sql FROM sqlite_schema WHERE type='index' AND name='idx_candidates_status_published'`).Scan(&indexSQL); err != nil {
		t.Fatalf("candidate published index: %v", err)
	}
	if !containsSQL(indexSQL, "CREATE INDEX idx_candidates_status_published ON candidates(status, published_at)") {
		t.Errorf("candidate published index = %s", indexSQL)
	}

	if _, err := db.Exec(`INSERT INTO authors(id,name,email,password_hash) VALUES (1,'Author','author@example.invalid','hash')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO categories(id,name,slug) VALUES (1,'Category','category')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO articles(id,title,slug,category_id,author_id) VALUES (1,'Article','article',1,1)`); err != nil {
		t.Fatal(err)
	}
	var role, description, color, status, excerpt, body string
	var views int
	if err := db.QueryRow(`SELECT au.role,c.description,c.color,a.status,a.excerpt,a.content,a.view_count FROM authors au,categories c,articles a WHERE au.id=1 AND c.id=1 AND a.id=1`).Scan(&role, &description, &color, &status, &excerpt, &body, &views); err != nil {
		t.Fatal(err)
	}
	if role != "editor" || description != "" || color != "#6366f1" || status != "draft" || excerpt != "" || body != "" || views != 0 {
		t.Errorf("defaults = role=%q description=%q color=%q status=%q excerpt=%q content=%q views=%d", role, description, color, status, excerpt, body, views)
	}

	constraintCases := []string{
		`INSERT INTO authors(name,email,password_hash) VALUES ('Duplicate','author@example.invalid','hash')`,
		`INSERT INTO authors(name,email,password_hash,role) VALUES ('Bad Role','bad-role@example.invalid','hash','owner')`,
		`INSERT INTO categories(name,slug) VALUES ('Duplicate','category')`,
		`INSERT INTO articles(title,slug,category_id,author_id) VALUES ('Duplicate','article',1,1)`,
		`INSERT INTO articles(title,slug,category_id,author_id,status) VALUES ('Bad Status','bad-status',1,1,'hidden')`,
		`INSERT INTO articles(title,slug,category_id,author_id) VALUES ('Bad Category','bad-category',999,1)`,
		`INSERT INTO articles(title,slug,category_id,author_id) VALUES ('Bad Author','bad-author',1,999)`,
	}
	for _, statement := range constraintCases {
		if _, err := db.Exec(statement); err == nil {
			t.Errorf("constraint statement unexpectedly succeeded: %s", statement)
		}
	}
}

func containsSQL(schema, fragment string) bool {
	return strings.Contains(strings.Join(strings.Fields(schema), " "), strings.Join(strings.Fields(fragment), " "))
}
