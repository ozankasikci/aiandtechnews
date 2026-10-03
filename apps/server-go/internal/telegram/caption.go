package telegram

import (
	"html"
	"net/url"
	"strings"
	"unicode/utf16"
)

const (
	// MaxCaptionLength is Telegram's photo caption limit, in UTF-16 code
	// units. The whole HTML text is counted, tags and entities included,
	// which is stricter than Telegram (it counts the text after parsing).
	MaxCaptionLength = 1024
	// maxTLDRPoints is how many TL;DR points a post shows at most.
	maxTLDRPoints = 3
	ellipsis      = "…"
)

// Caption is the post's HTML text (parse_mode=HTML): the bold title, the
// "why it matters" line (or the excerpt for articles without a summary), up
// to three TL;DR points and a tracked "Read more" link. It never exceeds
// MaxCaptionLength: TL;DR points are dropped from the end first, then the
// summary line is shortened with "…"; the link is never cut.
func Caption(post Post, siteURL string) string {
	title := strings.TrimSpace(post.Title)
	summary := strings.TrimSpace(post.WhyItMatters)
	if summary == "" {
		summary = strings.TrimSpace(post.Excerpt)
	}
	points := post.TLDR
	if len(points) > maxTLDRPoints {
		points = points[:maxTLDRPoints]
	}
	link := ArticleURL(siteURL, post.Slug)

	text := buildCaption(title, summary, points, link)
	for captionLength(text) > MaxCaptionLength && len(points) > 0 {
		points = points[:len(points)-1]
		text = buildCaption(title, summary, points, link)
	}
	if captionLength(text) <= MaxCaptionLength {
		return text
	}
	// Shorten the summary, then (for an absurd title) the title itself.
	summary = shorten(summary, func(s string) bool {
		return captionLength(buildCaption(title, s, nil, link)) <= MaxCaptionLength
	})
	text = buildCaption(title, summary, nil, link)
	if captionLength(text) <= MaxCaptionLength {
		return text
	}
	title = shorten(title, func(s string) bool {
		return captionLength(buildCaption(s, "", nil, link)) <= MaxCaptionLength
	})
	return buildCaption(title, "", nil, link)
}

// ArticleURL is the article's site link with the channel's UTM parameters.
func ArticleURL(siteURL, slug string) string {
	return strings.TrimRight(siteURL, "/") + "/article/" + url.PathEscape(slug) +
		"?utm_source=telegram&utm_medium=social&utm_campaign=channel"
}

func buildCaption(title, summary string, points []string, link string) string {
	var parts []string
	if title != "" {
		parts = append(parts, "<b>"+escape(title)+"</b>")
	}
	if summary != "" {
		parts = append(parts, escape(summary))
	}
	if len(points) > 0 {
		lines := make([]string, 0, len(points))
		for _, point := range points {
			lines = append(lines, "• "+escape(point))
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}
	parts = append(parts, `<a href="`+html.EscapeString(link)+`">Read more</a>`)
	return strings.Join(parts, "\n\n")
}

// escape is the HTML escaping Telegram's HTML parse mode requires: &, < and
// > (quotes need no escaping outside attributes).
func escape(text string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
}

// shorten returns the longest rune prefix of text, trimmed and followed by
// "…", for which fits holds, or "" when none does.
func shorten(text string, fits func(string) bool) string {
	runes := []rune(text)
	low, high := 0, len(runes) // fits(prefix of length low) is assumed true for 0.
	for low < high {
		middle := (low + high + 1) / 2
		if fits(strings.TrimSpace(string(runes[:middle])) + ellipsis) {
			low = middle
		} else {
			high = middle - 1
		}
	}
	prefix := strings.TrimSpace(string(runes[:low]))
	if prefix == "" {
		return ""
	}
	return prefix + ellipsis
}

func captionLength(text string) int {
	return len(utf16.Encode([]rune(text)))
}
