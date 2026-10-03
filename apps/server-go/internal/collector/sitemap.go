package collector

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
)

// A sitemap source is a site with no feed (Anthropic): its sitemap lists
// every page with a last-modified time. Pages under the feed's paths that
// changed recently are fetched, and each becomes an item only when the page
// itself says it was published recently, because a sitemap's last-modified
// time also moves when an old page is edited.

// maxSitemapPages caps the pages fetched from one sitemap in a run.
const maxSitemapPages = 12

var (
	sitemapEntry   = regexp.MustCompile(`(?is)<url>(.*?)</url>`)
	sitemapLoc     = regexp.MustCompile(`(?is)<loc>\s*(.*?)\s*</loc>`)
	sitemapLastMod = regexp.MustCompile(`(?is)<lastmod>\s*(.*?)\s*</lastmod>`)
	metaTag        = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	titleTag       = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)

// SitemapPages returns the URLs under paths whose last-modified time is at
// or after since, newest first, at most maxSitemapPages.
func SitemapPages(xml string, paths []string, since time.Time) []string {
	type page struct {
		url      string
		modified time.Time
	}
	var pages []page
	for _, entry := range sitemapEntry.FindAllStringSubmatch(xml, -1) {
		loc := sitemapLoc.FindStringSubmatch(entry[1])
		lastMod := sitemapLastMod.FindStringSubmatch(entry[1])
		if loc == nil || lastMod == nil {
			continue
		}
		modified, ok := parseFeedDate(lastMod[1])
		if !ok || modified.Before(since) {
			continue
		}
		normalized, err := content.NormalizeSourceURL(DecodeHTMLEntities(loc[1]))
		if err != nil || !underPaths(normalized, paths) {
			continue
		}
		pages = append(pages, page{normalized, modified})
	}
	sort.SliceStable(pages, func(i, j int) bool { return pages[i].modified.After(pages[j].modified) })
	if len(pages) > maxSitemapPages {
		pages = pages[:maxSitemapPages]
	}
	urls := make([]string, len(pages))
	for i, p := range pages {
		urls[i] = p.url
	}
	return urls
}

func underPaths(pageURL string, paths []string) bool {
	rest := pageURL
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	slash := strings.Index(rest, "/")
	if slash < 0 {
		return false
	}
	path := rest[slash:]
	for _, prefix := range paths {
		// A page under the section, not the section's index page itself.
		if strings.HasPrefix(path, prefix) && len(path) > len(prefix) {
			return true
		}
	}
	return false
}

// PageItem builds a feed item from a page's own metadata: og:title (or
// <title>), og:description, og:image and article:published_time. It reports
// false when the page has no title or no publish time.
func PageItem(html, pageURL, source string) (FeedItem, bool) {
	meta := map[string]string{}
	for _, tag := range metaTag.FindAllString(html, -1) {
		attrs := attributes(tag)
		key := attrs["property"]
		if key == "" {
			key = attrs["name"]
		}
		if key != "" && meta[key] == "" {
			meta[key] = DecodeHTMLEntities(attrs["content"])
		}
	}
	title := strings.TrimSpace(meta["og:title"])
	if title == "" {
		if match := titleTag.FindStringSubmatch(html); match != nil {
			title = strings.TrimSpace(DecodeHTMLEntities(match[1]))
		}
	}
	published, ok := parseFeedDate(meta["article:published_time"])
	if title == "" || !ok {
		return FeedItem{}, false
	}
	return FeedItem{
		Title:       title,
		URL:         pageURL,
		Source:      source,
		PublishedAt: &published,
		Summary:     summarize(meta["og:description"]),
		ImageURL:    httpURL(meta["og:image"]),
	}, true
}

// FeedItems fetches one feed and returns its items, marked with the feed's
// URL and whether it is a primary source. Release feed titles get the
// project name; a sitemap source is read page by page.
func FeedItems(ctx context.Context, fetcher FeedFetcher, feed content.ApprovedFeed, now time.Time) ([]FeedItem, error) {
	body, _, err := fetcher.FetchText(ctx, feed.URL, "")
	if err != nil {
		return nil, err
	}
	var items []FeedItem
	if len(feed.SitemapPaths) > 0 {
		for _, pageURL := range SitemapPages(body, feed.SitemapPaths, now.Add(-content.PrimaryItemMaxAge)) {
			page, _, err := fetcher.FetchText(ctx, pageURL, feed.Source)
			if err != nil {
				continue
			}
			if item, ok := PageItem(page, pageURL, feed.Source); ok {
				items = append(items, item)
			}
		}
	} else {
		items = ParseFeed(body, feed.Source)
	}
	for i := range items {
		items[i].FeedURL = feed.URL
		items[i].Primary = feed.Primary
		if feed.TitlePrefix != "" {
			// A release feed: titles are bare versions.
			items[i].Prerelease = content.IsPrerelease(items[i].Title)
			if !strings.HasPrefix(items[i].Title, feed.TitlePrefix) {
				items[i].Title = feed.TitlePrefix + " " + items[i].Title
			}
		}
	}
	return items, nil
}
