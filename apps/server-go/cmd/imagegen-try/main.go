// Command imagegen-try runs the featured-image pipeline once, as a dry run,
// and writes the final image and a JSON report to a local folder. It never
// touches a database and never uploads anything.
//
//	go run ./cmd/imagegen-try -candidate 136 -title "..." -excerpt "..." \
//	    -image-url https://... -chain codex,gemini,source -out ./tmp/try-136
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
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
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
	analyzeOnly := flags.Bool("analyze-only", false, "only run the analyzers and print the brief")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *title == "" {
		return errors.New("-title is required")
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

	request := publisher.IllustrationRequest{Slug: "imagegen-try-" + label, Title: *title, Excerpt: *excerpt, ReferenceImageURL: *imageURL}
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
