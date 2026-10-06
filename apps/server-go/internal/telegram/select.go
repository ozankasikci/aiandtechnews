package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// The channel does not carry every article: a Selector decides which ones
// are worth a push to subscribers' phones.

// Selector decides whether an article is posted to the channel. notes are
// the owner's standing instructions (may be empty).
type Selector interface {
	Select(ctx context.Context, post Post, notes string) (bool, string, error)
}

// TextGenerator is the model behind TextSelector (Codex or Gemini).
type TextGenerator interface {
	GenerateJSON(ctx context.Context, prompt string) (string, error)
}

// TextSelector asks a language model.
type TextSelector struct{ Text TextGenerator }

// SelectBrief is what belongs on the channel.
const SelectBrief = `You run the Telegram channel of an AI and technology news site. The site publishes many stories a day, but the channel only carries high tech and AI news: the stories a busy engineer, founder or researcher would want pushed to their phone.

Post it when the story is centrally about technology itself and is significant:
- a new AI model, product, agent, tool or major capability, or a major change to one (pricing, availability, shutdown);
- an AI research result or technical breakthrough with real impact;
- chips, GPUs, compute, data centers, robotics, self-driving, space and other deep technology: launches, records, major deals and investments;
- a major security incident or vulnerability involving AI systems or widely used technology;
- a decisive industry move by a major AI or tech company: large funding rounds, acquisitions, landmark partnerships.

Do not post it when it is:
- politics, regulation, lawsuits, court or government process, unless it directly changes what a major AI product or company can do;
- local or procedural news, public records disputes, surveillance policy, labour, education or social-impact stories;
- opinion, commentary, surveys, profiles, culture and entertainment;
- minor or incremental updates, small funding rounds, routine partnerships, customer stories;
- crypto, consumer gadget reviews and deals.

When unsure, do not post it.`

// SelectPrompt builds the question for one article.
func SelectPrompt(post Post, notes string) string {
	article, _ := json.MarshalIndent(map[string]any{
		"headline": post.Title, "summary": post.Excerpt, "why_it_matters": post.WhyItMatters,
		"key_facts": post.TLDR, "category": post.Category,
	}, "", "  ")
	prompt := SelectBrief
	if notes = strings.TrimSpace(notes); notes != "" {
		prompt += "\n\nStanding instructions from the site's owner (follow them over your own judgement):\n" + notes
	}
	return prompt + "\n\nArticle (JSON):\n" + string(article) +
		"\n\nReturn only JSON with this exact shape: {\"post\":true,\"reason\":\"one short sentence\"}"
}

var selectObject = regexp.MustCompile(`(?s)\{.*\}`)

// Select asks the model about one article.
func (s TextSelector) Select(ctx context.Context, post Post, notes string) (bool, string, error) {
	raw, err := s.Text.GenerateJSON(ctx, SelectPrompt(post, notes))
	if err != nil {
		return false, "", err
	}
	var answer struct {
		Post   *bool  `json:"post"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(selectObject.FindString(raw)), &answer); err != nil || answer.Post == nil {
		return false, "", fmt.Errorf("channel selection answer is not usable JSON: %.200s", raw)
	}
	return *answer.Post, strings.TrimSpace(answer.Reason), nil
}
