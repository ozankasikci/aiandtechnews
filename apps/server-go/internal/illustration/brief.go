package illustration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration/brands"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration/styles"
)

// Brief is the analyzer's art direction for one article.
type Brief struct {
	Scene       string `json:"scene"`
	Foreground  string `json:"foreground"`
	Background  string `json:"background"`
	Mood        string `json:"mood"`
	Style       string `json:"style"`
	StyleReason string `json:"style_reason"`
	// Palette names the colour scheme (styles/palettes.json). It is optional
	// in the answer; the pipeline picks one when it is missing or not allowed.
	Palette string `json:"palette,omitempty"`
	// Composition is CompositionScene or CompositionSimple. It is optional
	// in the answer; the pipeline defaults it to the first allowed one.
	Composition string `json:"composition,omitempty"`
	// Brands are ids from the brand catalog the story is centrally about;
	// their real logos are placed in the image. Optional in the answer.
	Brands []string `json:"brands,omitempty"`
	// CompositionReason says why people are or are not central to the news.
	CompositionReason string        `json:"composition_reason,omitempty"`
	PublicFigure      *PublicFigure `json:"public_figure"`
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
	brief.Palette = strings.ToLower(strings.TrimSpace(brief.Palette))
	brief.Composition = strings.ToLower(strings.TrimSpace(brief.Composition))
	brief.CompositionReason = strings.Join(strings.Fields(brief.CompositionReason), " ")
	for i, id := range brief.Brands {
		brief.Brands[i] = strings.ToLower(strings.TrimSpace(id))
	}
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

// BuildAnalyzePrompt asks a vision model for a Brief. imageURL is the source
// image's address: its file name often names the person pictured. palettes
// are the colour schemes the analyzer may choose from (none: no palette key).
func BuildAnalyzePrompt(title, excerpt, imageURL string, catalog styles.Catalog, hasImage bool, palettes []styles.Palette, compositions []string, prefer string, brandList []brands.Brand) string {
	var styleLines strings.Builder
	for _, style := range catalog.All() {
		fmt.Fprintf(&styleLines, "- %q: %s Look: %s\n", style.Name, style.Summary, style.Prompt)
	}
	imageLine := "No source image is available; work from the headline and summary."
	if hasImage {
		imageLine = "The attached image is the news source's own image. Use it to understand the story, but do not describe it for copying: the new illustration must be original."
		if imageURL != "" {
			imageLine += fmt.Sprintf("\nSource image address (its file name may say who is pictured): %s", imageURL)
		}
	}
	paletteKey := ""
	if len(palettes) > 0 {
		var lines strings.Builder
		for _, palette := range palettes {
			fmt.Fprintf(&lines, "  - %q: %s. Suits %s.\n", palette.Name, palette.Colors, palette.Suits)
		}
		paletteKey = fmt.Sprintf("- \"palette\": exactly one of %s. Pick the colour scheme whose mood fits this story best:\n%s", quotedNames(paletteNames(palettes)), lines.String())
	}
	compositionKey := ""
	if len(compositions) > 0 {
		var lines strings.Builder
		for _, name := range compositions {
			fmt.Fprintf(&lines, "  - %q: %s\n", name, compositionGuide[name])
		}
		compositionKey = fmt.Sprintf("- \"composition\": exactly one of %s:\n%s  %s\n", quotedNames(compositions), lines.String(), strings.ReplaceAll(compositionRule, "\n", "\n  "))
		compositionKey += "- \"composition_reason\": one short sentence: are people central to this news, and why.\n"
		if prefer != "" && len(compositions) > 1 {
			compositionKey += fmt.Sprintf("  For variety on the homepage, prefer %q this time, but only if the rule above allows it.\n", prefer)
		}
	}
	brandKey := ""
	if len(brandList) > 0 {
		var lines strings.Builder
		for _, brand := range brandList {
			fmt.Fprintf(&lines, "%q (%s", brand.ID, brand.Name)
			if len(brand.Aliases) > 0 {
				fmt.Fprintf(&lines, ": %s", strings.Join(brand.Aliases, ", "))
			}
			lines.WriteString("), ")
		}
		brandKey = fmt.Sprintf("- \"brands\": a list of up to %d ids of the companies this story is centrally about, whose real logos will be placed in the image, chosen from: %s. Use [] when none of them is central.\n", MaxBrands, strings.TrimSuffix(lines.String(), ", "))
	}
	return fmt.Sprintf(`You are the art director of an AI and technology news site. Write the brief for an ORIGINAL featured illustration of this story.

Headline: %s
Summary: %s

%s

Return only a JSON object with exactly these keys:
- "scene": one or two sentences showing literally what happened in the story, as an action in progress with a visible result: the actual kinds of people, machines, products and places involved, and what they are doing. A reader must understand the news from the image alone. Never replace the story with a visual metaphor or symbol (no puzzles, bridges, ribbons, chess pieces or similar).
- "foreground": one sentence, the main subject. For a "scene", the people and the action or reaction that carries the story: who is affected or acting, and their visible reaction (a gesture, a posture, an expression). For a "simple" image, the one object or machine the story is about.
- "background": one sentence, the setting (for a "simple" image, a plain backdrop and at most two small supporting elements).
- "mood": a few words, the emotional tone.
- "style": exactly one of %s. Pick the style that fits this story best:
%s- "style_reason": one short sentence on why that style fits.
%s%s%s- "public_figure": null, or {"name": "...", "visible_in_source": true|false}. This field is separate from the illustration and is used to credit a real press photo. Set it when the headline or summary names a newsworthy public figure (such as a CEO, founder, prominent researcher or politician) who is part of the story, even if only quoted; use the most central one. Never set it for private individuals, anonymous people or crowds. "visible_in_source" is true when the attached image is a photo whose main subject is one clearly visible real person and the context (headline, summary, image file name) indicates that person is the named figure; you do not need to recognize the face. Otherwise false.

Rules for scene, foreground, background and mood:
- Never ask for logos (except that the chosen brands' real logos will be added), readable text, letters, numbers, signs, screens with text, flags, national emblems or coats of arms. Do not write brand, product or company names; describe how a real product looks instead (shape, colours, materials).
- Never ask for a real, recognizable person or a likeness; if people are needed, they are ordinary anonymous people (workers, engineers, judges, users) with natural expressions, never a specific real person.
- No injury, violence or distress unless the summary states it.
- Stay factual: depict only what the story supports.`,
		title, excerpt, imageLine, quotedNames(catalog.Names()), styleLines.String(), paletteKey, compositionKey, brandKey)
}

// MaxBrands is how many brand logos one image may carry.
const MaxBrands = 2

// Compositions: a busy scene with people, or a simple image of one subject.
const (
	CompositionScene  = "scene"
	CompositionSimple = "simple"
)

// Compositions lists every composition, scene first.
var Compositions = []string{CompositionScene, CompositionSimple}

var compositionGuide = map[string]string{
	CompositionScene:  "people in a setting, built around a human moment. Required when people are central to the news.",
	CompositionSimple: "no people; a clean, uncluttered image with a plain backdrop and lots of empty space, where the actor of the story (the AI drawn as a robot or agent, a machine, a product) performs the headline's action and its result is visible. Only when people are not central to the news.",
}

// compositionRule is how the analyzer decides between a scene and a simple image.
const compositionRule = `Decide first whether people are central to this news. They are when people are the subject or the ones acting or affected: workers push back, a court rules, researchers quit, staff are laid off, users are harmed, a study of people's jobs. They are not when the story is about a thing: a product, model, chip, price, deal, outage, leak or research result, where people would only be decoration (staff looking at a screen). People central: "scene". Not central: "simple".
Every image, with or without people, must tell the story in one frame: an actor, the action from the headline, and its visible result. Build it on the element that makes the headline news (a price, a size, a first, a failure, a ruling), not a background detail: for a leaked $500 plan the news is the steep price, not the leak. Never an arrangement of objects where nothing happens: that carries no message. For a "simple" image the actor is the AI (drawn as a small robot or agent), a machine or a product, caught in the act: an AI agent carrying private photos out of a folder and pinning them onto a public wall; a robot inspector crawling through a website's structure as a crack lights up where it found the flaw; a new subscription card towering far above the familiar ones, roped off like a luxury item; huge cables being plugged from an AI system into an endless hall of servers. Stay literal to the story (no puzzles, bridges or unrelated symbols), and never use a generic monitor, laptop or phone screen unless the story is about that device.`

func brandIDs(list []brands.Brand) []string {
	ids := make([]string, len(list))
	for i, brand := range list {
		ids[i] = brand.ID
	}
	return ids
}

func paletteNames(palettes []styles.Palette) []string {
	names := make([]string, len(palettes))
	for i, palette := range palettes {
		names[i] = palette.Name
	}
	return names
}

func quotedNames(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = fmt.Sprintf("%q", name)
	}
	return strings.Join(quoted, " or ")
}

// briefGeminiSchema is the Gemini responseSchema for a Brief. With palette
// names it also requires a palette from them.
func briefGeminiSchema(styleNames, paletteNames, compositions, brandIDs []string) map[string]any {
	str := map[string]any{"type": "STRING"}
	schema := map[string]any{
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
	addEnumProperty(schema, "palette", map[string]any{"type": "STRING", "enum": paletteNames}, paletteNames)
	addEnumProperty(schema, "composition", map[string]any{"type": "STRING", "enum": compositions}, compositions)
	addEnumProperty(schema, "composition_reason", str, compositions)
	addEnumProperty(schema, "brands", map[string]any{"type": "ARRAY", "items": map[string]any{"type": "STRING", "enum": brandIDs}}, brandIDs)
	return schema
}

func addEnumProperty(schema map[string]any, key string, property map[string]any, values []string) {
	if len(values) == 0 {
		return
	}
	schema["properties"].(map[string]any)[key] = property
	schema["required"] = append(schema["required"].([]string), key)
}

// briefJSONSchema is the JSON Schema passed to codex exec --output-schema.
// With palette names it also requires a palette from them.
func briefJSONSchema(styleNames, paletteNames, compositions, brandIDs []string) ([]byte, error) {
	str := map[string]any{"type": "string"}
	schema := map[string]any{
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
	}
	addEnumProperty(schema, "palette", map[string]any{"type": "string", "enum": paletteNames}, paletteNames)
	addEnumProperty(schema, "composition", map[string]any{"type": "string", "enum": compositions}, compositions)
	addEnumProperty(schema, "composition_reason", str, compositions)
	addEnumProperty(schema, "brands", map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": brandIDs}}, brandIDs)
	return json.Marshal(schema)
}
