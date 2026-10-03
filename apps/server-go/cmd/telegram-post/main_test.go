package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
)

func TestPostsOneArticleAndRecordsNothing(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "t.db")
	db, err := database.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate.Run(ctx, db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO categories(id,name,slug,description,color) VALUES (1,'AI','ai','AI','#111111')`,
		`INSERT INTO authors(id,name,email,password_hash,role) VALUES (1,'E','e@example.invalid','fake','admin')`,
		`INSERT INTO articles(id,title,slug,excerpt,content,featured_image,category_id,author_id,status,published_at,created_at,updated_at) VALUES
(1,'Hello & bye','hello','Excerpt.','c','https://techcrunch.com/a.jpg',1,1,'published','2026-10-03 08:00:00','x','x')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()

	var form map[string][]string
	var path2 string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path2 = r.URL.Path
		_ = r.ParseForm()
		form = r.PostForm
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":777}}`)
	}))
	defer server.Close()
	env := map[string]string{"TELEGRAM_BOT_TOKEN": "1:tok", "TELEGRAM_CHANNEL": "@default", "NEWSLETTER_SITE_URL": "https://aiandtech.news"}
	var stdout bytes.Buffer
	err = run(ctx, []string{"-db", path, "-slug", "hello", "-chat", "@test"}, func(key string) string { return env[key] }, server.URL, &stdout)
	if err != nil || stdout.String() != "777\n" {
		t.Fatalf("stdout = %q err = %v", stdout.String(), err)
	}
	// A source photo is never posted, so this is a text message.
	if path2 != "/bot1:tok/sendMessage" || form["chat_id"][0] != "@test" || !strings.HasPrefix(form["text"][0], "<b>Hello &amp; bye</b>\n\nExcerpt.") {
		t.Fatalf("path = %s form = %v", path2, form)
	}

	check, err := database.OpenExisting(ctx, path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	var rows int
	if err := check.QueryRow(`SELECT COUNT(*) FROM telegram_posts`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("telegram_posts rows = %d err = %v", rows, err)
	}

	if err := run(ctx, []string{"-db", path, "-slug", "missing"}, func(key string) string { return env[key] }, server.URL, io.Discard); err == nil {
		t.Fatal("a missing slug was posted")
	}
}
