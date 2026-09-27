package content

import (
	"strings"
	"testing"
)

func TestTidySubheadings(t *testing.T) {
	long := strings.Repeat("x", 81)
	cases := []struct{ name, in, want string }{
		{"keeps well placed", "<p>a</p><h2>One</h2><p>b</p><h2>Two</h2><p>c</p>", "<p>a</p><h2>One</h2><p>b</p><h2>Two</h2><p>c</p>"},
		{"drops leading", "<h2>Lead</h2><p>a</p><p>b</p>", "<p>a</p><p>b</p>"},
		{"drops trailing", "<p>a</p><p>b</p><h2>End</h2>", "<p>a</p><p>b</p>"},
		{"drops second of a pair", "<p>a</p><h2>One</h2><h2>Two</h2><p>b</p>", "<p>a</p><h2>One</h2><p>b</p>"},
		{"keeps three at most", "<p>a</p><h2>1</h2><p>b</p><h2>2</h2><p>c</p><h2>3</h2><p>d</p><h2>4</h2><p>e</p>", "<p>a</p><h2>1</h2><p>b</p><h2>2</h2><p>c</p><h2>3</h2><p>d</p><p>e</p>"},
		{"drops too long or empty", "<p>a</p><h2>" + long + "</h2><p>b</p><h2> </h2><p>c</p>", "<p>a</p><p>b</p><p>c</p>"},
		{"no subheadings", "<p>a</p><p>b</p>", "<p>a</p><p>b</p>"},
		{"keeps whitespace between blocks", "<p>a</p>\n<h2>One</h2>\n<p>b</p>", "<p>a</p>\n<h2>One</h2>\n<p>b</p>"},
	}
	for _, tc := range cases {
		if got := TidySubheadings(tc.in); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
}
