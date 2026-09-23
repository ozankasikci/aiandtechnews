package illustration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/smithy-go"

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
		return publisher.Illustration{}, classifyStoreError(fmt.Errorf("store illustration: %w", err))
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

// systemFaultS3Codes are S3 error codes that mean the bucket or credentials
// are misconfigured; no candidate is to blame and retrying soon will not help.
var systemFaultS3Codes = map[string]bool{
	"AccessDenied":          true,
	"NoSuchBucket":          true,
	"InvalidAccessKeyId":    true,
	"SignatureDoesNotMatch": true,
	"ExpiredToken":          true,
}

// classifyStoreError marks storage misconfiguration (rejected or missing
// credentials, missing bucket, a public URL answering 403/404) as a system
// fault so the candidate keeps its attempts; everything else stays transient.
func classifyStoreError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && systemFaultS3Codes[apiErr.ErrorCode()] {
		return publisher.SystemFault(err)
	}
	var signingErr *v4.SigningError
	if errors.As(err, &signingErr) || isCredentialRetrievalError(err) {
		return publisher.SystemFault(err)
	}
	var statusErr *media.PublicStatusError
	if errors.As(err, &statusErr) && (statusErr.Status == http.StatusForbidden || statusErr.Status == http.StatusNotFound) {
		return publisher.SystemFault(err)
	}
	return err
}

// isCredentialRetrievalError matches the AWS SDK's untyped credential
// resolution failures ("get identity: get credentials: ...", "failed to
// refresh cached credentials, ...").
func isCredentialRetrievalError(err error) bool {
	message := err.Error()
	for _, marker := range []string{"get identity:", "failed to retrieve credentials", "failed to refresh cached credentials"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
