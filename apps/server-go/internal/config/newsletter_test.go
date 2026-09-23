package config

import (
	"fmt"
	"strings"
	"testing"
)

func TestLoadReadsNewsletterSettingsLikeNode(t *testing.T) {
	cfg, err := Load(mapLookup(map[string]string{
		"NEWSLETTER_SITE_URL":     "https://aiandtech.news",
		"NEWSLETTER_TOKEN_SECRET": "  synthetic-token-secret-with-32-characters  ",
		"RESEND_API_KEY":          " re_synthetic ",
		"NEWSLETTER_FROM":         "AI & Tech News <news@example.invalid>",
		"NEWSLETTER_REPLY_TO":     "reply@example.invalid",
		"NEWSLETTER_CRON_SECRET":  "cron-secret",
		"CRON_SECRET":             "vercel-secret",
	}), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Values are kept raw: Node trims them where it uses them, and so does Go.
	if cfg.NewsletterSiteURL != "https://aiandtech.news" ||
		cfg.NewsletterTokenSecret != "  synthetic-token-secret-with-32-characters  " ||
		cfg.ResendAPIKey != " re_synthetic " ||
		cfg.NewsletterFrom != "AI & Tech News <news@example.invalid>" ||
		cfg.NewsletterReplyTo != "reply@example.invalid" ||
		cfg.NewsletterCronSecret != "cron-secret" {
		t.Fatalf("newsletter settings were not loaded: %v", cfg)
	}
}

func TestLoadNewsletterSettingsDefaultToUnset(t *testing.T) {
	cfg, err := Load(func(string) string { return "" }, t.TempDir())
	if err != nil {
		t.Fatalf("an unconfigured newsletter must not block startup: %v", err)
	}
	if cfg.NewsletterSiteURL != "" || cfg.NewsletterTokenSecret != "" || cfg.ResendAPIKey != "" ||
		cfg.NewsletterFrom != "" || cfg.NewsletterReplyTo != "" || cfg.NewsletterCronSecret != "" {
		t.Fatalf("newsletter defaults = %v", cfg)
	}
}

// Node: process.env.NEWSLETTER_CRON_SECRET || process.env.CRON_SECRET || "".
// An empty NEWSLETTER_CRON_SECRET falls through; a blank one does not.
func TestNewsletterCronSecretFallsBackToCronSecretLikeNode(t *testing.T) {
	tests := []struct {
		name       string
		newsletter string
		cron       string
		want       string
	}{
		{name: "newsletter secret wins", newsletter: "a", cron: "b", want: "a"},
		{name: "empty falls back to CRON_SECRET", newsletter: "", cron: "b", want: "b"},
		{name: "blank does not fall back", newsletter: "  ", cron: "b", want: "  "},
		{name: "neither", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(mapLookup(map[string]string{"NEWSLETTER_CRON_SECRET": tt.newsletter, "CRON_SECRET": tt.cron}), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if cfg.NewsletterCronSecret != tt.want {
				t.Fatalf("cron secret = %q, want %q", cfg.NewsletterCronSecret, tt.want)
			}
		})
	}
}

func TestConfigFormattingRedactsNewsletterSecrets(t *testing.T) {
	secrets := []string{"resend-key-that-must-never-be-formatted", "token-secret-that-must-never-be-formatted", "cron-secret-that-must-never-be-formatted"}
	cfg := Config{Mode: ModeDevelopment, Address: DefaultAddress, DatabasePath: "/tmp/synthetic.db",
		ResendAPIKey: secrets[0], NewsletterTokenSecret: secrets[1], NewsletterCronSecret: secrets[2],
		NewsletterSiteURL: "https://site.example.invalid", NewsletterFrom: "from@example.invalid", NewsletterReplyTo: "reply@example.invalid"}
	for _, formatted := range []string{fmt.Sprint(cfg), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%#v", cfg)} {
		for _, secret := range secrets {
			if strings.Contains(formatted, secret) {
				t.Fatalf("formatted config leaked a newsletter secret: %s", formatted)
			}
		}
		for _, visible := range []string{"https://site.example.invalid", "from@example.invalid", "reply@example.invalid"} {
			if !strings.Contains(formatted, visible) {
				t.Errorf("formatted config hides non-secret %q: %s", visible, formatted)
			}
		}
	}
}
