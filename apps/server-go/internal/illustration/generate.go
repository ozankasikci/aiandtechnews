package illustration

import (
	"context"
	"errors"
	"log/slog"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
)

const (
	MaxGenerationAttempts = 3
	referenceMaxEdge      = 1024
	referenceQuality      = 85
	reviewMaxEdge         = 1024
	reviewQuality         = 80
)

// ErrNoCompliantImage means every attempt was rejected or failed.
var ErrNoCompliantImage = errors.New("no compliant illustration after 3 generation attempts")

type ImageModel interface {
	GenerateImage(ctx context.Context, prompt string, reference *gemini.InlineImage) ([]byte, error)
	ReviewImage(ctx context.Context, prompt string, jpeg []byte, schema map[string]any) (string, error)
}

type Generator struct {
	model  ImageModel
	logger *slog.Logger
}

func NewGenerator(model ImageModel, logger *slog.Logger) *Generator { return &Generator{model: model, logger: logger} }

// Generate ports generateIllustration: at most 3 generation calls; a failed
// referenced call retries once text-only; rejected images regenerate with a
// correction; text/logo rejections drop the reference.
func (g *Generator) Generate(ctx context.Context, article Article, rawReference []byte) ([]byte, error) {
	var reference *gemini.InlineImage
	if len(rawReference) > 0 {
		if normalized, err := imaging.FitJPEG(rawReference, referenceMaxEdge, referenceQuality); err == nil {
			reference = &gemini.InlineImage{MIMEType: "image/jpeg", Data: normalized}
		} else {
			g.logger.InfoContext(ctx, "reference image could not be decoded; generating from text", "error", err)
		}
	}
	correction := ""
	var lastErr error
	for attempt := 1; attempt <= MaxGenerationAttempts; attempt++ {
		image, err := g.model.GenerateImage(ctx, BuildPrompt(article, reference != nil, correction), reference)
		if err != nil {
			lastErr = err
			if reference != nil {
				g.logger.InfoContext(ctx, "referenced illustration failed; retrying from text", "error", err)
				reference = nil
				continue
			}
			return nil, err
		}
		verdict := g.review(ctx, article, image)
		if verdict.Compliant {
			return image, nil
		}
		g.logger.InfoContext(ctx, "illustration rejected by compliance check", "attempt", attempt,
			"text", verdict.HasText, "logo", verdict.HasLogo, "injury", verdict.HasInjury, "unverified", verdict.Unverified, "notes", verdict.Notes)
		correction = Correction(verdict)
		if reference != nil && (verdict.HasText || verdict.HasLogo) {
			reference = nil
		}
		lastErr = ErrNoCompliantImage
	}
	if lastErr == nil {
		lastErr = ErrNoCompliantImage
	}
	if !errors.Is(lastErr, ErrNoCompliantImage) {
		return nil, lastErr
	}
	return nil, ErrNoCompliantImage
}

func (g *Generator) review(ctx context.Context, article Article, image []byte) Verdict {
	jpeg, err := imaging.FitJPEG(image, reviewMaxEdge, reviewQuality)
	if err != nil {
		return unverified("compliance check could not decode the generated image")
	}
	raw, err := g.model.ReviewImage(ctx, BuildCompliancePrompt(article), jpeg, ComplianceSchema)
	if err != nil {
		return unverified("compliance check request failed: " + err.Error())
	}
	return ParseVerdict(raw)
}
