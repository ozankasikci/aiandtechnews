package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
)

const storageCheckTimeout = 15 * time.Second

// bucketHeader is the S3 call the startup check needs.
type bucketHeader interface {
	HeadBucket(ctx context.Context, input *s3.HeadBucketInput, options ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
}

// openPublisherStorage loads the AWS configuration, verifies credentials and
// bucket access, and returns the S3 client for the feature image store. It is
// a variable so app tests can stay offline.
var openPublisherStorage = func(ctx context.Context, cfg config.Config) (media.ObjectAPI, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.AWSRegion))
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration: %w", err)
	}
	client := s3.NewFromConfig(awsCfg)
	if err := checkPublisherStorage(ctx, awsCfg.Credentials, client, cfg.S3Bucket); err != nil {
		return nil, err
	}
	return client, nil
}

// checkPublisherStorage fails fast on missing or rejected AWS credentials and
// on a bucket that does not exist or cannot be reached, so the server does not
// start with a publisher that can never store an image.
func checkPublisherStorage(ctx context.Context, credentials aws.CredentialsProvider, bucket bucketHeader, name string) error {
	ctx, cancel := context.WithTimeout(ctx, storageCheckTimeout)
	defer cancel()
	if credentials == nil {
		return errors.New("no AWS credentials provider configured")
	}
	if _, err := credentials.Retrieve(ctx); err != nil {
		return fmt.Errorf("retrieve AWS credentials: %w", err)
	}
	if _, err := bucket.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(name)}); err != nil {
		return fmt.Errorf("head bucket %q: %w", name, err)
	}
	return nil
}
