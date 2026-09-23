package illustration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

const (
	MaxGenerationAttempts = 3
	referenceMaxEdge      = 1024
	referenceQuality      = 85
	reviewMaxEdge         = 1024
	reviewQuality         = 80
)

// ErrNoCompliantImage means every attempt was rejected or failed.
var ErrNoCompliantImage = fmt.Errorf("no compliant illustration after %d generation attempts", MaxGenerationAttempts)

type ImageModel interface {
	GenerateImage(ctx context.Context, prompt string, reference *gemini.InlineImage) ([]byte, error)
	ReviewImage(ctx context.Context, prompt string, jpeg []byte, schema map[string]any) (string, error)
}

type Generator struct {
	model  ImageModel
	logger *slog.Logger
}

func NewGenerator(model ImageModel, logger *slog.Logger) *Generator {
	return &Generator{model: model, logger: logger}
}

// Generate ports generateIllustration: at most 3 generation calls; a failed
// referenced call retries once text-only; rejected images regenerate with a
// correction; text/logo rejections drop the reference. A compliance review
// that fails for a system fault or a transient reason stops immediately
// instead of spending another generation.
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
	var generateErr error
	for attempt := 1; attempt <= MaxGenerationAttempts; attempt++ {
		image, err := g.model.GenerateImage(ctx, BuildPrompt(article, reference != nil, correction), reference)
		generateErr = err
		if err != nil {
			if reference != nil {
				g.logger.InfoContext(ctx, "referenced illustration failed; retrying from text", "error", err)
				reference = nil
				continue
			}
			return nil, err
		}
		verdict, err := g.review(ctx, article, image)
		if err != nil {
			return nil, err
		}
		if verdict.Compliant {
			return image, nil
		}
		g.logger.InfoContext(ctx, "illustration rejected by compliance check", "attempt", attempt,
			"text", verdict.HasText, "logo", verdict.HasLogo, "injury", verdict.HasInjury, "unverified", verdict.Unverified, "notes", verdict.Notes)
		correction = Correction(verdict)
		if reference != nil && (verdict.HasText || verdict.HasLogo) {
			reference = nil
		}
	}
	if generateErr != nil {
		return nil, generateErr
	}
	return nil, ErrNoCompliantImage
}

// review returns an unverified verdict (so the caller regenerates) when the
// image cannot be decoded, the reviewer rejects the request permanently, or
// its answer is empty or unusable. System faults (bad key, unknown model) and
// transient failures (rate limits, 5xx, network, context) are returned as
// errors: regenerating would only burn paid generations.
func (g *Generator) review(ctx context.Context, article Article, image []byte) (Verdict, error) {
	jpeg, err := imaging.FitJPEG(image, reviewMaxEdge, reviewQuality)
	if err != nil {
		return unverified("compliance check could not decode the generated image"), nil
	}
	raw, err := g.model.ReviewImage(ctx, BuildCompliancePrompt(article), jpeg, ComplianceSchema)
	if err != nil {
		if errors.Is(err, gemini.ErrEmptyResponse) {
			return unverified("compliance check returned no content"), nil
		}
		classified := publisher.ClassifyGeminiError(err)
		if publisher.IsPermanent(classified) {
			return unverified("compliance check request failed: " + err.Error()), nil
		}
		return Verdict{}, fmt.Errorf("compliance review: %w", classified)
	}
	return ParseVerdict(raw), nil
}
