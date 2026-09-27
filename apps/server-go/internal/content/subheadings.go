package content

import (
	"encoding/json"
	"errors"
	"fmt"
	stdhtml "html"
	"regexp"
	"slices"
	"strings"
)

// MaxSubheadings caps the <h2> subheadings kept in an article body.
const MaxSubheadings = 3

const maxSubheadingChars = 80

var bodyBlock = regexp.MustCompile(`(?is)<(p|h2)>(.*?)</(?:p|h2)>\s*`)

// TidySubheadings removes subheadings the rewrite placed badly instead of
// failing the draft: one that opens or closes the body, one right after
// another, an empty or overlong one, and any past MaxSubheadings.
func TidySubheadings(html string) string {
	blocks := bodyBlock.FindAllStringSubmatchIndex(html, -1)
	lastParagraph := -1
	for i, block := range blocks {
		if strings.EqualFold(html[block[2]:block[3]], "p") {
			lastParagraph = i
		}
	}
	var out strings.Builder
	cursor, kept, previous := 0, 0, ""
	for i, block := range blocks {
		kind := strings.ToLower(html[block[2]:block[3]])
		if kind == "h2" {
			text := StripHTML(html[block[4]:block[5]])
			if previous != "p" || i > lastParagraph || text == "" || JavaScriptLength(text) > maxSubheadingChars || kept >= MaxSubheadings {
				out.WriteString(html[cursor:block[0]])
				cursor = block[1]
				continue
			}
			kept++
		}
		previous = kind
	}
	out.WriteString(html[cursor:])
	return out.String()
}

// Subheading is one heading to add above paragraph BeforeParagraph (1-based).
type Subheading struct {
	BeforeParagraph int    `json:"beforeParagraph"`
	Text            string `json:"text"`
}

// minSectionParagraphs is how many paragraphs an added subheading needs
// above it, both from the start and from the previous subheading.
const minSectionParagraphs = 2

// ParagraphTexts returns each paragraph's plain text, in order.
func ParagraphTexts(html string) []string {
	matches := paragraphs.FindAllStringSubmatch(html, -1)
	texts := make([]string, len(matches))
	for i, match := range matches {
		texts[i] = StripHTML(match[1])
	}
	return texts
}

// SameParagraphs reports whether two bodies have byte-identical paragraphs.
func SameParagraphs(a, b string) bool {
	left, right := paragraphs.FindAllString(a, -1), paragraphs.FindAllString(b, -1)
	return slices.Equal(left, right)
}

// SubheadingPrompt asks for 0 to MaxSubheadings subheadings for a published
// article, placed by paragraph number, without rewriting any of it.
func SubheadingPrompt(title string, paragraphTexts []string) string {
	var numbered strings.Builder
	for i, text := range paragraphTexts {
		fmt.Fprintf(&numbered, "%d. %s\n", i+1, text)
	}
	return fmt.Sprintf(`You add subheadings to a published news article without changing its text. Its paragraphs are numbered below.

Rules:
- Add 0 to %d subheadings, only where the story moves to a distinct part. A short story with one thread needs none: return an empty list.
- Each is a short factual statement with a verb, saying what the section below it shows, maximum 60 characters, like "Government websites were a target" or "Pricing starts at $20 a month". Never a topic label like "Background", "Industry context" or "Capabilities and limitations", never a question or a teaser, with no em dash and no final period.
- Use only facts stated in the article.
- beforeParagraph is the number of the paragraph the subheading goes above. Never use 1 or 2, and leave at least 2 paragraphs between subheadings.

Headline: %s

%s
Return only JSON with this exact shape:
{"subheadings":[{"beforeParagraph":4,"text":"Short factual subheading"}]}`, MaxSubheadings, title, numbered.String())
}

// ParseSubheadings reads the model's JSON answer, tolerating a code fence.
func ParseSubheadings(raw string) ([]Subheading, error) {
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return nil, errors.New("subheadings response is not JSON")
	}
	var parsed struct {
		Subheadings []Subheading `json:"subheadings"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err != nil {
		return nil, fmt.Errorf("subheadings response: %w", err)
	}
	return parsed.Subheadings, nil
}

// InsertSubheadings adds the subheadings that fit the house rules above
// their paragraphs and leaves every paragraph byte-identical. Ones placed too
// early, too close to another, past the last paragraph, or holding markup or
// an em dash are skipped; TidySubheadings then applies the general rules.
func InsertSubheadings(html string, subs []Subheading) string {
	subs = slices.Clone(subs)
	slices.SortFunc(subs, func(a, b Subheading) int { return a.BeforeParagraph - b.BeforeParagraph })
	blocks := paragraphs.FindAllStringIndex(html, -1)
	above := make(map[int]string)
	previous := 1
	for _, sub := range subs {
		text := strings.TrimSuffix(strings.TrimSpace(sub.Text), ".")
		n := sub.BeforeParagraph
		if n < 1+minSectionParagraphs || n > len(blocks) || n-previous < minSectionParagraphs || text == "" ||
			strings.ContainsAny(text, "<>—") || len(above) >= MaxSubheadings {
			continue
		}
		above[n] = "<h2>" + stdhtml.EscapeString(text) + "</h2>"
		previous = n
	}
	if len(above) == 0 {
		return html
	}
	var out strings.Builder
	cursor := 0
	for i, block := range blocks {
		if heading, ok := above[i+1]; ok {
			out.WriteString(html[cursor:block[0]])
			out.WriteString(heading)
			cursor = block[0]
		}
	}
	out.WriteString(html[cursor:])
	return TidySubheadings(out.String())
}
