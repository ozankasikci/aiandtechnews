// Package media stores feature images in S3 (port of feature-image-storage.ts).
package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
)

const (
	MaxImageBytes = 10 << 20
	cacheControl  = "public, max-age=31536000, immutable"
	verifyTimeout = 20 * time.Second
)

type Config struct {
	Region        string
	Bucket        string
	Prefix        string
	PublicBaseURL string
}

// Validate ports getFeatureImageStorageConfig's checks.
func (c Config) Validate() error {
	var missing []string
	if c.Region == "" {
		missing = append(missing, "AWS_REGION")
	}
	if c.Bucket == "" {
		missing = append(missing, "S3_FEATURE_IMAGE_BUCKET")
	}
	if c.PublicBaseURL == "" {
		missing = append(missing, "S3_FEATURE_IMAGE_PUBLIC_URL")
	}
	if len(missing) > 0 {
		return fmt.Errorf("feature image storage is not configured; missing %s", strings.Join(missing, ", "))
	}
	if c.Prefix == "" {
		return errors.New("S3_FEATURE_IMAGE_PREFIX must not be empty")
	}
	if !strings.HasPrefix(c.PublicBaseURL, "https://") {
		return errors.New("S3_FEATURE_IMAGE_PUBLIC_URL must be an https URL")
	}
	return nil
}

// ObjectAPI is the subset of the S3 client the store uses.
type ObjectAPI interface {
	PutObject(ctx context.Context, input *s3.PutObjectInput, options ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, input *s3.DeleteObjectInput, options ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type Stored struct {
	Key string
	URL string
}

type Store struct {
	config Config
	api    ObjectAPI
	http   *http.Client
	now    func() time.Time
}

func NewStore(config Config, api ObjectAPI, httpClient *http.Client, now func() time.Time) *Store {
	config.Prefix = strings.Trim(config.Prefix, "/")
	config.PublicBaseURL = strings.TrimRight(config.PublicBaseURL, "/")
	return &Store{config: config, api: api, http: httpClient, now: now}
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// SanitizeSlug ports sanitizeFeatureImageSlug.
func SanitizeSlug(slug string) (string, error) {
	safe := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(slug), "-"), "-")
	if len(safe) > 120 {
		safe = strings.Trim(safe[:120], "-")
	}
	if safe == "" {
		return "", fmt.Errorf("feature image slug is empty after sanitizing: %q", slug)
	}
	return safe, nil
}

// Key ports buildFeatureImageKey: <prefix>/YYYY/MM/<slug>-<sha16>.<ext> (UTC).
func Key(prefix, slug, sha string, extension string, now time.Time) string {
	now = now.UTC()
	return fmt.Sprintf("%s/%04d/%02d/%s-%s.%s", prefix, now.Year(), int(now.Month()), slug, sha[:16], extension)
}

// StoreWebP uploads WebP bytes, verifies the public URL serves exactly those
// bytes, and deletes the object again if verification fails.
func (s *Store) StoreWebP(ctx context.Context, slug string, data []byte) (Stored, error) {
	if len(data) == 0 || len(data) > MaxImageBytes {
		return Stored{}, fmt.Errorf("feature image size %d is outside 1..%d bytes", len(data), MaxImageBytes)
	}
	if imaging.SniffMIME(data) != "image/webp" {
		return Stored{}, errors.New("feature image bytes are not WebP")
	}
	safeSlug, err := SanitizeSlug(slug)
	if err != nil {
		return Stored{}, err
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	key := Key(s.config.Prefix, safeSlug, sha, "webp", s.now())
	url := s.config.PublicBaseURL + "/" + key

	if _, err := s.api.PutObject(ctx, &s3.PutObjectInput{
		Bucket:         aws.String(s.config.Bucket),
		Key:            aws.String(key),
		Body:           bytes.NewReader(data),
		ContentType:    aws.String("image/webp"),
		CacheControl:   aws.String(cacheControl),
		ChecksumSHA256: aws.String(base64.StdEncoding.EncodeToString(sum[:])),
	}); err != nil {
		return Stored{}, fmt.Errorf("upload feature image: %w", err)
	}
	if err := s.verify(ctx, url, sha, len(data)); err != nil {
		_ = s.Delete(context.WithoutCancel(ctx), key)
		return Stored{}, fmt.Errorf("uploaded feature image failed public verification: %w", err)
	}
	return Stored{Key: key, URL: url}, nil
}

func (s *Store) verify(ctx context.Context, url, sha string, size int) error {
	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Cache-Control", "no-store")
	client := *s.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("redirects are not allowed") }
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("public URL returned HTTP %d", resp.StatusCode)
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mediaType != "image/webp" {
		return fmt.Errorf("public URL returned Content-Type %q, expected image/webp", mediaType)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxImageBytes+1))
	if err != nil {
		return err
	}
	if len(body) != size {
		return fmt.Errorf("public URL returned %d bytes, expected %d", len(body), size)
	}
	got := sha256.Sum256(body)
	if hex.EncodeToString(got[:]) != sha {
		return errors.New("public URL content hash does not match the uploaded bytes")
	}
	return nil
}

// Delete removes an uploaded object; keys outside the prefix are refused.
func (s *Store) Delete(ctx context.Context, key string) error {
	if !strings.HasPrefix(key, s.config.Prefix+"/") || strings.Contains(key, "..") {
		return fmt.Errorf("refusing to delete a key outside %s/: %s", s.config.Prefix, key)
	}
	_, err := s.api.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.config.Bucket), Key: aws.String(key)})
	return err
}
