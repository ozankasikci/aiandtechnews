// Package publisher turns queued newsroom candidates into published articles.
package publisher

import "unicode/utf16"

// jsLength counts UTF-16 code units, matching JavaScript's String.length,
// so length thresholds behave exactly like the Node importer.
func jsLength(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// truncateJS returns the longest prefix of s whose JavaScript length is at most limit.
func truncateJS(s string, limit int) string {
	n := 0
	for i, r := range s {
		width := utf16.RuneLen(r)
		if n+width > limit {
			return s[:i]
		}
		n += width
	}
	return s
}
