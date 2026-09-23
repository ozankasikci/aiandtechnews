package publisher_test

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

const storyParagraph = "OpenAI users reported a change in model behavior after the latest release, and developers shared detailed examples of outputs that no longer matched their earlier results. The company has not confirmed a deliberate capability reduction, so the evidence remains based on user reports and comparative testing."

func jsLen(s string) int { return len(utf16.Encode([]rune(s))) }

// Port of news-policy.test.ts "extracts the richest article scope when a short article card precedes the real story".
func TestExtractSourceTextPrefersRichestScope(t *testing.T) {
	var paragraphs strings.Builder
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&paragraphs, "<p>%s Reported example %d describes a distinct test performed by a different user.</p>", storyParagraph, i)
	}
	html := "<html><body><article><p>Short card.</p></article><main>" + paragraphs.String() + "</main></body></html>"

	extracted := publisher.ExtractSourceText(html)
	if jsLen(extracted) < 800 || !strings.Contains(extracted, "developers shared detailed examples") {
		t.Fatalf("extracted (%d) = %q", jsLen(extracted), extracted)
	}
}

func TestExtractSourceTextDropsNoiseAndBoilerplate(t *testing.T) {
	long := strings.Repeat("Anthropic described the new model's safety evaluations in detail. ", 3)
	html := `<html><body>
		<nav><p>` + long + ` navigation copy</p></nav>
		<!-- <p>` + long + ` commented out</p> -->
		<article>
		  <p>` + long + `</p>
		  <p>Subscribe to our newsletter for the latest AI stories and analysis every single morning.</p>
		  <p>We use cookies to improve your experience on this site, read our cookie policy today.</p>
		  <p>` + long + `</p>
		  <p>Too short.</p>
		  <p>The rollout begins next week for enterprise customers, followed by consumer apps later this year.</p>
		</article>
		<footer><p>` + long + ` footer copy</p></footer>
	</body></html>`

	extracted := publisher.ExtractSourceText(html)
	for _, unwanted := range []string{"navigation copy", "commented out", "Subscribe", "cookies", "Too short", "footer copy"} {
		if strings.Contains(extracted, unwanted) {
			t.Errorf("extracted contains %q: %q", unwanted, extracted)
		}
	}
	if strings.Count(extracted, "safety evaluations") != 3 {
		t.Errorf("duplicate paragraph should appear once: %q", extracted)
	}
	if !strings.Contains(extracted, "The rollout begins next week") {
		t.Errorf("missing last paragraph: %q", extracted)
	}
}

func TestExtractSourceTextUsesJSONLDArticleBodyAndCaps(t *testing.T) {
	body := strings.Repeat("Google announced a smaller Gemini model that runs on device. ", 400)
	html := `<script type="application/ld+json">{"@graph":[{"@type":"WebPage"},{"@type":"NewsArticle","articleBody":"` + body + `"}]}</script><p>ignored</p>`
	extracted := publisher.ExtractSourceText(html)
	if jsLen(extracted) != 14000 || !strings.HasPrefix(extracted, "Google announced") {
		t.Fatalf("len=%d prefix=%q", jsLen(extracted), extracted[:40])
	}
}

func TestExtractCanonicalURL(t *testing.T) {
	page := "https://techcrunch.com/2026/09/23/story/?utm_source=rss"
	cases := map[string]string{
		`<link rel="canonical" href="https://techcrunch.com/2026/09/23/story/">`:    "https://techcrunch.com/2026/09/23/story",
		`<link href="/2026/09/23/relative/" rel="canonical">`:                       "https://techcrunch.com/2026/09/23/relative",
		`<link rel="canonical" href="https://elsewhere.example.com/copied-story/">`: "https://techcrunch.com/2026/09/23/story",
		`<p>no canonical</p>`: "https://techcrunch.com/2026/09/23/story",
	}
	for html, want := range cases {
		if got := publisher.ExtractCanonicalURL(html, page); got != want {
			t.Errorf("ExtractCanonicalURL(%q) = %q, want %q", html, got, want)
		}
	}
}

func TestExtractOGImage(t *testing.T) {
	page := "https://www.theverge.com/2026/9/23/story"
	cases := map[string]string{
		`<meta property="og:image" content="https://cdn.example.com/a.jpg?w=1200&amp;h=630">`: "https://cdn.example.com/a.jpg?w=1200&h=630",
		`<meta content="/images/b.png" property="og:image">`:                                  "https://www.theverge.com/images/b.png",
		`<meta property="og:image" content="data:image/png;base64,AAAA">`:                     "",
		`<meta name="twitter:image" content="https://cdn.example.com/c.jpg">`:                 "",
	}
	for html, want := range cases {
		if got := publisher.ExtractOGImage(html, page); got != want {
			t.Errorf("ExtractOGImage(%q) = %q, want %q", html, got, want)
		}
	}
}
