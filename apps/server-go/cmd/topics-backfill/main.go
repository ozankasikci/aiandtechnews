// Command topics-backfill tags published articles that have no topics,
// asking the text model which 1 to 4 companies, products, people or themes
// each article is substantially about, and lists the resulting topics.
//
//	go run ./cmd/topics-backfill -db /path/technews.db            # dry run (read-only)
//	go run ./cmd/topics-backfill -db /path/technews.db -write     # store topics
//
// It reads GEMINI_API_KEY and GEMINI_VISION_MODEL (default: Flash) from the
// environment. With -write and SITE_REVALIDATE_URL set (plus
// NEWSLETTER_CRON_SECRET or CRON_SECRET) it asks the website to refresh the
// pages that changed. Hub summaries are written later by the API's topics
// loop (TOPICS_ENABLED). Back up the database before running with -write.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/siterevalidate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/topics"
)

func main() {
	getenv := os.Getenv
	flags := flag.NewFlagSet("topics-backfill", flag.ExitOnError)
	dbPath := flags.String("db", getenv("DATABASE_PATH"), "SQLite database path")
	write := flags.Bool("write", false, "store the topics (default: dry run, read-only)")
	limit := flags.Int("limit", 0, "stop after this many articles (0: all)")
	slug := flags.String("slug", "", "only this article")
	_ = flags.Parse(os.Args[1:])
	if getenv("GEMINI_API_KEY") == "" {
		fail(errors.New("GEMINI_API_KEY is required"))
	}
	ctx := context.Background()
	db, err := database.OpenExisting(ctx, *dbPath, !*write)
	if err != nil {
		fail(err)
	}
	defer db.Close()
	text := gemini.New(getenv("GEMINI_API_KEY"), cmpOr(getenv("GEMINI_VISION_MODEL"), gemini.DefaultVisionModel))
	site, drain := siterevalidate.New("", "", nil)
	if *write {
		secret := cmpOr(getenv("NEWSLETTER_CRON_SECRET"), getenv("CRON_SECRET"))
		site, drain = siterevalidate.New(getenv("SITE_REVALIDATE_URL"), secret, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	}
	err = run(ctx, db, text, site, options{write: *write, limit: *limit, slug: *slug}, os.Stdout)
	drain()
	if err != nil {
		fail(err)
	}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func fail(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "topics-backfill: %v\n", err)
	os.Exit(1)
}

type options struct {
	write bool
	limit int
	slug  string
}

func run(ctx context.Context, db *sql.DB, text topics.TextGenerator, site siterevalidate.Notifier, opts options, out io.Writer) error {
	store := topics.NewStore(db, nowUTC)
	articles, err := store.Untagged(ctx, opts.slug, opts.limit)
	if err != nil {
		return err
	}
	sim, err := topics.NewSimulator(ctx, db)
	if err != nil {
		return err
	}
	extractor := topics.Extractor{Text: text}
	var tagged, empty, failed int
	touchedArticles, touchedTopics := map[string]bool{}, map[string]bool{}
	fmt.Fprintf(out, "%-60s  %s\n", "ARTICLE", "TOPICS")
	for _, article := range articles {
		entities, err := extractor.Extract(ctx, article)
		if err != nil {
			failed++
			fmt.Fprintf(out, "%-60s  FAIL: %v\n", clip(article.Slug, 60), err)
			continue
		}
		if len(entities) == 0 {
			empty++
			fmt.Fprintf(out, "%-60s  (none)\n", clip(article.Slug, 60))
			continue
		}
		var names []string
		for _, entity := range entities {
			names = append(names, entity.Name+" ("+entity.Kind+")")
		}
		fmt.Fprintf(out, "%-60s  %s\n", clip(article.Slug, 60), strings.Join(names, ", "))
		sim.Add(entities)
		if opts.write {
			assignment, err := store.Assign(ctx, article.ID, entities)
			if err != nil {
				return fmt.Errorf("store topics of %s: %w", article.Slug, err)
			}
			for _, slug := range assignment.Articles {
				touchedArticles[slug] = true
			}
			for _, slug := range assignment.Topics {
				touchedTopics[slug] = true
			}
		}
		tagged++
	}
	mode := "dry run"
	counts := sim.Counts()
	if opts.write {
		mode = "written"
		if counts, err = store.Counts(ctx); err != nil {
			return err
		}
		siterevalidate.NotifyWithTopics(site, keys(touchedArticles), keys(touchedTopics))
	}
	fmt.Fprintf(out, "\n%s: %d articles checked, %d tagged, %d without topics, %d failed\n\n", mode, len(articles), tagged, empty, failed)
	fmt.Fprintf(out, "%-8s %-40s %-9s %s\n", "ARTICLES", "TOPIC", "KIND", "STATUS")
	live := 0
	for _, topic := range counts {
		status := "needs more articles"
		if topic.Articles >= content.MinLiveTopicArticles {
			status = "live"
			live++
		}
		fmt.Fprintf(out, "%-8d %-40s %-9s %s\n", topic.Articles, clip(topic.Name+" ("+topic.Slug+")", 40), topic.Kind, status)
	}
	fmt.Fprintf(out, "\n%d topics, %d live (%d or more articles)\n", len(counts), live, content.MinLiveTopicArticles)
	return nil
}

func keys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	return out
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "~"
}

func nowUTC() time.Time { return time.Now().UTC() }
