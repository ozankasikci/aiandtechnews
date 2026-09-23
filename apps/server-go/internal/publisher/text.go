// Package publisher turns queued newsroom candidates into published articles.
package publisher

import "unicode/utf16"

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
