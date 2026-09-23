package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Mode string

const (
	ModeDevelopment Mode = "development"
	ModeProduction  Mode = "production"

	DefaultAddress         = "127.0.0.1:4401"
	ProductionDatabasePath = "/Users/ozan/Projects/technews/apps/server/data/technews.db"

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
)

var ErrProductionDatabaseAlias = errors.New("production database path is forbidden outside APP_ENV=production")

type Config struct {
	Mode         Mode
	Address      string
	DatabasePath string
	JWTSecret    string

	// CollectorEnabled wires the feed collector (manual "Collect now" and the loop).
	CollectorEnabled  bool
	CollectorInterval time.Duration

	// PublisherEnabled wires the publisher loop (queued candidate -> published article).
	PublisherEnabled  bool
	PublisherInterval time.Duration
	GeminiAPIKey      string
	GeminiTextModel   string
	GeminiImageModel  string
	GeminiVisionModel string
	AWSRegion         string
	S3Bucket          string
	S3Prefix          string
	S3PublicURL       string
}

func (c Config) String() string {
	return fmt.Sprintf("Config{Mode:%q Address:%q DatabasePath:%q JWTSecret:[REDACTED] CollectorEnabled:%t CollectorInterval:%s "+
		"PublisherEnabled:%t PublisherInterval:%s GeminiAPIKey:[REDACTED] GeminiTextModel:%q GeminiImageModel:%q GeminiVisionModel:%q "+
		"AWSRegion:%q S3Bucket:%q S3Prefix:%q S3PublicURL:%q}",
		c.Mode, c.Address, c.DatabasePath, c.CollectorEnabled, c.CollectorInterval,
		c.PublisherEnabled, c.PublisherInterval, c.GeminiTextModel, c.GeminiImageModel, c.GeminiVisionModel,
		c.AWSRegion, c.S3Bucket, c.S3Prefix, c.S3PublicURL)
}

func (c Config) GoString() string { return c.String() }

// Load builds configuration from environment values supplied by lookup.
// worktreeRoot is explicit so callers and tests control where development data lives.
func Load(lookup func(string) string, worktreeRoot string) (Config, error) {
	if lookup == nil {
		return Config{}, errors.New("environment lookup is required")
	}
	if worktreeRoot == "" {
		return Config{}, errors.New("worktree root is required")
	}

	cfg := Config{
		Mode:         ModeDevelopment,
		Address:      DefaultAddress,
		DatabasePath: filepath.Join(worktreeRoot, "data", "technews.db"),
	}
	if value := lookup("APP_ENV"); value != "" {
		cfg.Mode = Mode(value)
	}
	if value := lookup("SERVER_ADDR"); value != "" {
		cfg.Address = value
	}
	if value := lookup("DATABASE_PATH"); value != "" {
		cfg.DatabasePath = value
	}
	cfg.JWTSecret = lookup("JWT_SECRET")

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
	cfg.GeminiVisionModel = lookup("GEMINI_VISION_MODEL")
	cfg.AWSRegion = lookup("AWS_REGION")
	cfg.S3Bucket = lookup("S3_FEATURE_IMAGE_BUCKET")
	cfg.S3Prefix = DefaultS3FeatureImagePrefix
	if value := lookup("S3_FEATURE_IMAGE_PREFIX"); value != "" {
		cfg.S3Prefix = value
	}
	cfg.S3PublicURL = lookup("S3_FEATURE_IMAGE_PUBLIC_URL")

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

	_, port, err := net.SplitHostPort(c.Address)
	if err != nil {
		return fmt.Errorf("invalid SERVER_ADDR %q: %w", c.Address, err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("invalid SERVER_ADDR port %q", port)
	}
	if portNumber == 3001 || portNumber == 3002 {
		return fmt.Errorf("port %d is reserved and cannot be used by the Go API", portNumber)
	}
	if c.Mode != ModeProduction && portNumber == 4001 {
		return errors.New("port 4001 is reserved for production; set APP_ENV=production explicitly to use it")
	}
	if c.Mode != ModeProduction {
		equivalent, err := pathsEquivalent(c.DatabasePath, ProductionDatabasePath)
		if err != nil {
			return fmt.Errorf("compare DATABASE_PATH with production database: %w", err)
		}
		if equivalent {
			return fmt.Errorf("%w: %q", ErrProductionDatabaseAlias, c.DatabasePath)
		}
	}
	return nil
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
	if c.S3Prefix == "" {
		return errors.New("S3_FEATURE_IMAGE_PREFIX must not be empty")
	}
	if !strings.HasPrefix(c.S3PublicURL, "https://") {
		return errors.New("S3_FEATURE_IMAGE_PUBLIC_URL must be an https URL")
	}
	return nil
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
