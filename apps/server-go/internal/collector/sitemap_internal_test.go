package collector

import (
	"slices"
	"testing"
	"time"
)

func TestSitemapPagesKeepsRecentPagesUnderThePathsNewestFirst(t *testing.T) {
	xml := `<urlset>
<url><loc>https://www.anthropic.com/news/old</loc><lastmod>2026-09-11T12:00:00.000Z</lastmod></url>
<url><loc>https://www.anthropic.com/news/new-thing</loc><lastmod>2026-10-02T17:12:50.000Z</lastmod></url>
<url><loc>https://www.anthropic.com/transparency</loc><lastmod>2026-10-03T00:27:03.000Z</lastmod></url>
<url><loc>https://www.anthropic.com/research/a-result</loc><lastmod>2026-10-03T01:00:00.000Z</lastmod></url>
<url><loc>https://www.anthropic.com/news/</loc><lastmod>2026-10-03T02:00:00.000Z</lastmod></url>
<url><loc>https://www.anthropic.com/news/no-date</loc></url>
</urlset>`
	since := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	got := SitemapPages(xml, []string{"/news/", "/research/"}, since)
	want := []string{"https://www.anthropic.com/research/a-result", "https://www.anthropic.com/news/new-thing"}
	if !slices.Equal(got, want) {
		t.Fatalf("pages = %v, want %v", got, want)
	}
}

func TestPageItemReadsThePagesOwnMetadata(t *testing.T) {
	html := `<html><head><title>Claude Frontier Academy \ Anthropic</title>
<meta property="og:title" content="Claude Frontier Academy: $100M to train 10,000 engineers"/>
<meta property="og:description" content="A $100 million commitment &amp; more."/>
<meta property="og:image" content="https://www.anthropic.com/api/opengraph-illustration?name=Hand"/>
<meta property="article:published_time" content="2026-10-02T23:01:00.000Z"/></head></html>`
	item, ok := PageItem(html, "https://www.anthropic.com/news/claude-frontier-academy", "Anthropic")
	if !ok {
		t.Fatal("no item")
	}
	if item.Title != "Claude Frontier Academy: $100M to train 10,000 engineers" || item.Source != "Anthropic" ||
		item.Summary != "A $100 million commitment & more." || item.ImageURL == "" ||
		item.PublishedAt == nil || !item.PublishedAt.Equal(time.Date(2026, 10, 2, 23, 1, 0, 0, time.UTC)) {
		t.Fatalf("item = %+v", item)
	}
	if _, ok := PageItem(`<html><head><title>Old page</title></head></html>`, "https://www.anthropic.com/news/x", "Anthropic"); ok {
		t.Fatal("a page without a publish time must not become an item")
	}
}
