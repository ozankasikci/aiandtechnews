// Command photo-try shows which real photo the featured-image "photo" step
// would choose for published articles. It opens an immutable database copy
// read-only and writes report.html and the chosen 16:9 photos to -out.
//
//	go run ./cmd/photo-try -db ~/copy-of-technews.db -latest 30 -out ./photo-try
//	go run ./cmd/photo-try -db ~/copy-of-technews.db -slugs a-slug,another-slug
//
// The copy must have migration 19. The command reads GEMINI_API_KEY,
// GEMINI_TEXT_MODEL and GEMINI_VISION_MODEL from the environment. It excludes
// featured photo files used in the last 30 days and skips stand-ins that match
// a stand-in or exact person used in the last 7 days. It also tracks photo
// files, stand-ins and exact people chosen earlier in the run.
package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/inlineimage"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
	_ "modernc.org/sqlite"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "photo-try: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string) error {
	flags := flag.NewFlagSet("photo-try", flag.ContinueOnError)
	dbPath := flags.String("db", getenv("DATABASE_PATH"), "stable SQLite database copy, opened read-only")
	latest := flags.Int("latest", 30, "how many of the newest published articles to try")
	slugs := flags.String("slugs", "", "comma-separated slugs to try instead of -latest")
	outDir := flags.String("out", "photo-try", "folder for report.html and photo files")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" {
		return errors.New("needs -db (or DATABASE_PATH)")
	}
	db, err := openDatabaseCopy(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	output, err := os.OpenRoot(*outDir)
	if err != nil {
		return err
	}
	defer func() { _ = output.Close() }()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	geminiClient := gemini.New(getenv("GEMINI_API_KEY"), getenv("GEMINI_TEXT_MODEL"), gemini.WithVisionModel(getenv("GEMINI_VISION_MODEL")))
	finder := realphoto.NewFinder(geminiClient, geminiClient, illustration.NewReferenceClient(), logger).WithSearch(geminiClient)
	if err := writeRun(ctx, db, output, finder, *slugs, *latest); err != nil {
		return err
	}
	fmt.Printf("\nreport: %s\n", filepath.Join(*outDir, reportName))
	return nil
}

const reportName = "report.html"

func writeRun(ctx context.Context, db *sql.DB, output *os.Root, finder illustration.PhotoFinder, slugs string, latest int) error {
	store := inlineimage.NewSQLiteStore(db, nil)
	published := publisher.NewSQLiteArticles(db, nil)
	now := time.Now()
	used, err := published.RecentFeaturedPhotoURLs(ctx, now.Add(-illustration.PhotoReuseWindow))
	if err != nil {
		if strings.Contains(err.Error(), "no such table: article_featured_image_credits") {
			return fmt.Errorf("%w (run cmd/migrate on this copy first: migration 18 is missing)", err)
		}
		return err
	}
	subjects, err := published.RecentFeaturedSubjects(ctx, now.Add(-illustration.StandInReuseWindow))
	if err != nil {
		if strings.Contains(err.Error(), "no such table: article_featured_stand_ins") {
			return fmt.Errorf("%w (run cmd/migrate on this copy first: migration 19 is missing)", err)
		}
		return err
	}

	var articles []inlineimage.Article
	if slugs != "" {
		seen := make(map[string]bool)
		for _, slug := range strings.Split(slugs, ",") {
			if slug = strings.TrimSpace(slug); slug == "" || seen[slug] {
				continue
			}
			seen[slug] = true
			article, err := store.BySlug(ctx, slug)
			if err != nil {
				return fmt.Errorf("%s: %w", slug, err)
			}
			articles = append(articles, article)
		}
	} else if articles, err = store.Latest(ctx, latest); err != nil {
		return err
	}

	var rows []reportRow
	for _, article := range articles {
		request := realphoto.Request{
			Slug: article.Slug, Title: article.Title, Text: content.StripHTML(article.Content),
			SourceURL: article.SourceURL, Slot: realphoto.SlotFeatured, Exclude: used, RecentFeaturedSubjects: subjects,
		}
		found := finder.Find(ctx, request)
		row := reportRow{Slug: article.Slug, Title: article.Title, Current: article.FeaturedImage, Reason: found.Reason, Candidates: found.Candidates}
		if found.Found && found.Credit != nil && found.Chosen != nil {
			focusX, focusY := 0.5, 0.5
			if verdict := found.Chosen.Verdict; verdict != nil {
				focusX, focusY = verdict.FocusX, verdict.FocusY
			}
			cropped, err := imaging.CropToAspect(found.Image, 16.0/9.0, focusX, focusY)
			if err != nil {
				row.Reason = "crop: " + err.Error()
			} else {
				// CropToAspect preserves the original format for an image already at 16:9.
				extension := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/webp": ".webp"}[imaging.SniffMIME(cropped)]
				row.Image = article.Slug + extension
				if err := output.WriteFile(row.Image, cropped, 0o644); err != nil {
					return err
				}
				row.Found, row.Credit, row.CreditURL = true, found.Credit.Text, found.Credit.URL
				row.Verdict = found.Chosen.Verdict
				row.Cropped = !bytes.Equal(cropped, found.Image)
				if found.StandIn != nil {
					row.StandIn = found.StandIn.Subject
				}
				if use := found.FeaturedSubjectUse(); use != nil {
					subjects = append(subjects, *use)
				}
				used = append(used, found.Chosen.ImageURL)
			}
		}
		outcome := "drawn"
		if row.Found {
			outcome = "photo"
		}
		fmt.Printf("%-6s %-60s %s\n", outcome, article.Slug, row.Reason)
		rows = append(rows, row)
		if ctx.Err() != nil {
			break
		}
	}

	file, err := output.OpenFile(reportName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	return errors.Join(renderReport(file, rows), file.Close())
}

func openDatabaseCopy(ctx context.Context, path string) (*sql.DB, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database copy path: %w", err)
	}
	// An ordinary read-only connection may create WAL and shared-memory files.
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(absolute), RawQuery: "mode=ro&immutable=1"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, fmt.Errorf("open database copy: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("open database copy: %w", err), db.Close())
	}
	return db, nil
}
