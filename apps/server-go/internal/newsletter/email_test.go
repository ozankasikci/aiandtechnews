package newsletter

import (
	"reflect"
	"strings"
	"testing"
)

func TestReadingMinutesMatchesNode(t *testing.T) {
	vectors := golden(t).ReadingMinutes
	if len(vectors) < 30 {
		t.Fatalf("recorded reading-time vectors = %d", len(vectors))
	}
	for _, vector := range vectors {
		input := ""
		if vector.Input != nil {
			input = *vector.Input
		}
		if got := readingMinutes(input); got != vector.Minutes {
			t.Errorf("readingMinutes(%.60q) = %d, want %d", input, got, vector.Minutes)
		}
	}
}

// Expected values were checked against V8 with the Node regex
// /<(script|style)\b[^>]*>[\s\S]*?<\/\1>/gi.
func TestStripScriptAndStyleFollowsTheBackreferenceRegex(t *testing.T) {
	for input, want := range map[string]string{
		"a<script>x</script>b":                "a b",
		"a<SCRIPT>x</Script>b":                "a b",
		"a<script>x</style>y</script>b":       "a b",
		"a<style>x</script>b":                 "a<style>x</script>b",
		"a<scriptx>x</scriptx>b":              "a<scriptx>x</scriptx>b",
		"a<script>x</script >b":               "a<script>x</script >b",
		"<script>1</script><style>2</style>3": "  3",
		"a<script":                            "a<script",
		"<script>unclosed <style>s</style>":   "<script>unclosed  ",
		"<script\n>x</script>":                " ",
	} {
		if got := stripScriptAndStyle(input); got != want {
			t.Errorf("stripScriptAndStyle(%q) = %q, want %q", input, got, want)
		}
	}
}

func headerMap(headers []Header) map[string]string {
	if headers == nil {
		return nil
	}
	values := map[string]string{}
	for _, header := range headers {
		values[header.Name] = header.Value
	}
	return values
}

func tagsOf(tags []Tag) []goldenTag {
	if tags == nil {
		return nil
	}
	converted := make([]goldenTag, len(tags))
	for i, tag := range tags {
		converted[i] = goldenTag{Name: tag.Name, Value: tag.Value}
	}
	return converted
}

func assertEmail(t *testing.T, name string, got Email, want nodeEmail) {
	t.Helper()
	if got.To != want.To || got.Subject != want.Subject {
		t.Errorf("%s: to/subject = %q %q, want %q %q", name, got.To, got.Subject, want.To, want.Subject)
	}
	if got.HTML != want.HTML {
		t.Errorf("%s: HTML differs from Node's renderer at byte %d\ngot:  %q\nwant: %q", name, firstDifference(got.HTML, want.HTML), got.HTML, want.HTML)
	}
	if got.Text != want.Text {
		t.Errorf("%s: text = %q\nwant %q", name, got.Text, want.Text)
	}
	if !reflect.DeepEqual(headerMap(got.Headers), want.Headers) || !reflect.DeepEqual(tagsOf(got.Tags), want.Tags) {
		t.Errorf("%s: headers/tags = %v %v, want %v %v", name, got.Headers, got.Tags, want.Headers, want.Tags)
	}
	if len(got.Headers) == 2 && (got.Headers[0].Name != "List-Unsubscribe" || got.Headers[1].Name != "List-Unsubscribe-Post") {
		t.Errorf("%s: header order = %v", name, got.Headers)
	}
}

func firstDifference(a, b string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

func TestWelcomeEmailIsByteIdenticalToNode(t *testing.T) {
	for _, vector := range golden(t).Emails.Welcome {
		assertEmail(t, "welcome to "+vector.To, WelcomeEmail(vector.To, vector.SiteURL, vector.UnsubscribeURL), vector.Email)
	}
}

func TestDigestEmailLeadsWithTheFirstStory(t *testing.T) {
	articles := []DigestArticle{
		{Title: "Lead <story>", Slug: "lead story", Excerpt: "Lead excerpt", Category: "AI", ReadingMinutes: 3,
			Image: "https://img.example/lead.webp", Source: "The Verge", WhyItMatters: "It matters & how", TLDR: []string{"First point", "Second point"}},
		{Title: "Second", Slug: "second", Excerpt: "Second excerpt", Category: "Tech", ReadingMinutes: 2, Image: "/uploads/second.jpg"},
	}
	email := DigestEmail("reader@example.com", articles, "https://www.aiandtech.news", "https://www.aiandtech.news/unsub?t=1", "2026-10-01")
	for _, want := range []string{
		"Thu, Oct 1 · Daily Brief",
		"Lead &lt;story&gt;, plus 1 more story",
		"2 stories · about 5 minutes of reading",
		`src="https://img.example/lead.webp"`,
		`src="https://www.aiandtech.news/uploads/second.jpg"`,
		"The Verge · 3 min",
		"Why it matters", "It matters &amp; how",
		"First point", "Second point",
		`href="https://www.aiandtech.news/article/lead%20story?utm_source=newsletter&amp;utm_medium=email&amp;utm_campaign=daily_2026-10-01"`,
		"Also today", "Second excerpt",
		`href="https://www.aiandtech.news/unsub?t=1"`,
		"prefers-color-scheme:dark",
	} {
		if !strings.Contains(email.HTML, want) {
			t.Errorf("HTML is missing %q", want)
		}
	}
	if email.Subject != "Lead <story> (+1 more)" {
		t.Errorf("subject = %q", email.Subject)
	}
	for _, want := range []string{"Lead <story> (3 min)\nIt matters & how\n1. First point\n2. Second point\n", "ALSO TODAY", "Unsubscribe: https://www.aiandtech.news/unsub?t=1"} {
		if !strings.Contains(email.Text, want) {
			t.Errorf("text is missing %q in %q", want, email.Text)
		}
	}
	if !reflect.DeepEqual(email.Headers, unsubscribeHeaders("https://www.aiandtech.news/unsub?t=1")) || email.Tags[0].Value != "daily_digest" {
		t.Errorf("headers = %v tags = %v", email.Headers, email.Tags)
	}
}

func TestDigestEmailFallsBackWithoutImageOrSummary(t *testing.T) {
	articles := []DigestArticle{{Title: "Only", Slug: "only", Excerpt: "Plain excerpt", Category: "AI", ReadingMinutes: 1, Image: "javascript:alert(1)"}}
	email := DigestEmail("reader@example.com", articles, "https://www.aiandtech.news", "https://u", "2026-10-01")
	if strings.Contains(email.HTML, "<img") || strings.Contains(email.HTML, "javascript:") {
		t.Error("an unusable image was rendered")
	}
	if strings.Contains(email.HTML, "Why it matters") || strings.Contains(email.HTML, "Also today") {
		t.Error("empty sections were rendered")
	}
	if !strings.Contains(email.HTML, "Plain excerpt") || !strings.Contains(email.HTML, "1 story · about 1 minute of reading") {
		t.Error("excerpt or intro missing")
	}
}

func TestParseTLDR(t *testing.T) {
	if got := parseTLDR(`["a", " ", "b "]`); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("got %q", got)
	}
	if got := parseTLDR("not json"); got != nil {
		t.Errorf("got %q", got)
	}
}
