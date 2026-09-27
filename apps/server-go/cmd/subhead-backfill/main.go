// Command subhead-backfill adds 0 to 3 subheadings to published articles
// that have none, asking the text model only where headings go. Paragraphs
// are never rewritten: a result whose paragraphs differ is refused.
//
//	go run ./cmd/subhead-backfill -db /path/technews.db            # dry run
//	go run ./cmd/subhead-backfill -db /path/technews.db -write     # update
//
// It reads GEMINI_API_KEY and GEMINI_TEXT_MODEL from the environment. Back up
// the database before running with -write.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
)

// TextGenerator is the one Gemini call the backfill needs.
type TextGenerator interface {
	GenerateJSON(ctx context.Context, prompt string) (string, error)
}

func main() {
	getenv := os.Getenv
	flags := flag.NewFlagSet("subhead-backfill", flag.ExitOnError)
	dbPath := flags.String("db", getenv("DATABASE_PATH"), "SQLite database path")
	write := flags.Bool("write", false, "update the articles (default: dry run)")
	limit := flags.Int("limit", 0, "stop after this many articles (0: all)")
	slug := flags.String("slug", "", "only this article")
	minParagraphs := flags.Int("min-paragraphs", 5, "skip articles with fewer paragraphs")
	_ = flags.Parse(os.Args[1:])
	if getenv("GEMINI_API_KEY") == "" || getenv("GEMINI_TEXT_MODEL") == "" {
		fail(errors.New("GEMINI_API_KEY and GEMINI_TEXT_MODEL are required"))
	}
	ctx := context.Background()
	db, err := database.OpenExisting(ctx, *dbPath, !*write)
	if err != nil {
		fail(err)
	}
	defer db.Close()
	text := gemini.New(getenv("GEMINI_API_KEY"), getenv("GEMINI_TEXT_MODEL"))
	if err := run(ctx, db, text, options{write: *write, limit: *limit, slug: *slug, minParagraphs: *minParagraphs}, os.Stdout); err != nil {
		fail(err)
	}
}

func fail(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "subhead-backfill: %v\n", err)
	os.Exit(1)
}

type options struct {
	write         bool
	limit         int
	slug          string
	minParagraphs int
}

type article struct {
	id                int64
	slug, title, body string
}

func run(ctx context.Context, db *sql.DB, text TextGenerator, opts options, out io.Writer) error {
	query := `SELECT id, slug, title, content FROM articles WHERE status = 'published' AND content NOT LIKE '%<h2>%'`
	args := []any{}
	if opts.slug != "" {
		query += ` AND slug = ?`
		args = append(args, opts.slug)
	}
	rows, err := db.QueryContext(ctx, query+` ORDER BY published_at DESC`, args...)
	if err != nil {
		return err
	}
	var articles []article
	for rows.Next() {
		var a article
		if err := rows.Scan(&a.id, &a.slug, &a.title, &a.body); err != nil {
			rows.Close()
			return err
		}
		articles = append(articles, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	var seen, added, skipped, failed int
	for _, a := range articles {
		if opts.limit > 0 && seen >= opts.limit {
			break
		}
		paragraphs := content.ParagraphTexts(a.body)
		if len(paragraphs) < opts.minParagraphs {
			continue
		}
		seen++
		updated, err := plan(ctx, text, a, paragraphs)
		if err != nil {
			failed++
			fmt.Fprintf(out, "FAIL %s: %v\n", a.slug, err)
			continue
		}
		headings := headingTexts(updated)
		if len(headings) == 0 {
			skipped++
			fmt.Fprintf(out, "none %s\n", a.slug)
			continue
		}
		added++
		fmt.Fprintf(out, "add  %s: %s\n", a.slug, strings.Join(headings, " | "))
		if !opts.write {
			continue
		}
		// Only replace the body we read, so a concurrent edit is never lost.
		result, err := db.ExecContext(ctx, `UPDATE articles SET content = ?, updated_at = ? WHERE id = ? AND content = ?`,
			updated, time.Now().UTC().Format("2006-01-02 15:04:05"), a.id, a.body)
		if err != nil {
			return fmt.Errorf("update %s: %w", a.slug, err)
		}
		if n, _ := result.RowsAffected(); n != 1 {
			fmt.Fprintf(out, "SKIP %s: changed while planning\n", a.slug)
		}
	}
	mode := "dry run"
	if opts.write {
		mode = "written"
	}
	fmt.Fprintf(out, "%s: %d articles checked, %d with subheadings, %d without, %d failed\n", mode, seen, added, skipped, failed)
	return nil
}

func plan(ctx context.Context, text TextGenerator, a article, paragraphs []string) (string, error) {
	raw, err := text.GenerateJSON(ctx, content.SubheadingPrompt(a.title, paragraphs))
	if err != nil {
		return "", err
	}
	subs, err := content.ParseSubheadings(raw)
	if err != nil {
		return "", err
	}
	updated := content.InsertSubheadings(a.body, subs)
	if !content.SameParagraphs(a.body, updated) {
		return "", errors.New("paragraphs changed; refused")
	}
	return updated, nil
}

func headingTexts(body string) []string {
	var texts []string
	for _, part := range strings.Split(body, "<h2>")[1:] {
		texts = append(texts, content.StripHTML(part[:strings.Index(part, "</h2>")]))
	}
	return texts
}
