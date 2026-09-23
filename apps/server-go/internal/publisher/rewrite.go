package publisher

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
)

const (
	minArticleWords = 150
	maxArticleWords = 800
	rewriteAttempts = 2
)

type TextGenerator interface {
	GenerateJSON(ctx context.Context, prompt string) (string, error)
}

type RewriteInput struct {
	Source       string
	Title        string
	CanonicalURL string
	SourceText   string
}

type Rewriter struct {
	text TextGenerator
}

func NewRewriter(text TextGenerator) *Rewriter { return &Rewriter{text: text} }

var (
	leadingFence  = regexp.MustCompile("(?i)^```(?:json)?\\s*")
	trailingFence = regexp.MustCompile("\\s*```$")
	jsonObject    = regexp.MustCompile(`(?s)\{.*\}`)
)

// Rewrite ports rewriteArticle: up to two drafts, the second carrying a
// correction for invalid JSON or failed validation.
func (r *Rewriter) Rewrite(ctx context.Context, input RewriteInput) (content.RewrittenArticle, error) {
	base := rewritePrompt(input)
	correction := ""
	var lastProblem string
	for attempt := 1; attempt <= rewriteAttempts; attempt++ {
		raw, err := r.text.GenerateJSON(ctx, base+correction)
		if err != nil {
			return content.RewrittenArticle{}, ClassifyGeminiError(err)
		}
		article, ok := parseRewrittenArticle(raw)
		if !ok {
			lastProblem = "response was not valid JSON"
			correction = "\n\nYour previous response was not valid JSON. Return only the required JSON object."
			continue
		}
		minWords, maxWords := minArticleWords, maxArticleWords
		problems := content.ValidateRewrittenArticle(article, content.ArticleValidationOptions{MinWords: &minWords, MaxWords: &maxWords})
		if len(problems) == 0 {
			return article, nil
		}
		lastProblem = strings.Join(problems, "; ")
		correction = "\n\nYour previous draft failed these checks: " + lastProblem +
			". Rewrite it again from the same source reporting and satisfy every requirement."
	}
	return content.RewrittenArticle{}, Permanent(fmt.Errorf("rewrite failed validation: %s", lastProblem))
}

func parseRewrittenArticle(value string) (content.RewrittenArticle, bool) {
	cleaned := strings.TrimSpace(trailingFence.ReplaceAllString(leadingFence.ReplaceAllString(value, ""), ""))
	candidate := cleaned
	if match := jsonObject.FindString(cleaned); match != "" {
		candidate = match
	}
	var parsed struct {
		Title   *string `json:"title"`
		Excerpt *string `json:"excerpt"`
		Content *string `json:"content"`
	}
	if err := json.Unmarshal([]byte(candidate), &parsed); err != nil || parsed.Title == nil || parsed.Excerpt == nil || parsed.Content == nil {
		return content.RewrittenArticle{}, false
	}
	return content.RewrittenArticle{
		Title:   strings.TrimSpace(*parsed.Title),
		Excerpt: strings.TrimSpace(*parsed.Excerpt),
		Content: strings.TrimSpace(*parsed.Content),
	}, true
}

func rewritePrompt(input RewriteInput) string {
	return fmt.Sprintf(`You are TechNews Editorial. Rewrite the source reporting below as an original news article.

Requirements:
- Preserve every fact, name, number, date, and quotation accurately.
- Never invent quotations, statistics, motives, consequences, or unsupported details.
- Do not copy the source wording. Use a conversational but factual voice.
- Use short, punchy sentences and no filler.
- Never use an em dash.
- Never use the phrases "In a move that" or "It remains to be seen".
- Never use "groundbreaking", "revolutionary", or "game-changing".
- Write a short, direct, factual, non-clickbait headline, maximum 120 characters.
- Write one plain-sentence excerpt, maximum 180 characters, with no HTML.
- Write %d to %d words in 5 to 12 paragraphs.
- Open with a clear lede explaining what happened.
- Include relevant context and background.
- Include supported industry implications or analysis without presenting speculation as fact.
- End with the next known step. Do not invent a next step.
- Use only <p> and optional <h2> tags, with no tag attributes.
- Do not include the headline in the body.
- Do not add a source or sources footer. Attribution is stored separately.

Publication: %s
Original headline: %s
Canonical source URL: %s

Source reporting:
%s

Return only JSON with this exact shape:
{"title":"Short factual headline","excerpt":"One sentence.","content":"<p>Article body.</p>"}`,
		minArticleWords, maxArticleWords, input.Source, input.Title, input.CanonicalURL, input.SourceText)
}
