package publisher

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
)

const (
	minArticleWords = 150
	maxArticleWords = 800
	rewriteAttempts = 2

	maxMetaTitleChars       = 60
	maxMetaDescriptionChars = 155
	tldrPoints              = 3
	maxTLDRPointChars       = 140
	maxWhyItMattersChars    = 200
)

type TextGenerator interface {
	GenerateJSON(ctx context.Context, prompt string) (string, error)
}

type RewriteInput struct {
	Source       string
	Title        string
	CanonicalURL string
	SourceText   string
	// Primary means SourceText is a primary document (content.IsPrimarySource):
	// the article is reported from it and fact-checked against it.
	Primary bool
}

// Publisher is the organisation behind a primary document.
func (i RewriteInput) Publisher() string { return content.PrimaryPublisher(i.Source) }

type Rewriter struct {
	text TextGenerator
	// reporter drafts reports from primary documents; nil means text does.
	// text always runs the fact check, so with a reporter set a second model
	// checks the first.
	reporter TextGenerator
}

func NewRewriter(text TextGenerator) *Rewriter { return &Rewriter{text: text} }

// WithReporter makes reporter draft the reports written from primary documents.
func (r *Rewriter) WithReporter(reporter TextGenerator) *Rewriter {
	r.reporter = reporter
	return r
}

var (
	leadingFence  = regexp.MustCompile("(?i)^```(?:json)?\\s*")
	trailingFence = regexp.MustCompile("\\s*```$")
	jsonObject    = regexp.MustCompile(`(?s)\{.*\}`)
)

// Rewrite ports rewriteArticle: up to two drafts, the second carrying a
// correction for invalid JSON or failed validation.
func (r *Rewriter) Rewrite(ctx context.Context, input RewriteInput) (content.RewrittenArticle, error) {
	if input.Primary {
		return r.report(ctx, input)
	}
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
			article.Content = content.TidySubheadings(article.Content)
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
		Title           *string  `json:"title"`
		Excerpt         *string  `json:"excerpt"`
		Content         *string  `json:"content"`
		MetaTitle       string   `json:"metaTitle"`
		MetaDescription string   `json:"metaDescription"`
		TLDR            []string `json:"tldr"`
		WhyItMatters    string   `json:"whyItMatters"`
	}
	if err := json.Unmarshal([]byte(candidate), &parsed); err != nil || parsed.Title == nil || parsed.Excerpt == nil || parsed.Content == nil {
		return content.RewrittenArticle{}, false
	}
	return content.RewrittenArticle{
		Title:   strings.TrimSpace(*parsed.Title),
		Excerpt: strings.TrimSpace(*parsed.Excerpt),
		Content: strings.TrimSpace(*parsed.Content),

		MetaTitle:       searchSnippet(parsed.MetaTitle, maxMetaTitleChars),
		MetaDescription: searchSnippet(parsed.MetaDescription, maxMetaDescriptionChars),
		TLDR:            summaryPoints(parsed.TLDR),
		WhyItMatters:    searchSnippet(parsed.WhyItMatters, maxWhyItMattersChars),
	}, true
}

// summaryPoints returns the trimmed TL;DR, or nil unless it is exactly three
// non-empty sentences that each pass searchSnippet's checks. Like the search
// snippets, a bad summary is dropped instead of failing the rewrite.
func summaryPoints(points []string) []string {
	if len(points) != tldrPoints {
		return nil
	}
	out := make([]string, 0, tldrPoints)
	for _, point := range points {
		point = searchSnippet(point, maxTLDRPointChars)
		if point == "" {
			return nil
		}
		out = append(out, point)
	}
	return out
}

// searchSnippet returns the trimmed value, or "" when it is too long or breaks
// the writing contract. Search metadata is optional, so a bad snippet is
// dropped instead of failing the rewrite.
func searchSnippet(value string, maxChars int) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > maxChars || strings.ContainsAny(value, "—<>") {
		return ""
	}
	return value
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
- Write a search title, maximum 60 characters, that starts with the main searchable subject (the product, company, project, or person the story is about). Keep it factual and non-clickbait.
- Write a search description, maximum 155 characters, as one plain sentence with the key facts and no HTML.
- Write a TL;DR of exactly 3 plain sentences, each maximum 140 characters, giving the key facts in order of importance, with no HTML.
- Write one plain sentence, maximum 200 characters, on why the story matters, using only what the source reporting supports.
- In the TL;DR and the why-it-matters sentence, keep any limit the source puts on a claim, such as "in this test", "in the US" or "according to the company".
- Write %d to %d words in 5 to 12 paragraphs.
- Open with a clear lede explaining what happened.
- Include relevant context and background.
- Include supported industry implications or analysis without presenting speculation as fact.
- End with the next known step. Do not invent a next step.
- Use only <p> and <h2> tags, with no tag attributes.
- Where the story has distinct parts, add up to 3 <h2> subheadings. Each is a short factual statement with a verb, saying what the next section shows, maximum 60 characters, like "Government websites were a target". Never a topic label like "Background" or "Industry context", never a question or a teaser. Never open or close the body with a subheading and never put two in a row. A short story with one thread needs none.
- Do not include the headline in the body.
- Do not add a source or sources footer. Attribution is stored separately.

Publication: %s
Original headline: %s
Canonical source URL: %s

Source reporting:
%s

Return only JSON with this exact shape:
{"title":"Short factual headline","excerpt":"One sentence.","content":"<p>Article body.</p>","metaTitle":"Subject first search title","metaDescription":"One sentence for search results.","tldr":["First key fact.","Second key fact.","Third key fact."],"whyItMatters":"One sentence on why it matters."}`,
		minArticleWords, maxArticleWords, input.Source, input.Title, input.CanonicalURL, input.SourceText)
}
