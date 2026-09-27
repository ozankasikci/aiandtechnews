package content

import (
	"regexp"
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
