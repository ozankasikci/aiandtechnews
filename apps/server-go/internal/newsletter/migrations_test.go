package newsletter

import (
	"context"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

func normalizedSQL(sql string) string {
	return strings.Join(strings.Fields(strings.TrimSuffix(strings.TrimSpace(sql), ";")), " ")
}

func TestNewsletterMigrationCreatesNodesExactSchema(t *testing.T) {
	descriptors := Migrations()
	if len(descriptors) != 1 || descriptors[0].Version != 6 || descriptors[0].Name != "newsletter" {
		t.Fatalf("descriptors = %#v", descriptors)
	}
	db, _ := testutil.OpenDatabase(t)
	for i := 0; i < 2; i++ {
		if err := migrate.Run(context.Background(), db, Migrations()); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	// Node's schema as better-sqlite3 left it in sqlite_schema (recorded).
	want := map[string]string{}
	for _, object := range golden(t).Schema {
		want[object.Name] = normalizedSQL(object.SQL)
	}
	if len(want) != 5 {
		t.Fatalf("recorded schema objects = %d, want 5", len(want))
	}
	rows, err := db.Query(`SELECT name, sql FROM sqlite_schema WHERE name IN ('subscribers','newsletter_deliveries','newsletter_editions','idx_subscribers_status','idx_newsletter_deliveries_edition')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var name, sql string
		if err := rows.Scan(&name, &sql); err != nil {
			t.Fatal(err)
		}
		got[name] = normalizedSQL(sql)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for name, sql := range want {
		if got[name] != sql {
			t.Errorf("%s schema =\n%s\nwant (Node)\n%s", name, got[name], sql)
		}
	}
	if len(got) != len(want) {
		t.Errorf("schema objects = %v", got)
	}
}

func TestNewsletterMigrationEnforcesNodesConstraints(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, Migrations()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscribers (email) VALUES ('a@example.invalid')`); err != nil {
		t.Fatal(err)
	}
	var status, createdAt, updatedAt string
	if err := db.QueryRow(`SELECT status, created_at, updated_at FROM subscribers`).Scan(&status, &createdAt, &updatedAt); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || len(createdAt) != len("2006-01-02 15:04:05") || len(updatedAt) != len(createdAt) {
		t.Fatalf("subscriber defaults = %q %q %q", status, createdAt, updatedAt)
	}
	if _, err := db.Exec(`INSERT INTO newsletter_deliveries (subscriber_id, edition_key) VALUES (1, '2026-09-20')`); err != nil {
		t.Fatal(err)
	}
	var deliveryStatus string
	if err := db.QueryRow(`SELECT status FROM newsletter_deliveries`).Scan(&deliveryStatus); err != nil || deliveryStatus != "sending" {
		t.Fatalf("delivery default status = %q, %v", deliveryStatus, err)
	}
	if _, err := db.Exec(`INSERT INTO newsletter_editions (edition_key, subject, articles) VALUES ('2026-09-20', 's', '[]')`); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO subscribers (email) VALUES ('a@example.invalid')`,
		`INSERT INTO subscribers (email) VALUES (NULL)`,
		`INSERT INTO newsletter_deliveries (subscriber_id, edition_key) VALUES (1, '2026-09-20')`,
		`INSERT INTO newsletter_deliveries (subscriber_id, edition_key) VALUES (999, '2026-09-21')`,
		`INSERT INTO newsletter_editions (edition_key, subject, articles) VALUES ('2026-09-20', 's', '[]')`,
		`INSERT INTO newsletter_editions (edition_key, articles) VALUES ('2026-09-21', '[]')`,
	} {
		if _, err := db.Exec(statement); err == nil {
			t.Errorf("constraint not enforced: %s", statement)
		}
	}
	// ON DELETE CASCADE removes a subscriber's deliveries.
	if _, err := db.Exec(`DELETE FROM subscribers WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	var deliveries int
	if err := db.QueryRow(`SELECT COUNT(*) FROM newsletter_deliveries`).Scan(&deliveries); err != nil || deliveries != 0 {
		t.Fatalf("deliveries after subscriber delete = %d, %v", deliveries, err)
	}
}

// Production databases already have these tables (Node creates them at every
// start). migrate.Run refuses unmanaged databases, so the future adoption
// command will stamp them; the migration SQL itself must be a no-op there,
// including on a legacy subscribers table whose columns Node added with
// ALTER TABLE (apps/server/src/db.ts:116-137) and therefore sit at the end
// without NOT NULL defaults.
func TestNewsletterMigrationSQLKeepsTablesNodeAlreadyCreated(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		db, _ := testutil.OpenDatabase(t)
		if legacy {
			for _, statement := range []string{
				`CREATE TABLE subscribers (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT NOT NULL UNIQUE, created_at TEXT)`,
				`ALTER TABLE subscribers ADD COLUMN status TEXT NOT NULL DEFAULT 'pending'`,
				`ALTER TABLE subscribers ADD COLUMN source_placement TEXT`,
				`ALTER TABLE subscribers ADD COLUMN confirmation_sent_at TEXT`,
				`ALTER TABLE subscribers ADD COLUMN confirmed_at TEXT`,
				`ALTER TABLE subscribers ADD COLUMN unsubscribed_at TEXT`,
				`ALTER TABLE subscribers ADD COLUMN updated_at TEXT`,
			} {
				if _, err := db.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
		}
		for _, object := range golden(t).Schema {
			if legacy && object.Name == "subscribers" {
				continue
			}
			if object.Type == "table" {
				if _, err := db.Exec(object.SQL); err != nil {
					t.Fatalf("create Node %s: %v", object.Name, err)
				}
			}
		}
		for _, object := range golden(t).Schema {
			if object.Type == "index" {
				if _, err := db.Exec(object.SQL); err != nil {
					t.Fatalf("create Node %s: %v", object.Name, err)
				}
			}
		}
		if _, err := db.Exec(`INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (503, 'old@example.invalid', 'active', 'c', 'u')`); err != nil {
			t.Fatal(err)
		}
		var before string
		if err := db.QueryRow(`SELECT group_concat(sql, ';') FROM sqlite_schema`).Scan(&before); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(Migrations()[0].SQL); err != nil {
			t.Fatalf("legacy=%t: %v", legacy, err)
		}
		var after string
		if err := db.QueryRow(`SELECT group_concat(sql, ';') FROM sqlite_schema`).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if before != after {
			t.Fatalf("legacy=%t: migration changed a Node schema:\n%s\n->\n%s", legacy, before, after)
		}
		result, err := db.Exec(`INSERT INTO subscribers (email, status, created_at, updated_at) VALUES ('new@example.invalid', 'active', 'c', 'u')`)
		if err != nil {
			t.Fatal(err)
		}
		if id, err := result.LastInsertId(); err != nil || id != 504 {
			t.Fatalf("legacy=%t: next subscriber id = %d, %v", legacy, id, err)
		}
	}
}
