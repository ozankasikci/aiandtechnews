package autopick

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
)

// DefaultModel, DefaultEffort and DefaultTimeout configure the Codex editor.
const (
	DefaultModel   = "gpt-6-sol"
	DefaultEffort  = "medium"
	DefaultTimeout = 15 * time.Minute
)

// CodexEditor implements Editor with codex exec (ChatGPT plan login).
type CodexEditor struct {
	Runner *illustration.CodexRunner
	Model  string
	Effort string
}

// NewCodexEditor returns an editor using the given runner.
func NewCodexEditor(runner *illustration.CodexRunner, model, effort string) *CodexEditor {
	if model == "" {
		model = DefaultModel
	}
	if effort == "" {
		effort = DefaultEffort
	}
	return &CodexEditor{Runner: runner, Model: model, Effort: effort}
}

// Brief is the editorial brief the editor applies.
const Brief = `You are the editor in chief of an AI and technology news site. Every few hours you go through all candidate stories the feeds brought in and decide, for each one, whether the site publishes it (the site rewrites it into its own article) or drops it.

Publish a story when it is real, fresh news that readers of an AI and tech site would want: launches, releases, research results with impact, funding and deals, policy and court decisions, security incidents, notable company moves, and stories with a clear "why it matters".

Drop a story when:
- it covers the same event as a story the site already published (see "Published lately") or as a better candidate in this batch; of several candidates about one event, publish only the one with the most substance and drop the others;
- it is minor or incremental (small feature tweaks, routine updates, rumors without substance, reposted press releases);
- it is not really news: opinion, explainers, how-tos, listicles, reviews, deals, podcasts, event announcements, sponsored posts;
- AI and technology are only a side note;
- it is stale (the event is several days old and has been widely covered).
- it is local or procedural: a city, county or school board dispute, a public records request or its fees, a local lawsuit or permit fight, an administrative quarrel. Publish these only when the outcome matters nationally or to the AI industry as a whole.
- it would bore a general tech reader: nothing new is launched, decided, discovered, broken or changed, and no well-known company, product or person is at the centre of it.

Keep the mix varied: do not publish many stories about the same company or topic in one batch unless each is genuinely separate news.

Quality comes before volume. The site would rather publish 10 strong stories a day than 30 mixed ones, and nothing at all rather than filler.

Score every candidate's significance from 1 to 10:
- 9-10: major news most tech readers will hear about: a flagship model or product from a leading AI or tech company, a multi-billion deal, a serious incident at a major company, a landmark ruling or law.
- 7-8: clearly significant: a notable launch, model or capability from a known company or lab, a research result with real impact, a funding round or acquisition of roughly $100 million or more, a real security incident, a decisive move by a major company.
- 5-6: real but minor: a small startup's launch or funding, a niche product update, a routine partnership, a survey or poll, a story where the company or product is little known.
- 1-4: not worth a reader's time: human interest, local items, opinion, promotion, thin or speculative stories.
Publish only candidates that score 7 or more and are not dropped by a rule above. "Published lately" and "Rejected lately" show what the site did before; use them to avoid repeats, not as a target to match.

Give every candidate a decision and its score. For published ones set priority 1, 2, 3... in the order they should go out (most important first); use 0 for dropped ones. Keep each reason to one short sentence.`

type promptInput struct {
	Candidates []Candidate `json:"candidates"`
	Published  []Story     `json:"published_lately"`
	Rejected   []Story     `json:"rejected_lately"`
}

// Prompt builds the full prompt for a batch.
func Prompt(candidates []Candidate, history History) (string, error) {
	data, err := json.MarshalIndent(promptInput{Candidates: candidates, Published: history.Published, Rejected: history.Rejected}, "", "  ")
	if err != nil {
		return "", err
	}
	brief := Brief
	if history.Notes != "" {
		brief += "\n\nStanding instructions from the site's owner. They come from stories the owner did not want published; follow them over your own judgement and apply them to similar stories, not only to the exact example:\n" + history.Notes
	}
	return brief + "\n\nInput (JSON):\n" + string(data) +
		"\n\nAnswer with one decision per candidate id. Do not run any commands.", nil
}

// Decide sends the batch to Codex.
func (e *CodexEditor) Decide(ctx context.Context, candidates []Candidate, history History) ([]Decision, error) {
	dir, err := os.MkdirTemp("", "autopick-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	prompt, err := Prompt(candidates, history)
	if err != nil {
		return nil, err
	}
	schemaPath, answerPath := filepath.Join(dir, "schema.json"), filepath.Join(dir, "answer.json")
	if err := os.WriteFile(schemaPath, []byte(schema), 0o600); err != nil {
		return nil, err
	}
	output, err := e.Runner.Run(ctx, illustration.CodexRun{
		Dir:     dir,
		Sandbox: "read-only",
		Extra:   []string{"-m", e.Model, "-c", "model_reasoning_effort=" + e.Effort, "--output-schema", schemaPath, "-o", answerPath},
		Prompt:  prompt,
	})
	if err != nil {
		return nil, err
	}
	raw, readErr := os.ReadFile(answerPath)
	if readErr != nil || strings.TrimSpace(string(raw)) == "" {
		raw = []byte(output)
	}
	return ParseAnswer(raw)
}

// ParseAnswer reads the editor's JSON answer.
func ParseAnswer(raw []byte) ([]Decision, error) {
	text := strings.TrimSpace(string(raw))
	if start, end := strings.Index(text, "{"), strings.LastIndex(text, "}"); start >= 0 && end > start {
		text = text[start : end+1]
	}
	var parsed struct {
		Decisions []Decision `json:"decisions"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil, fmt.Errorf("autopick answer is not JSON: %w", err)
	}
	for i := range parsed.Decisions {
		parsed.Decisions[i].Reason = clip(strings.TrimSpace(parsed.Decisions[i].Reason), 300)
	}
	return parsed.Decisions, nil
}

const schema = `{"type":"object","additionalProperties":false,"required":["decisions"],"properties":{"decisions":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["id","publish","score","priority","reason"],"properties":{"id":{"type":"integer"},"publish":{"type":"boolean"},"score":{"type":"integer"},"priority":{"type":"integer"},"reason":{"type":"string"}}}}}}`
