package illustration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

// ErrNoSourceImage reports that the article has no usable original image.
var ErrNoSourceImage = errors.New("no usable source image")

// SourceImageIllustrator implements publisher.Illustrator without generation:
// it copies the source article's own image (og:image, else the feed image) to
// S3 as WebP, so the site never hotlinks the publisher's server.
type SourceImageIllustrator struct {
	store  ImageStore
	http   *http.Client
	logger *slog.Logger
}

func NewSourceImageIllustrator(store ImageStore, httpClient *http.Client, logger *slog.Logger) *SourceImageIllustrator {
	return &SourceImageIllustrator{store: store, http: httpClient, logger: logger}
}

func (s *SourceImageIllustrator) Illustrate(ctx context.Context, request publisher.IllustrationRequest) (publisher.Illustration, error) {
	original, err := FetchReference(ctx, s.http, request.ReferenceImageURL)
	if err != nil || original == nil {
		reason := "the source has no og:image or feed image"
		if err != nil {
			reason = err.Error()
		}
		return publisher.Illustration{}, publisher.Permanent(fmt.Errorf("%w: %s", ErrNoSourceImage, reason))
	}
	webp, _, _, err := imaging.EncodeWebP(original, webpQuality)
	if err != nil {
		return publisher.Illustration{}, publisher.Permanent(fmt.Errorf("%w: %w", ErrNoSourceImage, err))
	}
	stored, err := s.store.StoreWebP(ctx, request.Slug, webp)
	if err != nil {
		return publisher.Illustration{}, classifyStoreError(fmt.Errorf("store source image: %w", err))
	}
	return publisher.Illustration{
		URL: stored.URL,
		Discard: func(ctx context.Context) {
			if err := s.store.Delete(ctx, stored.Key); err != nil {
				s.logger.WarnContext(ctx, "could not delete unused source image", "key", stored.Key, "error", err)
			}
		},
	}, nil
}
