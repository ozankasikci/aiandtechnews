package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

func TestNewWithDatabaseOpensObjectStorageOnceForS3Media(t *testing.T) {
	for name, publisherEnabled := range map[string]bool{"media only": false, "media and publisher": true} {
		t.Run(name, func(t *testing.T) {
			opened := 0
			stubPublisherStorage(t, func(context.Context, config.Config) (media.ObjectAPI, error) {
				opened++
				return unusedObjectAPI{}, nil
			})
			cfg := publisherConfig(t)
			cfg.PublisherEnabled = publisherEnabled
			cfg.MediaStorage, cfg.MediaS3Prefix = config.MediaStorageS3, "uploads"
			db, _ := testutil.OpenDatabase(t)
			if _, err := NewWithDatabase(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db); err != nil {
				t.Fatal(err)
			}
			if opened != 1 {
				t.Fatalf("object storage opened %d times, want 1", opened)
			}
		})
	}
}

func TestNewWithDatabaseFailsWhenS3MediaStorageCheckFails(t *testing.T) {
	checkErr := errors.New("head bucket: NoSuchBucket")
	stubPublisherStorage(t, func(context.Context, config.Config) (media.ObjectAPI, error) { return nil, checkErr })
	cfg := publisherConfig(t)
	cfg.PublisherEnabled = false
	cfg.MediaStorage, cfg.MediaS3Prefix = config.MediaStorageS3, "uploads"
	db, _ := testutil.OpenDatabase(t)
	_, err := NewWithDatabase(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db)
	if !errors.Is(err, checkErr) || !strings.Contains(err.Error(), "media storage check failed") {
		t.Fatalf("err = %v, want media storage check failure", err)
	}
}

func TestNewWithDatabaseDoesNotOpenObjectStorageForLocalMedia(t *testing.T) {
	stubPublisherStorage(t, func(context.Context, config.Config) (media.ObjectAPI, error) {
		t.Fatal("object storage opened for local media without the publisher")
		return nil, nil
	})
	cfg := publisherConfig(t)
	cfg.PublisherEnabled = false
	cfg.MediaStorage = config.MediaStorageLocal
	db, _ := testutil.OpenDatabase(t)
	if _, err := NewWithDatabase(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db); err != nil {
		t.Fatal(err)
	}
}
