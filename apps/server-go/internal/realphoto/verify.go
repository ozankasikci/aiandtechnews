package realphoto

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/imaging"
)

const (
	verifyMaxEdge = 1024
	verifyQuality = 85
)

// Match values of a Verdict.
const (
	MatchExact     = "exact"
	MatchSameLine  = "same_line"
	MatchDifferent = "different"
)

// Verdict is the vision model's judgement of one candidate photo.
type Verdict struct {
	Match           string `json:"match"`
	Photographic    bool   `json:"photographic"`
	Watermark       bool   `json:"watermark"`
	TextHeavy       bool   `json:"text_heavy"`
	MarketingBanner bool   `json:"marketing_banner"`
	Misleading      bool   `json:"misleading"`
	Prominent       bool   `json:"subject_prominent"`
	OtherBranding   bool   `json:"other_branding"`
	Quality         int    `json:"quality"`
	Alt             string `json:"alt"`
	Notes           string `json:"notes"`
}

// usable says whether the verdict lets the photo be used, and if not why.
func (v Verdict) usable(mode string) (bool, string) {
	switch {
	case v.Match != MatchExact:
		return false, "not the exact subject (" + v.Match + ")"
	case !v.Photographic:
		return false, "not a photo"
	case v.Watermark:
		return false, "watermark"
	case v.TextHeavy:
		return false, "text or logos dominate"
	case v.MarketingBanner:
		return false, "marketing banner"
	case v.Misleading:
		return false, "misleading"
	case !v.Prominent:
		return false, "subject too small or not the focus"
	case v.OtherBranding:
		return false, "other brands' signs or logos in view"
	case v.Quality < MinQuality:
		return false, fmt.Sprintf("quality %d below %d", v.Quality, MinQuality)
	}
	return true, ""
}

const verifyPrompt = `You are checking a candidate image for the in-body picture of a technology news article. It will be shown full width (16:9 crop) in the middle of the article.

Headline: %s
Subject the picture must show: %s
Maker or owner: %s
%s
Judge the image:
match: "exact" only if it clearly shows this exact subject (the same product and model, the same person, the same building, place or event). "same_line" if it shows an older or different model, a sibling product or the same product line, but not this exact subject. "different" if it shows something else or you cannot tell.
photographic: %s
watermark: true if there is a visible watermark, stock-photo stamp or photographer's mark across the image.
text_heavy: true if text, logos, UI screenshots, captions or charts dominate the image.
marketing_banner: true if it is an advert or banner: slogans, prices, buttons, "buy now" or large overlaid text.
misleading: true if using it to illustrate this article would mislead a reader: the wrong thing, an unrelated or staged context, a person in an unrelated, embarrassing or sensitive situation, or an edited or fake-looking picture.
subject_prominent: true only if the subject is the clear focus and fills a good part of the frame (roughly a quarter or more), not a small figure among other things.
other_branding: true if signs, logos or names of companies, events or venues other than the subject's own maker are visible (a stage backdrop, a banner, a booth sign). The maker's own name on the product itself is fine.
quality: 1-10, how good it would look full width: sharp, well lit, well composed, the subject clearly visible and large enough, not cluttered, survives a 16:9 crop. 10 is a professional press photo.
alt: one plain sentence saying what the image shows, for alt text (do not start with "Image of" or "Photo of").
notes: one short sentence.

Return only JSON with exactly this shape:
{"match":"exact","photographic":true,"watermark":false,"text_heavy":false,"marketing_banner":false,"misleading":false,"subject_prominent":true,"other_branding":false,"quality":8,"alt":"...","notes":"..."}`

const (
	commonsContext   = "The image is a freely licensed photo from Wikimedia Commons.\n"
	officialContext  = "The image comes from the maker's own official website and would be credited to the maker.\n"
	commonsPhoto     = "true only if it is a real photograph. False for illustrations, renders, diagrams, screenshots, maps, charts or collages."
	officialPhotoArg = "true if it is a real photograph or a photorealistic official product or press image of the subject. False for drawings, icons, diagrams, screenshots, charts or collages."
)

var verdictSchema = map[string]any{
	"type": "OBJECT",
	"properties": map[string]any{
		"match":             map[string]any{"type": "STRING", "enum": []string{MatchExact, MatchSameLine, MatchDifferent}},
		"photographic":      map[string]any{"type": "BOOLEAN"},
		"watermark":         map[string]any{"type": "BOOLEAN"},
		"text_heavy":        map[string]any{"type": "BOOLEAN"},
		"marketing_banner":  map[string]any{"type": "BOOLEAN"},
		"misleading":        map[string]any{"type": "BOOLEAN"},
		"subject_prominent": map[string]any{"type": "BOOLEAN"},
		"other_branding":    map[string]any{"type": "BOOLEAN"},
		"quality":           map[string]any{"type": "INTEGER"},
		"alt":               map[string]any{"type": "STRING"},
		"notes":             map[string]any{"type": "STRING"},
	},
	"required": []string{"match", "photographic", "watermark", "text_heavy", "marketing_banner", "misleading", "subject_prominent", "other_branding", "quality", "alt", "notes"},
}

// VerifyPrompt is the vision prompt for one candidate in the given mode.
func VerifyPrompt(plan Plan, title, mode string) string {
	contextLine, photographic := commonsContext, commonsPhoto
	if mode == KindOfficial {
		contextLine, photographic = officialContext, officialPhotoArg
	}
	maker := plan.Maker
	if maker == "" {
		maker = "unknown"
	}
	return fmt.Sprintf(verifyPrompt, title, plan.Subject, maker, contextLine, photographic)
}

func (f *Finder) verify(ctx context.Context, plan Plan, request Request, mode string, data []byte) (Verdict, error) {
	jpeg, err := imaging.FitJPEG(data, verifyMaxEdge, verifyQuality)
	if err != nil {
		return Verdict{}, err
	}
	raw, err := f.vision.ReviewImage(ctx, VerifyPrompt(plan, request.Title, mode), jpeg, verdictSchema)
	if err != nil {
		return Verdict{}, err
	}
	return ParseVerdict(raw)
}

// ParseVerdict reads the vision model's answer.
func ParseVerdict(raw string) (Verdict, error) {
	var answer struct {
		Verdict
		Quality *int `json:"quality"`
	}
	if err := json.Unmarshal([]byte(cleanJSON(raw)), &answer); err != nil {
		return Verdict{}, fmt.Errorf("unparsable verdict: %w", err)
	}
	if answer.Quality == nil || answer.Match == "" {
		return Verdict{}, fmt.Errorf("incomplete verdict")
	}
	verdict := answer.Verdict
	verdict.Quality = min(max(*answer.Quality, 0), 10)
	verdict.Match = strings.ToLower(strings.TrimSpace(verdict.Match))
	verdict.Alt = oneLine(verdict.Alt, 250)
	verdict.Notes = oneLine(verdict.Notes, 250)
	return verdict, nil
}
