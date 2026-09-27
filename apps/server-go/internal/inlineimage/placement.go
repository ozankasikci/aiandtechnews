// Package inlineimage adds a second illustration inside the body of longer
// articles. A background worker picks recently published articles, asks the
// illustration pipeline for an image of one passage, drawn in the featured
// image's style, and records where in the body it goes.
package inlineimage

import (
	"math"
	"regexp"
	"strings"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
)

// MinParagraphs is the shortest body, in <p> blocks, that gets an inline image.
const MinParagraphs = 6

// minSlot keeps the image below the read-next card the website shows after
// paragraph 3.
const minSlot = 4

// bodyBlock matches the body's <p> and <h2> blocks, the only tags a body holds.
var bodyBlock = regexp.MustCompile(`(?is)<(p|h2)\b[^>]*>(.*?)</(?:p|h2)\s*>`)

type block struct {
	heading bool
	text    string
}

func blocks(body string) []block {
	var out []block
	for _, match := range bodyBlock.FindAllStringSubmatch(body, -1) {
		out = append(out, block{heading: strings.EqualFold(match[1], "h2"), text: match[2]})
	}
	return out
}

// Paragraphs returns the stripped text of each <p> block, in order.
func Paragraphs(body string) []string {
	var paragraphs []string
	for _, b := range blocks(body) {
		if !b.heading {
			paragraphs = append(paragraphs, content.StripHTML(b.text))
		}
	}
	return paragraphs
}

// Slot is the number of paragraphs the image comes after, and ok is false
// when the body is too short for one. With two or more subheadings the image
// goes right before the second one; otherwise at 60% of the paragraphs. The
// slot is kept within [4, N-2].
func Slot(body string) (slot int, ok bool) {
	paragraphs, headings := 0, 0
	beforeSecondHeading := -1
	for _, b := range blocks(body) {
		if !b.heading {
			paragraphs++
			continue
		}
		headings++
		if headings == 2 {
			beforeSecondHeading = paragraphs
		}
	}
	if paragraphs < MinParagraphs {
		return 0, false
	}
	slot = int(math.Round(float64(paragraphs) * 0.6))
	if beforeSecondHeading >= 0 {
		slot = beforeSecondHeading
	}
	return min(max(slot, minSlot), paragraphs-2), true
}

// SectionText is the passage the image illustrates: the stripped text of
// paragraphs slot-1, slot and slot+1 (1-based), those that exist.
func SectionText(body string, slot int) string {
	paragraphs := Paragraphs(body)
	var parts []string
	for index := slot - 1; index <= slot+1; index++ {
		if index >= 1 && index <= len(paragraphs) && paragraphs[index-1] != "" {
			parts = append(parts, paragraphs[index-1])
		}
	}
	return strings.Join(parts, "\n\n")
}
