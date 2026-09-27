package main

import (
	"context"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
)

type fixedText struct{ answer string }

func (f fixedText) GenerateJSON(context.Context, string) (string, error) { return f.answer, nil }

func TestRunAddsSubheadingsOnlyWithWrite(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	body := "<p>1</p><p>2</p><p>3</p><p>4</p><p>5</p><p>6</p>"
	for _, stmt := range []string{
		`CREATE TABLE articles (id INTEGER PRIMARY KEY, slug TEXT, title TEXT, content TEXT, status TEXT, published_at TEXT, updated_at TEXT)`,
		`INSERT INTO articles VALUES (1, 'long', 'Long', '` + body + `', 'published', '2026-09-27', '')`,
		`INSERT INTO articles VALUES (2, 'short', 'Short', '<p>1</p><p>2</p>', 'published', '2026-09-26', '')`,
		`INSERT INTO articles VALUES (3, 'has', 'Has', '<p>1</p><h2>X</h2><p>2</p><p>3</p><p>4</p><p>5</p>', 'published', '2026-09-25', '')`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	text := fixedText{`{"subheadings":[{"beforeParagraph":4,"text":"Middle part"}]}`}
	var out strings.Builder
	if err := run(ctx, db, text, options{minParagraphs: 5}, &out); err != nil {
		t.Fatal(err)
	}
	var stored string
	_ = db.QueryRowContext(ctx, `SELECT content FROM articles WHERE id = 1`).Scan(&stored)
	if stored != body || !strings.Contains(out.String(), "add  long: Middle part") || strings.Contains(out.String(), "short") || strings.Contains(out.String(), "has") {
		t.Fatalf("dry run changed data or reported wrongly:\n%s\n%s", out.String(), stored)
	}
	if err := run(ctx, db, text, options{minParagraphs: 5, write: true}, &out); err != nil {
		t.Fatal(err)
	}
	_ = db.QueryRowContext(ctx, `SELECT content FROM articles WHERE id = 1`).Scan(&stored)
	if want := "<p>1</p><p>2</p><p>3</p><h2>Middle part</h2><p>4</p><p>5</p><p>6</p>"; stored != want {
		t.Fatalf("stored %q, want %q", stored, want)
	}
}
