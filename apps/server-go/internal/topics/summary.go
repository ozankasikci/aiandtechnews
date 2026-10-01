package topics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/siterevalidate"
)

const (
	// SummaryArticles is how many of a topic's latest articles feed its summary.
	SummaryArticles = 20
	// maxFacts is how many key facts a hub keeps.
	maxFacts = 6
	// Accepted summary length in words. The prompt asks for 150 to 250; the
	// check is looser so a faithful shorter summary is not thrown away.
	minSummaryWords = 100
	maxSummaryWords = 300
	maxFactChars    = 200
	// maxPerTick bounds the work of one pass of the loop.
	maxPerTick = 20
)

// errRejected marks a draft that failed its checks (as opposed to a model or
// storage error), so the topic is not retried until new articles arrive.
var errRejected = errors.New("summary rejected")

// Summarizer keeps each live topic's summary and key facts fresh.
type Summarizer struct {
	Store *Store
	// Text writes the summary and checks it (Gemini Flash).
	Text   TextGenerator
	Site   siterevalidate.Notifier
	Logger *slog.Logger
}

// Loop refreshes due topics every interval until ctx is done.
func (s *Summarizer) Loop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if n, err := s.Refresh(ctx); err != nil && ctx.Err() == nil {
			s.Logger.ErrorContext(ctx, "topic summary pass failed", "error", err)
		} else if n > 0 {
			s.Logger.InfoContext(ctx, "topic summaries refreshed", "topics", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Refresh writes a summary for each due topic and reports how many changed.
func (s *Summarizer) Refresh(ctx context.Context) (int, error) {
	due, err := s.Store.Due(ctx, maxPerTick)
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, topic := range due {
		if ctx.Err() != nil {
			break
		}
		updated, err := s.RefreshTopic(ctx, topic)
		if err != nil {
			if ctx.Err() == nil {
				s.Logger.WarnContext(ctx, "topic summary kept; refresh failed", "topic", topic.Slug, "error", err)
				if errors.Is(err, errRejected) {
					_ = s.Store.MarkChecked(ctx, topic.ID)
				}
			}
			continue
		}
		if updated {
			changed++
			if s.Site != nil {
				siterevalidate.NotifyWithTopics(s.Site, nil, []string{topic.Slug})
			}
		}
	}
	return changed, nil
}

// RefreshTopic drafts, verifies and stores one topic's summary. It reports
// whether the stored summary changed; on any failure the previous summary
// and facts stay.
func (s *Summarizer) RefreshTopic(ctx context.Context, topic Topic) (bool, error) {
	articles, err := s.Store.Latest(ctx, topic.ID, SummaryArticles)
	if err != nil {
		return false, err
	}
	raw, err := s.Text.GenerateJSON(ctx, SummaryPrompt(topic, articles))
	if err != nil {
		return false, err
	}
	summary, facts, err := ParseSummary(raw)
	if err != nil {
		return false, fmt.Errorf("%w: %w", errRejected, err)
	}
	raw, err = s.Text.GenerateJSON(ctx, VerifyPrompt(topic, articles, summary, facts))
	if err != nil {
		return false, err
	}
	summary, facts, err = ApplyVerdict(raw, summary, facts)
	if err != nil {
		return false, fmt.Errorf("%w: %w", errRejected, err)
	}
	if err := s.Store.SaveSummary(ctx, topic.ID, summary, facts); err != nil {
		return false, err
	}
	return true, nil
}

func sourceBlock(articles []SourceArticle) string {
	var b strings.Builder
	for i, a := range articles {
		fmt.Fprintf(&b, "[%d] %s (%s)\n", i+1, a.Title, a.PublishedAt)
		for _, point := range a.TLDR {
			b.WriteString("  - " + point + "\n")
		}
		if a.WhyItMatters != "" {
			b.WriteString("  Why it matters: " + a.WhyItMatters + "\n")
		}
	}
	return b.String()
}

// SummaryPrompt asks for a hub summary and key facts from the topic's
// latest articles only.
func SummaryPrompt(topic Topic, articles []SourceArticle) string {
	return `You write the hub page for the topic "` + topic.Name + `" (` + topic.Kind + `) on a tech news site. Write only from the articles below, newest first. Never add anything you know from elsewhere.

Reply with JSON only: {"summary":"...","facts":["..."]}

summary: 150 to 250 words of plain text (no markdown, no lists, no headings). Say what it is, where things stand now, and the latest key developments. Use the present tense for how things stand. Be factual: no hype, no predictions, no opinions. Do not use em dashes or en dashes. Do not mention the articles or this site.
facts: 3 to 6 short key facts (one plain sentence each, for example a product, a date, a number, a latest model). Include only facts stated in the articles; fewer is better than a guess.

ARTICLES
` + sourceBlock(articles)
}

// VerifyPrompt asks the model to check each summary sentence and fact
// against the same articles.
func VerifyPrompt(topic Topic, articles []SourceArticle, summary string, facts []string) string {
	var b strings.Builder
	b.WriteString(`You are a strict fact checker. Below are source articles, then a draft summary split into numbered sentences and a numbered list of facts about "` + topic.Name + `". A sentence or fact is supported only if the articles state it or it follows directly from what they state. Anything extra, invented, or more specific than the articles is not supported.

Reply with JSON only: {"summary":[{"n":1,"supported":true}],"facts":[{"n":1,"supported":true}]} with one entry for every sentence and every fact.

ARTICLES
`)
	b.WriteString(sourceBlock(articles))
	b.WriteString("\nSUMMARY SENTENCES\n")
	for i, sentence := range SplitSentences(summary) {
		fmt.Fprintf(&b, "%d. %s\n", i+1, sentence)
	}
	b.WriteString("\nFACTS\n")
	for i, fact := range facts {
		fmt.Fprintf(&b, "%d. %s\n", i+1, fact)
	}
	return b.String()
}

var dashes = regexp.MustCompile(`\s*[\x{2014}\x{2013}]\s*`)

// clean makes model text plain: one line, no em or en dashes, no markdown.
func clean(text string) string {
	text = dashes.ReplaceAllString(text, ", ")
	text = strings.NewReplacer("**", "", "__", "", "`", "").Replace(text)
	return strings.Join(strings.Fields(text), " ")
}

// ParseSummary reads and checks the drafted summary: plain text of a sane
// length, and facts that are short sentences (at most six).
func ParseSummary(raw string) (string, []string, error) {
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return "", nil, errors.New("summary response is not JSON")
	}
	var parsed struct {
		Summary string   `json:"summary"`
		Facts   []string `json:"facts"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err != nil {
		return "", nil, fmt.Errorf("summary response: %w", err)
	}
	summary := clean(parsed.Summary)
	if words := len(strings.Fields(summary)); words < minSummaryWords || words > maxSummaryWords {
		return "", nil, fmt.Errorf("summary has %d words, want %d to %d", words, minSummaryWords, maxSummaryWords)
	}
	if strings.ContainsAny(summary, "#*") {
		return "", nil, errors.New("summary is not plain text")
	}
	var facts []string
	for _, fact := range parsed.Facts {
		if fact = clean(fact); fact != "" && len(fact) <= maxFactChars && len(facts) < maxFacts {
			facts = append(facts, fact)
		}
	}
	return summary, facts, nil
}

// ApplyVerdict applies the fact checker's answer: unsupported facts are
// dropped; if any summary sentence is unsupported (or unanswered) the whole
// summary fails and the caller keeps the previous one.
func ApplyVerdict(raw, summary string, facts []string) (string, []string, error) {
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return "", nil, errors.New("verification response is not JSON")
	}
	type verdict struct {
		N         int  `json:"n"`
		Supported bool `json:"supported"`
	}
	var parsed struct {
		Summary []verdict `json:"summary"`
		Facts   []verdict `json:"facts"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err != nil {
		return "", nil, fmt.Errorf("verification response: %w", err)
	}
	supported := func(verdicts []verdict) map[int]bool {
		ok := map[int]bool{}
		for _, v := range verdicts {
			ok[v.N] = v.Supported
		}
		return ok
	}
	sentences := SplitSentences(summary)
	ok := supported(parsed.Summary)
	for i, sentence := range sentences {
		if !ok[i+1] {
			return "", nil, fmt.Errorf("summary sentence %d is not supported by the articles: %q", i+1, sentence)
		}
	}
	ok = supported(parsed.Facts)
	var kept []string
	for i, fact := range facts {
		if ok[i+1] {
			kept = append(kept, fact)
		}
	}
	return summary, kept, nil
}

// SplitSentences splits plain text after ".", "!" or "?" followed by a space
// and a character that is not lowercase.
func SplitSentences(text string) []string {
	runes := []rune(strings.TrimSpace(text))
	var out []string
	start := 0
	for i := 0; i < len(runes); i++ {
		if (runes[i] == '.' || runes[i] == '!' || runes[i] == '?') && i+2 < len(runes) && runes[i+1] == ' ' &&
			!(runes[i+2] >= 'a' && runes[i+2] <= 'z') {
			out = append(out, strings.TrimSpace(string(runes[start:i+1])))
			start = i + 2
		}
	}
	if rest := strings.TrimSpace(string(runes[start:])); rest != "" {
		out = append(out, rest)
	}
	return out
}
