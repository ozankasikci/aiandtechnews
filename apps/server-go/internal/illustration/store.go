package illustration

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/smithy-go"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

const webpQuality = 82

type ImageStore interface {
	StoreWebP(ctx context.Context, slug string, data []byte) (media.Stored, error)
	Delete(ctx context.Context, key string) error
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

// storeWebP uploads a WebP image, returning an
// Illustration whose Discard deletes the object.
func storeWebP(ctx context.Context, store ImageStore, slug string, webp []byte, logger *slog.Logger) (publisher.Illustration, error) {
	stored, err := store.StoreWebP(ctx, slug, webp)
	if err != nil {
		return publisher.Illustration{}, classifyStoreError(err)
	}
	return publisher.Illustration{
		URL: stored.URL,
		Discard: func(ctx context.Context) {
			if err := store.Delete(ctx, stored.Key); err != nil {
				logger.WarnContext(ctx, "could not delete unused featured image", "key", stored.Key, "error", err)
			}
		},
	}, nil
}
