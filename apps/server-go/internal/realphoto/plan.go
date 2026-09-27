package realphoto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// maxPlanText caps how much article text the planner reads.
const maxPlanText = 8000

// Plan is the planner's reading of the article: is there one concrete,
// photographable subject, and how to look for it.
type Plan struct {
	Photographable bool     `json:"photographable"`
	Subject        string   `json:"subject"`
	Kind           string   `json:"kind,omitempty"`
	Queries        []string `json:"queries"`
	Maker          string   `json:"maker"`
	Reason         string   `json:"reason,omitempty"`
}

const planPrompt = `You pick the in-article photo for a technology news article. Decide whether the story has ONE concrete, photographable subject that a real photo could show.

Photographable: a specific physical product or device (including brand-new hardware that has just been announced), a robot, a vehicle, a chip or server you can hold, a building or data centre, a place, a physical event (a launch, a keynote, a trade show), or a person who is the story's central actor (the one acting or announcing, not someone merely quoted).
Not photographable: software, apps, AI models, websites, features, policies, laws, lawsuits, studies, reports, funding rounds, earnings, prices, market trends, abstract topics, or stories about many different things.

Return only JSON with exactly this shape:
{"photographable":true,"subject":"...","kind":"product|device|robot|vehicle|building|place|event|person|none","queries":["...","..."],"maker":"...","reason":"..."}

subject: the exact subject, as specific as possible (maker, product name and model, e.g. "WiCi One desk robot"); empty when not photographable.
queries: two short Wikimedia Commons search queries for a photo of that exact subject, the most specific first (e.g. "WiCi One robot", "WiCi robot"). Plain words, no quotes or operators.
maker: the company or organisation that makes or owns the subject (for a person, their organisation); empty if unknown.
reason: one short sentence.

Headline: %s

Article:
%s`

var (
	leadingFence  = regexp.MustCompile("^```(?:json)?\\s*")
	trailingFence = regexp.MustCompile("\\s*```$")
	jsonObject    = regexp.MustCompile(`(?s)\{.*\}`)
)

// cleanJSON strips code fences and text around the JSON object.
func cleanJSON(raw string) string {
	cleaned := strings.TrimSpace(trailingFence.ReplaceAllString(leadingFence.ReplaceAllString(strings.TrimSpace(raw), ""), ""))
	if match := jsonObject.FindString(cleaned); match != "" {
		return match
	}
	return cleaned
}

func (f *Finder) plan(ctx context.Context, request Request) (Plan, error) {
	text := strings.TrimSpace(request.Text)
	if len(text) > maxPlanText {
		text = text[:maxPlanText]
	}
	raw, err := f.text.GenerateJSON(ctx, fmt.Sprintf(planPrompt, request.Title, text))
	if err != nil {
		return Plan{}, err
	}
	return ParsePlan(raw)
}

// ParsePlan reads the planner's answer. A photographable plan needs a
// subject and at least one query.
func ParsePlan(raw string) (Plan, error) {
	var plan Plan
	if err := json.Unmarshal([]byte(cleanJSON(raw)), &plan); err != nil {
		return Plan{}, fmt.Errorf("unparsable plan: %w", err)
	}
	plan.Subject = oneLine(plan.Subject, 120)
	plan.Maker = oneLine(plan.Maker, 80)
	plan.Reason = oneLine(plan.Reason, 200)
	var queries []string
	for _, query := range plan.Queries {
		if query = oneLine(strings.NewReplacer(`"`, "", "|", " ").Replace(query), 100); query != "" {
			queries = append(queries, query)
		}
		if len(queries) == 2 {
			break
		}
	}
	plan.Queries = queries
	if plan.Photographable && plan.Subject == "" {
		return Plan{}, errors.New("photographable plan without a subject")
	}
	if plan.Photographable && len(plan.Queries) == 0 {
		plan.Queries = []string{plan.Subject}
	}
	return plan, nil
}

// oneLine collapses whitespace and cuts to at most limit bytes on a word
// boundary.
func oneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= limit {
		return text
	}
	cut := text[:limit]
	if space := strings.LastIndex(cut, " "); space > limit/2 {
		cut = cut[:space]
	}
	return strings.TrimRight(cut, " ,;:-") + "…"
}
