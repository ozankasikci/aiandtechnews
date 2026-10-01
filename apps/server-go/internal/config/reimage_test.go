package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadReimageIsOffByDefault(t *testing.T) {
	cfg, err := Load(mapLookup(publisherEnv()), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReimageEnabled || cfg.ReimageInterval != 10*time.Minute {
		t.Fatalf("reimage = %t every %s", cfg.ReimageEnabled, cfg.ReimageInterval)
	}
	if !strings.Contains(cfg.String(), "ReimageEnabled:false ReimageInterval:10m0s") {
		t.Fatalf("String() = %s", cfg.String())
	}
}

func TestLoadReimageSettings(t *testing.T) {
	root := t.TempDir()
	env := publisherEnv()
	env["REIMAGE_ENABLED"] = "true"
	env["REIMAGE_INTERVAL"] = "20m"
	cfg, err := Load(mapLookup(env), root)
	if err != nil || !cfg.ReimageEnabled || cfg.ReimageInterval != 20*time.Minute {
		t.Fatalf("cfg = %+v err = %v", cfg, err)
	}
	for name, change := range map[string]func(map[string]string){
		"without the publisher": func(env map[string]string) { env["PUBLISHER_ENABLED"] = "0" },
		"interval under 1m":     func(env map[string]string) { env["REIMAGE_INTERVAL"] = "30s" },
		"bad interval":          func(env map[string]string) { env["REIMAGE_INTERVAL"] = "soon" },
		"bad switch":            func(env map[string]string) { env["REIMAGE_ENABLED"] = "maybe" },
	} {
		bad := publisherEnv()
		bad["REIMAGE_ENABLED"] = "1"
		change(bad)
		if _, err := Load(mapLookup(bad), root); err == nil {
			t.Errorf("%s: Load() error = nil", name)
		}
	}
}
