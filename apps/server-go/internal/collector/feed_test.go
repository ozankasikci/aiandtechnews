package collector_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/collector"
)

const rssFeed = `<?xml version="1.0"?>
<rss version="2.0" xmlns:media="http://search.yahoo.com/mrss/" xmlns:content="http://purl.org/rss/1.0/modules/content/">
<channel><title>TechCrunch</title>
<item>
  <title><![CDATA[OpenAI &amp; Microsoft sign a new compute deal]]></title>
  <link>https://techcrunch.com/2026/09/23/openai-microsoft-compute/?utm_source=rss</link>
  <pubDate>Wed, 23 Sep 2026 10:02:00 +0000</pubDate>
  <description><![CDATA[<p>The agreement covers <b>five years</b> of GPU capacity.</p><img src="https://cdn.example.com/inline.jpg">]]></description>
  <media:content url="https://cdn.example.com/lead.jpg" medium="image" width="1200"/>
</item>
<item>
  <title>Anthropic opens a Tokyo office</title>
  <link>https://techcrunch.com/2026/09/22/anthropic-tokyo/</link>
  <pubDate>Tue, 22 Sep 2026 08:00:00 GMT</pubDate>
  <description>&lt;p&gt;Its first Asian office.&lt;/p&gt;&lt;img src=&quot;https://cdn.example.com/escaped.jpg&quot;&gt;</description>
  <enclosure url="https://cdn.example.com/podcast.mp3" type="audio/mpeg"/>
</item>
<item>
  <title>No link item</title>
</item>
<item>
  <title>Bad URL</title>
  <link>not a url</link>
</item>
</channel></rss>`

const atomFeed = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:media="http://search.yahoo.com/mrss/">
<entry>
  <title type="html">Google&#8217;s Gemini gets a smaller on-device model</title>
  <link rel="alternate" type="text/html" href="https://www.theregister.com/2026/09/23/gemini_nano/"/>
  <published>2026-09-23T09:30:00Z</published>
  <summary type="html">Runs &lt;em&gt;offline&lt;/em&gt; on recent phones.</summary>
  <media:thumbnail url="https://regmedia.example.com/thumb.png"/>
</entry>
<entry>
  <title>Enclosure image entry about AI</title>
  <link href="https://www.theregister.com/2026/09/22/ai_enclosure/"/>
  <updated>2026-09-22T09:30:00+02:00</updated>
  <content type="html">Long body text.</content>
  <enclosure type="image/jpeg" url="https://regmedia.example.com/enclosure.jpg"/>
</entry>
</feed>`

func TestParseRSSFeed(t *testing.T) {
	items := collector.ParseFeed(rssFeed, "TechCrunch")
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2 (items without link or with an invalid URL are skipped): %+v", len(items), items)
	}
	first := items[0]
	if first.Title != "OpenAI & Microsoft sign a new compute deal" {
		t.Errorf("title = %q", first.Title)
	}
	if first.URL != "https://techcrunch.com/2026/09/23/openai-microsoft-compute" {
		t.Errorf("url = %q (tracking params must be normalized away)", first.URL)
	}
	if first.Source != "TechCrunch" {
		t.Errorf("source = %q", first.Source)
	}
	if first.PublishedAt == nil || !first.PublishedAt.Equal(time.Date(2026, 9, 23, 10, 2, 0, 0, time.UTC)) {
		t.Errorf("publishedAt = %v", first.PublishedAt)
	}
	if first.Summary != "The agreement covers five years of GPU capacity." {
		t.Errorf("summary = %q", first.Summary)
	}
	if first.ImageURL != "https://cdn.example.com/lead.jpg" {
		t.Errorf("image = %q (media:content wins over inline img)", first.ImageURL)
	}
	second := items[1]
	if second.Summary != "Its first Asian office." || second.ImageURL != "https://cdn.example.com/escaped.jpg" {
		t.Errorf("second = %+v (audio enclosure must be ignored, escaped img used)", second)
	}
	if second.PublishedAt == nil || !second.PublishedAt.Equal(time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("GMT pubDate = %v", second.PublishedAt)
	}
}

func TestParseAtomFeed(t *testing.T) {
	items := collector.ParseFeed(atomFeed, "The Register")
	if len(items) != 2 {
		t.Fatalf("items = %d", len(items))
	}
	if items[0].Title != "Google’s Gemini gets a smaller on-device model" ||
		items[0].URL != "https://www.theregister.com/2026/09/23/gemini_nano" ||
		items[0].Summary != "Runs offline on recent phones." ||
		items[0].ImageURL != "https://regmedia.example.com/thumb.png" {
		t.Errorf("first entry = %+v", items[0])
	}
	if items[1].ImageURL != "https://regmedia.example.com/enclosure.jpg" || items[1].Summary != "Long body text." {
		t.Errorf("second entry = %+v", items[1])
	}
	if items[1].PublishedAt == nil || !items[1].PublishedAt.Equal(time.Date(2026, 9, 22, 7, 30, 0, 0, time.UTC)) {
		t.Errorf("updated with offset = %v", items[1].PublishedAt)
	}
}

func TestParseFeedTakesFirstThirtyItemsAndCapsSummary(t *testing.T) {
	var b strings.Builder
	b.WriteString("<rss><channel>")
	for i := 0; i < 35; i++ {
		fmt.Fprintf(&b, "<item><title>AI story %d</title><link>https://techcrunch.com/2026/09/%02d/ai-%d/</link><description>%s</description></item>", i, i%28+1, i, strings.Repeat("word ", 200))
	}
	b.WriteString("</channel></rss>")
	items := collector.ParseFeed(b.String(), "TechCrunch")
	if len(items) != 30 {
		t.Fatalf("items = %d, want 30", len(items))
	}
	if runes := []rune(items[0].Summary); len(runes) > 401 || !strings.HasSuffix(items[0].Summary, "…") {
		t.Errorf("summary length %d, suffix %q", len(runes), string(runes[len(runes)-1:]))
	}
}

func TestParseFeedIgnoresNonHTTPImages(t *testing.T) {
	feed := `<rss><channel><item><title>AI item</title><link>https://techcrunch.com/2026/09/23/ai/</link>
		<media:content url="data:image/png;base64,AAAA" medium="image"/><description>&lt;img src="javascript:alert(1)"&gt;</description></item></channel></rss>`
	items := collector.ParseFeed(feed, "TechCrunch")
	if len(items) != 1 || items[0].ImageURL != "" {
		t.Fatalf("items = %+v", items)
	}
}
