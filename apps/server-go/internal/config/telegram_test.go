package config

import (
	"strings"
	"testing"
	"time"
)

func telegramEnv() map[string]string {
	return map[string]string{
		"JWT_SECRET":         "x",
		"TELEGRAM_ENABLED":   "1",
		"TELEGRAM_BOT_TOKEN": "123456:synthetic-telegram-token",
		"TELEGRAM_CHANNEL":   "@codingatnight",
	}
}

func TestLoadTelegramIsOffByDefault(t *testing.T) {
	cfg, err := Load(mapLookup(map[string]string{"JWT_SECRET": "x"}), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TelegramEnabled || cfg.TelegramInterval != time.Minute || cfg.TelegramQuietHours != "00:00-08:00" || cfg.TelegramMinGap != 5*time.Minute {
		t.Fatalf("telegram = %t %s %q %s", cfg.TelegramEnabled, cfg.TelegramInterval, cfg.TelegramQuietHours, cfg.TelegramMinGap)
	}
}

func TestLoadTelegramSettingsAndRedaction(t *testing.T) {
	root := t.TempDir()
	env := telegramEnv()
	env["TELEGRAM_INTERVAL"] = "30s"
	env["TELEGRAM_QUIET_HOURS"] = "23:00-07:30"
	env["TELEGRAM_MIN_GAP"] = "10m"
	cfg, err := Load(mapLookup(env), root)
	if err != nil || !cfg.TelegramEnabled || cfg.TelegramChannel != "@codingatnight" || cfg.TelegramInterval != 30*time.Second ||
		cfg.TelegramQuietHours != "23:00-07:30" || cfg.TelegramMinGap != 10*time.Minute {
		t.Fatalf("cfg = %+v err = %v", cfg, err)
	}
	text := cfg.String()
	if strings.Contains(text, "synthetic-telegram-token") || !strings.Contains(text, "TelegramBotToken:[REDACTED]") ||
		!strings.Contains(text, `TelegramEnabled:true`) || !strings.Contains(text, `TelegramChannel:"@codingatnight"`) {
		t.Fatalf("String() = %s", text)
	}

	env["TELEGRAM_QUIET_HOURS"] = "off"
	if cfg, err := Load(mapLookup(env), root); err != nil || cfg.TelegramQuietHours != "off" {
		t.Fatalf("quiet hours off: %q, %v", cfg.TelegramQuietHours, err)
	}
	// Disabled, nothing else is required.
	if _, err := Load(mapLookup(map[string]string{"JWT_SECRET": "x", "TELEGRAM_ENABLED": "0"}), root); err != nil {
		t.Fatal(err)
	}
}

func TestLoadTelegramRejectsBadSettings(t *testing.T) {
	root := t.TempDir()
	for name, change := range map[string]func(map[string]string){
		"without a token":    func(env map[string]string) { env["TELEGRAM_BOT_TOKEN"] = " " },
		"without a channel":  func(env map[string]string) { env["TELEGRAM_CHANNEL"] = "" },
		"interval under 30s": func(env map[string]string) { env["TELEGRAM_INTERVAL"] = "10s" },
		"bad interval":       func(env map[string]string) { env["TELEGRAM_INTERVAL"] = "often" },
		"bad gap":            func(env map[string]string) { env["TELEGRAM_MIN_GAP"] = "later" },
		"negative gap":       func(env map[string]string) { env["TELEGRAM_MIN_GAP"] = "-1m" },
		"bad quiet hours":    func(env map[string]string) { env["TELEGRAM_QUIET_HOURS"] = "night" },
		"empty window":       func(env map[string]string) { env["TELEGRAM_QUIET_HOURS"] = "08:00-08:00" },
		"bad switch":         func(env map[string]string) { env["TELEGRAM_ENABLED"] = "maybe" },
	} {
		bad := telegramEnv()
		change(bad)
		if _, err := Load(mapLookup(bad), root); err == nil {
			t.Errorf("%s: Load() error = nil", name)
		}
	}
}
