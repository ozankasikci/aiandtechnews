package topics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
)

const (
	// MaxEntities is how many topics one article gets.
	MaxEntities = 4
	maxAliases  = 6
	maxNameLen  = 80
	bodyChars   = 1800
)

// TextGenerator is the Gemini text call (gemini.Client, publisher.TextGenerator).
type TextGenerator interface {
	GenerateJSON(ctx context.Context, prompt string) (string, error)
}

// Entity is something an article is substantially about.
type Entity struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	Aliases []string `json:"aliases"`
}

// ArticleInput is what extraction reads about an article. It never holds
// the original source.
type ArticleInput struct {
	ID           int64
	Slug         string
	Title        string
	Excerpt      string
	Body         string // plain text
	TLDR         []string
	WhyItMatters string
}

// Extractor asks the model which entities an article is about.
type Extractor struct{ Text TextGenerator }

func (e Extractor) Extract(ctx context.Context, a ArticleInput) ([]Entity, error) {
	raw, err := e.Text.GenerateJSON(ctx, ExtractionPrompt(a))
	if err != nil {
		return nil, err
	}
	return ParseEntities(raw)
}

// ExtractionPrompt asks for 1 to 4 entities the article is substantially about.
func ExtractionPrompt(a ArticleInput) string {
	var b strings.Builder
	b.WriteString(`You tag news articles with topics for a tech news site. Name the 1 to 4 entities this article is substantially about: the subject of the headline, not companies, products or people that are only mentioned in passing.

Rules:
- kind is one of: company, product, person, theme.
- name is the canonical short name people use: "Nvidia", "OpenAI", "GPT-5", "Sam Altman", "Humanoid robots". No legal suffixes (Inc, Corp, Ltd).
- A theme is a broad, recurring subject area ("AI agents", "Humanoid robots", "Open-weight models"), never a one-off event or a headline.
- aliases are other names and spellings for the same entity (0 to 6), for example the legal name or a common abbreviation. Do not invent aliases.
- Use only what the article says. If nothing qualifies, return an empty list.

Reply with JSON only: {"entities":[{"name":"...","kind":"company","aliases":["..."]}]}

ARTICLE
Title: `)
	b.WriteString(a.Title)
	b.WriteString("\nSummary: " + a.Excerpt)
	for _, point := range a.TLDR {
		b.WriteString("\n- " + point)
	}
	if a.WhyItMatters != "" {
		b.WriteString("\nWhy it matters: " + a.WhyItMatters)
	}
	body := a.Body
	if len(body) > bodyChars {
		body = body[:bodyChars]
	}
	b.WriteString("\nStart of the text: " + body + "\n")
	return b.String()
}

// ParseEntities reads the model's answer and keeps only valid entities:
// a known kind, a usable name, at most MaxEntities, no two with the same key.
func ParseEntities(raw string) ([]Entity, error) {
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return nil, errors.New("topics response is not JSON")
	}
	var parsed struct {
		Entities []Entity `json:"entities"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err != nil {
		return nil, fmt.Errorf("topics response: %w", err)
	}
	var out []Entity
	seen := map[string]bool{}
	for _, entity := range parsed.Entities {
		entity.Name = strings.Join(strings.Fields(entity.Name), " ")
		entity.Kind = strings.ToLower(strings.TrimSpace(entity.Kind))
		key := Key(entity.Name)
		if len(key) < 2 || len(entity.Name) > maxNameLen || !Kinds[entity.Kind] || seen[key] || content.Slugify(entity.Name) == "" {
			continue
		}
		seen[key] = true
		var aliases []string
		for _, alias := range entity.Aliases {
			if alias = strings.Join(strings.Fields(alias), " "); alias != "" && len(alias) <= maxNameLen && len(aliases) < maxAliases {
				aliases = append(aliases, alias)
			}
		}
		entity.Aliases = aliases
		out = append(out, entity)
		if len(out) == MaxEntities {
			break
		}
	}
	return out, nil
}
