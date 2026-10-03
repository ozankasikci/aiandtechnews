// Command primary-try shows what the primary source collector would do,
// without a database. By default it fetches the primary feeds and prints
// which fresh items the AI relevance judge would turn into candidates. With
// -write URL it writes the report for one primary document and prints it.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/codextext"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/collector"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/relevance"
)

func main() {
	write := flag.String("write", "", "write the report for this primary document URL")
	title := flag.String("title", "", "document title for -write")
	maxAge := flag.Duration("max-age", content.PrimaryItemMaxAge, "how old an item may be")
	flag.Parse()
	ctx := context.Background()
	fetcher := collector.NewFetcher()
	if *write != "" {
		writeReport(ctx, fetcher, *write, *title)
		return
	}

	now := time.Now()
	var items []collector.JudgeItem
	for _, feed := range content.PrimaryFeeds() {
		body, _, err := fetcher.FetchText(ctx, feed.URL, "")
		if err != nil {
			fmt.Printf("%-34s FETCH FAILED %v\n", feed.Source, err)
			continue
		}
		parsed := collector.ParseFeed(body, feed.Source)
		fresh, mismatched := 0, 0
		for _, item := range parsed {
			if feed.TitlePrefix != "" {
				if content.IsPrerelease(item.Title) {
					continue
				}
				item.Title = feed.TitlePrefix + " " + item.Title
			}
			if item.PublishedAt == nil || now.Sub(*item.PublishedAt) > *maxAge {
				continue
			}
			if reason := content.StructuralItemRejectionReason(item.Title, item.URL, item.Source, now); reason != "" {
				if strings.Contains(reason, "RSS publication") || strings.Contains(reason, "approved") {
					mismatched++
				}
				continue
			}
			fresh++
			judgeItem := collector.JudgeItem{Key: item.URL, Source: item.Source, Title: item.Title, Summary: item.Summary, Primary: true}
			if page, _, err := fetcher.FetchText(ctx, item.URL, item.Source); err == nil {
				judgeItem.Text = collector.ArticleText(page)
			}
			items = append(items, judgeItem)
		}
		fmt.Printf("%-34s items=%d fresh=%d url-mismatch=%d\n", feed.Source, len(parsed), fresh, mismatched)
	}
	runner := &illustration.CodexRunner{Bin: os.Getenv("CODEX_BIN"), NodeDir: os.Getenv("CODEX_NODE_DIR"), Timeout: relevance.DefaultTimeout}
	judge := relevance.New(runner, os.Getenv("NEWSROOM_RELEVANCE_MODEL"), os.Getenv("NEWSROOM_RELEVANCE_EFFORT"))
	fmt.Printf("\n%d fresh items to judge\n\n", len(items))
	for start := 0; start < len(items); start += 25 {
		batch := items[start:min(start+25, len(items))]
		verdicts, err := judge.Judge(ctx, batch)
		if err != nil {
			fmt.Fprintln(os.Stderr, "judge:", err)
			os.Exit(1)
		}
		for _, item := range batch {
			verdict := verdicts[item.Key]
			mark := "no "
			if verdict.Candidate {
				mark = "YES"
			}
			fmt.Printf("%s [%s] %s\n      %s\n      %s\n", mark, item.Source, item.Title, verdict.Reason, item.Key)
		}
	}
}

func writeReport(ctx context.Context, fetcher *collector.Fetcher, pageURL, title string) {
	source, ok := content.SourceForURL(pageURL)
	if !ok || !content.IsPrimarySource(source) {
		fmt.Fprintln(os.Stderr, "not a primary source URL:", pageURL)
		os.Exit(2)
	}
	body, finalURL, err := fetcher.FetchText(ctx, pageURL, source)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fetch:", err)
		os.Exit(1)
	}
	document := publisher.ExtractDocumentText(body)
	fmt.Printf("source: %s\ndocument: %d chars\n--- document start ---\n%s\n--- end ---\n\n", source, len(document), clip(document, 1500))
	if content.JavaScriptLength(document) < publisher.MinDocumentTextLength {
		fmt.Fprintln(os.Stderr, "document too short")
		os.Exit(1)
	}
	client := gemini.New(os.Getenv("GEMINI_API_KEY"), os.Getenv("GEMINI_TEXT_MODEL"))
	started := time.Now()
	rewriter := publisher.NewRewriter(client)
	if bin := os.Getenv("CODEX_BIN"); bin != "" && os.Getenv("PRIMARY_TRY_WRITER") != "gemini" {
		runner := &illustration.CodexRunner{Bin: bin, NodeDir: os.Getenv("CODEX_NODE_DIR"), Timeout: codextext.DefaultTimeout}
		rewriter.WithReporter(codextext.New(runner, os.Getenv("PRIMARY_REPORT_MODEL"), os.Getenv("PRIMARY_REPORT_EFFORT")))
	}
	article, err := rewriter.Rewrite(ctx, publisher.RewriteInput{
		Source: source, Title: title, CanonicalURL: publisher.ExtractCanonicalURL(body, finalURL), SourceText: document, Primary: true,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "report:", err)
		os.Exit(1)
	}
	fmt.Printf("written and fact-checked in %s\n\nTITLE: %s\nEXCERPT: %s\nTLDR: %s\nWHY: %s\n\n%s\n", time.Since(started).Round(time.Second),
		article.Title, article.Excerpt, strings.Join(article.TLDR, " / "), article.WhyItMatters,
		strings.ReplaceAll(strings.ReplaceAll(article.Content, "</p>", "</p>\n\n"), "</h2>", "</h2>\n"))
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
