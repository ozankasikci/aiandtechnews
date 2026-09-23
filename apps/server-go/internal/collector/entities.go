// Package collector fetches the approved news feeds and stores policy-passing
// items as newsroom candidates. It never publishes.
package collector

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	cdataSection  = regexp.MustCompile(`(?s)<!\[CDATA\[(.*?)\]\]>`)
	hexEntity     = regexp.MustCompile(`(?i)&#x([0-9a-f]+);`)
	decimalEntity = regexp.MustCompile(`&#(\d+);`)
	namedEntities = []struct {
		pattern     *regexp.Regexp
		replacement string
	}{
		{regexp.MustCompile(`(?i)&nbsp;`), " "},
		{regexp.MustCompile(`(?i)&amp;`), "&"},
		{regexp.MustCompile(`(?i)&lt;`), "<"},
		{regexp.MustCompile(`(?i)&gt;`), ">"},
		{regexp.MustCompile(`(?i)&quot;`), `"`},
		{regexp.MustCompile(`(?i)&apos;`), "'"},
	}
)

// DecodeHTMLEntities ports decodeHtmlEntities from news-importer.ts, in the
// same order: CDATA, numeric entities, named entities, whitespace collapse.
func DecodeHTMLEntities(value string) string {
	value = cdataSection.ReplaceAllString(value, "$1")
	value = replaceNumericEntities(value, hexEntity, 16)
	value = replaceNumericEntities(value, decimalEntity, 10)
	for _, entity := range namedEntities {
		value = entity.pattern.ReplaceAllLiteralString(value, entity.replacement)
	}
	return strings.Join(strings.Fields(value), " ")
}

func replaceNumericEntities(value string, pattern *regexp.Regexp, base int) string {
	return pattern.ReplaceAllStringFunc(value, func(match string) string {
		code, err := strconv.ParseInt(pattern.FindStringSubmatch(match)[1], base, 32)
		if err != nil || !utf8.ValidRune(rune(code)) {
			return match
		}
		return string(rune(code))
	})
}
