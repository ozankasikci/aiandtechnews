// Package relevance decides which feed items become newsroom candidates by
// asking Codex (ChatGPT plan login) to read each item's title, feed summary
// and the start of the article. It replaces the AI keyword rule.
package relevance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/collector"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
)

// DefaultModel and DefaultEffort are the Codex model and reasoning effort.
const (
	DefaultModel   = "gpt-6-sol"
	DefaultEffort  = "medium"
	DefaultTimeout = 8 * time.Minute
)

// CodexJudge implements collector.Judge with codex exec.
type CodexJudge struct {
	Runner *illustration.CodexRunner
	Model  string
	Effort string
}

// New returns a judge using the given Codex runner.
func New(runner *illustration.CodexRunner, model, effort string) *CodexJudge {
	if model == "" {
		model = DefaultModel
	}
	if effort == "" {
		effort = DefaultEffort
	}
	return &CodexJudge{Runner: runner, Model: model, Effort: effort}
}

type promptItem struct {
	ID      int    `json:"id"`
	Source  string `json:"source"`
	Title   string `json:"title"`
	Summary string `json:"summary,omitempty"`
	Article string `json:"article_start,omitempty"`
	Primary bool   `json:"primary_source,omitempty"`
}

type answer struct {
	Items []struct {
		ID        int    `json:"id"`
		Candidate bool   `json:"candidate"`
		Reason    string `json:"reason"`
	} `json:"items"`
}

// Policy is the editorial brief the judge applies.
const Policy = `You are the news editor of an AI and technology news site. Decide for each feed item below whether it should become a candidate story for the site.

A candidate is a real news story where AI is central or directly affected, for example:
- AI models, products, launches, features and pricing from any company;
- AI companies: funding, deals, partnerships, executives, lawsuits, layoffs;
- AI chips, GPUs, compute, data centers and the energy they use;
- AI policy, regulation, government use of AI, court cases about AI;
- AI safety, security incidents, misuse, deepfakes, agents behaving badly;
- robotics, humanoids, self-driving cars and autonomous systems;
- notable AI research results and their real-world impact on jobs, health, education, science or society.

Not a candidate:
- general tech, gadget, gaming, crypto, business or politics news where AI is absent or only a passing mention;
- product reviews, buying guides, deals, discounts, how-tos, listicles and roundups;
- opinion or commentary without a news event, sponsored posts, event announcements, podcasts and videos.

Judge by what the article is actually about (use the article text when given), not by keywords. When unsure, prefer candidate.

Items marked "primary_source": true come straight from a company's own blog, newsroom, changelog or release feed, and the site would report them itself. Be stricter with these, and when unsure, prefer not a candidate. A primary item is a candidate only when it announces something new and concrete that an AI and tech news reader would want to know: a new model, product or major feature, a price or policy change, a deal, acquisition or funding, a significant research result, a security or safety disclosure, or a major software release with notable new capabilities. Not candidates: customer stories and case studies, marketing and thought leadership, tutorials and how-tos, event recaps and invitations, hiring and culture posts, minor or patch releases, bug-fix changelogs, and anything where AI is not central.`

// Judge sends one batch to Codex and maps the answers back to item keys.
func (j *CodexJudge) Judge(ctx context.Context, items []collector.JudgeItem) (map[string]collector.Verdict, error) {
	if len(items) == 0 {
		return map[string]collector.Verdict{}, nil
	}
	dir, err := os.MkdirTemp("", "relevance-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	list := make([]promptItem, len(items))
	for i, item := range items {
		list[i] = promptItem{ID: i + 1, Source: item.Source, Title: item.Title, Summary: clip(item.Summary, 600), Article: clip(item.Text, 2000), Primary: item.Primary}
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return nil, err
	}
	schemaPath, answerPath := filepath.Join(dir, "schema.json"), filepath.Join(dir, "answer.json")
	if err := os.WriteFile(schemaPath, []byte(schema), 0o600); err != nil {
		return nil, err
	}
	prompt := Policy + "\n\nFeed items (JSON):\n" + string(data) +
		"\n\nAnswer with one entry per item id: candidate true or false and a short reason. Do not run any commands."
	output, err := j.Runner.Run(ctx, illustration.CodexRun{
		Dir:     dir,
		Sandbox: "read-only",
		Extra:   []string{"-m", j.Model, "-c", "model_reasoning_effort=" + j.Effort, "--output-schema", schemaPath, "-o", answerPath},
		Prompt:  prompt,
	})
	if err != nil {
		return nil, err
	}
	raw, readErr := os.ReadFile(answerPath)
	if readErr != nil || strings.TrimSpace(string(raw)) == "" {
		raw = []byte(output)
	}
	return parseAnswer(raw, items)
}

func parseAnswer(raw []byte, items []collector.JudgeItem) (map[string]collector.Verdict, error) {
	text := strings.TrimSpace(string(raw))
	if start, end := strings.Index(text, "{"), strings.LastIndex(text, "}"); start >= 0 && end > start {
		text = text[start : end+1]
	}
	var parsed answer
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil, fmt.Errorf("relevance answer is not JSON: %w", err)
	}
	verdicts := map[string]collector.Verdict{}
	for _, entry := range parsed.Items {
		if entry.ID < 1 || entry.ID > len(items) {
			continue
		}
		verdicts[items[entry.ID-1].Key] = collector.Verdict{Candidate: entry.Candidate, Reason: clip(strings.TrimSpace(entry.Reason), 300)}
	}
	return verdicts, nil
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n]
}

const schema = `{"type":"object","additionalProperties":false,"required":["items"],"properties":{"items":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["id","candidate","reason"],"properties":{"id":{"type":"integer"},"candidate":{"type":"boolean"},"reason":{"type":"string"}}}}}}`
