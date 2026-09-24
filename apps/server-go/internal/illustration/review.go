package illustration

import (
	"context"
	"errors"
	"fmt"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

const (
	reviewMaxEdge = 1024
	reviewQuality = 80
)

// VisionModel is the Gemini vision call the reviewer and analyzer use.
type VisionModel interface {
	ReviewImage(ctx context.Context, prompt string, jpeg []byte, schema map[string]any) (string, error)
}

// Reviewer is the compliance check every generated image passes before use.
type Reviewer interface {
	Review(ctx context.Context, article Article, image []byte) (Verdict, error)
}

// GeminiReviewer checks images with the Gemini vision model.
type GeminiReviewer struct{ model VisionModel }

func NewGeminiReviewer(model VisionModel) *GeminiReviewer { return &GeminiReviewer{model: model} }

// Review returns an unverified verdict (so the caller regenerates) when the
// image cannot be decoded, the reviewer rejects the request permanently, or
// its answer is empty or unusable. System faults (bad key, unknown model) and
// transient failures (rate limits, 5xx, network, context) are returned as
// errors: regenerating would only burn generations that cannot be checked.
func (r *GeminiReviewer) Review(ctx context.Context, article Article, image []byte) (Verdict, error) {
	jpeg, err := imaging.FitJPEG(image, reviewMaxEdge, reviewQuality)
	if err != nil {
		return unverified("compliance check could not decode the generated image"), nil
	}
	raw, err := r.model.ReviewImage(ctx, BuildCompliancePrompt(article), jpeg, ComplianceSchema)
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
