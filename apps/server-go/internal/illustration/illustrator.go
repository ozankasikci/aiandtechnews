package illustration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

const webpQuality = 82

type ImageStore interface {
	StoreWebP(ctx context.Context, slug string, data []byte) (media.Stored, error)
	Delete(ctx context.Context, key string) error
}

// S3Illustrator implements publisher.Illustrator with a generated, verified,
// S3-hosted WebP. The source image is only an in-memory reference.
type S3Illustrator struct {
	generator *Generator
	store     ImageStore
	http      *http.Client
	logger    *slog.Logger
}

func NewS3Illustrator(generator *Generator, store ImageStore, httpClient *http.Client, logger *slog.Logger) *S3Illustrator {
	return &S3Illustrator{generator: generator, store: store, http: httpClient, logger: logger}
}

func (s *S3Illustrator) Illustrate(ctx context.Context, request publisher.IllustrationRequest) (publisher.Illustration, error) {
	reference, err := FetchReference(ctx, s.http, request.ReferenceImageURL)
	if err != nil {
		s.logger.InfoContext(ctx, "reference image unusable; generating from text", "url", request.ReferenceImageURL, "reason", err)
	}
	image, err := s.generator.Generate(ctx, Article{Title: request.Title, Excerpt: request.Excerpt}, reference)
	if err != nil {
		if errors.Is(err, ErrNoCompliantImage) {
			// Retrying later repeats three paid generations with the same inputs;
			// leave it to an editor's Retry in the app.
			return publisher.Illustration{}, publisher.Permanent(err)
		}
		return publisher.Illustration{}, publisher.ClassifyGeminiError(fmt.Errorf("generate illustration: %w", err))
	}
	webp, _, _, err := imaging.EncodeWebP(image, webpQuality)
	if err != nil {
		return publisher.Illustration{}, publisher.Permanent(fmt.Errorf("encode illustration: %w", err))
	}
	stored, err := s.store.StoreWebP(ctx, request.Slug, webp)
	if err != nil {
		return publisher.Illustration{}, fmt.Errorf("store illustration: %w", err)
	}
	return publisher.Illustration{
		URL: stored.URL,
		Discard: func(ctx context.Context) {
			if err := s.store.Delete(ctx, stored.Key); err != nil {
				s.logger.WarnContext(ctx, "could not delete unused illustration", "key", stored.Key, "error", err)
			}
		},
	}, nil
}
