package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/siterevalidate"
)

type fixedText struct{ answer string }

func (f fixedText) GenerateJSON(context.Context, string) (string, error) { return f.answer, nil }

func TestRunTagsOnlyWithWrite(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrate.Run(ctx, db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO authors (id, name, email, password_hash, role) VALUES (1, 'Ed', 'ed@example.invalid', 'x', 'editor')`,
		`INSERT INTO categories (id, name, slug, description, color) VALUES (1, 'AI', 'ai', 'd', '#000')`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 3; i++ {
		if _, err := db.ExecContext(ctx, `INSERT INTO articles (id, title, slug, excerpt, content, category_id, author_id, status, published_at, source, source_url)
			VALUES (?, ?, ?, 'e', '<p>c</p>', 1, 1, 'published', '2026-09-0'||?, 's', 'https://s.example/'||?)`, i, fmt.Sprint("T", i), fmt.Sprint("a-", i), i, i); err != nil {
			t.Fatal(err)
		}
	}
	text := fixedText{`{"entities":[{"name":"Nvidia","kind":"company","aliases":["NVIDIA Corp"]}]}`}
	var out strings.Builder
	if err := run(ctx, db, text, siterevalidate.Disabled{}, options{}, &out); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM topics`).Scan(&n)
	if n != 0 || !strings.Contains(out.String(), "a-3") || !strings.Contains(out.String(), "dry run: 3 articles checked, 3 tagged") ||
		!strings.Contains(out.String(), "3        Nvidia (nvidia)") || !strings.Contains(out.String(), "live") {
		t.Fatalf("dry run wrote data or reported wrongly (%d topics):\n%s", n, out.String())
	}
	out.Reset()
	if err := run(ctx, db, text, siterevalidate.Disabled{}, options{write: true, limit: 2}, &out); err != nil {
		t.Fatal(err)
	}
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM article_topics`).Scan(&n)
	if n != 2 || !strings.Contains(out.String(), "written: 2 articles checked") || !strings.Contains(out.String(), "needs more articles") {
		t.Fatalf("write run:\n%s", out.String())
	}
}
