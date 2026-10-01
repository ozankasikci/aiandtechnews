package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

func TestReimageLatestListsWithoutWriting(t *testing.T) {
	db, path := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO categories(id,name,slug,description,color) VALUES (1,'AI','ai','AI','#111111')`,
		`INSERT INTO authors(id,name,email,password_hash,role) VALUES (1,'E','e@example.invalid','fake','admin')`,
		`INSERT INTO articles(id,title,slug,excerpt,content,featured_image,category_id,author_id,status,published_at,created_at,updated_at) VALUES
(1,'A','a-source','e','c','https://techcrunch.com/a.jpg',1,1,'published','2026-09-20 10:00:00','x','x'),
(2,'B','b-ours','e','c','https://aiandtech.news/b.webp',1,1,'published','2026-09-19 10:00:00','x','x'),
(3,'C','c-empty','e','c',NULL,1,1,'published','2026-09-18 10:00:00','x','x')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := runReimageLatest(context.Background(), &out, path, 1); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "a-source") || strings.Contains(text, "b-ours") || strings.Contains(text, "c-empty") ||
		!strings.Contains(text, "2 article(s) would be replaced in all") {
		t.Fatalf("output:\n%s", text)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM featured_reimage`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("rows = %d, %v", rows, err)
	}
}
