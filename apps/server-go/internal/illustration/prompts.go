package illustration

import (
	"fmt"
	"strings"
)

const styleRules = `Style rules:
- Use a bright, bold, thick-outlined cartoon treatment with flat confident shapes and clean edges.
- Use a landscape 16:9 composition suitable for a news hero image.

No writing rules:
- Put no text of any kind in the image: no words, letters, numbers, captions, labels, logos, watermarks, or branding.
- Draw no speech bubbles and no thought bubbles, whether empty or filled.
- Draw no signs, banners, posters, placards, name plates, book covers, newspapers, documents, slides, or packaging that carry legible writing.
- Draw no readable screen content, user interface text, menus, buttons with labels, dashboards, charts with labels, keyboards with legible keys, clocks with numerals, or digits of any kind.
- Write nothing in any language or script, including English, Chinese, Japanese, Korean, Arabic, Cyrillic, Devanagari, Hebrew, Greek, emoji, mathematical symbols, and invented or alien glyphs.
- Any surface that would normally carry writing must be left blank, or filled only with abstract non-letterlike squiggles, dots, or simple shapes that cannot be read as characters.

Accuracy rules:
- Match the specific subject and factual tone of the article.
- Depict only what the article supports. Never invent facts, events, products, people, or claims.
- Do not depict injury, wounds, bruises, black eyes, blood, damage, violence, physical struggle, illness, or visible distress unless the article explicitly states it.
- Do not add any physical state, action, or event that the article does not state.
- Do not caricature a real, identifiable person in a demeaning, mocking, or degrading way. Draw people respectfully and neutrally.
- Keep a neutral, factual editorial tone. Do not editorialise the subject through the illustration.`

const (
	TextViolationCorrection   = "The previous attempt contained writing. Remove all writing, speech bubbles and signs."
	LogoViolationCorrection   = "The previous attempt contained a logo or watermark. Remove every logo, brand mark and watermark."
	InjuryViolationCorrection = "The previous attempt depicted injury or violence the article does not state. Depict only what the article supports."
	UnverifiedCorrection      = "The previous attempt could not be verified. Follow every no writing and accuracy rule exactly."
)

type Article struct {
	Title   string
	Excerpt string
}

// BuildPrompt ports buildIllustrationPrompt.
func BuildPrompt(article Article, hasReference bool, correction string) string {
	referenceRules := noReferenceRules
	if hasReference {
		referenceRules = withReferenceRules
	}
	correctionRules := ""
	if trimmed := strings.TrimSpace(correction); trimmed != "" {
		correctionRules = "\n\nCorrection for this attempt:\n- " + trimmed
	}
	return fmt.Sprintf("Create an original editorial illustration for this artificial intelligence news article.\n\nHeadline: %s\nSummary: %s\n\n%s\n\n%s%s",
		article.Title, article.Excerpt, referenceRules, styleRules, correctionRules)
}

const withReferenceRules = `A reference image from the source report is attached for visual guidance only.
- Preserve the core subject of the reference image and its colour palette.
- Creatively change the composition, perspective, staging, poses, spacing, and visual narrative so the illustration retells the story in a new way.
- Do not trace the reference image and do not reproduce its layout, framing, or crop.
- Do not copy or include any text, captions, logos, watermarks, or branding from the reference image or from the source publication.`

const noReferenceRules = `No reference image is available, so build the illustration from the article text alone.
- Choose a bright, bold colour palette that suits the subject of the article.
- Invent a fresh composition, perspective, staging, and visual narrative for the story.
- Do not trace, copy, or recreate any existing photograph, artwork, screenshot, or brand image.
- Do not copy or include any text, captions, logos, watermarks, or branding.`

// ComplianceSchema is COMPLIANCE_RESPONSE_SCHEMA.
var ComplianceSchema = map[string]any{
	"type": "OBJECT",
	"properties": map[string]any{
		"has_text":                               map[string]any{"type": "BOOLEAN"},
		"has_logo_or_watermark":                  map[string]any{"type": "BOOLEAN"},
		"depicts_unsupported_injury_or_violence": map[string]any{"type": "BOOLEAN"},
		"notes":                                  map[string]any{"type": "STRING"},
	},
	"required": []string{"has_text", "has_logo_or_watermark", "depicts_unsupported_injury_or_violence", "notes"},
}

// BuildCompliancePrompt ports buildCompliancePrompt.
func BuildCompliancePrompt(article Article) string {
	return fmt.Sprintf(`You are reviewing a generated editorial illustration for a news website against a strict publishing policy.

Article headline: %s
Article summary: %s

Inspect the attached image and answer these questions.

1. has_text: true if the image contains ANY writing, in ANY language or script, anywhere. This includes words, letters, numbers, digits, punctuation used as writing, emoji, mathematical symbols, invented or alien glyphs, captions, labels, name plates, signs, banners, posters, book or newspaper text, document text, slide text, user interface text, menu or button labels, chart labels, legible keyboard keys, clock numerals, and any writing inside a speech bubble or thought bubble. A speech bubble or thought bubble that contains any characters counts as true. Abstract squiggles that cannot be read as characters do not count.
2. has_logo_or_watermark: true if the image contains any brand logo, company mark, product mark, watermark, signature, or publication branding.
3. depicts_unsupported_injury_or_violence: true if the image depicts injury, wounds, bruises, a black eye, blood, violence, physical struggle, damage, illness, or visible physical distress that the article headline and summary above do not state. Also true if a real identifiable person is caricatured in a demeaning or mocking way.
4. notes: one short sentence naming what you found, or "clean" when nothing was found.

Be strict. When you are unsure whether a marking is writing, answer true.

Return only JSON with exactly this shape:
{"has_text":false,"has_logo_or_watermark":false,"depicts_unsupported_injury_or_violence":false,"notes":"clean"}`, article.Title, article.Excerpt)
}
