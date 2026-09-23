package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/indexnow"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

type fakeBucketHeader struct {
	err         error
	bucket      string
	hadDeadline bool
}

func (f *fakeBucketHeader) HeadBucket(ctx context.Context, input *s3.HeadBucketInput, _ ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	f.bucket = aws.ToString(input.Bucket)
	_, f.hadDeadline = ctx.Deadline()
	return &s3.HeadBucketOutput{}, f.err
}

func credentialsReturning(err error) aws.CredentialsProvider {
	return aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		if err != nil {
			return aws.Credentials{}, err
		}
		return aws.Credentials{AccessKeyID: "AKIDSYNTHETIC", SecretAccessKey: "synthetic"}, nil
	})
}

func TestCheckPublisherStorage(t *testing.T) {
	ctx := context.Background()

	header := &fakeBucketHeader{}
	if err := checkPublisherStorage(ctx, credentialsReturning(nil), header, "bucket"); err != nil {
		t.Fatalf("healthy storage: %v", err)
	}
	if header.bucket != "bucket" || !header.hadDeadline {
		t.Fatalf("HeadBucket bucket=%q deadline=%v, want bucket with a timeout", header.bucket, header.hadDeadline)
	}

	credsErr := errors.New("no credentials")
	header = &fakeBucketHeader{}
	if err := checkPublisherStorage(ctx, credentialsReturning(credsErr), header, "bucket"); !errors.Is(err, credsErr) {
		t.Fatalf("credential failure: err = %v", err)
	}
	if header.bucket != "" {
		t.Fatal("HeadBucket ran after credential retrieval failed")
	}

	headErr := errors.New("NotFound")
	if err := checkPublisherStorage(ctx, credentialsReturning(nil), &fakeBucketHeader{err: headErr}, "bucket"); !errors.Is(err, headErr) {
		t.Fatalf("head bucket failure: err = %v", err)
	}

	if err := checkPublisherStorage(ctx, nil, &fakeBucketHeader{}, "bucket"); err == nil {
		t.Fatal("nil credentials provider accepted")
	}
}

func stubPublisherStorage(t *testing.T, open func(context.Context, config.Config) (media.ObjectAPI, error)) {
	t.Helper()
	original := openPublisherStorage
	openPublisherStorage = open
	t.Cleanup(func() { openPublisherStorage = original })
}

func publisherConfig(t *testing.T) config.Config {
	return config.Config{
		Mode: config.ModeDevelopment, Address: "127.0.0.1:4401", DatabasePath: filepath.Join(t.TempDir(), "dev.db"), JWTSecret: "synthetic-test-secret",
		PublisherEnabled: true, PublisherInterval: time.Minute, GeminiAPIKey: "synthetic-gemini-key",
		AWSRegion: "us-east-1", S3Bucket: "bucket", S3Prefix: "features", S3PublicURL: "https://images.example.invalid",
	}
}

func TestNewWithDatabaseFailsWhenPublisherStorageCheckFails(t *testing.T) {
	checkErr := errors.New("head bucket: AccessDenied")
	stubPublisherStorage(t, func(context.Context, config.Config) (media.ObjectAPI, error) { return nil, checkErr })
	db, _ := testutil.OpenDatabase(t)
	_, err := NewWithDatabase(publisherConfig(t), slog.New(slog.NewTextHandler(io.Discard, nil)), db)
	if !errors.Is(err, checkErr) || !strings.Contains(err.Error(), "publisher storage check failed") {
		t.Fatalf("err = %v, want publisher storage check failure", err)
	}
}

func TestNewWithDatabaseSkipsStorageWhenPublisherDisabled(t *testing.T) {
	stubPublisherStorage(t, func(context.Context, config.Config) (media.ObjectAPI, error) {
		t.Fatal("publisher storage opened with the publisher disabled")
		return nil, nil
	})
	cfg := publisherConfig(t)
	cfg.PublisherEnabled = false
	db, _ := testutil.OpenDatabase(t)
	if _, err := NewWithDatabase(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db); err != nil {
		t.Fatal(err)
	}
}

func TestNewPublisherNotifierChoosesIndexNowOnlyWhenEnabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	cfg := publisherConfig(t)
	cfg.IndexNowEnabled = false
	if notifier := newPublisherNotifier(cfg, logger); notifier == nil {
		t.Fatal("IndexNowEnabled=false: notifier = nil")
	} else if _, ok := notifier.(publisher.NoopNotifier); !ok {
		t.Fatalf("IndexNowEnabled=false: notifier = %T, want publisher.NoopNotifier", notifier)
	}

	cfg.IndexNowEnabled = true
	if notifier := newPublisherNotifier(cfg, logger); notifier == nil {
		t.Fatal("IndexNowEnabled=true: notifier = nil")
	} else if _, ok := notifier.(*indexnow.Client); !ok {
		t.Fatalf("IndexNowEnabled=true: notifier = %T, want *indexnow.Client", notifier)
	}
}

type unusedObjectAPI struct{ media.ObjectAPI }

func TestNewWithDatabaseWiresPublisherWhenStorageCheckPasses(t *testing.T) {
	stubPublisherStorage(t, func(context.Context, config.Config) (media.ObjectAPI, error) { return unusedObjectAPI{}, nil })
	db, _ := testutil.OpenDatabase(t)
	application, err := NewWithDatabase(publisherConfig(t), slog.New(slog.NewTextHandler(io.Discard, nil)), db)
	if err != nil {
		t.Fatal(err)
	}
	if len(application.background) != 1 {
		t.Fatalf("background tasks = %d, want the publisher loop", len(application.background))
	}
}

// Least-privilege importer users may write objects without s3:ListBucket, which
// HeadBucket needs; a 403 must not stop the server. A missing bucket still does.
func TestCheckPublisherStorageToleratesForbiddenHeadBucket(t *testing.T) {
	ctx := context.Background()
	forbidden := &awshttp.ResponseError{ResponseError: &smithyhttp.ResponseError{Response: &smithyhttp.Response{Response: &http.Response{StatusCode: http.StatusForbidden}}, Err: errors.New("Forbidden")}}
	if err := checkPublisherStorage(ctx, credentialsReturning(nil), &fakeBucketHeader{err: forbidden}, "bucket"); err != nil {
		t.Fatalf("403 should be tolerated: %v", err)
	}
	missing := &awshttp.ResponseError{ResponseError: &smithyhttp.ResponseError{Response: &smithyhttp.Response{Response: &http.Response{StatusCode: http.StatusNotFound}}, Err: errors.New("NotFound")}}
	if err := checkPublisherStorage(ctx, credentialsReturning(nil), &fakeBucketHeader{err: missing}, "bucket"); err == nil {
		t.Fatal("404 must fail the check")
	}
}
