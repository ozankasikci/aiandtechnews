package illustration_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/smithy-go"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

type fakeImageStore struct {
	storeErr     error
	deletedKeys  []string
	storedSlug   string
	storedResult media.Stored
}

func (f *fakeImageStore) StoreWebP(_ context.Context, slug string, _ []byte) (media.Stored, error) {
	if f.storeErr != nil {
		return media.Stored{}, f.storeErr
	}
	f.storedSlug = slug
	if f.storedResult.Key == "" {
		f.storedResult = media.Stored{Key: "dev/2026/09/" + slug + "-abc.webp", URL: "https://example.test/dev/2026/09/" + slug + "-abc.webp"}
	}
	return f.storedResult, nil
}

func (f *fakeImageStore) Delete(_ context.Context, key string) error {
	f.deletedKeys = append(f.deletedKeys, key)
	return nil
}

func TestS3IllustratorSuccessStoresAndDiscardDeletes(t *testing.T) {
	img := tinyPNG(t)
	model := &fakeModel{t: t, generatedImage: img, reviews: []scriptedReview{{json: compliantJSON}}}
	store := &fakeImageStore{}
	illustrator := illustration.NewS3Illustrator(illustration.NewGenerator(model, discardLogger()), store, nil, discardLogger())

	result, err := illustrator.Illustrate(context.Background(), publisher.IllustrationRequest{Slug: "my-slug", Title: "T", Excerpt: "E"})
	if err != nil {
		t.Fatal(err)
	}
	if result.URL != store.storedResult.URL {
		t.Fatalf("url = %q", result.URL)
	}
	if store.storedSlug != "my-slug" {
		t.Fatalf("storedSlug = %q", store.storedSlug)
	}
	if result.Discard == nil {
		t.Fatal("expected Discard")
	}
	result.Discard(context.Background())
	if len(store.deletedKeys) != 1 || store.deletedKeys[0] != store.storedResult.Key {
		t.Fatalf("deletedKeys = %v", store.deletedKeys)
	}
}

func TestS3IllustratorNoCompliantImageIsPermanent(t *testing.T) {
	img := tinyPNG(t)
	textViolation := `{"has_text":true,"has_logo_or_watermark":false,"depicts_unsupported_injury_or_violence":false,"notes":"text"}`
	model := &fakeModel{t: t, generatedImage: img, reviews: []scriptedReview{{json: textViolation}, {json: textViolation}, {json: textViolation}}}
	store := &fakeImageStore{}
	illustrator := illustration.NewS3Illustrator(illustration.NewGenerator(model, discardLogger()), store, nil, discardLogger())

	_, err := illustrator.Illustrate(context.Background(), publisher.IllustrationRequest{Slug: "slug", Title: "T", Excerpt: "E"})
	if !errors.Is(err, illustration.ErrNoCompliantImage) {
		t.Fatalf("err = %v", err)
	}
	if !publisher.IsPermanent(err) {
		t.Fatalf("expected permanent, got %v", err)
	}
}

func TestS3IllustratorStoreFailureIsTransient(t *testing.T) {
	img := tinyPNG(t)
	model := &fakeModel{t: t, generatedImage: img, reviews: []scriptedReview{{json: compliantJSON}}}
	store := &fakeImageStore{storeErr: errors.New("s3 unavailable")}
	illustrator := illustration.NewS3Illustrator(illustration.NewGenerator(model, discardLogger()), store, nil, discardLogger())

	_, err := illustrator.Illustrate(context.Background(), publisher.IllustrationRequest{Slug: "slug", Title: "T", Excerpt: "E"})
	if err == nil {
		t.Fatal("expected error")
	}
	if publisher.IsPermanent(err) || publisher.IsSystemFault(err) {
		t.Fatalf("expected transient error, got %v", err)
	}
}

func TestS3IllustratorClassifiesStoreErrors(t *testing.T) {
	operation := func(err error) error {
		return fmt.Errorf("upload feature image: %w", &smithy.OperationError{ServiceID: "S3", OperationName: "PutObject", Err: err})
	}
	cases := []struct {
		name   string
		err    error
		system bool
	}{
		{"access denied", operation(&smithy.GenericAPIError{Code: "AccessDenied"}), true},
		{"no such bucket", operation(&smithy.GenericAPIError{Code: "NoSuchBucket"}), true},
		{"invalid access key", operation(&smithy.GenericAPIError{Code: "InvalidAccessKeyId"}), true},
		{"bad signature", operation(&smithy.GenericAPIError{Code: "SignatureDoesNotMatch"}), true},
		{"expired token", operation(&smithy.GenericAPIError{Code: "ExpiredToken"}), true},
		{"credential retrieval", operation(fmt.Errorf("get identity: get credentials: %w", errors.New("failed to refresh cached credentials, no EC2 IMDS role found"))), true},
		{"signing credentials", operation(&v4.SigningError{Err: errors.New("failed to retrieve credentials: boom")}), true},
		{"public 403", fmt.Errorf("uploaded feature image failed public verification: %w", &media.PublicStatusError{Status: 403}), true},
		{"public 404", fmt.Errorf("uploaded feature image failed public verification: %w", &media.PublicStatusError{Status: 404}), true},
		{"public 503", fmt.Errorf("uploaded feature image failed public verification: %w", &media.PublicStatusError{Status: 503}), false},
		{"slow down", operation(&smithy.GenericAPIError{Code: "SlowDown"}), false},
		{"internal error", operation(&smithy.GenericAPIError{Code: "InternalError"}), false},
		{"network", errors.New("dial tcp: connection reset"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := &fakeModel{t: t, generatedImage: tinyPNG(t), reviews: []scriptedReview{{json: compliantJSON}}}
			store := &fakeImageStore{storeErr: tc.err}
			illustrator := illustration.NewS3Illustrator(illustration.NewGenerator(model, discardLogger()), store, nil, discardLogger())
			_, err := illustrator.Illustrate(context.Background(), publisher.IllustrationRequest{Slug: "slug", Title: "T", Excerpt: "E"})
			if !errors.Is(err, tc.err) || publisher.IsSystemFault(err) != tc.system || publisher.IsPermanent(err) {
				t.Fatalf("err = %v system=%v, want system=%v", err, publisher.IsSystemFault(err), tc.system)
			}
		})
	}
}
