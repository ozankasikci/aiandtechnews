// Command telegram-select-try prints which of the newest published articles
// the Telegram channel's selector would post. It opens the database
// read-only and posts nothing.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	_ "modernc.org/sqlite"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/codextext"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/telegram"
)

func main() {
	dbPath := flag.String("db", "", "SQLite database (opened read-only)")
	n := flag.Int("n", 30, "how many of the newest published articles to judge")
	notes := flag.String("notes", "", "owner's standing instructions to use instead of the stored ones")
	flag.Parse()
	if *dbPath == "" {
		fmt.Fprintln(os.Stderr, "-db is required")
		os.Exit(2)
	}
	db, err := sql.Open("sqlite", "file:"+*dbPath+"?mode=ro")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer db.Close()
	ctx := context.Background()
	if *notes == "" {
		_ = db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, telegram.SelectNotesKey).Scan(notes)
	}
	rows, err := db.QueryContext(ctx, `SELECT a.id, a.title, COALESCE(a.excerpt, ''), COALESCE(c.name, ''),
		COALESCE(sm.why_it_matters, ''), COALESCE(sm.tldr, '')
		FROM articles a LEFT JOIN categories c ON c.id = a.category_id
		LEFT JOIN article_summaries sm ON sm.article_id = a.id
		WHERE a.status = 'published' ORDER BY datetime(a.published_at) DESC, a.id DESC LIMIT ?`, *n)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var posts []telegram.Post
	for rows.Next() {
		var post telegram.Post
		var tldr string
		if err := rows.Scan(&post.ID, &post.Title, &post.Excerpt, &post.Category, &post.WhyItMatters, &tldr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		_ = json.Unmarshal([]byte(tldr), &post.TLDR)
		posts = append(posts, post)
	}
	rows.Close()
	runner := &illustration.CodexRunner{Bin: os.Getenv("CODEX_BIN"), NodeDir: os.Getenv("CODEX_NODE_DIR")}
	selector := telegram.TextSelector{Text: codextext.New(runner, "", "low")}
	posted := 0
	for _, post := range posts {
		ok, reason, err := selector.Select(ctx, post, *notes)
		verdict := "SKIP"
		switch {
		case err != nil:
			verdict, reason = "ERR ", err.Error()
		case ok:
			verdict = "POST"
			posted++
		}
		fmt.Printf("%s %s\n       %s\n", verdict, post.Title, reason)
	}
	fmt.Printf("\n%d of %d would be posted\n", posted, len(posts))
}
