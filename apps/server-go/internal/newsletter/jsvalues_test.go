package newsletter

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"
)

func istanbulZone(t testing.TB) *time.Location {
	t.Helper()
	location, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		t.Fatal(err)
	}
	return location
}

func TestEscapeHTMLMatchesNode(t *testing.T) {
	for _, vector := range golden(t).Emails.EscapeHTML {
		if got := escapeHTML(vector.Input); got != vector.Output {
			t.Errorf("escapeHTML(%q) = %q, want %q", vector.Input, got, vector.Output)
		}
	}
}

func TestEncodeURIComponentMatchesNode(t *testing.T) {
	for _, vector := range golden(t).Emails.EncodeURIComponent {
		if got := encodeURIComponent(vector.Input); got != vector.Output {
			t.Errorf("encodeURIComponent(%q) = %q, want %q", vector.Input, got, vector.Output)
		}
	}
}

func TestJSONStringMatchesJSONStringify(t *testing.T) {
	var recorded struct {
		Primitives struct {
			Stringified []stringPair `json:"stringified"`
		} `json:"primitives"`
	}
	data, err := os.ReadFile(filepath.Join("testdata", "node-golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &recorded); err != nil {
		t.Fatal(err)
	}
	if len(recorded.Primitives.Stringified) == 0 {
		t.Fatal("no JSON.stringify vectors")
	}
	for _, vector := range recorded.Primitives.Stringified {
		var builder strings.Builder
		writeJSONString(&builder, vector.Input)
		if builder.String() != vector.Output {
			t.Errorf("JSON string of %q = %q, want %q", vector.Input, builder.String(), vector.Output)
		}
	}
	// A lone surrogate (stored by better-sqlite3 as WTF-8) is escaped like
	// JSON.stringify's well-formed output.
	var builder strings.Builder
	writeJSONString(&builder, "x\xed\xa0\xbd")
	if builder.String() != `"x\ud83d"` {
		t.Errorf("lone surrogate = %s", builder.String())
	}
}

func TestSliceUTF16MatchesStringSlice(t *testing.T) {
	for _, vector := range golden(t).Primitives.Truncations {
		want := vector.Slice80
		// encoding/json decodes the lone high surrogate Node produced as
		// U+FFFD; better-sqlite3 stores it as WTF-8 (recorded: 78EDA0BD).
		if strings.HasSuffix(want, "\ufffd") {
			want = strings.TrimSuffix(want, "\ufffd") + "\xed\xa0\xbd"
		}
		if got := sliceUTF16(vector.Input, 80); got != want {
			t.Errorf("sliceUTF16(%q, 80) = %q, want %q", vector.Input, got, want)
		}
	}
	if got := sliceUTF16("\u00e9\U0001f680", 2); got != "\u00e9\xed\xa0\xbd" {
		t.Errorf("split pair = %q", got)
	}
	if got := utf16Length("a\U0001f680\u00e9"); got != 4 {
		t.Errorf("utf16Length = %d", got)
	}
}

func TestParseIntMatchesNode(t *testing.T) {
	for _, vector := range golden(t).Primitives.ParseInts {
		got, ok := parseInt10(vector.Input)
		if vector.Value == nil {
			if ok {
				t.Errorf("parseInt(%q) = %v, want NaN", vector.Input, got)
			}
			continue
		}
		if !ok || got != *vector.Value {
			t.Errorf("parseInt(%q) = %v, %t, want %v", vector.Input, got, ok, *vector.Value)
		}
	}
}

// V8's legacy (non-ISO) date formats are not ported: Go treats them as
// unparseable, so such an article is left out of the digest instead of
// being parsed in the process time zone. No code path writes them.
var legacyOnlyDates = map[string]bool{
	"Sat, 19 Sep 2026 12:00:00 GMT": true,
	"September 19, 2026":            true,
	"19 Sep 2026 12:00":             true,
	"2026-9-19":                     true,
	"10000-01-01":                   true,
}

func TestParseArticleTimestampMatchesNodesDateParse(t *testing.T) {
	local := istanbulZone(t)
	vectors := golden(t).Primitives.DatesParsed
	if len(vectors) < 50 {
		t.Fatalf("only %d date vectors", len(vectors))
	}
	for _, vector := range vectors {
		got, ok := parseArticleTimestamp(vector.Input, local)
		if legacyOnlyDates[vector.Input] {
			if ok {
				t.Errorf("legacy date %q parsed as %d, want unparseable in Go", vector.Input, got)
			}
			continue
		}
		if vector.MS == nil {
			if ok {
				t.Errorf("Date.parse(%q) = %d, want NaN", vector.Input, got)
			}
			continue
		}
		if !ok || got != *vector.MS {
			t.Errorf("Date.parse(%q) = %d, %t, want %d", vector.Input, got, ok, *vector.MS)
		}
	}
}

func TestIstanbulEditionKeysMatchIntl(t *testing.T) {
	for _, vector := range golden(t).Primitives.IstanbulDates {
		instant, err := time.Parse(time.RFC3339Nano, vector.Instant)
		if err != nil {
			t.Fatal(err)
		}
		if got := editionKey(instant); got != vector.Edition {
			t.Errorf("editionKey(%s) = %q, want %q", vector.Instant, got, vector.Edition)
		}
	}
}

func TestISOTimestampMatchesToISOString(t *testing.T) {
	for instant, want := range map[time.Time]string{
		time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC):                         "2026-09-20T12:00:00.000Z",
		time.Date(2026, 9, 20, 15, 4, 5, 999_999_999, istanbulZone(t)):        "2026-09-20T12:04:05.999Z",
		time.Date(2026, 1, 2, 3, 4, 5, 6_000_000, time.FixedZone("x", -3600)): "2026-01-02T04:04:05.006Z",
	} {
		if got := isoTimestamp(instant); got != want {
			t.Errorf("isoTimestamp(%v) = %q, want %q", instant, got, want)
		}
	}
}

type caseMappings struct {
	UnicodeVersion string           `json:"unicodeVersion"`
	Upper          [][2]any         `json:"upper"`
	Lower          [][2]any         `json:"lower"`
	Contextual     []contextualCase `json:"contextual"`
}

type contextualCase struct {
	Input string `json:"input"`
	Lower string `json:"lower"`
}

func loadCaseMappings(t *testing.T) caseMappings {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "node-case-mappings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mappings caseMappings
	if err := json.Unmarshal(data, &mappings); err != nil {
		t.Fatal(err)
	}
	return mappings
}

// newerThanGo reports mappings that involve code points Go's Unicode tables
// (15.0) do not assign yet; Node's ICU tables are newer (recorded
// unicodeVersion). Such characters cannot be mapped by Go's unicode package.
func newerThanGo(values ...string) bool {
	for _, value := range values {
		for _, r := range value {
			if !unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S, unicode.Z, unicode.Cc, unicode.Cf, unicode.Co) {
				return true
			}
		}
	}
	return false
}

func TestJavaScriptCaseMappingMatchesNodeForEveryCodePoint(t *testing.T) {
	mappings := loadCaseMappings(t)
	check := func(name string, table [][2]any, convert func(string) string) {
		mismatches := 0
		for _, entry := range table {
			r := rune(entry[0].(float64))
			want := entry[1].(string)
			if got := convert(string(r)); got != want {
				if newerThanGo(string(r), want) {
					continue
				}
				mismatches++
				if mismatches < 20 {
					t.Errorf("%s(U+%04X) = %q, want %q", name, r, got, want)
				}
			}
		}
		if mismatches > 0 {
			t.Errorf("%s: %d mismatches", name, mismatches)
		}
	}
	check("toUpperCase", mappings.Upper, jsToUpper)
	check("toLowerCase", mappings.Lower, jsToLower)
	// Code points absent from Node's tables map to themselves in Node.
	mapped := map[rune]bool{}
	for _, entry := range mappings.Upper {
		mapped[rune(entry[0].(float64))] = true
	}
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if r >= 0xd800 && r <= 0xdfff || mapped[r] {
			continue
		}
		if got := jsToUpper(string(r)); got != string(r) {
			t.Errorf("toUpperCase(U+%04X) = %q, want unchanged", r, got)
		}
	}
	for _, vector := range mappings.Contextual {
		if got := jsToLower(vector.Input); got != vector.Lower {
			t.Errorf("toLowerCase(%q) = %q, want %q (Final_Sigma)", vector.Input, got, vector.Lower)
		}
	}
}

func TestJavaScriptWhitespaceTrim(t *testing.T) {
	if got := jsTrim("\u00a0\u2028 a b\ufeff\t"); got != "a b" {
		t.Errorf("jsTrim = %q", got)
	}
	if got := jsTrim("\u200ba\u200b"); got != "\u200ba\u200b" {
		t.Errorf("zero width space is not whitespace: %q", got)
	}
}

func TestRoundHalfUpMatchesMathRound(t *testing.T) {
	for input, want := range map[float64]float64{0.5: 1, 1.5: 2, 2.5: 3, 0.49999999999999994: 0, 1.4999: 1, 109.0 / 220: 0} {
		if got := mathRound(input); got != want {
			t.Errorf("Math.round(%v) = %v, want %v", input, got, want)
		}
	}
	if !math.IsNaN(mathRound(math.NaN())) {
		t.Error("Math.round(NaN) is NaN")
	}
}

// The dashboard editor sends published_at as toISOString() output ("...Z").
// Node's Date.parse and Go read it as the same instant whatever the process
// time zone, so the digest window no longer depends on the server's TZ.
func TestDashboardISOTimestampsAreTheSameInstantInEveryZone(t *testing.T) {
	const submitted = "2026-09-20T01:30:00.000Z"
	var recorded *int64
	for _, vector := range golden(t).Primitives.DatesParsed {
		if vector.Input == "2026-09-19T12:00:00.000Z" {
			recorded = vector.MS
		}
	}
	if recorded == nil || *recorded != time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("Node's Date.parse of an ISO Z timestamp = %v", recorded)
	}
	want := time.Date(2026, 9, 20, 1, 30, 0, 0, time.UTC).UnixMilli()
	for _, name := range []string{"UTC", "Europe/Istanbul", "America/New_York", "Asia/Kolkata"} {
		zone, err := time.LoadLocation(name)
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := parseArticleTimestamp(submitted, zone); !ok || got != want {
			t.Errorf("%s: %d, %t, want %d", name, got, ok, want)
		}
	}
}
