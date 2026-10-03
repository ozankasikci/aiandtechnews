// Command telegram-post sends one published article to a Telegram chat
// exactly as the channel worker would (internal/telegram), and prints the new
// message's id. It is for manual test posts: it opens the database read-only
// and records nothing in telegram_posts.
//
//	go run ./cmd/telegram-post -db /path/technews.db -slug some-article [-chat @mychannel]
//
// It reads TELEGRAM_BOT_TOKEN, TELEGRAM_CHANNEL (the default -chat),
// DATABASE_PATH (the default -db) and NEWSLETTER_SITE_URL (the links' site,
// default https://aiandtech.news) from the environment.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsletter"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/telegram"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv, telegram.DefaultBaseURL, os.Stdout); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "telegram-post: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, baseURL string, stdout io.Writer) error {
	flags := flag.NewFlagSet("telegram-post", flag.ContinueOnError)
	dbPath := flags.String("db", getenv("DATABASE_PATH"), "SQLite database path")
	slug := flags.String("slug", "", "slug of the published article to post (required)")
	chat := flags.String("chat", getenv("TELEGRAM_CHANNEL"), `chat to post to: "@name" or a numeric id`)
	if err := flags.Parse(args); err != nil {
		return err
	}
	token := getenv("TELEGRAM_BOT_TOKEN")
	switch {
	case *slug == "":
		return errors.New("-slug is required")
	case *chat == "":
		return errors.New("-chat or TELEGRAM_CHANNEL is required")
	case token == "":
		return errors.New("TELEGRAM_BOT_TOKEN is required")
	}
	siteURL, err := newsletter.SiteOrigin(getenv("NEWSLETTER_SITE_URL"))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	db, err := database.OpenExisting(ctx, *dbPath, true)
	if err != nil {
		return err
	}
	defer db.Close()
	post, err := telegram.NewSQLiteStore(db, nil, nil).ArticleBySlug(ctx, *slug)
	if err != nil {
		return err
	}
	client := telegram.NewClient(token, baseURL, nil)
	messageID, err := client.Post(ctx, *chat, telegram.Caption(post, siteURL), telegram.PhotoURL(post.FeaturedImage, siteURL))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, messageID)
	return err
}
