// Command inline-redraw redraws the in-article illustration of chosen
// published articles with the current rules. Real photos (credited) are
// kept. -dry-run writes <slug>.png per article into -out and changes
// nothing; a live run writes -undo first (the file must not exist; it is
// never overwritten), then stores each new image, updates article_images in
// place and refreshes the site pages. A failed draw is skipped and reported
// at the end (the exit status is then non-zero); the articles that did
// change are still refreshed. Run it while the API's inline worker is idle
// (INLINE_IMAGES_ENABLED off or no new long article pending).
//
//	inline-redraw -db ~/aiandtechnews/data/technews.db -slugs slugs.txt -dry-run -out ./redraw
//	inline-redraw -db ~/aiandtechnews/data/technews.db -slugs slugs.txt -undo ./undo.json
//
// It reads the same environment as the API: GEMINI_*, CODEX_*, CUTOUT_BIN,
// FEATURED_IMAGE_CHAIN, FEATURED_IMAGE_ANALYZER, AWS_REGION, S3_FEATURE_IMAGE_*,
// SITE_REVALIDATE_URL and NEWSLETTER_CRON_SECRET.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/inlineimage"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/siterevalidate"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "inline-redraw: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string) error {
	flags := flag.NewFlagSet("inline-redraw", flag.ContinueOnError)
	dbPath := flags.String("db", getenv("DATABASE_PATH"), "SQLite database")
	slugsFile := flags.String("slugs", "", "file with one article slug per line (required)")
	dryRun := flags.Bool("dry-run", false, "draw to -out only; change nothing")
	outDir := flags.String("out", "inline-redraw-out", "folder for dry-run images")
	fromDir := flags.String("from", "", "live run: store <dir>/<slug>.png from an approved dry run instead of drawing again; alt text from <dir>/alts.txt (\"slug | alt\" lines)")
	undoPath := flags.String("undo", "inline-redraw-undo.json", "file the current images are saved to before a live run")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" || *slugsFile == "" {
		return errors.New("-db (or DATABASE_PATH) and -slugs are required")
	}
	raw, err := os.ReadFile(*slugsFile)
	if err != nil {
		return err
	}
	var slugs []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			slugs = append(slugs, line)
		}
	}
	slugs = dedupe(slugs)
	chain, err := config.ParseImageChain(getenv("FEATURED_IMAGE_CHAIN"))
	if err != nil {
		return err
	}
	analyzers, err := config.ParseImageAnalyzers(getenv("FEATURED_IMAGE_ANALYZER"), chain)
	if err != nil {
		return err
	}
	db, err := database.OpenExisting(ctx, *dbPath, *dryRun)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	geminiClient := gemini.New(getenv("GEMINI_API_KEY"), getenv("GEMINI_TEXT_MODEL"),
		gemini.WithImageModel(getenv("GEMINI_IMAGE_MODEL")), gemini.WithImageSize(getenv("GEMINI_IMAGE_SIZE")),
		gemini.WithVisionModel(getenv("GEMINI_VISION_MODEL")))
	pipelineConfig := illustration.PipelineConfig{
		Chain:         chain,
		Analyzers:     analyzers,
		CodexBin:      getenv("CODEX_BIN"),
		CodexNodeDir:  getenv("CODEX_NODE_DIR"),
		CodexTimeout:  config.DefaultCodexTimeout,
		CutoutBin:     getenv("CUTOUT_BIN"),
		Gemini:        geminiClient,
		History:       illustration.NewSettingsHistory(db),
		InlineHistory: illustration.NewSettingsHistoryAt(db, illustration.InlineHistoryKey),
		HTTP:          illustration.NewReferenceClient(),
		Logger:        logger,
	}
	articles := &sqliteArticles{db: db, store: inlineimage.NewSQLiteStore(db, time.Now)}

	if *dryRun {
		// No Store: ProduceInline never uploads.
		pipeline := illustration.NewPipeline(illustration.BuildPipelineDeps(pipelineConfig))
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			return err
		}
		// Like the live pipeline: the covers' and inline images' scenes,
		// plus what this run has drawn so far, at most the last 20.
		var base []string
		for _, history := range []illustration.History{pipelineConfig.History, pipelineConfig.InlineHistory} {
			if recent, err := history.Recent(ctx); err == nil {
				base = append(base, illustration.VarietyFrom(recent).RecentScenes...)
			}
		}
		var seen []string
		for _, slug := range slugs {
			current, err := articles.Current(ctx, slug)
			if err != nil {
				return err
			}
			if current.URL == "" || current.Credited {
				fmt.Printf("skip %s (no inline image or a real photo)\n", slug)
				continue
			}
			request := inlineRequest(current)
			request.RecentScenes = lastScenes(base, seen, 20)
			result, err := pipeline.ProduceInline(ctx, request)
			if err != nil {
				fmt.Printf("FAIL %s: %v\n", slug, err)
				continue
			}
			if err := os.WriteFile(filepath.Join(*outDir, slug+".png"), result.Image, 0o644); err != nil {
				return err
			}
			if result.Report.Brief != nil {
				seen = append(seen, illustration.SceneNote(*result.Report.Brief))
			}
			fmt.Printf("ok %s | %s\n", slug, result.Alt)
		}
		return nil
	}

	bucket, publicURL := getenv("S3_FEATURE_IMAGE_BUCKET"), getenv("S3_FEATURE_IMAGE_PUBLIC_URL")
	if bucket == "" || publicURL == "" {
		return errors.New("S3_FEATURE_IMAGE_BUCKET and S3_FEATURE_IMAGE_PUBLIC_URL are required")
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(getenv("AWS_REGION")))
	if err != nil {
		return fmt.Errorf("load AWS configuration: %w", err)
	}
	pipelineConfig.Store = media.NewStore(media.Config{
		Region: getenv("AWS_REGION"), Bucket: bucket, Prefix: getenv("S3_FEATURE_IMAGE_PREFIX"), PublicBaseURL: publicURL,
	}, s3.NewFromConfig(awsCfg), &http.Client{Timeout: 30 * time.Second}, time.Now)
	pipeline := illustration.NewPipeline(illustration.BuildPipelineDeps(pipelineConfig))

	var drawer Drawer = pipelineDrawer{pipeline}
	if *fromDir != "" {
		alts, err := readAlts(filepath.Join(*fromDir, "alts.txt"))
		if err != nil {
			return err
		}
		drawer = fileDrawer{pipeline: pipeline, dir: *fromDir, alts: alts}
	}
	deps := Deps{Articles: articles, Drawer: drawer}
	if url := getenv("SITE_REVALIDATE_URL"); url != "" {
		deps.Refresher = siterevalidate.NewClient(url, getenv("NEWSLETTER_CRON_SECRET"), &http.Client{Timeout: 60 * time.Second})
	}
	report, err := Redraw(ctx, deps, slugs, *undoPath)
	fmt.Printf("redrawn %d: %v\nskipped %d: %v\nfailed %d: %v\nundo file: %s\n", len(report.Redrawn), report.Redrawn, len(report.Skipped), report.Skipped, len(report.Failed), report.Failed, *undoPath)
	return err
}

// lastScenes is base followed by seen, trimmed from the front to max entries.
func lastScenes(base, seen []string, max int) []string {
	all := append(append([]string{}, base...), seen...)
	if len(all) > max {
		all = all[len(all)-max:]
	}
	return all
}

func inlineRequest(current Current) illustration.InlineRequest {
	return illustration.InlineRequest{
		Slug: current.Slug, Title: current.Title,
		Section:          inlineimage.SectionText(current.Content, current.AfterParagraph),
		FeaturedImageURL: current.FeaturedImage, SourceImageURL: current.SourceImage,
	}
}

type pipelineDrawer struct{ pipeline *illustration.Pipeline }

func (d pipelineDrawer) Draw(ctx context.Context, current Current) (illustration.InlineIllustration, error) {
	return d.pipeline.IllustrateInline(ctx, inlineRequest(current))
}

type sqliteArticles struct {
	db    *sql.DB
	store *inlineimage.SQLiteStore
}

func (s *sqliteArticles) Current(ctx context.Context, slug string) (Current, error) {
	article, err := s.store.BySlug(ctx, slug)
	if err != nil {
		return Current{}, err
	}
	current := Current{ID: article.ID, Slug: article.Slug, Title: article.Title, Content: article.Content,
		FeaturedImage: article.FeaturedImage, SourceImage: article.SourceImage}
	err = s.db.QueryRowContext(ctx, `SELECT i.url, i.alt, i.after_paragraph,
		EXISTS(SELECT 1 FROM article_image_credits c WHERE c.article_image_id = i.id)
		FROM article_images i WHERE i.article_id = ? AND i.status = 'ready'`, article.ID).
		Scan(&current.URL, &current.Alt, &current.AfterParagraph, &current.Credited)
	if errors.Is(err, sql.ErrNoRows) {
		return current, nil
	}
	return current, err
}

func (s *sqliteArticles) Replace(ctx context.Context, current Current, url, alt string) error {
	return s.store.MarkReady(ctx, current.ID, url, alt, current.AfterParagraph, 1, nil)
}

// fileDrawer stores an image approved in a dry run instead of drawing a new
// one: <dir>/<slug>.png, with its alt text from the dry run's output.
type fileDrawer struct {
	pipeline *illustration.Pipeline
	dir      string
	alts     map[string]string
}

func (d fileDrawer) Draw(ctx context.Context, current Current) (illustration.InlineIllustration, error) {
	alt, ok := d.alts[current.Slug]
	if !ok || alt == "" {
		return illustration.InlineIllustration{}, fmt.Errorf("no alt text for %s in alts.txt", current.Slug)
	}
	image, err := os.ReadFile(filepath.Join(d.dir, current.Slug+".png"))
	if err != nil {
		return illustration.InlineIllustration{}, err
	}
	stored, err := d.pipeline.StoreInlinePhoto(ctx, current.Slug, image)
	if err != nil {
		return illustration.InlineIllustration{}, err
	}
	stored.Alt = alt
	return stored, nil
}

// readAlts reads "slug | alt" lines (the dry run prints them as "ok slug | alt").
func readAlts(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	alts := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimPrefix(strings.TrimSpace(line), "ok ")
		slug, alt, found := strings.Cut(line, " | ")
		if found {
			alts[strings.TrimSpace(slug)] = strings.TrimSpace(alt)
		}
	}
	return alts, nil
}
