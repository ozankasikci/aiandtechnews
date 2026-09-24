package illustration

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Verdict is the compliance reviewer's answer for one generated image.
type Verdict struct {
	Compliant  bool
	HasText    bool
	HasLogo    bool
	HasFlag    bool
	HasPerson  bool
	HasInjury  bool
	Notes      string
	Unverified bool
}

var (
	leadingFence  = regexp.MustCompile("(?i)^```(?:json)?\\s*")
	trailingFence = regexp.MustCompile("\\s*```$")
	jsonObject    = regexp.MustCompile(`(?s)\{.*\}`)
)

func unverified(notes string) Verdict { return Verdict{Notes: notes, Unverified: true} }

var verdictKeys = []string{"has_readable_text", "has_logo_or_watermark", "has_flag_or_emblem", "has_recognizable_real_person", "depicts_unsupported_injury_or_violence"}

// ParseVerdict fails closed on anything unparsable or incomplete.
func ParseVerdict(raw string) Verdict {
	cleaned := strings.TrimSpace(trailingFence.ReplaceAllString(leadingFence.ReplaceAllString(raw, ""), ""))
	candidate := cleaned
	if match := jsonObject.FindString(cleaned); match != "" {
		candidate = match
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(candidate), &parsed); err != nil {
		return unverified("compliance check returned unparsable JSON")
	}
	flags := make([]bool, len(verdictKeys))
	for i, key := range verdictKeys {
		value, ok := parsed[key].(bool)
		if !ok {
			return unverified("compliance check returned an incomplete verdict")
		}
		flags[i] = value
	}
	notes, _ := parsed["notes"].(string)
	verdict := Verdict{HasText: flags[0], HasLogo: flags[1], HasFlag: flags[2], HasPerson: flags[3], HasInjury: flags[4], Notes: notes}
	verdict.Compliant = !verdict.HasText && !verdict.HasLogo && !verdict.HasFlag && !verdict.HasPerson && !verdict.HasInjury
	return verdict
}

const (
	TextViolationCorrection   = "The previous attempt contained readable writing. Remove all words, letters, numbers, signs and labelled screens."
	LogoViolationCorrection   = "The previous attempt contained a logo or watermark. Remove every logo, brand mark and watermark."
	FlagViolationCorrection   = "The previous attempt contained a flag, national emblem or coat of arms. Remove them all."
	PersonViolationCorrection = "The previous attempt showed a recognizable real person. Make every figure small, stylized and anonymous."
	InjuryViolationCorrection = "The previous attempt depicted injury or violence the article does not state. Depict only what the article supports."
	UnverifiedCorrection      = "The previous attempt could not be verified. Follow every rule exactly."
)

// Correction is the line added to the next attempt's prompt.
func Correction(verdict Verdict) string {
	switch {
	case verdict.HasText:
		return TextViolationCorrection
	case verdict.HasLogo:
		return LogoViolationCorrection
	case verdict.HasFlag:
		return FlagViolationCorrection
	case verdict.HasPerson:
		return PersonViolationCorrection
	case verdict.HasInjury:
		return InjuryViolationCorrection
	}
	return UnverifiedCorrection
}

// ComplianceSchema is the Gemini responseSchema for a Verdict.
var ComplianceSchema = map[string]any{
	"type": "OBJECT",
	"properties": map[string]any{
		"has_readable_text":                      map[string]any{"type": "BOOLEAN"},
		"has_logo_or_watermark":                  map[string]any{"type": "BOOLEAN"},
		"has_flag_or_emblem":                     map[string]any{"type": "BOOLEAN"},
		"has_recognizable_real_person":           map[string]any{"type": "BOOLEAN"},
		"depicts_unsupported_injury_or_violence": map[string]any{"type": "BOOLEAN"},
		"notes":                                  map[string]any{"type": "STRING"},
	},
	"required": append(append([]string{}, verdictKeys...), "notes"),
}

// Article is what the reviewer knows about the story.
type Article struct {
	Title   string
	Excerpt string
}

// BuildCompliancePrompt asks the vision model to review a generated image.
// For a collage, the image is the background alone; the real photo is pasted
// on afterwards and is never sent through this check.
func BuildCompliancePrompt(article Article) string {
	return fmt.Sprintf(`You are reviewing a generated editorial illustration for a news website against a strict publishing policy.

Article headline: %s
Article summary: %s

Inspect the attached image and answer these questions.

1. has_readable_text: true if the image contains writing a reader could read, in any language or script: words, letters, numbers, digits, captions, labels, signs, banners, book or newspaper text, screen or interface text, chart labels, clock numerals, or writing in a speech bubble. Small decorative marks, squiggles or tiny glyph-like texture that cannot be read as characters do NOT count.
2. has_logo_or_watermark: true if the image contains any brand logo, company or product mark, watermark, signature or publication branding.
3. has_flag_or_emblem: true if the image contains a national or regional flag, a national emblem, a coat of arms or a government seal.
4. has_recognizable_real_person: true if any face is detailed enough to look like a specific, recognizable real person, or is a likeness of a public figure. Stylized, small, turned-away or anonymous figures are fine and count as false.
5. depicts_unsupported_injury_or_violence: true if the image depicts injury, wounds, blood, violence, physical struggle, damage, illness or visible distress that the headline and summary do not state.
6. notes: one short sentence naming what you found, or "clean" when nothing was found.

Be strict about readable text, logos, flags and emblems.

Return only JSON with exactly this shape:
{"has_readable_text":false,"has_logo_or_watermark":false,"has_flag_or_emblem":false,"has_recognizable_real_person":false,"depicts_unsupported_injury_or_violence":false,"notes":"clean"}`, article.Title, article.Excerpt)
}
