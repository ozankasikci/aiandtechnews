package telegram_test

import (
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/telegram"
)

const site = "https://aiandtech.news"

const link = `<a href="https://aiandtech.news/article/big-news?utm_source=telegram&amp;utm_medium=social&amp;utm_campaign=channel">Read more</a>`

func TestCaptionLayout(t *testing.T) {
	got := telegram.Caption(telegram.Post{
		Slug: "big-news", Title: "OpenAI ships a model", Excerpt: "unused",
		WhyItMatters: "It changes pricing.",
		TLDR:         []string{"One.", "Two.", "Three.", "Four."},
	}, site+"/")
	want := "<b>OpenAI ships a model</b>\n\nIt changes pricing.\n\n• One.\n• Two.\n• Three.\n\n" + link
	if got != want {
		t.Fatalf("Caption() =\n%s\nwant\n%s", got, want)
	}
}

func TestCaptionFallsBackToExcerptAndEscapes(t *testing.T) {
	got := telegram.Caption(telegram.Post{
		Slug: "big-news", Title: "AT&T <b>bold</b>", Excerpt: "x < y & z > w",
		TLDR: []string{"a&b"},
	}, site)
	want := "<b>AT&amp;T &lt;b&gt;bold&lt;/b&gt;</b>\n\nx &lt; y &amp; z &gt; w\n\n• a&amp;b\n\n" + link
	if got != want {
		t.Fatalf("Caption() =\n%s\nwant\n%s", got, want)
	}
	if got := telegram.Caption(telegram.Post{Slug: "big-news", Title: "T"}, site); got != "<b>T</b>\n\n"+link {
		t.Fatalf("Caption() without summary = %q", got)
	}
}

func length(text string) int { return len(utf16.Encode([]rune(text))) }

func TestCaptionDropsTLDRPointsFromTheEndFirst(t *testing.T) {
	long := strings.Repeat("p", 300)
	got := telegram.Caption(telegram.Post{
		Slug: "big-news", Title: "Title", WhyItMatters: "Why.",
		TLDR: []string{"first " + long, "second " + long, "third " + long},
	}, site)
	if length(got) > telegram.MaxCaptionLength {
		t.Fatalf("length = %d", length(got))
	}
	if !strings.Contains(got, "• first") || !strings.Contains(got, "• second") || strings.Contains(got, "third") {
		t.Fatalf("Caption() kept the wrong points:\n%s", got)
	}
	if !strings.Contains(got, "Why.") || !strings.HasSuffix(got, link) {
		t.Fatalf("Caption() lost the summary or link:\n%s", got)
	}
}

func TestCaptionTruncatesTheSummaryButNeverTheLink(t *testing.T) {
	// Emoji are two UTF-16 units each, and & grows to &amp;.
	summary := strings.Repeat("😀 & ", 600)
	got := telegram.Caption(telegram.Post{
		Slug: "big-news", Title: "Title", WhyItMatters: summary, TLDR: []string{"gone"},
	}, site)
	if n := length(got); n > telegram.MaxCaptionLength || n < telegram.MaxCaptionLength-10 {
		t.Fatalf("length = %d, want just under %d", n, telegram.MaxCaptionLength)
	}
	if strings.Contains(got, "gone") || !strings.HasSuffix(got, "…\n\n"+link) || !strings.HasPrefix(got, "<b>Title</b>\n\n😀 &amp;") {
		t.Fatalf("Caption() =\n%s", got)
	}
	if strings.Contains(got, "&amp…") || strings.Contains(got, "&a…") {
		t.Fatalf("Caption() cut an entity: %s", got[len(got)-200:])
	}

	huge := telegram.Caption(telegram.Post{Slug: "big-news", Title: strings.Repeat("t", 3000), WhyItMatters: "w"}, site)
	if length(huge) > telegram.MaxCaptionLength || !strings.HasSuffix(huge, link) {
		t.Fatalf("Caption() with a huge title = %d units", length(huge))
	}
}

func TestPhotoURLOnlyUsesOurImages(t *testing.T) {
	for image, want := range map[string]string{
		"https://aiandtech-feature-images-106111531869.s3.eu-west-1.amazonaws.com/features/a.webp": "https://aiandtech-feature-images-106111531869.s3.eu-west-1.amazonaws.com/features/a.webp",
		"/uploads/x.png":                     site + "/uploads/x.png",
		"/images/default-article.jpg":        "",
		"https://techcrunch.com/wp/a.jpg":    "",
		"":                                   "",
		"//evil.example/a.jpg":               "",
		"http://aiandtech.news/insecure.jpg": "",
	} {
		if got := telegram.PhotoURL(image, site+"/"); got != want {
			t.Errorf("PhotoURL(%q) = %q, want %q", image, got, want)
		}
	}
}
