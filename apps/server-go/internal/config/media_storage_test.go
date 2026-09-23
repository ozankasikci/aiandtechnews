package config

import (
	"strings"
	"testing"
)

func s3MediaEnv(extra map[string]string) map[string]string {
	env := map[string]string{
		"MEDIA_STORAGE":               "s3",
		"AWS_REGION":                  "eu-west-1",
		"S3_FEATURE_IMAGE_BUCKET":     "bucket",
		"S3_FEATURE_IMAGE_PUBLIC_URL": "https://images.example.invalid",
	}
	for key, value := range extra {
		env[key] = value
	}
	return env
}

func TestLoadDefaultsMediaStorageToLocal(t *testing.T) {
	cfg, err := Load(func(string) string { return "" }, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MediaStorage != MediaStorageLocal || cfg.MediaS3Prefix != DefaultMediaS3Prefix || DefaultMediaS3Prefix != "uploads" {
		t.Fatalf("MediaStorage = %q, MediaS3Prefix = %q", cfg.MediaStorage, cfg.MediaS3Prefix)
	}
	// Local storage needs no AWS settings.
	cfg, err = Load(mapLookup(map[string]string{"MEDIA_STORAGE": "LOCAL"}), t.TempDir())
	if err != nil || cfg.MediaStorage != MediaStorageLocal {
		t.Fatalf("cfg = %+v, err = %v", cfg, err)
	}
}

func TestLoadParsesMediaStorageStrictly(t *testing.T) {
	for _, value := range []string{"disk", "S3 ", "aws", "1"} {
		_, err := Load(mapLookup(map[string]string{"MEDIA_STORAGE": value}), t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "MEDIA_STORAGE: invalid value") {
			t.Errorf("MEDIA_STORAGE=%q error = %v", value, err)
		}
	}
}

func TestLoadS3MediaStorageReusesTheFeatureImageBucket(t *testing.T) {
	cfg, err := Load(mapLookup(s3MediaEnv(map[string]string{"MEDIA_S3_PREFIX": "/media/"})), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MediaStorage != MediaStorageS3 || cfg.MediaS3Prefix != "/media/" || cfg.S3Bucket != "bucket" || cfg.PublisherEnabled {
		t.Fatalf("cfg = %+v", cfg)
	}
	if !strings.Contains(cfg.String(), `MediaStorage:"s3" MediaS3Prefix:"/media/"`) {
		t.Fatalf("String() = %s", cfg.String())
	}
}

func TestValidateRequiresS3SettingsForS3MediaStorage(t *testing.T) {
	for name, tc := range map[string]struct {
		env  map[string]string
		want string
	}{
		"missing everything": {map[string]string{"MEDIA_STORAGE": "s3"}, "MEDIA_STORAGE=s3 requires AWS_REGION, S3_FEATURE_IMAGE_BUCKET, S3_FEATURE_IMAGE_PUBLIC_URL"},
		"http public URL":    {s3MediaEnv(map[string]string{"S3_FEATURE_IMAGE_PUBLIC_URL": "http://images.example.invalid"}), "S3_FEATURE_IMAGE_PUBLIC_URL must be an https URL with a host"},
		"empty prefix":       {s3MediaEnv(map[string]string{"MEDIA_S3_PREFIX": "/"}), "MEDIA_S3_PREFIX must not be empty"},
		"traversing prefix":  {s3MediaEnv(map[string]string{"MEDIA_S3_PREFIX": "uploads/../features"}), `MEDIA_S3_PREFIX must not contain ".."`},
		"feature prefix":     {s3MediaEnv(map[string]string{"MEDIA_S3_PREFIX": "features"}), "MEDIA_S3_PREFIX must not overlap S3_FEATURE_IMAGE_PREFIX"},
		"inside feature":     {s3MediaEnv(map[string]string{"MEDIA_S3_PREFIX": "features/uploads"}), "MEDIA_S3_PREFIX must not overlap S3_FEATURE_IMAGE_PREFIX"},
		"contains feature":   {s3MediaEnv(map[string]string{"MEDIA_S3_PREFIX": "media", "S3_FEATURE_IMAGE_PREFIX": "media/features"}), "MEDIA_S3_PREFIX must not overlap S3_FEATURE_IMAGE_PREFIX"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(mapLookup(tc.env), t.TempDir())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load() error = %v, want %q", err, tc.want)
			}
		})
	}
}
