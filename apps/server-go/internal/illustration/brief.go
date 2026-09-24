package illustration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration/styles"
)

// Brief is the analyzer's art direction for one article.
type Brief struct {
	Scene        string        `json:"scene"`
	Foreground   string        `json:"foreground"`
	Background   string        `json:"background"`
	Mood         string        `json:"mood"`
	Style        string        `json:"style"`
	StyleReason  string        `json:"style_reason"`
	PublicFigure *PublicFigure `json:"public_figure"`
}

// PublicFigure is a clearly identifiable, newsworthy person named in the
// story. VisibleInSource says the source image shows them.
type PublicFigure struct {
	Name            string `json:"name"`
	VisibleInSource bool   `json:"visible_in_source"`
}

// ErrInvalidBrief wraps every reason an analyzer answer is rejected.
var ErrInvalidBrief = errors.New("invalid brief")

const maxBriefField = 600

// ParseBrief reads an analyzer answer. It tolerates code fences and prose
// around the JSON object, but the object itself must have exactly the brief's
// fields, non-empty strings, a known style and a well-formed public_figure.
func ParseBrief(raw string, allowedStyles []string) (Brief, error) {
	cleaned := strings.TrimSpace(trailingFence.ReplaceAllString(leadingFence.ReplaceAllString(strings.TrimSpace(raw), ""), ""))
	candidate := cleaned
	if match := jsonObject.FindString(cleaned); match != "" {
		candidate = match
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(candidate)))
	decoder.DisallowUnknownFields()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(candidate), &fields); err != nil {
		return Brief{}, fmt.Errorf("%w: not a JSON object: %v", ErrInvalidBrief, err)
	}
	for _, key := range []string{"scene", "foreground", "background", "mood", "style", "style_reason", "public_figure"} {
		if _, ok := fields[key]; !ok {
			return Brief{}, fmt.Errorf("%w: missing %q", ErrInvalidBrief, key)
		}
	}
	var brief Brief
	if err := decoder.Decode(&brief); err != nil {
		return Brief{}, fmt.Errorf("%w: %v", ErrInvalidBrief, err)
	}
	for name, value := range map[string]*string{"scene": &brief.Scene, "foreground": &brief.Foreground, "background": &brief.Background, "mood": &brief.Mood, "style_reason": &brief.StyleReason} {
		*value = strings.Join(strings.Fields(*value), " ")
		if *value == "" {
			return Brief{}, fmt.Errorf("%w: %q is empty", ErrInvalidBrief, name)
		}
		if len(*value) > maxBriefField {
			return Brief{}, fmt.Errorf("%w: %q is longer than %d characters", ErrInvalidBrief, name, maxBriefField)
		}
	}
	brief.Style = strings.ToLower(strings.TrimSpace(brief.Style))
	if !slices.Contains(allowedStyles, brief.Style) {
		return Brief{}, fmt.Errorf("%w: style %q is not one of %v", ErrInvalidBrief, brief.Style, allowedStyles)
	}
	if brief.PublicFigure != nil {
		if raw := fields["public_figure"]; !bytes.Contains(raw, []byte(`"visible_in_source"`)) {
			return Brief{}, fmt.Errorf("%w: public_figure.visible_in_source is missing", ErrInvalidBrief)
		}
		brief.PublicFigure.Name = strings.Join(strings.Fields(brief.PublicFigure.Name), " ")
		if brief.PublicFigure.Name == "" {
			// A public figure without a name is not identifiable: treat as none.
			brief.PublicFigure = nil
		}
	}
	if brief.PublicFigure != nil {
		brief.scrubName(brief.PublicFigure.Name)
	}
	return brief, nil
}

// scrubName keeps the real person out of the image prompt: generated faces
// must be anonymous, so the name never reaches the image model.
func (b *Brief) scrubName(name string) {
	pattern, err := regexp.Compile(`(?i)\b` + regexp.QuoteMeta(name) + `\b`)
	if err != nil {
		return
	}
	for _, field := range []*string{&b.Scene, &b.Foreground, &b.Background, &b.Mood} {
		*field = pattern.ReplaceAllString(*field, "an anonymous figure")
	}
}

// Text is the brief as prompt lines.
func (b Brief) Text() string {
	return fmt.Sprintf("Scene: %s Foreground: %s Background: %s Mood: %s", b.Scene, b.Foreground, b.Background, b.Mood)
}

// WantsCollage reports whether the analyzer saw a named public figure in the
// source image.
func (b Brief) WantsCollage() bool { return b.PublicFigure != nil && b.PublicFigure.VisibleInSource }

// BuildAnalyzePrompt asks a vision model for a Brief.
func BuildAnalyzePrompt(title, excerpt string, catalog styles.Catalog, hasImage bool) string {
	var styleLines strings.Builder
	for _, style := range catalog.All() {
		fmt.Fprintf(&styleLines, "- %q: %s Look: %s\n", style.Name, style.Summary, style.Prompt)
	}
	imageLine := "No source image is available; work from the headline and summary."
	if hasImage {
		imageLine = "The attached image is the news source's own image. Use it to understand the story, but do not describe it for copying: the new illustration must be original."
	}
	return fmt.Sprintf(`You are the art director of an AI and technology news site. Write the brief for an ORIGINAL featured illustration of this story.

Headline: %s
Summary: %s

%s

Return only a JSON object with exactly these keys:
- "scene": one sentence, the core visual idea. Use a clear, interesting visual metaphor for the story rather than a literal photo of it.
- "foreground": one sentence, the main subject.
- "background": one sentence, the setting.
- "mood": a few words, the emotional tone.
- "style": exactly one of %s. Pick the style that fits this story best:
%s- "style_reason": one short sentence on why that style fits.
- "public_figure": null, or {"name": "...", "visible_in_source": true|false}. Set it ONLY for a clearly identifiable, newsworthy public figure (such as a CEO, founder, researcher of note or politician) who is named in the headline or summary. "visible_in_source" is true only when that person is clearly visible in the attached image. Never set it for private individuals, anonymous people or crowds.

Rules for scene, foreground, background and mood:
- Never ask for logos, brand names, product names, company names, readable text, letters, numbers, signs, screens with text, flags, national emblems or coats of arms.
- Never ask for a real, recognizable person or a likeness; if people are needed, they are small, anonymous figures without recognizable faces.
- No injury, violence or distress unless the summary states it.
- Stay factual: depict only what the story supports.`,
		title, excerpt, imageLine, quotedNames(catalog.Names()), styleLines.String())
}

func quotedNames(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = fmt.Sprintf("%q", name)
	}
	return strings.Join(quoted, " or ")
}

// briefGeminiSchema is the Gemini responseSchema for a Brief.
func briefGeminiSchema(styleNames []string) map[string]any {
	str := map[string]any{"type": "STRING"}
	return map[string]any{
		"type": "OBJECT",
		"properties": map[string]any{
			"scene": str, "foreground": str, "background": str, "mood": str,
			"style":        map[string]any{"type": "STRING", "enum": styleNames},
			"style_reason": str,
			"public_figure": map[string]any{
				"type":     "OBJECT",
				"nullable": true,
				"properties": map[string]any{
					"name":              str,
					"visible_in_source": map[string]any{"type": "BOOLEAN"},
				},
				"required": []string{"name", "visible_in_source"},
			},
		},
		"required": []string{"scene", "foreground", "background", "mood", "style", "style_reason", "public_figure"},
	}
}

// briefJSONSchema is the JSON Schema passed to codex exec --output-schema.
func briefJSONSchema(styleNames []string) ([]byte, error) {
	str := map[string]any{"type": "string"}
	return json.Marshal(map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"scene", "foreground", "background", "mood", "style", "style_reason", "public_figure"},
		"properties": map[string]any{
			"scene": str, "foreground": str, "background": str, "mood": str,
			"style":        map[string]any{"type": "string", "enum": styleNames},
			"style_reason": str,
			"public_figure": map[string]any{"anyOf": []any{
				map[string]any{"type": "null"},
				map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []string{"name", "visible_in_source"},
					"properties": map[string]any{
						"name":              str,
						"visible_in_source": map[string]any{"type": "boolean"},
					},
				},
			}},
		},
	})
}
