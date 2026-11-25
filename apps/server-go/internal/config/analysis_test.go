package config

import (
	"strings"
	"testing"
	"time"
)

func TestAnalysisConfig(t *testing.T) {
	env := publisherEnv()
	cfg, err := Load(mapLookup(env), t.TempDir())
	if err != nil || cfg.AnalysisEnabled || cfg.AnalysisInterval != 10*time.Minute {
		t.Fatalf("defaults=%+v err=%v", cfg, err)
	}
	if !strings.Contains(cfg.String(), "AnalysisEnabled:false AnalysisInterval:10m0s") {
		t.Fatal(cfg.String())
	}
	env["ANALYSIS_ENABLED"] = "true"
	env["ANALYSIS_INTERVAL"] = "2m"
	env["PUBLISHER_ENABLED"] = "0"
	cfg, err = Load(mapLookup(env), t.TempDir())
	if err != nil || !cfg.AnalysisEnabled || cfg.AnalysisInterval != 2*time.Minute {
		t.Fatalf("settings=%+v err=%v", cfg, err)
	}
	for name, change := range map[string]func(map[string]string){
		"switch":         func(e map[string]string) { e["ANALYSIS_ENABLED"] = "maybe" },
		"duration":       func(e map[string]string) { e["ANALYSIS_INTERVAL"] = "soon" },
		"short interval": func(e map[string]string) { e["ANALYSIS_INTERVAL"] = "30s" },
		"missing key":    func(e map[string]string) { e["GEMINI_API_KEY"] = "" },
	} {
		t.Run(name, func(t *testing.T) {
			e := publisherEnv()
			e["ANALYSIS_ENABLED"] = "1"
			change(e)
			if _, err := Load(mapLookup(e), t.TempDir()); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
