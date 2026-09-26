// Command upload-featured-image encodes an image as WebP and uploads it to
// the featured-image bucket the publisher uses, then prints its public URL.
// It never touches the database; set the URL on an article separately.
//
//	upload-featured-image -slug tesla-workers-push-back -in final.png
//
// It reads AWS_REGION (and the usual AWS credential variables, such as
// AWS_PROFILE), S3_FEATURE_IMAGE_BUCKET, S3_FEATURE_IMAGE_PREFIX and
// S3_FEATURE_IMAGE_PUBLIC_URL from the environment.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
)

const webpQuality = 82 // matches the illustration pipeline

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "upload-featured-image: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string) error {
	flags := flag.NewFlagSet("upload-featured-image", flag.ContinueOnError)
	slug := flags.String("slug", "", "article slug, used in the object key (required)")
	in := flags.String("in", "", "PNG, JPEG or WebP image to upload (required)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *slug == "" || *in == "" {
		return errors.New("-slug and -in are required")
	}
	bucket, publicURL := getenv("S3_FEATURE_IMAGE_BUCKET"), getenv("S3_FEATURE_IMAGE_PUBLIC_URL")
	if bucket == "" || publicURL == "" {
		return errors.New("S3_FEATURE_IMAGE_BUCKET and S3_FEATURE_IMAGE_PUBLIC_URL are required")
	}
	source, err := os.ReadFile(*in)
	if err != nil {
		return err
	}
	webp := source
	if imaging.SniffMIME(source) != "image/webp" {
		if webp, _, _, err = imaging.EncodeWebP(source, webpQuality); err != nil {
			return fmt.Errorf("encode WebP: %w", err)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(getenv("AWS_REGION")))
	if err != nil {
		return fmt.Errorf("load AWS configuration: %w", err)
	}
	store := media.NewStore(media.Config{
		Region: getenv("AWS_REGION"), Bucket: bucket, Prefix: getenv("S3_FEATURE_IMAGE_PREFIX"), PublicBaseURL: publicURL,
	}, s3.NewFromConfig(awsCfg), &http.Client{Timeout: 30 * time.Second}, time.Now)
	stored, err := store.StoreWebP(ctx, *slug, webp)
	if err != nil {
		return err
	}
	fmt.Println(stored.URL)
	return nil
}
