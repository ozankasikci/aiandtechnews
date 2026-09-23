package media

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3UploadsConfig configures the S3 backend of the media library. It shares
// the bucket and public base URL with the feature-image Store but uses its own
// key prefix.
type S3UploadsConfig struct {
	Bucket        string
	Prefix        string
	PublicBaseURL string
}

// S3Uploads is the S3 backend of the media library (MEDIA_STORAGE=s3).
// Objects are <prefix>/<multer-style name>, public at <base URL>/<key>,
// stored with the declared content type, an immutable cache policy, and a
// SHA-256 checksum, and verified through the public URL like feature images.
// A file is buffered in memory (at most MaxUploadBytes) to checksum it.
type S3Uploads struct {
	bucket string
	prefix string
	base   string
	api    ObjectAPI
	http   *http.Client
	random io.Reader
}

func NewS3Uploads(config S3UploadsConfig, api ObjectAPI, httpClient *http.Client) *S3Uploads {
	return &S3Uploads{
		bucket: config.Bucket,
		prefix: strings.Trim(config.Prefix, "/"),
		base:   strings.TrimRight(config.PublicBaseURL, "/"),
		api:    api,
		http:   httpClient,
		random: rand.Reader,
	}
}

func (s *S3Uploads) urlPrefix() string { return s.base + "/" + s.prefix + "/" }

// Owns reports whether url points into this backend's public prefix.
func (s *S3Uploads) Owns(url string) bool { return strings.HasPrefix(url, s.urlPrefix()) }

func (s *S3Uploads) Save(ctx context.Context, content io.Reader, originalName, contentType string) (StoredFile, error) {
	data, err := io.ReadAll(io.LimitReader(content, MaxUploadBytes))
	if err != nil {
		return StoredFile{}, err
	}
	if len(data) == MaxUploadBytes {
		return StoredFile{}, ErrFileTooLarge
	}
	name, err := newUploadName(s.random, originalName)
	if err != nil {
		return StoredFile{}, err
	}
	key := s.prefix + "/" + name
	sum := sha256.Sum256(data)
	if err := putObject(ctx, s.api, &s3.PutObjectInput{
		Bucket:         aws.String(s.bucket),
		Key:            aws.String(key),
		Body:           bytes.NewReader(data),
		ContentType:    aws.String(contentType),
		CacheControl:   aws.String(cacheControl),
		ChecksumSHA256: aws.String(base64.StdEncoding.EncodeToString(sum[:])),
	}); err != nil {
		return StoredFile{}, fmt.Errorf("upload media object: %w", err)
	}
	url := s.base + "/" + key
	if err := verifyPublic(ctx, s.http, url, contentType, hex.EncodeToString(sum[:]), len(data)); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		_ = s.deleteKey(cleanupCtx, key)
		cancel()
		return StoredFile{}, fmt.Errorf("uploaded media object failed public verification: %w", err)
	}
	return StoredFile{Name: name, Size: int64(len(data)), URL: url}, nil
}

// Remove deletes the object url names. Only single-segment names directly
// under the media prefix are accepted, so no other key (feature images,
// nested or traversing names) can be deleted. S3 deletes are idempotent, so a
// missing object is not an error.
func (s *S3Uploads) Remove(ctx context.Context, url string) error {
	name, ok := strings.CutPrefix(url, s.urlPrefix())
	if !ok {
		return fmt.Errorf("refusing to delete a media URL outside %s: %s", s.urlPrefix(), url)
	}
	if err := checkStoredName(name); err != nil {
		return err
	}
	return s.deleteKey(ctx, s.prefix+"/"+name)
}

func (s *S3Uploads) deleteKey(ctx context.Context, key string) error {
	_, err := s.api.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return err
}

// NewStorage returns the media library's Storage. With s3 nil (the default,
// MEDIA_STORAGE=local) it is the local directory. With s3 set, new uploads go
// to S3 while deletes are routed by URL shape: URLs under the S3 media prefix
// delete the object, anything else (the legacy /uploads/<file> rows) deletes
// from UPLOADS_DIR, which /uploads/* keeps serving.
func NewStorage(local *Uploads, s3 *S3Uploads) Storage {
	if s3 == nil {
		return local
	}
	return &routedStorage{local: local, s3: s3}
}

type routedStorage struct {
	local *Uploads
	s3    *S3Uploads
}

func (r *routedStorage) Save(ctx context.Context, content io.Reader, originalName, contentType string) (StoredFile, error) {
	return r.s3.Save(ctx, content, originalName, contentType)
}

func (r *routedStorage) Remove(ctx context.Context, url string) error {
	if r.s3.Owns(url) {
		return r.s3.Remove(ctx, url)
	}
	return r.local.Remove(ctx, url)
}
