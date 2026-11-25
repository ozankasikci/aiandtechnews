// Command analysis-try drafts and checks analysis without storing any result.
// It uses GEMINI_API_KEY and GEMINI_VISION_MODEL, like the API's Flash pass.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/analysis"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer, getenv func(string) string) error {
	flags := flag.NewFlagSet("analysis-try", flag.ContinueOnError)
	dbPath := flags.String("db", "", "SQLite database (opened read-only)")
	latest := flags.Int("latest", 10, "number of newest published articles")
	slug := flags.String("slug", "", "one published article slug")
	if err := flags.Parse(args); err != nil {
		return err
	}
	explicitLatest := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "latest" {
			explicitLatest = true
		}
	})
	if *dbPath == "" || *latest < 1 || (*slug != "" && explicitLatest) || flags.NArg() != 0 {
		return errors.New("use -db PATH and either -latest N (default 10) or -slug X")
	}
	if getenv("GEMINI_API_KEY") == "" {
		return errors.New("GEMINI_API_KEY is required")
	}
	db, err := database.OpenExisting(ctx, *dbPath, true)
	if err != nil {
		return err
	}
	defer db.Close()
	reader := analysis.NewReader(db)
	var articles []analysis.Article
	if *slug != "" {
		article, err := reader.BySlug(ctx, *slug)
		if err != nil {
			return err
		}
		articles = []analysis.Article{article}
	} else {
		articles, err = reader.Latest(ctx, *latest)
		if err != nil {
			return err
		}
	}
	model := getenv("GEMINI_VISION_MODEL")
	if model == "" {
		model = gemini.DefaultVisionModel
	}
	text := gemini.New(getenv("GEMINI_API_KEY"), model)
	var failures []error
	for _, article := range articles {
		fmt.Fprintf(out, "\nArticle: %s (%s)\n", article.Title, article.Slug)
		related, err := reader.Related(ctx, article.ID)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Related articles: %d\n", len(related))
		for _, r := range related {
			fmt.Fprintf(out, "  %s (%s, %s): %s\n", r.Title, r.Slug, r.PublishedAt, r.Excerpt)
		}
		attempts, result, err := analysis.Evaluate(ctx, analysis.Writer{Text: text}, analysis.Checker{Text: text}, article, related)
		printEvaluation(out, attempts, result)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", article.Slug, err))
		}
	}
	return errors.Join(failures...)
}

// printEvaluation keeps every draft and verdict visible, including a rejected
// first pass when the revision succeeds. It never stores diagnostic drafts.
func printEvaluation(out io.Writer, attempts []analysis.Attempt, result analysis.Result) {
	for i, attempt := range attempts {
		fmt.Fprintf(out, "Draft %d: %+v\n", i+1, attempt.Draft)
		if attempt.Verdict == nil {
			fmt.Fprintf(out, "Checker %d: not run or no valid verdict\n", i+1)
		} else {
			fmt.Fprintf(out, "Checker %d: ok=%t reason=%s\n", i+1, attempt.Verdict.OK, attempt.Verdict.Reason)
		}
	}
	fmt.Fprintf(out, "Final: %s %s\n", result.Status, result.Reason)
}
