package inlineimage_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/inlineimage"
)

// body builds a body from a layout: "p" is a paragraph, "h" a subheading.
func body(layout string) string {
	var out strings.Builder
	paragraph := 0
	for _, kind := range strings.Fields(layout) {
		if kind == "h" {
			out.WriteString("<h2>Heading</h2>")
			continue
		}
		paragraph++
		fmt.Fprintf(&out, "<p>Paragraph %d.</p>", paragraph)
	}
	return out.String()
}

func TestSlot(t *testing.T) {
	for _, test := range []struct {
		name   string
		layout string
		slot   int
		ok     bool
	}{
		{"empty", "", 0, false},
		{"five paragraphs are too short", "p p p p p", 0, false},
		{"five paragraphs with headings are too short", "p p h p p h p", 0, false},
		{"six paragraphs: 60% is 4", "p p p p p p", 4, true},
		{"seven paragraphs: round(4.2) is 4", "p p p p p p p", 4, true},
		{"eight paragraphs: round(4.8) is 5", "p p p p p p p p", 5, true},
		{"ten paragraphs: 6", "p p p p p p p p p p", 6, true},
		{"twelve paragraphs: round(7.2) is 7", "p p p p p p p p p p p p", 7, true},
		{"one heading uses 60%", "p p h p p p p p p", 5, true},
		{"second heading sets the slot", "p p h p p p h p p p p", 5, true},
		{"third heading is ignored", "p p h p p p h p p h p p", 5, true},
		{"slot before the second heading is clamped up to 4", "p h p h p p p p p p", 4, true},
		{"slot before the second heading is clamped down to N-2", "p p h p p p p p p h p", 7, true},
		{"six paragraphs with an early second heading", "p h p p h p p p p", 4, true},
		{"headings before any paragraph", "h h p p p p p p p p", 4, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			slot, ok := inlineimage.Slot(body(test.layout))
			if slot != test.slot || ok != test.ok {
				t.Fatalf("Slot(%q) = %d, %t; want %d, %t", test.layout, slot, ok, test.slot, test.ok)
			}
		})
	}
}

func TestSlotReadsRealMarkup(t *testing.T) {
	html := "<p>One <strong>x</strong></p>\n<p>Two</p>\n<P class=\"x\">Three</P><p>Four</p>\n<h2>A</h2>\n<p>Five</p><p>Six</p><H2>B</H2><p>Seven</p><p>Eight</p><p>Nine</p>"
	if slot, ok := inlineimage.Slot(html); !ok || slot != 6 {
		t.Fatalf("Slot = %d, %t; want 6, true", slot, ok)
	}
}

func TestSectionText(t *testing.T) {
	html := "<p>One &amp; <em>only</em>.</p><h2>Heading</h2><p>Two.</p><p>Three.</p><p>Four.</p>"
	for _, test := range []struct {
		slot int
		want string
	}{
		{1, "One & only .\n\nTwo."},
		{2, "One & only .\n\nTwo.\n\nThree."},
		{3, "Two.\n\nThree.\n\nFour."},
		{4, "Three.\n\nFour."},
		{5, "Four."},
		{9, ""},
	} {
		if got := inlineimage.SectionText(html, test.slot); got != test.want {
			t.Errorf("SectionText(slot %d) = %q, want %q", test.slot, got, test.want)
		}
	}
}

func TestParagraphsSkipHeadings(t *testing.T) {
	got := inlineimage.Paragraphs("<p>A</p><h2>H</h2><p>B</p>")
	if len(got) != 2 || got[0] != "A" || got[1] != "B" {
		t.Fatalf("Paragraphs = %q", got)
	}
}
