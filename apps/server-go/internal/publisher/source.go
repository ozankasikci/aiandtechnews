package publisher

import (
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/collector"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
)

const (
	maxSourceTextLength    = 14_000
	minJSONLDArticleLength = 500
	// MinSourceTextLength is the shortest extracted text accepted for a rewrite.
	MinSourceTextLength = 800
)

var (
	canonicalRelFirst   = regexp.MustCompile(`(?i)<link[^>]*rel=["'][^"']*canonical[^"']*["'][^>]*href=["']([^"']+)["'][^>]*>`)
	canonicalHrefFirst  = regexp.MustCompile(`(?i)<link[^>]*href=["']([^"']+)["'][^>]*rel=["'][^"']*canonical[^"']*["'][^>]*>`)
	ogImagePropFirst    = regexp.MustCompile(`(?i)<meta[^>]*property=["']og:image["'][^>]*content=["']([^"']+)["'][^>]*>`)
	ogImageContentFirst = regexp.MustCompile(`(?i)<meta[^>]*content=["']([^"']+)["'][^>]*property=["']og:image["'][^>]*>`)
	jsonLDScript        = regexp.MustCompile(`(?is)<script[^>]*type=["']application/ld\+json["'][^>]*>(.*?)</script>`)
	htmlComment         = regexp.MustCompile(`(?s)<!--.*?-->`)
	noiseBlock          = regexp.MustCompile(`(?is)<(?:script|style|nav|header|footer|aside|form|iframe|svg|noscript)\b[^>]*>.*?</(?:script|style|nav|header|footer|aside|form|iframe|svg|noscript)>`)
	articleScope        = regexp.MustCompile(`(?is)<article\b[^>]*>(.*?)</article>`)
	mainScope           = regexp.MustCompile(`(?is)<main\b[^>]*>(.*?)</main>`)
	paragraphBlock      = regexp.MustCompile(`(?is)<p\b[^>]*>.*?</p>`)
	boilerplateStart    = regexp.MustCompile(`(?i)^(?:advertisement|subscribe|sign up|read more|all rights reserved)\b`)
	boilerplateMention  = regexp.MustCompile(`(?i)cookie|newsletter preferences|privacy policy`)
)

// ExtractCanonicalURL ports extractCanonicalUrl: the page's canonical link if
// it points at an approved source, otherwise the normalized page URL.
func ExtractCanonicalURL(html, pageURL string) string {
	raw := pageURL
	if match := canonicalRelFirst.FindStringSubmatch(html); match != nil {
		raw = match[1]
	} else if match := canonicalHrefFirst.FindStringSubmatch(html); match != nil {
		raw = match[1]
	}
	raw = collector.DecodeHTMLEntities(raw)
	if base, err := url.Parse(pageURL); err == nil {
		if resolved, err := base.Parse(raw); err == nil {
			if _, approved := content.SourceForURL(resolved.String()); approved {
				if normalized, err := content.NormalizeSourceURL(resolved.String()); err == nil {
					return normalized
				}
			}
		}
	}
	if normalized, err := content.NormalizeSourceURL(pageURL); err == nil {
		return normalized
	}
	return pageURL
}

// ExtractOGImage ports extractOgImage: an absolute http(s) og:image URL or "".
func ExtractOGImage(html, pageURL string) string {
	var raw string
	if match := ogImagePropFirst.FindStringSubmatch(html); match != nil {
		raw = match[1]
	} else if match := ogImageContentFirst.FindStringSubmatch(html); match != nil {
		raw = match[1]
	}
	raw = collector.DecodeHTMLEntities(raw)
	if raw == "" {
		return ""
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return ""
	}
	resolved, err := base.Parse(raw)
	if err != nil || (resolved.Scheme != "http" && resolved.Scheme != "https") {
		return ""
	}
	return resolved.String()
}

// ExtractSourceText ports extractSourceText: a long JSON-LD articleBody when
// present, otherwise the richest paragraph set among <article>, <main> and the
// whole page, with navigation/boilerplate removed; capped at 14000 JS chars.
func ExtractSourceText(html string) string {
	if body := extractJSONLDArticleBody(html); jsLength(body) >= MinSourceTextLength {
		return truncateJS(body, maxSourceTextLength)
	}
	cleaned := noiseBlock.ReplaceAllString(htmlComment.ReplaceAllString(html, " "), " ")
	var scopes []string
	for _, match := range articleScope.FindAllStringSubmatch(cleaned, -1) {
		scopes = append(scopes, match[1])
	}
	if match := mainScope.FindStringSubmatch(cleaned); match != nil {
		scopes = append(scopes, match[1])
	}
	scopes = append(scopes, cleaned)

	best := ""
	for _, scope := range scopes {
		if text := extractParagraphs(scope); jsLength(text) > jsLength(best) {
			best = text
		}
	}
	return best
}

func extractParagraphs(scope string) string {
	seen := map[string]bool{}
	var paragraphs []string
	for _, block := range paragraphBlock.FindAllString(scope, -1) {
		text := collector.DecodeHTMLEntities(content.StripHTML(block))
		if jsLength(text) < 60 || boilerplateStart.MatchString(text) {
			continue
		}
		if boilerplateMention.MatchString(text) && jsLength(text) < 250 {
			continue
		}
		key := strings.ToLower(text)
		if seen[key] {
			continue
		}
		seen[key] = true
		paragraphs = append(paragraphs, text)
		if jsLength(strings.Join(paragraphs, "\n\n")) >= maxSourceTextLength {
			break
		}
	}
	return truncateJS(strings.Join(paragraphs, "\n\n"), maxSourceTextLength)
}

func extractJSONLDArticleBody(html string) string {
	for _, match := range jsonLDScript.FindAllStringSubmatch(html, -1) {
		var value any
		if err := json.Unmarshal([]byte(strings.TrimSpace(match[1])), &value); err != nil {
			continue
		}
		if body := findArticleBody(value); body != "" {
			return collector.DecodeHTMLEntities(content.StripHTML(body))
		}
	}
	return ""
}

// findArticleBody searches depth-first for an articleBody over 500 chars.
// Object keys are visited in sorted order for determinism (Node uses
// insertion order; pages carry at most one articleBody in practice).
func findArticleBody(value any) string {
	switch typed := value.(type) {
	case []any:
		for _, child := range typed {
			if body := findArticleBody(child); body != "" {
				return body
			}
		}
	case map[string]any:
		if body, ok := typed["articleBody"].(string); ok && jsLength(body) > minJSONLDArticleLength {
			return body
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if body := findArticleBody(typed[key]); body != "" {
				return body
			}
		}
	}
	return ""
}
