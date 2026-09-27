// Command imagegen-try runs the featured-image pipeline once, as a dry run,
// and writes the final image and a JSON report to a local folder. It never
// touches a database and never uploads anything.
//
//	go run ./cmd/imagegen-try -candidate 136 -title "..." -excerpt "..." \
//	    -image-url https://... -chain codex,gemini,source -out ./tmp/try-136
//
// With -inline <slug> it instead makes the in-article illustration of a
// published article, read from -db (opened read-only), and writes it to the
// output folder; nothing is uploaded and no row is written:
//
//	go run ./cmd/imagegen-try -inline some-article-slug -db ~/aiandtechnews/data/technews.db
//
// Like the worker, it first looks for a real photo (a Wikimedia Commons
// photo, then the maker's official image) and draws only when none passes;
// -real-photos-only stops after the photo search, -no-real-photos skips it.
// Every candidate is printed with its source, URL, licence, verdict and
// score. -inline-latest N runs the photo search over the newest N published
// articles and prints a table, writing each chosen photo to the output
// folder:
//
//	go run ./cmd/imagegen-try -inline-latest 10 -db ~/aiandtechnews/data/technews.db
//
// It reads GEMINI_API_KEY, GEMINI_TEXT_MODEL, GEMINI_IMAGE_MODEL,
// GEMINI_IMAGE_SIZE, GEMINI_VISION_MODEL, CODEX_BIN, CODEX_NODE_DIR,
// CODEX_TIMEOUT, CUTOUT_BIN, FEATURED_IMAGE_CHAIN and FEATURED_IMAGE_ANALYZER
// from the environment; -chain and -analyzer override the last two.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/inlineimage"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "imagegen-try: %v\n", err)
		os.Exit(1)
	}
}

type output struct {
	Candidate string              `json:"candidate,omitempty"`
	Title     string              `json:"title"`
	ImageURL  string              `json:"image_url,omitempty"`
	Chain     []string            `json:"chain"`
	Analyzers []string            `json:"analyzers"`
	Image     string              `json:"image,omitempty"`
	WebP      string              `json:"webp,omitempty"`
	Error     string              `json:"error,omitempty"`
	Report    illustration.Report `json:"report"`
}

func run(ctx context.Context, args []string, getenv func(string) string) error {
	flags := flag.NewFlagSet("imagegen-try", flag.ContinueOnError)
	candidate := flags.String("candidate", "", "candidate id, used only as a label and for the slug")
	title := flags.String("title", "", "headline (required)")
	excerpt := flags.String("excerpt", "", "summary or feed excerpt")
	imageURL := flags.String("image-url", "", "source og:image or feed image URL")
	chainFlag := flags.String("chain", getenv("FEATURED_IMAGE_CHAIN"), "providers in order, e.g. codex,gemini,source")
	analyzerFlag := flags.String("analyzer", getenv("FEATURED_IMAGE_ANALYZER"), "analyzers in order (default: from the chain)")
	outDir := flags.String("out", "", "output folder (default ./imagegen-try-<candidate or time>)")
	prefer := flags.String("prefer", "", "composition to lean towards for variety (scene or simple)")
	compositions := flags.String("compositions", "", "comma-separated compositions allowed (scene,simple); default both")
	avoidStyles := flags.String("avoid-styles", "", "comma-separated styles to avoid, as if the latest articles used them")
	avoidShots := flags.String("avoid-shots", "", "comma-separated camera framings to avoid (as the history would after recent images)")
	avoid := flags.String("avoid", "", "comma-separated palettes to avoid, as if the latest articles used them")
	analyzeOnly := flags.Bool("analyze-only", false, "only run the analyzers and print the brief")
	inline := flags.String("inline", "", "make the inline (in-article) illustration of this published article's slug")
	dbPath := flags.String("db", getenv("DATABASE_PATH"), "SQLite database for -inline, opened read-only")
	photosOnly := flags.Bool("real-photos-only", false, "with -inline: only look for a real photo, never draw")
	noPhotos := flags.Bool("no-real-photos", false, "with -inline: skip the real photo search and draw")
	latest := flags.Int("inline-latest", 0, "look for real photos for the newest N published articles and print a table")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *inline == "" && *title == "" && *latest <= 0 {
		return errors.New("-title is required")
	}
	if *photosOnly && *noPhotos {
		return errors.New("-real-photos-only and -no-real-photos exclude each other")
	}
	if *chainFlag == "" {
		*chainFlag = "codex,gemini,source"
	}
	chain, err := config.ParseImageChain(*chainFlag)
	if err != nil {
		return err
	}
	analyzers, err := config.ParseImageAnalyzers(*analyzerFlag, chain)
	if err != nil {
		return err
	}
	codexTimeout := config.DefaultCodexTimeout
	if value := getenv("CODEX_TIMEOUT"); value != "" {
		if codexTimeout, err = time.ParseDuration(value); err != nil {
			return fmt.Errorf("CODEX_TIMEOUT: %w", err)
		}
	}
	label := *candidate
	if *inline != "" {
		label = "inline-" + *inline
	}
	if *latest > 0 {
		label = "inline-latest-" + time.Now().Format("20060102-150405")
	}
	if label == "" {
		label = time.Now().Format("20060102-150405")
	}
	if *outDir == "" {
		*outDir = "imagegen-try-" + label
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	geminiClient := gemini.New(getenv("GEMINI_API_KEY"), getenv("GEMINI_TEXT_MODEL"),
		gemini.WithImageModel(getenv("GEMINI_IMAGE_MODEL")), gemini.WithImageSize(getenv("GEMINI_IMAGE_SIZE")),
		gemini.WithVisionModel(getenv("GEMINI_VISION_MODEL")))
	pipeline := illustration.NewPipeline(illustration.BuildPipelineDeps(illustration.PipelineConfig{
		Chain:        chain,
		Analyzers:    analyzers,
		CodexBin:     getenv("CODEX_BIN"),
		CodexNodeDir: getenv("CODEX_NODE_DIR"),
		CodexTimeout: codexTimeout,
		CutoutBin:    getenv("CUTOUT_BIN"),
		Gemini:       geminiClient,
		HTTP:         illustration.NewReferenceClient(),
		Logger:       logger,
		// No Store: Produce never uploads.
	}))

	var photos *realphoto.Finder
	if !*noPhotos {
		photos = realphoto.NewFinder(geminiClient, geminiClient, illustration.NewReferenceClient(), logger)
	}
	if *latest > 0 {
		return runInlineLatest(ctx, photos, *latest, *dbPath, *outDir, getenv)
	}
	if *inline != "" {
		return runInline(ctx, pipeline, photos, *photosOnly, *inline, *dbPath, *outDir, chain, analyzers, getenv)
	}
	request := publisher.IllustrationRequest{AvoidPalettes: splitList(*avoid), AvoidStyles: splitList(*avoidStyles), Compositions: splitList(*compositions), PreferComposition: *prefer, AvoidShots: splitList(*avoidShots), Slug: "imagegen-try-" + label, Title: *title, Excerpt: *excerpt, ReferenceImageURL: *imageURL}
	if *analyzeOnly {
		report, err := pipeline.AnalyzeOnly(ctx, request)
		encoded, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(encoded))
		return err
	}
	result, produceErr := pipeline.Produce(ctx, request)
	out := output{Candidate: *candidate, Title: *title, ImageURL: *imageURL, Chain: chain, Analyzers: analyzers, Report: result.Report}
	if produceErr != nil {
		out.Error = produceErr.Error()
	} else {
		extension := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/webp": ".webp"}[imaging.SniffMIME(result.Image)]
		out.Image = filepath.Join(*outDir, "final"+extension)
		if err := os.WriteFile(out.Image, result.Image, 0o644); err != nil {
			return err
		}
		// The WebP is what the publisher would upload.
		if webp, _, _, err := imaging.EncodeWebP(result.Image, 82); err == nil {
			out.WebP = filepath.Join(*outDir, "final.webp")
			if err := os.WriteFile(out.WebP, webp, 0o644); err != nil {
				return err
			}
		}
	}
	report, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*outDir, "report.json"), append(report, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Println(string(report))
	return produceErr
}

func splitList(value string) []string {
	var items []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

type inlineOutput struct {
	Slug           string               `json:"slug"`
	Title          string               `json:"title"`
	FeaturedImage  string               `json:"featured_image"`
	AfterParagraph int                  `json:"after_paragraph"`
	Section        string               `json:"section"`
	Path           string               `json:"path"`
	Photo          *realphoto.Result    `json:"photo,omitempty"`
	Chain          []string             `json:"chain,omitempty"`
	Analyzers      []string             `json:"analyzers,omitempty"`
	Alt            string               `json:"alt,omitempty"`
	Credit         *realphoto.Credit    `json:"credit,omitempty"`
	Image          string               `json:"image,omitempty"`
	WebP           string               `json:"webp,omitempty"`
	Error          string               `json:"error,omitempty"`
	Report         *illustration.Report `json:"report,omitempty"`
}

func openArticles(ctx context.Context, dbPath string) (*inlineimage.SQLiteStore, func(), error) {
	if dbPath == "" {
		return nil, nil, errors.New("-inline and -inline-latest need -db (or DATABASE_PATH)")
	}
	db, err := database.OpenExisting(ctx, dbPath, true)
	if err != nil {
		return nil, nil, err
	}
	return inlineimage.NewSQLiteStore(db, nil), func() { _ = db.Close() }, nil
}

// runInline makes one article's inline image as the worker would: a real
// photo when one passes, else an illustration; without uploading it or
// writing to the database.
func runInline(ctx context.Context, pipeline *illustration.Pipeline, photos *realphoto.Finder, photosOnly bool, slug, dbPath, outDir string, chain, analyzers []string, getenv func(string) string) error {
	store, closeDB, err := openArticles(ctx, dbPath)
	if err != nil {
		return err
	}
	defer closeDB()
	article, err := store.BySlug(ctx, slug)
	if err != nil {
		return err
	}
	slot, ok := inlineimage.Slot(article.Content)
	if photos != nil {
		// Like the worker: a real photo also reaches shorter articles.
		slot, ok = inlineimage.PhotoSlot(article.Content)
	}
	if !ok {
		return fmt.Errorf("%s is too short for an inline image; the worker would skip it", slug)
	}
	if prefix := inlineimage.OwnImagePrefix(getenv("S3_FEATURE_IMAGE_PUBLIC_URL"), s3Prefix(getenv)); photos == nil && prefix != "" && !strings.HasPrefix(article.FeaturedImage, prefix) {
		return fmt.Errorf("featured image %s is not under %s; the worker would skip it", article.FeaturedImage, prefix)
	}
	out := inlineOutput{Slug: slug, Title: article.Title, FeaturedImage: article.FeaturedImage, AfterParagraph: slot,
		Section: inlineimage.SectionText(article.Content, slot)}
	var runErr error
	if photos != nil {
		found := photos.Find(ctx, inlineimage.PhotoRequest(article))
		out.Photo = &found
		printPhotoResult(os.Stdout, article.Slug, found)
		if found.Found {
			out.Path, out.Alt, out.Credit = found.Credit.Kind, found.Alt, found.Credit
			out.Image, out.WebP, runErr = writePhoto(outDir, "inline", found.Image)
		}
	}
	if out.Path == "" && photosOnly {
		out.Path = "none"
	}
	if out.Path == "" {
		out.Path, out.Chain, out.Analyzers = "generated", chain, analyzers
		result, produceErr := pipeline.ProduceInline(ctx, illustration.InlineRequest{
			Slug: article.Slug, Title: article.Title, Section: out.Section,
			FeaturedImageURL: article.FeaturedImage, SourceImageURL: article.SourceImage,
		})
		out.Report = &result.Report
		if produceErr != nil {
			out.Error, runErr = produceErr.Error(), produceErr
		} else {
			out.Alt = result.Alt
			out.Image = filepath.Join(outDir, "inline.png")
			if err := os.WriteFile(out.Image, result.Image, 0o644); err != nil {
				return err
			}
			if webp, _, _, err := imaging.EncodeWebP(result.Image, 82); err == nil {
				out.WebP = filepath.Join(outDir, "inline.webp")
				if err := os.WriteFile(out.WebP, webp, 0o644); err != nil {
					return err
				}
			}
		}
	}
	report, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "report.json"), append(report, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("\npath: %s", out.Path)
	if out.Image != "" {
		fmt.Printf("  image: %s", out.Image)
	}
	fmt.Printf("\nreport: %s\n", filepath.Join(outDir, "report.json"))
	return runErr
}

// writePhoto writes the photo as downloaded and as the WebP the worker would
// store.
func writePhoto(outDir, name string, photo []byte) (string, string, error) {
	extension := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/webp": ".webp"}[imaging.SniffMIME(photo)]
	original := filepath.Join(outDir, name+"-original"+extension)
	if err := os.WriteFile(original, photo, 0o644); err != nil {
		return "", "", err
	}
	webp, _, _, err := imaging.EncodeWebPWidth(photo, 82, 1200, 1600)
	if err != nil {
		return original, "", nil
	}
	stored := filepath.Join(outDir, name+".webp")
	if err := os.WriteFile(stored, webp, 0o644); err != nil {
		return "", "", err
	}
	return original, stored, nil
}

// printPhotoResult prints the plan, every candidate and the choice.
func printPhotoResult(w io.Writer, slug string, found realphoto.Result) {
	fmt.Fprintf(w, "== %s\n", slug)
	if plan := found.Plan; plan != nil {
		fmt.Fprintf(w, "plan: photographable=%t subject=%q maker=%q queries=%q (%s)\n", plan.Photographable, plan.Subject, plan.Maker, plan.Queries, plan.Reason)
	}
	if found.Official != "" {
		fmt.Fprintf(w, "official page: %s\n", found.Official)
	}
	table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if len(found.Candidates) > 0 {
		fmt.Fprintln(table, "SOURCE\tSIZE\tLICENCE\tMATCH\tSCORE\tVERDICT\tURL")
	}
	for _, candidate := range found.Candidates {
		match, score, verdict := "-", "-", candidate.Reason
		if candidate.Verdict != nil {
			match, score = candidate.Verdict.Match, fmt.Sprint(candidate.Verdict.Quality)
			if candidate.Usable {
				verdict = "usable: " + candidate.Verdict.Notes
			} else {
				verdict = candidate.Reason + ": " + candidate.Verdict.Notes
			}
		}
		license := candidate.License
		if license == "" {
			license = "-"
		}
		link := candidate.ImageURL
		if candidate.Source == realphoto.KindCommons {
			link = candidate.PageURL
		}
		fmt.Fprintf(table, "%s\t%dx%d\t%s\t%s\t%s\t%s\t%s\n", candidate.Source, candidate.Width, candidate.Height, license, match, score, cut(verdict, 70), link)
	}
	_ = table.Flush()
	for _, note := range found.Notes {
		fmt.Fprintf(w, "note: %s\n", note)
	}
	if found.Found {
		fmt.Fprintf(w, "chosen: %s %s\ncredit: %s -> %s\nalt: %s\n", found.Chosen.Source, found.Chosen.ImageURL, found.Credit.Text, found.Credit.URL, found.Alt)
	} else {
		fmt.Fprintf(w, "no photo: %s\n", found.Reason)
	}
	fmt.Fprintf(w, "(%.0fs)\n\n", found.Seconds)
}

func cut(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit-1] + "…"
}

type latestRow struct {
	Slug   string            `json:"slug"`
	Skip   string            `json:"skip,omitempty"`
	Result *realphoto.Result `json:"result,omitempty"`
	Image  string            `json:"image,omitempty"`
}

// runInlineLatest runs the photo search over the newest published articles
// and prints one line per article. Nothing is uploaded or written to the
// database.
func runInlineLatest(ctx context.Context, photos *realphoto.Finder, count int, dbPath, outDir string, getenv func(string) string) error {
	if photos == nil {
		return errors.New("-inline-latest searches for real photos; drop -no-real-photos")
	}
	store, closeDB, err := openArticles(ctx, dbPath)
	if err != nil {
		return err
	}
	defer closeDB()
	articles, err := store.Latest(ctx, count)
	if err != nil {
		return err
	}
	prefix := inlineimage.OwnImagePrefix(getenv("S3_FEATURE_IMAGE_PUBLIC_URL"), s3Prefix(getenv))
	var rows []latestRow
	for _, article := range articles {
		row := latestRow{Slug: article.Slug}
		if _, ok := inlineimage.PhotoSlot(article.Content); !ok {
			row.Skip = fmt.Sprintf("fewer than %d paragraphs", inlineimage.MinPhotoParagraphs)
			rows = append(rows, row)
			continue
		}
		if prefix != "" && !strings.HasPrefix(article.FeaturedImage, prefix) {
			row.Skip = "featured image not ours (the worker skips it)"
			rows = append(rows, row)
			continue
		}
		found := photos.Find(ctx, inlineimage.PhotoRequest(article))
		row.Result = &found
		printPhotoResult(os.Stdout, article.Slug, found)
		if found.Found {
			if original, _, err := writePhoto(outDir, article.Slug, found.Image); err == nil {
				row.Image = original
			}
		}
		rows = append(rows, row)
		if ctx.Err() != nil {
			break
		}
	}
	table := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "SLUG\tPATH\tSUBJECT\tSCORE\tCANDIDATES\tCREDIT / REASON")
	for _, row := range rows {
		if row.Skip != "" {
			fmt.Fprintf(table, "%s\tskip\t-\t-\t-\t%s\n", cut(row.Slug, 48), row.Skip)
			continue
		}
		found := row.Result
		path, subject, score, detail := "generated", "-", "-", found.Reason
		if found.Plan != nil && found.Plan.Subject != "" {
			subject = found.Plan.Subject
		}
		if found.Found {
			path, score, detail = found.Credit.Kind, fmt.Sprint(found.Chosen.Verdict.Quality), found.Credit.Text
		}
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%d\t%s\n", cut(row.Slug, 48), path, cut(subject, 36), score, len(found.Candidates), cut(detail, 90))
	}
	_ = table.Flush()
	report, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	reportPath := filepath.Join(outDir, "report.json")
	if err := os.WriteFile(reportPath, append(report, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("\nreport: %s\n", reportPath)
	return nil
}

func s3Prefix(getenv func(string) string) string {
	if prefix := getenv("S3_FEATURE_IMAGE_PREFIX"); prefix != "" {
		return prefix
	}
	return config.DefaultS3FeatureImagePrefix
}
