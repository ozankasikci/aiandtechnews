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

func TestDigestEmailIsByteIdenticalToNode(t *testing.T) {
	digests := golden(t).Emails.Digests
	if len(digests) != 6 {
		t.Fatalf("recorded digests = %d", len(digests))
	}
	for _, vector := range digests {
		articles := make([]DigestArticle, len(vector.Articles))
		for i, article := range vector.Articles {
			articles[i] = DigestArticle(article)
		}
		assertEmail(t, "digest "+vector.Name, DigestEmail(vector.To, articles, vector.SiteURL, vector.UnsubscribeURL), vector.Email)
		if got := DigestSubject(articles); got != vector.Email.Subject {
			t.Errorf("DigestSubject(%s) = %q", vector.Name, got)
		}
	}
}

func TestDigestGroupsByCategoryInSelectionOrder(t *testing.T) {
	sections := groupByCategory([]DigestArticle{{Title: "1", Category: "AI"}, {Title: "2", Category: "Code"}, {Title: "3", Category: "AI"}, {Title: "4", Category: "ai"}})
	var got []string
	for _, section := range sections {
		var titles []string
		for _, article := range section.articles {
			titles = append(titles, article.Title)
		}
		got = append(got, section.category+":"+strings.Join(titles, ","))
	}
	if want := []string{"AI:1,3", "Code:2", "ai:4"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sections = %v, want %v", got, want)
	}
}
