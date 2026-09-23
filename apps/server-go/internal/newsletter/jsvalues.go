package newsletter

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	// Edition keys are Europe/Istanbul dates; embed the zone database so they
	// never depend on the host's zoneinfo.
	_ "time/tzdata"
	"unicode"
	"unicode/utf8"
)

// This file ports the JavaScript semantics the Node newsletter relies on
// implicitly: String.prototype.trim/toLowerCase/toUpperCase/slice and the
// UTF-16 length, \s, encodeURIComponent, JSON.stringify for strings,
// parseInt(value, 10), Math.round, Date.prototype.toISOString, Date.parse,
// and Intl's Europe/Istanbul calendar date. Each is pinned against vectors
// recorded from Node (testdata/node-golden.json).

// isJSWhitespace is ECMAScript WhiteSpace plus LineTerminator: the set that
// \s matches and String.prototype.trim removes.
func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', '\u00a0', '\u1680', '\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff':
		return true
	}
	return r >= '\u2000' && r <= '\u200a'
}

// jsTrim is String.prototype.trim.
func jsTrim(s string) string { return strings.TrimFunc(s, isJSWhitespace) }

// nextUnit decodes the next character of s as JavaScript sees it. It
// returns the character, its length in UTF-16 code units, and its size in
// bytes. A lone surrogate stored as WTF-8 by better-sqlite3 (ED A0..BF xx)
// is one code unit; any other invalid byte is U+FFFD, as better-sqlite3
// decodes it.
func nextUnit(s string) (rune, int, int) {
	if len(s) >= 3 && s[0] == 0xed && s[1] >= 0xa0 && s[1] <= 0xbf && s[2] >= 0x80 && s[2] <= 0xbf {
		return 0xd000 | rune(s[1]&0x3f)<<6 | rune(s[2]&0x3f), 1, 3
	}
	r, size := utf8.DecodeRuneInString(s)
	if r >= 0x10000 {
		return r, 2, size
	}
	return r, 1, size
}

// utf16Length is String.prototype.length.
func utf16Length(s string) int {
	length := 0
	for len(s) > 0 {
		_, units, size := nextUnit(s)
		length += units
		s = s[size:]
	}
	return length
}

// sliceUTF16 is String.prototype.slice(0, n). When the cut splits a
// surrogate pair, JavaScript keeps the lone high surrogate, which
// better-sqlite3 writes as its three-byte WTF-8 form; so does sliceUTF16.
func sliceUTF16(s string, n int) string {
	units, offset := 0, 0
	for offset < len(s) {
		r, width, size := nextUnit(s[offset:])
		if units+width > n {
			if width == 2 && units+1 == n {
				high := 0xd800 + (r-0x10000)>>10
				return s[:offset] + string([]byte{0xe0 | byte(high>>12), 0x80 | byte(high>>6)&0x3f, 0x80 | byte(high)&0x3f})
			}
			break
		}
		units += width
		offset += size
	}
	return s[:offset]
}

// escapeHTML is the Node email module's escapeHtml.
var escapeHTML = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#039;").Replace

// encodeURIComponent is the JavaScript global of the same name. Invalid
// UTF-8 and lone surrogates are encoded as U+FFFD (JavaScript would throw
// URIError for a lone surrogate).
func encodeURIComponent(s string) string {
	const hex = "0123456789ABCDEF"
	var builder strings.Builder
	for len(s) > 0 {
		r, _, size := nextUnit(s)
		s = s[size:]
		if r < utf8.RuneSelf && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.!~*'()", r)) {
			builder.WriteByte(byte(r))
			continue
		}
		if r >= 0xd800 && r <= 0xdfff {
			r = utf8.RuneError
		}
		var encoded [utf8.UTFMax]byte
		for _, b := range encoded[:utf8.EncodeRune(encoded[:], r)] {
			builder.WriteByte('%')
			builder.WriteByte(hex[b>>4])
			builder.WriteByte(hex[b&0x0f])
		}
	}
	return builder.String()
}

// writeJSONString writes s the way JSON.stringify serializes a string: only
// '"', '\\', and control characters are escaped (U+2028, U+2029, '<', '>',
// and '&' are not, unlike encoding/json), and lone surrogates become \udxxx.
func writeJSONString(builder *strings.Builder, s string) {
	const hex = "0123456789abcdef"
	builder.WriteByte('"')
	for len(s) > 0 {
		r, _, size := nextUnit(s)
		raw := s[:size]
		s = s[size:]
		switch {
		case r == '"':
			builder.WriteString(`\"`)
		case r == '\\':
			builder.WriteString(`\\`)
		case r == '\b':
			builder.WriteString(`\b`)
		case r == '\f':
			builder.WriteString(`\f`)
		case r == '\n':
			builder.WriteString(`\n`)
		case r == '\r':
			builder.WriteString(`\r`)
		case r == '\t':
			builder.WriteString(`\t`)
		case r < 0x20 || r >= 0xd800 && r <= 0xdfff:
			builder.WriteString(`\u`)
			builder.WriteByte(hex[r>>12&0xf])
			builder.WriteByte(hex[r>>8&0xf])
			builder.WriteByte(hex[r>>4&0xf])
			builder.WriteByte(hex[r&0xf])
		case r == utf8.RuneError && size == 1:
			builder.WriteRune(utf8.RuneError)
		default:
			builder.WriteString(raw)
		}
	}
	builder.WriteByte('"')
}

// jsToUpper is String.prototype.toUpperCase (no locale): Go's single-rune
// mapping plus SpecialCasing's unconditional multi-character mappings.
func jsToUpper(s string) string {
	var builder strings.Builder
	for _, r := range s {
		if special, ok := upperSpecialCasing[r]; ok {
			builder.WriteString(special)
			continue
		}
		builder.WriteRune(unicode.ToUpper(r))
	}
	return builder.String()
}

// jsToLower is String.prototype.toLowerCase (no locale): Go's single-rune
// mapping, U+0130 to "i\u0307", and the Final_Sigma rule for U+03A3.
func jsToLower(s string) string {
	runes := []rune(s)
	var builder strings.Builder
	for index, r := range runes {
		switch r {
		case '\u0130':
			builder.WriteString("i\u0307")
		case '\u03a3':
			if finalSigma(runes, index) {
				builder.WriteRune('\u03c2')
			} else {
				builder.WriteRune('\u03c3')
			}
		default:
			builder.WriteRune(unicode.ToLower(r))
		}
	}
	return builder.String()
}

// finalSigma implements the Final_Sigma casing context of Unicode 3.13: the
// sigma follows a cased letter (skipping case-ignorable characters) and is
// not followed by one.
func finalSigma(runes []rune, index int) bool {
	before := false
	for i := index - 1; i >= 0; i-- {
		if cased(runes[i]) {
			before = true
			break
		}
		if !caseIgnorable(runes[i]) {
			break
		}
	}
	if !before {
		return false
	}
	for i := index + 1; i < len(runes); i++ {
		if cased(runes[i]) {
			return false
		}
		if !caseIgnorable(runes[i]) {
			break
		}
	}
	return true
}

func cased(r rune) bool {
	return unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r) ||
		unicode.Is(unicode.Other_Lowercase, r) || unicode.Is(unicode.Other_Uppercase, r)
}

// caseIgnorable is Unicode's Case_Ignorable: Mn, Me, Cf, Lm, Sk, and the
// Word_Break MidLetter, MidNumLet, and Single_Quote characters.
func caseIgnorable(r rune) bool {
	switch r {
	case '\'', '.', ':', '\u00b7', '\u0387', '\u055f', '\u05f4', '\u2018', '\u2019', '\u2024', '\u2027',
		'\ufe13', '\ufe52', '\ufe55', '\uff07', '\uff0e', '\uff1a':
		return true
	}
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Lm, unicode.Sk)
}

// parseInt10 is parseInt(value, 10); ok is false for NaN.
func parseInt10(value string) (float64, bool) {
	value = strings.TrimLeftFunc(value, isJSWhitespace)
	sign := 1.0
	if value != "" && (value[0] == '+' || value[0] == '-') {
		if value[0] == '-' {
			sign = -1
		}
		value = value[1:]
	}
	end := 0
	for end < len(value) && value[end] >= '0' && value[end] <= '9' {
		end++
	}
	if end == 0 {
		return math.NaN(), false
	}
	parsed, _ := strconv.ParseFloat(value[:end], 64)
	return sign * parsed, true
}

// mathRound is Math.round: the nearest integer, ties toward +Infinity.
func mathRound(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	floor := math.Floor(x)
	if x-floor >= 0.5 {
		return floor + 1
	}
	return floor
}

// isoTimestamp is Date.prototype.toISOString for years 0000-9999.
func isoTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// istanbul is the publication time zone. The embedded time/tzdata makes
// loading it infallible.
var istanbul = func() *time.Location {
	location, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		panic(err)
	}
	return location
}()

// editionKey is Node's formatIstanbulDate: the YYYY-MM-DD calendar date in
// Europe/Istanbul (Intl.DateTimeFormat("en-CA", {timeZone: "Europe/Istanbul"})).
func editionKey(now time.Time) string {
	return now.In(istanbul).Format("2006-01-02")
}

var sqliteTimestamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`)

// parseArticleTimestamp is Node's parseArticleTimestamp: SQLite's
// "YYYY-MM-DD HH:MM:SS" is read as UTC, anything else goes to Date.parse.
// It returns milliseconds since the epoch; ok is false for NaN. local is the
// zone V8 uses for date-times without an offset (the process time zone).
func parseArticleTimestamp(value string, local *time.Location) (int64, bool) {
	if sqliteTimestamp.MatchString(value) {
		value = strings.Replace(value, " ", "T", 1) + "Z"
	}
	return dateParse(value, local)
}

// dateParse is the part of V8's Date.parse that article timestamps use: the
// ECMAScript date-time string format and V8's accepted variations of it
// (lowercase t/z, a space instead of T, more than three fraction digits,
// offsets without a colon, hour 24, days 29-31 that overflow into the next
// month). Date-only forms are UTC; date-times without an offset are local.
// V8's legacy fallback formats ("Sep 19 2026", RFC 2822 dates, 1-digit
// months, 5-digit years without a sign) are not ported and report NaN.
func dateParse(value string, local *time.Location) (int64, bool) {
	p := dateScanner{s: value}
	year, ok := p.year()
	if !ok {
		return 0, false
	}
	month, day := 1, 1
	fullDate := false
	if p.consume('-') {
		if month, ok = p.fixed(2); !ok {
			return 0, false
		}
		if p.consume('-') {
			if day, ok = p.fixed(2); !ok {
				return 0, false
			}
			fullDate = true
		}
	}
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return 0, false
	}
	utcDate := func(hour, minute, second, millisecond int) int64 {
		return time.Date(year, time.Month(month), day, hour, minute, second, millisecond*int(time.Millisecond), time.UTC).UnixMilli()
	}
	if p.done() {
		return utcDate(0, 0, 0, 0), true
	}
	if fullDate && (p.peek() == 'Z' || p.peek() == 'z') && len(p.s)-p.i == 1 {
		return utcDate(0, 0, 0, 0), true
	}
	if separator := p.next(); separator != 'T' && separator != 't' && separator != ' ' {
		return 0, false
	}
	hour, ok := p.fixed(2)
	if !ok || !p.consume(':') {
		return 0, false
	}
	minute, ok := p.fixed(2)
	if !ok {
		return 0, false
	}
	second, millisecond := 0, 0
	if p.consume(':') {
		if second, ok = p.fixed(2); !ok {
			return 0, false
		}
		if p.consume('.') {
			digits := p.run()
			if digits == "" {
				return 0, false
			}
			digits = (digits + "00")[:3]
			millisecond, _ = strconv.Atoi(digits)
		}
	}
	if hour > 24 || minute > 59 || second > 59 || hour == 24 && (minute != 0 || second != 0 || millisecond != 0) {
		return 0, false
	}
	if p.done() {
		return time.Date(year, time.Month(month), day, hour, minute, second, millisecond*int(time.Millisecond), local).UnixMilli(), true
	}
	switch sign := p.next(); sign {
	case 'Z', 'z':
		if !p.done() {
			return 0, false
		}
		return utcDate(hour, minute, second, millisecond), true
	case '+', '-':
		offsetHours, ok := p.fixed(2)
		if !ok {
			return 0, false
		}
		p.consume(':')
		offsetMinutes, ok := p.fixed(2)
		if !ok || !p.done() || offsetHours > 23 || offsetMinutes > 59 {
			return 0, false
		}
		offset := int64(offsetHours*60+offsetMinutes) * int64(time.Minute/time.Millisecond)
		if sign == '-' {
			offset = -offset
		}
		return utcDate(hour, minute, second, millisecond) - offset, true
	}
	return 0, false
}

type dateScanner struct {
	s string
	i int
}

func (p *dateScanner) done() bool { return p.i == len(p.s) }

func (p *dateScanner) peek() byte {
	if p.done() {
		return 0
	}
	return p.s[p.i]
}

func (p *dateScanner) next() byte {
	c := p.peek()
	if !p.done() {
		p.i++
	}
	return c
}

func (p *dateScanner) consume(c byte) bool {
	if p.peek() == c && !p.done() {
		p.i++
		return true
	}
	return false
}

// fixed reads exactly n ASCII digits.
func (p *dateScanner) fixed(n int) (int, bool) {
	if len(p.s)-p.i < n {
		return 0, false
	}
	value := 0
	for _, c := range []byte(p.s[p.i : p.i+n]) {
		if c < '0' || c > '9' {
			return 0, false
		}
		value = value*10 + int(c-'0')
	}
	p.i += n
	return value, true
}

// run reads one or more ASCII digits.
func (p *dateScanner) run() string {
	start := p.i
	for !p.done() && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
		p.i++
	}
	return p.s[start:p.i]
}

// year reads YYYY or a signed six-digit \u00b1YYYYYY year.
func (p *dateScanner) year() (int, bool) {
	if c := p.peek(); c == '+' || c == '-' {
		p.i++
		year, ok := p.fixed(6)
		if c == '-' {
			year = -year
		}
		return year, ok
	}
	return p.fixed(4)
}
