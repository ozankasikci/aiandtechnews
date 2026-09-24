package illustration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration/styles"
)

// Provider names, as written in FEATURED_IMAGE_CHAIN.
const (
	ProviderCodex  = "codex"
	ProviderGemini = "gemini"
	ProviderSource = "source"
)

// ImageRules are appended to every generation prompt.
const ImageRules = "16:9 landscape. Absolutely no text, letters, numbers, logos, flags, national emblems, coats of arms, watermarks, UI or screens, and no real people or recognizable faces."

// CollageRules turn the brief into a background for the public-figure
// collage: the person's photo is pasted over the left side afterwards.
const CollageRules = "This is the background for a photo collage: a photo of a person will be pasted onto the LEFT 45% of the frame later, so keep the left 45% mostly empty except one large, simple backdrop shape (a big circle or arch) behind where a head and shoulders will go. Put the story's scene on the right side. No people, no faces, no hands."

const anchorRules = "The attached images are style references only: match their painting technique, brushwork, palette and finish, but do not copy their subjects or composition."

// GenerateRequest is one generation attempt.
type GenerateRequest struct {
	Brief      Brief
	Style      styles.Style
	Collage    bool
	Correction string
}

// Provider generates one candidate image per call. The pipeline reviews it.
type Provider interface {
	Name() string
	// Attempts is how many generations the pipeline may spend on it.
	Attempts() int
	Generate(ctx context.Context, request GenerateRequest) ([]byte, error)
}

// BuildImagePrompt is the provider-neutral prompt: brief, style and rules.
func BuildImagePrompt(request GenerateRequest) string {
	var prompt strings.Builder
	if request.Collage {
		prompt.WriteString(CollageRules + " ")
	}
	prompt.WriteString(request.Brief.Text())
	prompt.WriteString(" Style: " + request.Style.Prompt + " " + ImageRules)
	if correction := strings.TrimSpace(request.Correction); correction != "" {
		prompt.WriteString(" Correction: " + correction)
	}
	return prompt.String()
}

// BuildCodexPrompt wraps the image prompt for Codex's $imagegen skill.
func BuildCodexPrompt(request GenerateRequest, withAnchors bool, outputPath string) string {
	anchors := ""
	if withAnchors {
		anchors = " " + anchorRules
	}
	return fmt.Sprintf("$imagegen Create one image. %s%s Save the final image as %s and do nothing else.", BuildImagePrompt(request), anchors, outputPath)
}

// CodexProvider generates with the Codex CLI's built-in image tool on the
// ChatGPT plan, attaching the style's anchor images.
type CodexProvider struct {
	runner   *CodexRunner
	attempts int
	anchors  bool
}

// NewCodexProvider returns a provider that spends up to 2 generations.
func NewCodexProvider(runner *CodexRunner) *CodexProvider {
	return &CodexProvider{runner: runner, attempts: 2, anchors: true}
}

func (p *CodexProvider) Name() string  { return ProviderCodex }
func (p *CodexProvider) Attempts() int { return p.attempts }

// ErrCodexNoImage means codex finished without leaving an image.
var ErrCodexNoImage = errors.New("codex produced no image")

func (p *CodexProvider) Generate(ctx context.Context, request GenerateRequest) ([]byte, error) {
	dir, err := os.MkdirTemp("", "featured-codex-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	var images []string
	if p.anchors {
		for i, anchor := range request.Style.Anchors {
			path := filepath.Join(dir, fmt.Sprintf("style-reference-%d.jpg", i+1))
			if err := os.WriteFile(path, anchor.Data, 0o600); err != nil {
				return nil, err
			}
			images = append(images, path)
		}
	}
	outputPath := filepath.Join(dir, "featured.png")
	output, err := p.runner.Run(ctx, CodexRun{
		Dir:     dir,
		Sandbox: "workspace-write",
		Images:  images,
		Prompt:  BuildCodexPrompt(request, len(images) > 0, outputPath),
	})
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(outputPath)
	if err != nil || len(data) == 0 {
		home := p.runner.Home
		if home == "" {
			home = os.Getenv("HOME")
		}
		fallback := findGeneratedImage(output, home)
		if fallback == "" {
			return nil, fmt.Errorf("%w: %s", ErrCodexNoImage, tail(output, 300))
		}
		if data, err = os.ReadFile(fallback); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrCodexNoImage, err)
		}
	}
	return data, nil
}

// ImageModel is the Gemini image generation call.
type ImageModel interface {
	GenerateImage(ctx context.Context, prompt string, reference *gemini.InlineImage) ([]byte, error)
}

// GeminiProvider generates with GEMINI_IMAGE_MODEL at GEMINI_IMAGE_SIZE.
type GeminiProvider struct{ model ImageModel }

// MaxGeminiAttempts keeps the old generator's budget of three paid generations.
const MaxGeminiAttempts = 3

func NewGeminiProvider(model ImageModel) *GeminiProvider { return &GeminiProvider{model: model} }

func (p *GeminiProvider) Name() string  { return ProviderGemini }
func (p *GeminiProvider) Attempts() int { return MaxGeminiAttempts }

func (p *GeminiProvider) Generate(ctx context.Context, request GenerateRequest) ([]byte, error) {
	return p.model.GenerateImage(ctx, "Create one original editorial illustration for an AI and technology news story. "+BuildImagePrompt(request), nil)
}
