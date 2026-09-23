package collector

import (
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
)

const (
	maxItemsPerFeed = 30
	maxSummaryRunes = 400
)

// FeedItem is one parsed feed entry with a normalized source URL.
type FeedItem struct {
	Title       string
	URL         string
	Source      string
	FeedURL     string
	PublishedAt *time.Time
	Summary     string
	ImageURL    string
}

// The tag regexes below match up to the first ">" via [^>]*, which fails
// safe: a ">" inside a quoted attribute value ends the match early and the
// tag is dropped or truncated rather than over-matched into surrounding
// markup, so malformed input yields a missed field, not a corrupted one.
var (
	itemBlock    = regexp.MustCompile(`(?is)<(?:item|entry)\b.*?</(?:item|entry)>`)
	atomLink     = regexp.MustCompile(`(?i)<link\b[^>]*\bhref=["']([^"']+)["'][^>]*>`)
	mediaTag     = regexp.MustCompile(`(?i)<media:(?:content|thumbnail)\b[^>]*>`)
	enclosureTag = regexp.MustCompile(`(?i)<enclosure\b[^>]*>`)
	imgTag       = regexp.MustCompile(`(?i)<img\b[^>]*>`)
	attribute    = regexp.MustCompile(`(?i)([a-z][a-z0-9:_-]*)\s*=\s*(?:"([^"]*)"|'([^']*)')`)

	xmlValuePatterns = compileValuePatterns("title", "link", "pubDate", "published", "updated",
		"description", "summary", "content:encoded", "content")
)

type valuePattern struct{ cdata, regular *regexp.Regexp }

func compileValuePatterns(names ...string) map[string]valuePattern {
	patterns := make(map[string]valuePattern, len(names))
	for _, name := range names {
		quoted := regexp.QuoteMeta(name)
		patterns[name] = valuePattern{
			cdata:   regexp.MustCompile(`(?is)<` + quoted + `[^>]*><!\[CDATA\[(.*?)\]\]></` + quoted + `>`),
			regular: regexp.MustCompile(`(?is)<` + quoted + `[^>]*>(.*?)</` + quoted + `>`),
		}
	}
	return patterns
}

// ParseFeed ports parseFeed from news-importer.ts (regex-based so malformed
// feeds still yield items; first 30 items) and additionally extracts a plain
// summary and a lead image for the review app.
func ParseFeed(xml, source string) []FeedItem {
	blocks := itemBlock.FindAllString(xml, maxItemsPerFeed)
	items := make([]FeedItem, 0, len(blocks))
	for _, block := range blocks {
		title := extractXMLValue(block, "title")
		link := extractXMLValue(block, "link")
		if link == "" {
			if match := atomLink.FindStringSubmatch(block); match != nil {
				link = match[1]
			}
		}
		link = decodeHTMLEntities(link)
		if title == "" || link == "" {
			continue
		}
		normalized, err := content.NormalizeSourceURL(link)
		if err != nil {
			continue
		}
		item := FeedItem{
			Title:    title,
			URL:      normalized,
			Source:   source,
			Summary:  summarize(extractXMLValue(block, "description", "summary", "content:encoded", "content")),
			ImageURL: extractImageURL(block),
		}
		if published, ok := parseFeedDate(extractXMLValue(block, "pubDate", "published", "updated")); ok {
			item.PublishedAt = &published
		}
		items = append(items, item)
	}
	return items
}

func extractXMLValue(block string, names ...string) string {
	for _, name := range names {
		patterns := xmlValuePatterns[name]
		if match := patterns.cdata.FindStringSubmatch(block); match != nil && match[1] != "" {
			return decodeHTMLEntities(match[1])
		}
		if match := patterns.regular.FindStringSubmatch(block); match != nil && match[1] != "" {
			return decodeHTMLEntities(content.StripHTML(match[1]))
		}
	}
	return ""
}

// summarize turns a feed description into at most maxSummaryRunes of plain text.
// Decoding entities here on top of extractXMLValue's decode is intentional:
// descriptions often carry entity-escaped HTML (e.g. "&lt;img src=...&gt;"),
// so the markup itself only becomes visible, and strippable, after a first pass.
func summarize(value string) string {
	text := decodeHTMLEntities(content.StripHTML(value))
	runes := []rune(text)
	if len(runes) <= maxSummaryRunes {
		return text
	}
	cut := string(runes[:maxSummaryRunes])
	if space := strings.LastIndex(cut, " "); space > len(cut)/2 {
		cut = cut[:space]
	}
	return strings.TrimRight(cut, " ,;:.-") + "…"
}

// extractImageURL prefers media:content/thumbnail, then an image enclosure,
// then the first <img> in the (possibly entity-escaped) item markup.
func extractImageURL(block string) string {
	for _, tag := range mediaTag.FindAllString(block, -1) {
		attrs := attributes(tag)
		if medium := strings.ToLower(attrs["medium"]); medium != "" && medium != "image" {
			continue
		}
		if kind := strings.ToLower(attrs["type"]); kind != "" && !strings.HasPrefix(kind, "image/") {
			continue
		}
		if imageURL := httpURL(attrs["url"]); imageURL != "" {
			return imageURL
		}
	}
	for _, tag := range enclosureTag.FindAllString(block, -1) {
		attrs := attributes(tag)
		if strings.HasPrefix(strings.ToLower(attrs["type"]), "image/") {
			if imageURL := httpURL(attrs["url"]); imageURL != "" {
				return imageURL
			}
		}
	}
	for _, tag := range imgTag.FindAllString(html.UnescapeString(block), -1) {
		if imageURL := httpURL(attributes(tag)["src"]); imageURL != "" {
			return imageURL
		}
	}
	return ""
}

func attributes(tag string) map[string]string {
	attrs := map[string]string{}
	for _, match := range attribute.FindAllStringSubmatch(tag, -1) {
		value := match[2]
		if value == "" {
			value = match[3]
		}
		attrs[strings.ToLower(match[1])] = html.UnescapeString(value)
	}
	return attrs
}

func httpURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}
	return parsed.String()
}

var feedDateLayouts = []string{
	time.RFC1123Z, time.RFC1123, time.RFC3339Nano, time.RFC3339,
	"Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST",
	"Mon, 2 Jan 2006 15:04 -0700",
	"2 Jan 2006 15:04:05 -0700", "2006-01-02T15:04:05-0700",
	"2006-01-02 15:04:05", "2006-01-02",
}

// namedZoneOffsets maps RFC 822 zone abbreviations (case-insensitive) to
// their numeric UTC offset. time.Parse accepts an arbitrary letters-only
// zone abbreviation against a "MST"-style layout but silently treats it as
// UTC+0, so named zones must be rewritten to a numeric offset before
// parsing rather than relying on Go's built-in handling.
var namedZoneOffsets = map[string]string{
	"UT": "+0000", "UTC": "+0000", "GMT": "+0000", "Z": "+0000",
	"EST": "-0500", "EDT": "-0400",
	"CST": "-0600", "CDT": "-0500",
	"MST": "-0700", "MDT": "-0600",
	"PST": "-0800", "PDT": "-0700",
}

// normalizeNamedZone replaces a trailing RFC 822 zone abbreviation with its
// numeric offset, only when it is the last whitespace-separated token.
func normalizeNamedZone(value string) string {
	idx := strings.LastIndex(value, " ")
	if idx == -1 {
		return value
	}
	last := value[idx+1:]
	if offset, ok := namedZoneOffsets[strings.ToUpper(last)]; ok {
		return value[:idx+1] + offset
	}
	return value
}

func parseFeedDate(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	value = normalizeNamedZone(value)
	for _, layout := range feedDateLayouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}
