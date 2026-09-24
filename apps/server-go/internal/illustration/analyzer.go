package illustration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration/styles"
)

const analyzeTimeout = 2 * time.Minute

// AnalyzeInput is what an analyzer sees: the headline, excerpt and the
// source image as a normalized JPEG (nil when there is none).
type AnalyzeInput struct {
	Title    string
	Excerpt  string
	ImageURL string
	Source   []byte
}

// Analyzer turns a story and its source image into a Brief.
type Analyzer interface {
	Name() string
	Analyze(ctx context.Context, input AnalyzeInput) (Brief, error)
}

// GeminiBriefModel is the Gemini client surface the analyzer uses: the vision
// model with an image, the text model without one.
type GeminiBriefModel interface {
	VisionModel
	GenerateJSON(ctx context.Context, prompt string) (string, error)
}

// GeminiAnalyzer writes the brief with the Gemini vision model.
type GeminiAnalyzer struct {
	model   GeminiBriefModel
	catalog styles.Catalog
}

func NewGeminiAnalyzer(model GeminiBriefModel, catalog styles.Catalog) *GeminiAnalyzer {
	return &GeminiAnalyzer{model: model, catalog: catalog}
}

func (a *GeminiAnalyzer) Name() string { return ProviderGemini }

func (a *GeminiAnalyzer) Analyze(ctx context.Context, input AnalyzeInput) (Brief, error) {
	ctx, cancel := context.WithTimeout(ctx, analyzeTimeout)
	defer cancel()
	prompt := BuildAnalyzePrompt(input.Title, input.Excerpt, input.ImageURL, a.catalog, input.Source != nil)
	var raw string
	var err error
	if input.Source != nil {
		raw, err = a.model.ReviewImage(ctx, prompt, input.Source, briefGeminiSchema(a.catalog.Names()))
	} else {
		raw, err = a.model.GenerateJSON(ctx, prompt)
	}
	if err != nil {
		return Brief{}, fmt.Errorf("gemini analyze: %w", err)
	}
	return ParseBrief(raw, a.catalog.Names())
}

// CodexAnalyzer writes the brief with `codex exec` in a read-only sandbox.
type CodexAnalyzer struct {
	runner  *CodexRunner
	catalog styles.Catalog
}

func NewCodexAnalyzer(runner *CodexRunner, catalog styles.Catalog) *CodexAnalyzer {
	return &CodexAnalyzer{runner: runner, catalog: catalog}
}

func (a *CodexAnalyzer) Name() string { return ProviderCodex }

func (a *CodexAnalyzer) Analyze(ctx context.Context, input AnalyzeInput) (Brief, error) {
	ctx, cancel := context.WithTimeout(ctx, analyzeTimeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "featured-analyze-*")
	if err != nil {
		return Brief{}, err
	}
	defer os.RemoveAll(dir)
	schema, err := briefJSONSchema(a.catalog.Names())
	if err != nil {
		return Brief{}, err
	}
	schemaPath := filepath.Join(dir, "brief.schema.json")
	answerPath := filepath.Join(dir, "brief.json")
	if err := os.WriteFile(schemaPath, schema, 0o600); err != nil {
		return Brief{}, err
	}
	run := CodexRun{
		Dir:     dir,
		Sandbox: "read-only",
		Extra:   []string{"--output-schema", schemaPath, "-o", answerPath},
		Prompt:  BuildAnalyzePrompt(input.Title, input.Excerpt, input.ImageURL, a.catalog, input.Source != nil) + "\n\nDo not run any commands. Answer with the JSON object only.",
	}
	if input.Source != nil {
		sourcePath := filepath.Join(dir, "source.jpg")
		if err := os.WriteFile(sourcePath, input.Source, 0o600); err != nil {
			return Brief{}, err
		}
		run.Images = []string{sourcePath}
	}
	output, err := a.runner.Run(ctx, run)
	if err != nil {
		return Brief{}, fmt.Errorf("codex analyze: %w", err)
	}
	answer, readErr := os.ReadFile(answerPath)
	if readErr != nil || strings.TrimSpace(string(answer)) == "" {
		answer = []byte(output)
	}
	return ParseBrief(string(answer), a.catalog.Names())
}
