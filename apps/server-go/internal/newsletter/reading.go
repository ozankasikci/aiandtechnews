package newsletter

import (
	"regexp"
	"strings"
)

// wordsPerMinute is Node's WORDS_PER_MINUTE (apps/server/src/newsletter/reading-time.ts).
const wordsPerMinute = 220

var (
	htmlTag    = regexp.MustCompile(`<[^>]+>`)
	nbspEntity = regexp.MustCompile(`&[nN][bB][sS][pP];`)
	// JavaScript's /i folds ASCII letters only; (?i) in Go would also fold
	// the Kelvin sign and the long s, so the classes are spelled out.
	otherEntity = regexp.MustCompile(`&[a-zA-Z]+;|&#[0-9]+;`)
)

// readingMinutes is Node's readingMinutes: tags (and script/style bodies)
// are removed before counting whitespace-separated words, at 220 words per
// minute, rounded like Math.round, and never less than 1. An empty or NULL
// body is 1 minute.
func readingMinutes(html string) int {
	if html == "" {
		return 1
	}
	text := stripScriptAndStyle(html)
	text = htmlTag.ReplaceAllString(text, " ")
	text = nbspEntity.ReplaceAllString(text, " ")
	text = otherEntity.ReplaceAllString(text, "")
	text = jsTrim(text)
	if text == "" {
		return 1
	}
	words := len(strings.FieldsFunc(text, isJSWhitespace))
	return int(max(1, mathRound(float64(words)/wordsPerMinute)))
}

// stripScriptAndStyle replaces every match of Node's
// /<(script|style)\b[^>]*>[\s\S]*?<\/\1>/gi with a space. RE2 has no
// backreferences, so the scan is explicit: an opening tag whose name is
// followed by a non-word character runs to the next ">", then lazily to
// the first "</name>" with the same name (case-insensitively); without one,
// the regex fails at that position and scanning resumes one byte later.
func stripScriptAndStyle(html string) string {
	var builder strings.Builder
	i := 0
	for i < len(html) {
		if end, ok := scriptOrStyleAt(html, i); ok {
			builder.WriteByte(' ')
			i = end
			continue
		}
		builder.WriteByte(html[i])
		i++
	}
	return builder.String()
}

func scriptOrStyleAt(html string, start int) (int, bool) {
	if html[start] != '<' {
		return 0, false
	}
	for _, name := range []string{"script", "style"} {
		nameEnd := start + 1 + len(name)
		if nameEnd > len(html) || !asciiEqualFold(html[start+1:nameEnd], name) {
			continue
		}
		if nameEnd < len(html) && isWordByte(html[nameEnd]) {
			return 0, false
		}
		open := strings.IndexByte(html[nameEnd:], '>')
		if open < 0 {
			return 0, false
		}
		bodyStart := nameEnd + open + 1
		closing := "</" + name + ">"
		for i := bodyStart; i+len(closing) <= len(html); i++ {
			if asciiEqualFold(html[i:i+len(closing)], closing) {
				return i + len(closing), true
			}
		}
		return 0, false
	}
	return 0, false
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

// asciiEqualFold compares case-insensitively over ASCII only, like /i
// without the u flag for these ASCII patterns.
func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if x >= 'A' && x <= 'Z' {
			x += 'a' - 'A'
		}
		if y >= 'A' && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}
