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

func TestInsertSubheadings(t *testing.T) {
	body := "<p>1</p><p>2</p><p>3</p><p>4</p><p>5</p><p>6</p><p>7</p>"
	cases := []struct {
		name string
		subs []Subheading
		want string
	}{
		{"none", nil, body},
		{"two well spaced", []Subheading{{3, "Alpha"}, {6, "Beta"}}, "<p>1</p><p>2</p><h2>Alpha</h2><p>3</p><p>4</p><p>5</p><h2>Beta</h2><p>6</p><p>7</p>"},
		{"too early or out of range", []Subheading{{1, "A"}, {2, "B"}, {8, "C"}}, body},
		{"too close to the previous", []Subheading{{3, "A"}, {4, "B"}, {6, "C"}}, "<p>1</p><p>2</p><h2>A</h2><p>3</p><p>4</p><p>5</p><h2>C</h2><p>6</p><p>7</p>"},
		{"unordered input", []Subheading{{6, "B"}, {3, "A"}}, "<p>1</p><p>2</p><h2>A</h2><p>3</p><p>4</p><p>5</p><h2>B</h2><p>6</p><p>7</p>"},
		{"escapes and trims", []Subheading{{3, " R&D grows. "}}, "<p>1</p><p>2</p><h2>R&amp;D grows</h2><p>3</p><p>4</p><p>5</p><p>6</p><p>7</p>"},
		{"drops dashes and tags", []Subheading{{3, "A — B"}, {5, "<b>x</b>"}}, body},
	}
	for _, tc := range cases {
		got := InsertSubheadings(body, tc.subs)
		if got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
		if !SameParagraphs(body, got) {
			t.Errorf("%s: paragraphs changed", tc.name)
		}
	}
}

func TestParseSubheadings(t *testing.T) {
	subs, err := ParseSubheadings("```json\n{\"subheadings\":[{\"beforeParagraph\":4,\"text\":\"Pricing\"}]}\n```")
	if err != nil || len(subs) != 1 || subs[0].BeforeParagraph != 4 || subs[0].Text != "Pricing" {
		t.Fatalf("got %+v, %v", subs, err)
	}
	if _, err := ParseSubheadings("nope"); err == nil {
		t.Fatal("want error for non-JSON")
	}
}

func TestSubheadingPromptNumbersParagraphs(t *testing.T) {
	prompt := SubheadingPrompt("Title here", []string{"First.", "Second."})
	if !strings.Contains(prompt, "1. First.") || !strings.Contains(prompt, "2. Second.") || !strings.Contains(prompt, "Title here") {
		t.Fatalf("prompt missing parts:\n%s", prompt)
	}
}

func TestSameParagraphs(t *testing.T) {
	if !SameParagraphs("<p>a</p><p>b</p>", "<p>a</p><h2>X</h2><p>b</p>") {
		t.Fatal("adding a subheading must keep paragraphs the same")
	}
	if SameParagraphs("<p>a</p><p>b</p>", "<p>a</p><p>c</p>") {
		t.Fatal("changed paragraph must be detected")
	}
}
