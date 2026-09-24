package illustration

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration/styles"
)

// GeminiClient is everything the pipeline uses from *gemini.Client.
type GeminiClient interface {
	GenerateImage(ctx context.Context, prompt string, reference *gemini.InlineImage) ([]byte, error)
	GeminiBriefModel
}

// PipelineConfig is the configuration-level description of a pipeline.
type PipelineConfig struct {
	Chain        []string // codex, gemini, source
	Analyzers    []string // codex, gemini
	CodexBin     string
	CodexNodeDir string
	CodexTimeout time.Duration
	CutoutBin    string
	Gemini       GeminiClient
	Store        ImageStore
	HTTP         *http.Client
	Logger       *slog.Logger
}

// BuildPipelineDeps turns configuration into pipeline dependencies. Names
// were validated by internal/config; unknown ones are skipped.
func BuildPipelineDeps(cfg PipelineConfig) PipelineDeps {
	catalog := styles.MustLoad()
	runner := &CodexRunner{Bin: cfg.CodexBin, NodeDir: cfg.CodexNodeDir, Timeout: cfg.CodexTimeout}
	deps := PipelineDeps{
		Reviewer: NewGeminiReviewer(cfg.Gemini),
		Styles:   catalog,
		Store:    cfg.Store,
		HTTP:     cfg.HTTP,
		Logger:   cfg.Logger,
	}
	for _, name := range cfg.Chain {
		switch name {
		case ProviderCodex:
			deps.Chain = append(deps.Chain, ProviderStep(NewCodexProvider(runner)))
		case ProviderGemini:
			deps.Chain = append(deps.Chain, ProviderStep(NewGeminiProvider(cfg.Gemini)))
		case ProviderSource:
			deps.Chain = append(deps.Chain, SourceStep())
		}
	}
	for _, name := range cfg.Analyzers {
		switch name {
		case ProviderCodex:
			deps.Analyzers = append(deps.Analyzers, NewCodexAnalyzer(runner, catalog))
		case ProviderGemini:
			deps.Analyzers = append(deps.Analyzers, NewGeminiAnalyzer(cfg.Gemini, catalog))
		}
	}
	if cfg.CutoutBin != "" {
		deps.Cutter = &CutoutTool{Bin: cfg.CutoutBin}
	}
	return deps
}
