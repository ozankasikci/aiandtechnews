package publisher

import (
	"context"
	"strings"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
)

// minSubheadingParagraphs is the shortest fresh article the second pass
// looks at, the same bar the subheading backfill used.
const minSubheadingParagraphs = 5

// addSubheadings gives a fresh article that came back from the rewrite
// without subheadings a second chance: the placement pass the backfill uses
// asks only where 0 to 3 headings go, so the paragraphs never change. Any
// failure keeps the body as written.
func (p *Publisher) addSubheadings(ctx context.Context, title, body string) string {
	if p.Subheadings == nil || strings.Contains(strings.ToLower(body), "<h2>") {
		return body
	}
	paragraphs := content.ParagraphTexts(body)
	if len(paragraphs) < minSubheadingParagraphs {
		return body
	}
	raw, err := p.Subheadings.GenerateJSON(ctx, content.SubheadingPrompt(title, paragraphs))
	if err == nil {
		var subs []content.Subheading
		if subs, err = content.ParseSubheadings(raw); err == nil {
			if updated := content.InsertSubheadings(body, subs); content.SameParagraphs(body, updated) {
				return updated
			}
		}
	}
	if err != nil && p.Logger != nil {
		p.Logger.WarnContext(ctx, "subheading pass failed; publishing without subheadings", "error", err)
	}
	return body
}
