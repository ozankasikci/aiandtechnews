package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Mode string

const (
	ModeDevelopment Mode = "development"
	ModeProduction  Mode = "production"

	DefaultAddress = "127.0.0.1:4401"

	// ProductionMarker is the name of the file that marks a production data
	// root. With APP_ENV=production, DATABASE_PATH and UPLOADS_DIR must lie in
	// a directory that contains it or below one; in development, any path in
	// or below such a directory is refused. The marker travels with the data,
	// so a development run cannot open production files on any machine, and
	// production cannot run without the marker in place.
	ProductionMarker = ".technews-production"

	// LegacyNodeDatabasePath and LegacyNodeUploadsDir are the Node server's
	// database and uploads directory on the MacBook that served production
	// before the Go cutover (apps/server/src/index.ts). After cutover they are
	// the rollback copy, so development keeps refusing them (and any alias or,
	// for uploads, any overlapping directory). Production does not use them.
	LegacyNodeDatabasePath = "/Users/ozan/Projects/technews/apps/server/data/technews.db"
	LegacyNodeUploadsDir   = "/Users/ozan/Projects/technews/apps/server/uploads"

	// NodeFallbackJWTSecret is the public secret the Node server signs with
	// when JWT_SECRET is unset (apps/server/src/index.ts). Production refuses it.
	NodeFallbackJWTSecret = "technews-dev-secret-change-in-production"
	// minProductionJWTSecret is the shortest JWT_SECRET production accepts.
	minProductionJWTSecret = 32

	// DefaultCollectorInterval is how often the feed collector loop runs when
	// COLLECTOR_INTERVAL is not set.
	DefaultCollectorInterval = 30 * time.Minute

	// DefaultPublisherInterval is how often the publisher loop runs when
	// PUBLISHER_INTERVAL is not set.
	DefaultPublisherInterval = time.Minute
	// minPublisherInterval is the smallest interval Validate accepts when the
	// publisher is enabled.
	minPublisherInterval = 10 * time.Second

	// DefaultS3FeatureImagePrefix is the S3 key prefix used when
	// S3_FEATURE_IMAGE_PREFIX is not set.
	DefaultS3FeatureImagePrefix = "features"

	// DefaultMediaS3Prefix is the S3 key prefix of dashboard media uploads when
	// MEDIA_STORAGE=s3 and MEDIA_S3_PREFIX is not set.
	DefaultMediaS3Prefix = "uploads"
)

// MediaStorage selects where new dashboard media uploads are stored.
type MediaStorage string

const (
	// MediaStorageLocal keeps uploads in UploadsDir, served at /uploads/*.
	MediaStorageLocal MediaStorage = "local"
	// MediaStorageS3 puts new uploads in the feature-image bucket under
	// MediaS3Prefix; /uploads/* keeps serving the legacy local files.
	MediaStorageS3 MediaStorage = "s3"
)

var ErrProductionDatabaseAlias = errors.New("production database path is forbidden outside APP_ENV=production")

var ErrProductionUploadsAlias = errors.New("production uploads directory (or a directory overlapping it) is forbidden outside APP_ENV=production")

// ErrProductionMarkerMissing reports a production path that is not inside a
// directory marked with ProductionMarker.
var ErrProductionMarkerMissing = errors.New("APP_ENV=production requires the path to be inside a directory marked with " + ProductionMarker)

type Config struct {
	Mode         Mode
	Address      string
	DatabasePath string
	JWTSecret    string
	// UploadsDir is the dashboard media library's directory, served at
	// /uploads/*. Development defaults to <worktree>/data/uploads; production
	// has no default and requires UPLOADS_DIR explicitly.
	UploadsDir string
	// ProductionDatabaseGuard (PRODUCTION_DATABASE_PATH) and
	// ProductionUploadsGuard (PRODUCTION_UPLOADS_DIR) are optional extra
	// development guards: a development run refuses a DATABASE_PATH equivalent
	// to the first and an UPLOADS_DIR overlapping the second. Ignored in
	// production.
	ProductionDatabaseGuard string
	ProductionUploadsGuard  string
	// TimeZone is the TZ environment variable. Go reads TZ for time.Local;
	// production requires it explicitly, because offset-less published_at
	// values are read in the process zone (it must match the Node host's).
	TimeZone string
	// MediaStorage (MEDIA_STORAGE, default local) and MediaS3Prefix
	// (MEDIA_S3_PREFIX, default "uploads"). S3 reuses AWS_REGION,
	// S3_FEATURE_IMAGE_BUCKET, and S3_FEATURE_IMAGE_PUBLIC_URL.
	MediaStorage  MediaStorage
	MediaS3Prefix string

	// CollectorEnabled wires the feed collector (manual "Collect now" and the loop).
	CollectorEnabled  bool
	CollectorInterval time.Duration

	// PublisherEnabled wires the publisher loop (queued candidate -> published article).
	PublisherEnabled  bool
	PublisherInterval time.Duration
	GeminiAPIKey      string
	GeminiTextModel   string
	GeminiImageModel  string
	GeminiImageSize   string // GEMINI_IMAGE_SIZE: 1K, 2K or 4K (default 2K; lite image models only support 1K)
	// FeaturedImageSource is FEATURED_IMAGE_SOURCE: "generate" (Gemini illustration, default)
	// or "source" (copy the source article's own image).
	FeaturedImageSource string
	GeminiVisionModel   string
	AWSRegion           string
	S3Bucket            string
	S3Prefix            string
	S3PublicURL         string

	// IndexNowEnabled wires the IndexNow notifier into the publisher. When
	// false (the default), published URLs are not submitted to IndexNow.
	IndexNowEnabled bool

	// Newsletter settings keep Node's names (apps/server/.env.example) and
	// raw values: Node trims each one where it uses it, and so does
	// internal/newsletter. None of them is required to start: like Node,
	// signup works unconfigured, and the other routes answer 503 until
	// NEWSLETTER_TOKEN_SECRET is set.
	NewsletterSiteURL     string // NEWSLETTER_SITE_URL (default https://aiandtech.news)
	NewsletterTokenSecret string // NEWSLETTER_TOKEN_SECRET, signs confirm/unsubscribe links (secret)
	NewsletterCronSecret  string // NEWSLETTER_CRON_SECRET || CRON_SECRET, guards the digest (secret)
	ResendAPIKey          string // RESEND_API_KEY (secret)
	NewsletterFrom        string // NEWSLETTER_FROM
	NewsletterReplyTo     string // NEWSLETTER_REPLY_TO (optional)
}

func (c Config) String() string {
	return fmt.Sprintf("Config{Mode:%q Address:%q TimeZone:%q DatabasePath:%q UploadsDir:%q MediaStorage:%q MediaS3Prefix:%q JWTSecret:[REDACTED] CollectorEnabled:%t CollectorInterval:%s "+
		"PublisherEnabled:%t PublisherInterval:%s GeminiAPIKey:[REDACTED] GeminiTextModel:%q GeminiImageModel:%q GeminiImageSize:%q FeaturedImageSource:%q GeminiVisionModel:%q "+
		"AWSRegion:%q S3Bucket:%q S3Prefix:%q S3PublicURL:%q IndexNowEnabled:%t "+
		"NewsletterSiteURL:%q NewsletterTokenSecret:[REDACTED] NewsletterCronSecret:[REDACTED] ResendAPIKey:[REDACTED] NewsletterFrom:%q NewsletterReplyTo:%q}",
		c.Mode, c.Address, c.TimeZone, c.DatabasePath, c.UploadsDir, c.MediaStorage, c.MediaS3Prefix, c.CollectorEnabled, c.CollectorInterval,
		c.PublisherEnabled, c.PublisherInterval, c.GeminiTextModel, c.GeminiImageModel, c.GeminiImageSize, c.FeaturedImageSource, c.GeminiVisionModel,
		c.AWSRegion, c.S3Bucket, c.S3Prefix, c.S3PublicURL, c.IndexNowEnabled,
		c.NewsletterSiteURL, c.NewsletterFrom, c.NewsletterReplyTo)
}

func (c Config) GoString() string { return c.String() }

// Load builds configuration from environment values supplied by lookup.
// worktreeRoot is explicit so callers and tests control where development
// data lives. Production has no path defaults, so worktreeRoot may be empty
// there (a prebuilt binary runs without a repository checkout).
func Load(lookup func(string) string, worktreeRoot string) (Config, error) {
	if lookup == nil {
		return Config{}, errors.New("environment lookup is required")
	}

	cfg := Config{Mode: ModeDevelopment, Address: DefaultAddress}
	if value := lookup("APP_ENV"); value != "" {
		cfg.Mode = Mode(value)
	}
	if cfg.Mode != ModeProduction {
		if worktreeRoot == "" {
			return Config{}, errors.New("worktree root is required")
		}
		cfg.DatabasePath = filepath.Join(worktreeRoot, "data", "technews.db")
	}
	if value := lookup("SERVER_ADDR"); value != "" {
		cfg.Address = value
	}
	if value := lookup("DATABASE_PATH"); value != "" {
		cfg.DatabasePath = value
	}
	cfg.JWTSecret = lookup("JWT_SECRET")
	cfg.TimeZone = lookup("TZ")
	cfg.ProductionDatabaseGuard = lookup("PRODUCTION_DATABASE_PATH")
	cfg.ProductionUploadsGuard = lookup("PRODUCTION_UPLOADS_DIR")
	// Production never gets a default uploads directory: Validate requires an
	// explicit UPLOADS_DIR there.
	switch value := lookup("UPLOADS_DIR"); {
	case value != "":
		cfg.UploadsDir = value
	case cfg.Mode != ModeProduction:
		cfg.UploadsDir = filepath.Join(worktreeRoot, "data", "uploads")
	}

	cfg.CollectorInterval = DefaultCollectorInterval
	collectorEnabled, err := parseOnOff("COLLECTOR_ENABLED", lookup("COLLECTOR_ENABLED"))
	if err != nil {
		return Config{}, err
	}
	cfg.CollectorEnabled = collectorEnabled
	if value := lookup("COLLECTOR_INTERVAL"); value != "" {
		interval, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("COLLECTOR_INTERVAL: %w", err)
		}
		cfg.CollectorInterval = interval
	}

	cfg.PublisherInterval = DefaultPublisherInterval
	publisherEnabled, err := parseOnOff("PUBLISHER_ENABLED", lookup("PUBLISHER_ENABLED"))
	if err != nil {
		return Config{}, err
	}
	cfg.PublisherEnabled = publisherEnabled
	if value := lookup("PUBLISHER_INTERVAL"); value != "" {
		interval, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("PUBLISHER_INTERVAL: %w", err)
		}
		cfg.PublisherInterval = interval
	}
	cfg.GeminiAPIKey = lookup("GEMINI_API_KEY")
	cfg.GeminiTextModel = lookup("GEMINI_TEXT_MODEL")
	cfg.GeminiImageModel = lookup("GEMINI_IMAGE_MODEL")
	switch source := lookup("FEATURED_IMAGE_SOURCE"); source {
	case "", "generate":
		cfg.FeaturedImageSource = "generate"
	case "source":
		cfg.FeaturedImageSource = source
	default:
		return Config{}, fmt.Errorf("FEATURED_IMAGE_SOURCE must be generate or source, got %q", source)
	}
	switch size := lookup("GEMINI_IMAGE_SIZE"); size {
	case "":
		cfg.GeminiImageSize = "2K"
	case "1K", "2K", "4K":
		cfg.GeminiImageSize = size
	default:
		return Config{}, fmt.Errorf("GEMINI_IMAGE_SIZE must be 1K, 2K, or 4K, got %q", size)
	}
	cfg.GeminiVisionModel = lookup("GEMINI_VISION_MODEL")
	cfg.AWSRegion = lookup("AWS_REGION")
	cfg.S3Bucket = lookup("S3_FEATURE_IMAGE_BUCKET")
	cfg.S3Prefix = DefaultS3FeatureImagePrefix
	if value := lookup("S3_FEATURE_IMAGE_PREFIX"); value != "" {
		cfg.S3Prefix = value
	}
	cfg.S3PublicURL = lookup("S3_FEATURE_IMAGE_PUBLIC_URL")

	switch value := strings.ToLower(lookup("MEDIA_STORAGE")); value {
	case "", string(MediaStorageLocal):
		cfg.MediaStorage = MediaStorageLocal
	case string(MediaStorageS3):
		cfg.MediaStorage = MediaStorageS3
	default:
		return Config{}, fmt.Errorf("MEDIA_STORAGE: invalid value %q (want local or s3)", lookup("MEDIA_STORAGE"))
	}
	cfg.MediaS3Prefix = DefaultMediaS3Prefix
	if value := lookup("MEDIA_S3_PREFIX"); value != "" {
		cfg.MediaS3Prefix = value
	}

	indexNowEnabled, err := parseOnOff("INDEXNOW_ENABLED", lookup("INDEXNOW_ENABLED"))
	if err != nil {
		return Config{}, err
	}
	cfg.IndexNowEnabled = indexNowEnabled

	cfg.NewsletterSiteURL = lookup("NEWSLETTER_SITE_URL")
	cfg.NewsletterTokenSecret = lookup("NEWSLETTER_TOKEN_SECRET")
	// Node: process.env.NEWSLETTER_CRON_SECRET || process.env.CRON_SECRET || ""
	// (apps/server/src/index.ts), so only an empty value falls through.
	cfg.NewsletterCronSecret = lookup("NEWSLETTER_CRON_SECRET")
	if cfg.NewsletterCronSecret == "" {
		cfg.NewsletterCronSecret = lookup("CRON_SECRET")
	}
	cfg.ResendAPIKey = lookup("RESEND_API_KEY")
	cfg.NewsletterFrom = lookup("NEWSLETTER_FROM")
	cfg.NewsletterReplyTo = lookup("NEWSLETTER_REPLY_TO")

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// parseOnOff applies the same strict, case-insensitive on/off parsing to every
// *_ENABLED environment flag: "", "0", "false", "no" are off; "1", "true",
// "yes" are on; anything else is a configuration error naming the variable.
func parseOnOff(name, value string) (bool, error) {
	switch strings.ToLower(value) {
	case "", "0", "false", "no":
		return false, nil
	case "1", "true", "yes":
		return true, nil
	default:
		return false, fmt.Errorf("%s: invalid value %q", name, value)
	}
}

func (c Config) Validate() error {
	if c.Mode != ModeDevelopment && c.Mode != ModeProduction {
		return fmt.Errorf("unsupported APP_ENV %q", c.Mode)
	}
	if c.CollectorEnabled && c.CollectorInterval < time.Minute {
		return errors.New("COLLECTOR_INTERVAL must be at least 1m")
	}
	if c.PublisherEnabled {
		if err := c.validatePublisher(); err != nil {
			return err
		}
	}
	if c.MediaStorage == MediaStorageS3 {
		if err := c.validateMediaS3(); err != nil {
			return err
		}
	}

	_, port, err := net.SplitHostPort(c.Address)
	if err != nil {
		return fmt.Errorf("invalid SERVER_ADDR %q: %w", c.Address, err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("invalid SERVER_ADDR port %q", port)
	}
	if c.Mode != ModeProduction {
		// Development must never collide with the dashboard (3001), the other
		// local app (3002), or the port the Node production API used (4001).
		// Production may bind any port.
		if portNumber == 3001 || portNumber == 3002 {
			return fmt.Errorf("port %d is reserved and cannot be used by the Go API in development", portNumber)
		}
		if portNumber == 4001 {
			return errors.New("port 4001 is reserved for production; set APP_ENV=production explicitly to use it")
		}
	}
	if c.Mode == ModeProduction {
		if err := c.validateProductionPaths(); err != nil {
			return err
		}
		if err := c.validateProductionRuntime(); err != nil {
			return err
		}
	} else if err := c.validateDevelopmentDatabase(); err != nil {
		return err
	}
	if c.UploadsDir != "" {
		if !filepath.IsAbs(c.UploadsDir) {
			return fmt.Errorf("UPLOADS_DIR must be an absolute path, got %q", c.UploadsDir)
		}
		uploads, err := canonicalPath(c.UploadsDir)
		if err != nil {
			return fmt.Errorf("canonicalize UPLOADS_DIR: %w", err)
		}
		// The media library deletes files inside UPLOADS_DIR by name, so it
		// must never contain the database (or be its directory).
		if c.DatabasePath != "" {
			database, err := canonicalPath(c.DatabasePath)
			if err != nil {
				return fmt.Errorf("canonicalize DATABASE_PATH: %w", err)
			}
			if pathWithin(database, uploads) {
				return fmt.Errorf("UPLOADS_DIR must not contain DATABASE_PATH: %q contains %q", c.UploadsDir, c.DatabasePath)
			}
		}
		if c.Mode != ModeProduction {
			if err := c.validateDevelopmentUploads(uploads); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateProductionPaths requires explicit absolute paths for the database
// and the uploads directory, both inside a directory marked with
// ProductionMarker.
func (c Config) validateProductionPaths() error {
	if c.DatabasePath == "" {
		return errors.New("DATABASE_PATH is required when APP_ENV=production")
	}
	if !filepath.IsAbs(c.DatabasePath) {
		return fmt.Errorf("DATABASE_PATH must be an absolute path when APP_ENV=production, got %q", c.DatabasePath)
	}
	if c.UploadsDir == "" {
		return errors.New("UPLOADS_DIR is required when APP_ENV=production")
	}
	if !filepath.IsAbs(c.UploadsDir) {
		return fmt.Errorf("UPLOADS_DIR must be an absolute path, got %q", c.UploadsDir)
	}
	for _, target := range []struct {
		name, path string
		isDir      bool
	}{{"DATABASE_PATH", c.DatabasePath, false}, {"UPLOADS_DIR", c.UploadsDir, true}} {
		root, err := markedRoot(target.path, target.isDir)
		if err != nil {
			return fmt.Errorf("look for %s above %s: %w", ProductionMarker, target.name, err)
		}
		if root == "" {
			return fmt.Errorf("%w: %s %q", ErrProductionMarkerMissing, target.name, target.path)
		}
	}
	return nil
}

// validateProductionRuntime requires a strong JWT_SECRET and an explicit,
// loadable TZ. Error messages never include the secret.
func (c Config) validateProductionRuntime() error {
	if c.JWTSecret == NodeFallbackJWTSecret {
		return errors.New("JWT_SECRET must not be the Node server's public fallback secret when APP_ENV=production")
	}
	if len(c.JWTSecret) < minProductionJWTSecret {
		return fmt.Errorf("JWT_SECRET must be at least %d bytes when APP_ENV=production", minProductionJWTSecret)
	}
	if c.TimeZone == "" {
		return errors.New("TZ is required when APP_ENV=production (for example TZ=Europe/Istanbul, the Node host's zone)")
	}
	if _, err := time.LoadLocation(c.TimeZone); err != nil {
		return fmt.Errorf("TZ %q is not a known time zone: %w", c.TimeZone, err)
	}
	return nil
}

// validateDevelopmentDatabase refuses production databases outside
// production: anything inside a marked directory, the legacy Node database,
// and PRODUCTION_DATABASE_PATH, including aliases of either.
func (c Config) validateDevelopmentDatabase() error {
	if c.DatabasePath == "" {
		return nil
	}
	root, err := markedRoot(c.DatabasePath, false)
	if err != nil {
		return fmt.Errorf("look for %s above DATABASE_PATH: %w", ProductionMarker, err)
	}
	if root != "" {
		return fmt.Errorf("%w: %q is inside %q, which contains %s", ErrProductionDatabaseAlias, c.DatabasePath, root, ProductionMarker)
	}
	for _, protected := range []string{LegacyNodeDatabasePath, c.ProductionDatabaseGuard} {
		if protected == "" {
			continue
		}
		equivalent, err := pathsEquivalent(c.DatabasePath, protected)
		if err != nil {
			return fmt.Errorf("compare DATABASE_PATH with production database: %w", err)
		}
		if equivalent {
			return fmt.Errorf("%w: %q", ErrProductionDatabaseAlias, c.DatabasePath)
		}
	}
	return nil
}

// validateDevelopmentUploads refuses production uploads directories outside
// production: anything inside a marked directory, and any directory equal
// to, inside, or containing the legacy Node uploads directory or
// PRODUCTION_UPLOADS_DIR.
func (c Config) validateDevelopmentUploads(uploads string) error {
	root, err := markedRoot(c.UploadsDir, true)
	if err != nil {
		return fmt.Errorf("look for %s above UPLOADS_DIR: %w", ProductionMarker, err)
	}
	if root != "" {
		return fmt.Errorf("%w: %q is inside %q, which contains %s", ErrProductionUploadsAlias, c.UploadsDir, root, ProductionMarker)
	}
	for _, protected := range []string{LegacyNodeUploadsDir, c.ProductionUploadsGuard} {
		if protected == "" {
			continue
		}
		equivalent, err := pathsEquivalent(c.UploadsDir, protected)
		if err != nil {
			return fmt.Errorf("compare UPLOADS_DIR with production uploads directory: %w", err)
		}
		production, err := canonicalPath(protected)
		if err != nil {
			return fmt.Errorf("canonicalize production uploads directory: %w", err)
		}
		if equivalent || pathWithin(uploads, production) || pathWithin(production, uploads) {
			return fmt.Errorf("%w: %q", ErrProductionUploadsAlias, c.UploadsDir)
		}
	}
	return nil
}

// markedRoot returns the nearest directory at or above path (its parent
// directory when isDir is false) that contains ProductionMarker, or "" when
// there is none. It works on the canonical path, so symlinked aliases cannot
// escape a marked tree, and on paths that do not exist yet.
func markedRoot(path string, isDir bool) (string, error) {
	canonical, err := canonicalPath(path)
	if err != nil {
		return "", err
	}
	dir := canonical
	if !isDir {
		dir = filepath.Dir(canonical)
	}
	for {
		_, err := os.Lstat(filepath.Join(dir, ProductionMarker))
		switch {
		case err == nil:
			return dir, nil
		case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
		default:
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

// validatePublisher ports media.Config.Validate's checks so internal/config
// does not need to import internal/media. Keep both in sync.
func (c Config) validatePublisher() error {
	if c.PublisherInterval < minPublisherInterval {
		return fmt.Errorf("PUBLISHER_INTERVAL must be at least %s", minPublisherInterval)
	}
	var missing []string
	if c.GeminiAPIKey == "" {
		missing = append(missing, "GEMINI_API_KEY")
	}
	if c.AWSRegion == "" {
		missing = append(missing, "AWS_REGION")
	}
	if c.S3Bucket == "" {
		missing = append(missing, "S3_FEATURE_IMAGE_BUCKET")
	}
	if c.S3PublicURL == "" {
		missing = append(missing, "S3_FEATURE_IMAGE_PUBLIC_URL")
	}
	if len(missing) > 0 {
		return fmt.Errorf("PUBLISHER_ENABLED requires %s", strings.Join(missing, ", "))
	}
	prefix := strings.Trim(c.S3Prefix, "/")
	if prefix == "" {
		return errors.New("S3_FEATURE_IMAGE_PREFIX must not be empty")
	}
	if strings.Contains(prefix, "..") {
		return errors.New(`S3_FEATURE_IMAGE_PREFIX must not contain ".."`)
	}
	if parsed, err := url.Parse(c.S3PublicURL); err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return errors.New("S3_FEATURE_IMAGE_PUBLIC_URL must be an https URL with a host")
	}
	return nil
}

// validateMediaS3 checks the S3 media backend settings. The media prefix
// must not overlap the feature-image prefix, so neither store's key
// containment can reach the other's objects.
func (c Config) validateMediaS3() error {
	var missing []string
	if c.AWSRegion == "" {
		missing = append(missing, "AWS_REGION")
	}
	if c.S3Bucket == "" {
		missing = append(missing, "S3_FEATURE_IMAGE_BUCKET")
	}
	if c.S3PublicURL == "" {
		missing = append(missing, "S3_FEATURE_IMAGE_PUBLIC_URL")
	}
	if len(missing) > 0 {
		return fmt.Errorf("MEDIA_STORAGE=s3 requires %s", strings.Join(missing, ", "))
	}
	if parsed, err := url.Parse(c.S3PublicURL); err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return errors.New("S3_FEATURE_IMAGE_PUBLIC_URL must be an https URL with a host")
	}
	prefix := strings.Trim(c.MediaS3Prefix, "/")
	if prefix == "" {
		return errors.New("MEDIA_S3_PREFIX must not be empty")
	}
	if strings.Contains(prefix, "..") {
		return errors.New(`MEDIA_S3_PREFIX must not contain ".."`)
	}
	features := strings.Trim(c.S3Prefix, "/")
	if prefix == features || strings.HasPrefix(prefix, features+"/") || strings.HasPrefix(features, prefix+"/") {
		return fmt.Errorf("MEDIA_S3_PREFIX must not overlap S3_FEATURE_IMAGE_PREFIX (%q)", features)
	}
	return nil
}

// pathWithin reports whether path is parent or lies below it (both
// canonical). "/a/data" is not within "/a/dat".
func pathWithin(path, parent string) bool {
	if path == parent {
		return true
	}
	return strings.HasPrefix(path, strings.TrimSuffix(parent, string(filepath.Separator))+string(filepath.Separator))
}

// pathsEquivalent compares canonical names first, then file identity when both
// targets exist. The identity check catches aliases that canonical names do not,
// including hard links and case variants on case-insensitive filesystems.
func pathsEquivalent(first, second string) (bool, error) {
	canonicalFirst, err := canonicalPath(first)
	if err != nil {
		return false, fmt.Errorf("canonicalize first path: %w", err)
	}
	canonicalSecond, err := canonicalPath(second)
	if err != nil {
		return false, fmt.Errorf("canonicalize second path: %w", err)
	}
	if canonicalFirst == canonicalSecond {
		return true, nil
	}

	firstInfo, firstErr := os.Stat(canonicalFirst)
	secondInfo, secondErr := os.Stat(canonicalSecond)
	if firstErr != nil && !errors.Is(firstErr, os.ErrNotExist) {
		return false, fmt.Errorf("stat first path: %w", firstErr)
	}
	if secondErr != nil && !errors.Is(secondErr, os.ErrNotExist) {
		return false, fmt.Errorf("stat second path: %w", secondErr)
	}
	if firstErr != nil || secondErr != nil {
		return false, nil
	}
	return os.SameFile(firstInfo, secondInfo), nil
}

// canonicalPath resolves symlinks in the deepest existing ancestor, then
// reattaches any nonexistent suffix. This permits new development databases
// while ensuring aliases of existing protected paths compare equal.
func canonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	existing := filepath.Clean(absolute)
	var suffix []string
	for {
		_, err = os.Lstat(existing)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}

		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("no existing ancestor for %q", path)
		}
		suffix = append([]string{filepath.Base(existing)}, suffix...)
		existing = parent
	}

	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	parts := append([]string{resolved}, suffix...)
	return filepath.Clean(filepath.Join(parts...)), nil
}
