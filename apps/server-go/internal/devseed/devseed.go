// Package devseed fills a development database with a login and realistic
// newsroom candidates so the API and the Omni Control app can be exercised
// before the collector exists. It is never wired into the API server.
package devseed

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
)

// Email is the development editor account created by Seed.
const Email = "dev@example.invalid"

type Candidate struct {
	Slug    string
	Source  string
	Feed    string
	Title   string
	Summary string
	// Age is how long before "now" the item was discovered.
	Age time.Duration
}

// Candidates are fixture feed items. The last two are moved to failed and
// published so every section of the app's queue view has content.
var Candidates = []Candidate{
	{"openai-agents-sdk", "The Verge", "https://www.theverge.com/rss/ai-artificial-intelligence/index.xml", "OpenAI ships an agents SDK for long-running coding tasks", "The toolkit lets developers chain tool calls across hours-long sessions with built-in checkpoints.", 5 * time.Minute},
	{"anthropic-eu-datacenter", "TechCrunch", "https://techcrunch.com/category/artificial-intelligence/feed/", "Anthropic signs deal for its first European data center", "The facility in Finland is expected to come online next year and run largely on hydro power.", 18 * time.Minute},
	{"google-gemini-on-device", "Ars Technica", "https://feeds.arstechnica.com/arstechnica/technology-lab", "Google brings a smaller Gemini model fully on-device", "The model runs offline on recent Pixel phones and handles summarization and smart replies.", 41 * time.Minute},
	{"eu-ai-act-guidance", "WIRED", "https://www.wired.com/feed/tag/ai/latest/rss", "EU publishes first compliance guidance under the AI Act", "Providers of general-purpose models get a template for documenting training data sources.", 1 * time.Hour},
	{"nvidia-inference-chip", "The Verge", "https://www.theverge.com/rss/ai-artificial-intelligence/index.xml", "Nvidia details an inference-only chip aimed at cloud providers", "The part trades training throughput for memory bandwidth and lower power per token.", 2 * time.Hour},
	{"meta-open-weights", "TechCrunch", "https://techcrunch.com/category/artificial-intelligence/feed/", "Meta releases new open-weights model with a longer context window", "The release keeps a permissive license but adds usage restrictions for very large deployments.", 3 * time.Hour},
	{"hospital-ai-triage", "MIT Technology Review", "https://www.technologyreview.com/topic/artificial-intelligence/feed", "Hospitals test AI triage that flags sepsis hours earlier", "A multi-site trial reports fewer missed cases, though clinicians question alert fatigue.", 5 * time.Hour},
	{"startup-ai-chip-funding", "TechCrunch", "https://techcrunch.com/category/artificial-intelligence/feed/", "AI chip startup raises $300M to take on GPU incumbents", "The company says its compiler lets existing PyTorch models run without code changes.", 7 * time.Hour},
	{"apple-private-cloud-compute", "Ars Technica", "https://feeds.arstechnica.com/arstechnica/technology-lab", "Apple opens Private Cloud Compute to outside security researchers", "Researchers get access to a virtual environment and a bounty for verified vulnerabilities.", 9 * time.Hour},
	{"deepfake-election-rules", "WIRED", "https://www.wired.com/feed/tag/ai/latest/rss", "Election regulators propose labels for AI-generated campaign ads", "The draft rule would require on-screen disclosure for synthetic audio and video.", 12 * time.Hour},
	{"robotics-foundation-model", "MIT Technology Review", "https://www.technologyreview.com/topic/artificial-intelligence/feed", "A robotics foundation model learns new tasks from a single demo", "Lab tests show household tasks transferring across three different robot arms.", 20 * time.Hour},
	{"ai-coding-survey", "The Verge", "https://www.theverge.com/rss/ai-artificial-intelligence/index.xml", "Survey finds most developers now use AI assistants daily", "Respondents report faster prototyping but spend more time reviewing generated code.", 26 * time.Hour},
}

type Summary struct {
	Email    string
	Inserted int
}

// Seed upserts the development editor (resetting its password) and inserts
// any fixture candidates not already present. It is safe to run repeatedly.
func Seed(ctx context.Context, db *sql.DB, now time.Time, password string) (Summary, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return Summary{}, fmt.Errorf("hash password: %w", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO authors (name, email, password_hash, role)
		VALUES ('Dev Editor', ?, ?, 'admin')
		ON CONFLICT(email) DO UPDATE SET password_hash = excluded.password_hash, role = 'admin'`,
		Email, string(hash)); err != nil {
		return Summary{}, fmt.Errorf("upsert dev editor: %w", err)
	}

	if err := seedDefaultSettings(ctx, db); err != nil {
		return Summary{}, err
	}

	store := newsroom.NewSQLiteStore(db)
	summary := Summary{Email: Email}
	for i, fixture := range Candidates {
		image := "https://picsum.photos/seed/" + fixture.Slug + "/240"
		discovered := now.Add(-fixture.Age)
		id, inserted, err := store.Insert(ctx, newsroom.NewCandidate{
			SourceURL:       "https://example.com/dev/" + fixture.Slug,
			SourceName:      fixture.Source,
			FeedURL:         fixture.Feed,
			Title:           fixture.Title,
			FeedSummary:     fixture.Summary,
			SourceImageURL:  &image,
			FeedPublishedAt: &discovered,
		}, discovered)
		if err != nil {
			return Summary{}, err
		}
		if !inserted {
			continue
		}
		summary.Inserted++
		if err := setFixtureStatus(ctx, db, id, len(Candidates)-1-i, now); err != nil {
			return Summary{}, err
		}
	}
	return summary, nil
}

// setFixtureStatus moves the last two fixtures to failed and published.
func setFixtureStatus(ctx context.Context, db *sql.DB, id int64, fromEnd int, now time.Time) error {
	stamp := now.UTC().Format(time.RFC3339)
	var err error
	switch fromEnd {
	case 1:
		_, err = db.ExecContext(ctx, `UPDATE candidates SET status = 'failed', attempts = 3,
			last_error = 'Rewrite failed validation: 912 words exceeds the 800-word limit', updated_at = ? WHERE id = ?`, stamp, id)
	case 0:
		published := now.Add(-time.Hour).UTC().Format(time.RFC3339)
		_, err = db.ExecContext(ctx, `UPDATE candidates SET status = 'published', published_at = ?, updated_at = ? WHERE id = ?`,
			published, published, id)
	}
	if err != nil {
		return fmt.Errorf("set fixture status for candidate %d: %w", id, err)
	}
	return nil
}

// defaultSettings mirrors Node's seedDefaults (apps/server/src/db.ts:184-198)
// in the exact key order Node inserts them.
var defaultSettings = []struct{ key, value string }{
	{"site_name", "TechNews"},
	{"site_description", "AI & Tech News, Daily."},
	{"social_twitter", ""},
	{"social_linkedin", ""},
	{"social_github", ""},
	{"newsletter_enabled", "false"},
	{"newsletter_provider", "none"},
	{"newsletter_webhook_url", ""},
}

// seedDefaultSettings inserts Node's default site settings with INSERT OR
// IGNORE, exactly like Node's seedDefaults: a fresh database gets them, and
// a row an editor already changed (or a prior seed run left behind) is never
// overwritten.
func seedDefaultSettings(ctx context.Context, db *sql.DB) error {
	for _, setting := range defaultSettings {
		if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO settings (key, value) VALUES (?, ?)`,
			setting.key, setting.value); err != nil {
			return fmt.Errorf("seed default setting %s: %w", setting.key, err)
		}
	}
	return nil
}
