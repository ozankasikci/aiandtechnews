package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadInlineImagesAreOffByDefault(t *testing.T) {
	cfg, err := Load(mapLookup(publisherEnv()), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InlineImagesEnabled || cfg.InlineImagesInterval != 5*time.Minute {
		t.Fatalf("inline images = %t every %s", cfg.InlineImagesEnabled, cfg.InlineImagesInterval)
	}
	if !strings.Contains(cfg.String(), "InlineImagesEnabled:false InlineImagesInterval:5m0s") {
		t.Fatalf("String() = %s", cfg.String())
	}
}

func TestLoadInlineImagesSettings(t *testing.T) {
	root := t.TempDir()
	env := publisherEnv()
	env["INLINE_IMAGES_ENABLED"] = "true"
	env["INLINE_IMAGES_INTERVAL"] = "10m"
	cfg, err := Load(mapLookup(env), root)
	if err != nil || !cfg.InlineImagesEnabled || cfg.InlineImagesInterval != 10*time.Minute {
		t.Fatalf("cfg = %+v err = %v", cfg, err)
	}
	for name, change := range map[string]func(map[string]string){
		"without the publisher": func(env map[string]string) { env["PUBLISHER_ENABLED"] = "0" },
		"interval under 1m":     func(env map[string]string) { env["INLINE_IMAGES_INTERVAL"] = "30s" },
		"bad interval":          func(env map[string]string) { env["INLINE_IMAGES_INTERVAL"] = "soon" },
		"bad switch":            func(env map[string]string) { env["INLINE_IMAGES_ENABLED"] = "maybe" },
	} {
		bad := publisherEnv()
		bad["INLINE_IMAGES_ENABLED"] = "1"
		change(bad)
		if _, err := Load(mapLookup(bad), root); err == nil {
			t.Errorf("%s: Load() error = nil", name)
		}
	}
}
