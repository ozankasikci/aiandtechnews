package config

import (
	"testing"
	"time"
)

func TestLoadTopicsAreOffByDefault(t *testing.T) {
	cfg, err := Load(mapLookup(publisherEnv()), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TopicsEnabled || cfg.TopicsInterval != time.Hour {
		t.Fatalf("topics = %t every %s", cfg.TopicsEnabled, cfg.TopicsInterval)
	}
}

func TestLoadTopicsSettings(t *testing.T) {
	root := t.TempDir()
	env := publisherEnv()
	env["TOPICS_ENABLED"] = "true"
	env["TOPICS_INTERVAL"] = "30m"
	cfg, err := Load(mapLookup(env), root)
	if err != nil || !cfg.TopicsEnabled || cfg.TopicsInterval != 30*time.Minute {
		t.Fatalf("cfg = %+v err = %v", cfg, err)
	}
	for name, change := range map[string]func(map[string]string){
		"without the publisher": func(env map[string]string) { env["PUBLISHER_ENABLED"] = "0" },
		"interval under 1m":     func(env map[string]string) { env["TOPICS_INTERVAL"] = "30s" },
		"bad interval":          func(env map[string]string) { env["TOPICS_INTERVAL"] = "soon" },
		"bad switch":            func(env map[string]string) { env["TOPICS_ENABLED"] = "maybe" },
	} {
		bad := publisherEnv()
		bad["TOPICS_ENABLED"] = "1"
		change(bad)
		if _, err := Load(mapLookup(bad), root); err == nil {
			t.Errorf("%s: Load() error = nil", name)
		}
	}
}
