// Command relevance-try fetches the approved feeds and prints what the AI
// relevance judge would decide for up to -n items that pass the structural
// rules, next to the old keyword rule. It never writes to a database.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/collector"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/relevance"
)

func main() {
	n := flag.Int("n", 25, "items to judge")
	model := flag.String("model", relevance.DefaultModel, "Codex model")
	effort := flag.String("effort", relevance.DefaultEffort, "reasoning effort")
	flag.Parse()
	ctx := context.Background()
	fetcher := collector.NewFetcher()
	var items []collector.JudgeItem
	keyword := map[string]bool{}
	now := time.Now()
	for _, feed := range content.ApprovedFeeds() {
		body, _, err := fetcher.FetchText(ctx, feed.URL, "")
		if err != nil {
			continue
		}
		for _, item := range collector.ParseFeed(body, feed.Source) {
			if len(items) >= *n {
				break
			}
			if content.StructuralItemRejectionReason(item.Title, item.URL, item.Source, now) != "" {
				continue
			}
			judgeItem := collector.JudgeItem{Key: item.URL, Source: item.Source, Title: item.Title, Summary: item.Summary}
			if page, _, err := fetcher.FetchText(ctx, item.URL, item.Source); err == nil {
				judgeItem.Text = collector.ArticleText(page)
			}
			items = append(items, judgeItem)
			keyword[item.URL] = content.MentionsAI(item.Title, item.URL)
		}
	}
	runner := &illustration.CodexRunner{Bin: os.Getenv("CODEX_BIN"), NodeDir: os.Getenv("CODEX_NODE_DIR"), Timeout: relevance.DefaultTimeout}
	started := time.Now()
	verdicts, err := relevance.New(runner, *model, *effort).Judge(ctx, items)
	if err != nil {
		fmt.Fprintln(os.Stderr, "judge:", err)
		os.Exit(1)
	}
	fmt.Printf("%d items judged in %s\n", len(verdicts), time.Since(started).Round(time.Second))
	for _, item := range items {
		v := verdicts[item.Key]
		mark := "no "
		if v.Candidate {
			mark = "YES"
		}
		fmt.Printf("%s keyword=%-5v %-12.12s %-80.80s | %s\n", mark, keyword[item.Key], item.Source, item.Title, v.Reason)
	}
}
