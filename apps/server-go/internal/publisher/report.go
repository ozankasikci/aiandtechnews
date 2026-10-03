package publisher

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/collector"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
)

// MinDocumentTextLength is the shortest primary document accepted for a
// report. Release notes and changelogs are shorter than news articles.
const MinDocumentTextLength = 400

var documentBlock = regexp.MustCompile(`(?is)<(p|li|h[1-4]|td|th|blockquote|pre)\b[^>]*>.*?</(?:p|li|h[1-4]|td|th|blockquote|pre)>`)

var angleBrackets = strings.NewReplacer("<", "", ">", "")

// ExtractDocumentText is ExtractSourceText for a primary document: it also
// keeps headings, list items and table cells, which carry most of a release
// note or changelog. The longer of the two extractions wins.
func ExtractDocumentText(html string) string {
	article := ExtractSourceText(html)
	cleaned := noiseBlock.ReplaceAllString(htmlComment.ReplaceAllString(html, " "), " ")
	scopes := []string{cleaned}
	if match := mainScope.FindStringSubmatch(cleaned); match != nil {
		scopes = append(scopes, match[1])
	}
	for _, match := range articleScope.FindAllStringSubmatch(cleaned, -1) {
		scopes = append(scopes, match[1])
	}
	// Prefer the narrowest scope that still has enough text.
	document := ""
	for _, scope := range scopes {
		if text := extractDocumentBlocks(scope); content.JavaScriptLength(text) >= MinDocumentTextLength {
			document = text
		}
	}
	if content.JavaScriptLength(document) > content.JavaScriptLength(article) {
		return document
	}
	return article
}

func extractDocumentBlocks(scope string) string {
	seen := map[string]bool{}
	var blocks []string
	total := 0
	for _, block := range documentBlock.FindAllString(scope, -1) {
		text := strings.Join(strings.Fields(collector.DecodeHTMLEntities(content.StripHTML(block))), " ")
		// Literal angle brackets (placeholders like <name>) come back as tags in the draft.
		text = angleBrackets.Replace(text)
		if content.JavaScriptLength(text) < 8 || boilerplateStart.MatchString(text) {
			continue
		}
		if boilerplateMention.MatchString(text) && content.JavaScriptLength(text) < 250 {
			continue
		}
		key := strings.ToLower(text)
		if seen[key] {
			continue
		}
		seen[key] = true
		blocks = append(blocks, text)
		if total += content.JavaScriptLength(text) + 1; total >= maxSourceTextLength {
			break
		}
	}
	return truncateJS(strings.Join(blocks, "\n"), maxSourceTextLength)
}

// report writes an original news report from a primary document and checks
// every claim against it. A draft with unsupported claims is rewritten once
// with the checker's findings; a second failure is permanent.
func (r *Rewriter) report(ctx context.Context, input RewriteInput) (content.RewrittenArticle, error) {
	writer := r.reporter
	if writer == nil {
		writer = r.text
	}
	base := reportPrompt(input)
	correction := ""
	var lastProblem string
	for attempt := 1; attempt <= rewriteAttempts+1; attempt++ {
		raw, err := writer.GenerateJSON(ctx, base+correction)
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
			unsupported, err := r.factCheck(ctx, article, input.SourceText)
			if err != nil {
				return content.RewrittenArticle{}, err
			}
			if len(unsupported) == 0 {
				article.Content = content.TidySubheadings(article.Content)
				return article, nil
			}
			lastProblem = "claims the document does not support: " + strings.Join(unsupported, " | ")
			correction = "\n\nA fact checker found these statements in your previous draft that the document does not support: " +
				strings.Join(unsupported, " | ") + ". Write the report again from the same document without them and without any other unsupported statement."
			continue
		}
		lastProblem = strings.Join(problems, "; ")
		correction = "\n\nYour previous draft failed these checks: " + lastProblem +
			". Write it again from the same document and satisfy every requirement."
	}
	return content.RewrittenArticle{}, Permanent(fmt.Errorf("report failed validation: %s", lastProblem))
}

// factCheck returns the statements in article that the document does not support.
func (r *Rewriter) factCheck(ctx context.Context, article content.RewrittenArticle, document string) ([]string, error) {
	draft, err := json.Marshal(map[string]any{
		"title": article.Title, "excerpt": article.Excerpt, "content": article.Content,
		"tldr": article.TLDR, "whyItMatters": article.WhyItMatters,
	})
	if err != nil {
		return nil, err
	}
	raw, err := r.text.GenerateJSON(ctx, fmt.Sprintf(`You are a fact checker. Below are a primary document and a news report written from it.

List every statement in the report that states as fact something the document does not say: a name, number, date, price, quotation, feature, comparison, cause, reaction or consequence that is not in the document. Widely known background about a company or product (what it is, who makes it) is fine. Clearly labelled interpretation ("this suggests", "this could") is fine when it follows from the document. Do not list wording or style issues.

Document:
%s

Report (JSON):
%s

Return only JSON with this exact shape: {"unsupported":["quote the unsupported statement briefly"]}. Use an empty list when every statement is supported.`, document, draft))
	if err != nil {
		return nil, ClassifyGeminiError(err)
	}
	cleaned := strings.TrimSpace(trailingFence.ReplaceAllString(leadingFence.ReplaceAllString(raw, ""), ""))
	if match := jsonObject.FindString(cleaned); match != "" {
		cleaned = match
	}
	var parsed struct {
		Unsupported []string `json:"unsupported"`
	}
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return nil, fmt.Errorf("fact check answer is not JSON: %w", err)
	}
	var unsupported []string
	for _, statement := range parsed.Unsupported {
		if statement = strings.TrimSpace(statement); statement != "" {
			unsupported = append(unsupported, statement)
		}
	}
	return unsupported, nil
}

func reportPrompt(input RewriteInput) string {
	return fmt.Sprintf(`You are a reporter at TechNews Editorial. The document below was published by %s itself: an announcement, press release, blog post, research write-up, changelog or release note. Report it as an original news article for readers of an AI and technology news site.

Requirements:
- Report what is new: what was announced or released, the key details and numbers, who it affects and when it is available.
- Use only what the document says. Preserve every fact, name, number, date, and quotation accurately. Never invent quotations, statistics, reactions, motives, comparisons or consequences.
- You are reporting on the organisation, not speaking for it. Attribute its claims ("the company says", "the agency says", "according to the release notes") and drop marketing language and superlatives.
- Lead with the one or two changes that matter most to readers and explain in plain words what each does. Do not list every item of a changelog; leave out minor fixes.
- Never refer to "the document". Mention something that is missing only when a reader would expect it, such as the price of a new paid product, and say the company has not said.
- Ignore page furniture in the document such as sign-in prompts, star and fork counts, menus and error messages.
- Do not copy the document's wording. Use a conversational but factual voice.
- Use short, punchy sentences and no filler.
- Never use an em dash.
- Never use the phrases "In a move that" or "It remains to be seen".
- Never use "groundbreaking", "revolutionary", or "game-changing".
- Write a short, direct, factual, non-clickbait headline, maximum 120 characters, in sentence case, that names the company or product and what happened. Never a bare version number.
- Write one plain-sentence excerpt, maximum 180 characters, with no HTML.
- Write a search title, maximum 60 characters, that starts with the main searchable subject (the product, company or project). Keep it factual and non-clickbait.
- Write a search description, maximum 155 characters, as one plain sentence with the key facts and no HTML.
- Write a TL;DR of exactly 3 plain sentences, each maximum 140 characters, giving the key facts in order of importance, with no HTML.
- Write one plain sentence, maximum 200 characters, on why this matters, using only what the document supports.
- In the TL;DR and the why-it-matters sentence, keep any limit the document puts on a claim, such as "in preview", "in the US" or "according to the company".
- Write %d to %d words in 5 to 12 paragraphs.
- Open with a clear lede explaining what happened.
- Add brief, widely known background on the company or product only where a reader needs it.
- End with the next known step from the document (availability, rollout, a date). Do not invent a next step.
- Use only <p> and <h2> tags, with no tag attributes. Write commands, settings and placeholders as plain words; never use angle brackets, code tags or lists.
- Where the story has distinct parts, add up to 3 <h2> subheadings. Each is a short factual statement with a verb, saying what the next section shows, maximum 60 characters. Never a topic label like "Background", never a question or a teaser. Never open or close the body with a subheading and never put two in a row. A short story with one thread needs none.
- Do not include the headline in the body.
- Do not add a source or sources footer. Attribution is stored separately.

Published by: %s
Document title: %s
Document URL: %s

Document:
%s

Return only JSON with this exact shape:
{"title":"Short factual headline","excerpt":"One sentence.","content":"<p>Article body.</p>","metaTitle":"Subject first search title","metaDescription":"One sentence for search results.","tldr":["First key fact.","Second key fact.","Third key fact."],"whyItMatters":"One sentence on why it matters."}`,
		input.Publisher(), minArticleWords, maxArticleWords, input.Publisher(), input.Title, input.CanonicalURL, input.SourceText)
}
