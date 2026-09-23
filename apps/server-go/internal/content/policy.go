package content

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

const MaxArticlesPerRun = 1

type ApprovedFeed struct {
	Source string
	URL    string
}

type EditorialAuthorDetails struct {
	Name  string
	Email string
	Bio   string
}

type RewrittenArticle struct {
	Title   string
	Excerpt string
	Content string
}

type ArticleValidationOptions struct {
	MinWords *int
	MaxWords *int
}

var approvedFeeds = [...]ApprovedFeed{
	{"TechCrunch", "https://techcrunch.com/feed/"},
	{"The Verge", "https://www.theverge.com/rss/index.xml"},
	{"Ars Technica", "https://feeds.arstechnica.com/arstechnica/index"},
	{"WIRED", "https://www.wired.com/feed/rss"},
	{"Engadget", "https://www.engadget.com/rss.xml"},
	{"BleepingComputer", "https://www.bleepingcomputer.com/feed/"},
	{"The Register", "https://www.theregister.com/headlines.atom"},
	{"MIT Technology Review", "https://www.technologyreview.com/feed/"},
	{"VentureBeat", "https://venturebeat.com/category/ai/feed"},
	{"404 Media", "https://www.404media.co/rss/"},
	{"Rest of World", "https://restofworld.org/feed/"},
	{"Decrypt", "https://decrypt.co/feed"},
	{"The Decoder", "https://the-decoder.com/feed/"},
	{"ZDNET", "https://www.zdnet.com/topic/artificial-intelligence/rss.xml"},
	{"InfoQ", "https://feed.infoq.com/ai-ml-data-eng/"},
	{"IEEE Spectrum", "https://spectrum.ieee.org/feeds/topic/artificial-intelligence.rss"},
	{"SiliconANGLE", "https://siliconangle.com/category/ai/feed/"},
	{"AI Business", "https://aibusiness.com/rss.xml"},
	{"ScienceDaily", "https://www.sciencedaily.com/rss/computers_math/artificial_intelligence.xml"},
}

var sourceHosts = [...]struct {
	source string
	domain string
}{
	{"TechCrunch", "techcrunch.com"},
	{"The Verge", "theverge.com"},
	{"Ars Technica", "arstechnica.com"},
	{"WIRED", "wired.com"},
	{"Engadget", "engadget.com"},
	{"BleepingComputer", "bleepingcomputer.com"},
	{"The Register", "theregister.com"},
	{"MIT Technology Review", "technologyreview.com"},
	{"VentureBeat", "venturebeat.com"},
	{"404 Media", "404media.co"},
	{"Rest of World", "restofworld.org"},
	{"Decrypt", "decrypt.co"},
	{"The Decoder", "the-decoder.com"},
	{"ZDNET", "zdnet.com"},
	{"InfoQ", "infoq.com"},
	{"IEEE Spectrum", "spectrum.ieee.org"},
	{"SiliconANGLE", "siliconangle.com"},
	{"AI Business", "aibusiness.com"},
	{"ScienceDaily", "sciencedaily.com"},
}

func ApprovedFeeds() []ApprovedFeed {
	feeds := make([]ApprovedFeed, len(approvedFeeds))
	copy(feeds, approvedFeeds[:])
	return feeds
}

func EditorialAuthor() EditorialAuthorDetails {
	return EditorialAuthorDetails{
		Name:  "TechNews Editorial",
		Email: "editorial@technews.dev",
		Bio:   "The TechNews editorial team covers artificial intelligence.",
	}
}

var nonSlugCharacter = regexp.MustCompile(`[^a-z0-9]+`)

func Slugify(title string) string {
	slug := nonSlugCharacter.ReplaceAllString(strings.ToLower(title), "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 120 {
		slug = slug[:120]
	}
	return slug
}

var (
	scriptElement = regexp.MustCompile(`(?is)<script.*?</script>`)
	styleElement  = regexp.MustCompile(`(?is)<style.*?</style>`)
	htmlTag       = regexp.MustCompile(`<[^>]+>`)
)

func StripHTML(html string) string {
	text := scriptElement.ReplaceAllString(html, " ")
	text = styleElement.ReplaceAllString(text, " ")
	text = htmlTag.ReplaceAllString(text, " ")
	entities := []struct{ pattern, replacement string }{
		{`(?i)&nbsp;`, " "},
		{`(?i)&amp;`, "&"},
		{`(?i)&lt;`, "<"},
		{`(?i)&gt;`, ">"},
		{`(?i)&quot;`, `"`},
		{`(?i)&#39;`, `'`},
	}
	for _, entity := range entities {
		text = regexp.MustCompile(entity.pattern).ReplaceAllString(text, entity.replacement)
	}
	return collapseWhitespace(text)
}

func collapseWhitespace(value string) string {
	return strings.Join(strings.FieldsFunc(value, func(r rune) bool {
		return isJavaScriptWhitespace(r)
	}), " ")
}

func trimJavaScriptWhitespace(value string) string {
	return strings.TrimFunc(value, isJavaScriptWhitespace)
}

func WordCount(html string) int {
	text := StripHTML(html)
	if text == "" {
		return 0
	}
	return len(strings.FieldsFunc(text, func(r rune) bool {
		return isJavaScriptWhitespace(r)
	}))
}

type urlProtection struct {
	marker      string
	replacement string
}

func parsePolicyURL(value string) (*url.URL, error) {
	parsed, _, err := parsePolicyURLDetailed(value)
	return parsed, err
}

func parsePolicyURLDetailed(value string) (*url.URL, []urlProtection, error) {
	value = strings.NewReplacer("	", "", "\n", "", "\r", "").Replace(value)
	value = strings.TrimFunc(value, func(r rune) bool { return r <= ' ' })
	colon := strings.IndexByte(value, ':')
	if colon <= 0 {
		return nil, nil, errors.New("invalid source URL")
	}
	scheme := strings.ToLower(value[:colon])
	if !validURLScheme(scheme) {
		return nil, nil, errors.New("invalid source URL")
	}
	rest := value[colon+1:]
	if isSpecialURLScheme(scheme) {
		suffix := ""
		if index := strings.IndexAny(rest, "?#"); index >= 0 {
			suffix = rest[index:]
			rest = rest[:index]
		}
		rest = strings.ReplaceAll(rest, `\`, "/")
		value = scheme + "://" + strings.TrimLeft(rest, "/") + suffix
	} else {
		value = scheme + ":" + rest
	}
	if isSpecialURLScheme(scheme) {
		value = decodeUnreservedHostEscapes(value, scheme)
	}
	value, protections := protectURLPath(value, scheme)
	parsed, err := url.Parse(value)
	if err != nil {
		return nil, nil, err
	}
	parsed.RawQuery = canonicalizeURLQuery(parsed.RawQuery, isSpecialURLScheme(scheme))
	if isSpecialURLScheme(scheme) {
		if err := normalizeSpecialURLPath(parsed); err != nil {
			return nil, nil, err
		}
	}
	return parsed, protections, nil
}

func restoreURLProtections(value string, protections []urlProtection) string {
	for _, protection := range protections {
		value = strings.ReplaceAll(value, protection.marker, protection.replacement)
	}
	return value
}

func decodeUnreservedHostEscapes(value, scheme string) string {
	authorityStart := len(scheme) + 3
	if authorityStart >= len(value) {
		return value
	}
	authorityEnd := len(value)
	if offset := strings.IndexAny(value[authorityStart:], "/?#"); offset >= 0 {
		authorityEnd = authorityStart + offset
	}
	authority := value[authorityStart:authorityEnd]
	hostStart := 0
	if at := strings.LastIndexByte(authority, '@'); at >= 0 {
		hostStart = at + 1
	}
	host := authority[hostStart:]
	var decoded strings.Builder
	decoded.Grow(len(host))
	for index := 0; index < len(host); {
		if host[index] == '%' && index+2 < len(host) {
			hi, hiOK := hexValue(host[index+1])
			lo, loOK := hexValue(host[index+2])
			if hiOK && loOK {
				value := hi<<4 | lo
				if isURLUnreserved(value) {
					decoded.WriteByte(value)
					index += 3
					continue
				}
			}
		}
		decoded.WriteByte(host[index])
		index++
	}
	return value[:authorityStart+hostStart] + decoded.String() + value[authorityEnd:]
}

func isURLUnreserved(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '-' || value == '.' || value == '_' || value == '~'
}

func protectURLPath(value, scheme string) (string, []urlProtection) {
	pathStart := len(scheme) + 1
	if isSpecialURLScheme(scheme) {
		authorityStart := len(scheme) + 3
		offset := strings.IndexByte(value[authorityStart:], '/')
		if offset < 0 {
			return value, nil
		}
		pathStart = authorityStart + offset
	}
	pathEnd := len(value)
	if offset := strings.IndexAny(value[pathStart:], "?#"); offset >= 0 {
		pathEnd = pathStart + offset
	}
	path := value[pathStart:pathEnd]
	var protected strings.Builder
	protected.Grow(len(path))
	protections := make([]urlProtection, 0)
	for index := 0; index < len(path); index++ {
		replacement := ""
		if path[index] == '|' {
			replacement = "|"
		} else if path[index] == '%' {
			validEscape := false
			if index+2 < len(path) {
				_, hiOK := hexValue(path[index+1])
				_, loOK := hexValue(path[index+2])
				validEscape = hiOK && loOK
			}
			if !validEscape {
				replacement = "%"
			}
		}
		if replacement == "" {
			protected.WriteByte(path[index])
			continue
		}
		marker := "zzurlprotect" + strconv.Itoa(len(protections)) + "zz"
		for strings.Contains(value, marker) {
			marker += "z"
		}
		protected.WriteString(marker)
		protections = append(protections, urlProtection{marker: marker, replacement: replacement})
	}
	return value[:pathStart] + protected.String() + value[pathEnd:], protections
}

func canonicalizeURLQuery(raw string, special bool) string {
	const hex = "0123456789ABCDEF"
	var result strings.Builder
	result.Grow(len(raw))
	for _, r := range raw {
		encode := r <= 0x20 || r > 0x7e || r == '"' || r == '#' || r == '<' || r == '>' || (special && r == '\'')
		if !encode {
			result.WriteRune(r)
			continue
		}
		for _, b := range []byte(string(r)) {
			result.WriteByte('%')
			result.WriteByte(hex[b>>4])
			result.WriteByte(hex[b&0x0f])
		}
	}
	return result.String()
}

func normalizeSpecialURLPath(parsed *url.URL) error {
	escapedPath := parsed.EscapedPath()
	if escapedPath == "" {
		return nil
	}
	segments := strings.Split(escapedPath, "/")
	normalized := make([]string, 0, len(segments))
	for index, segment := range segments {
		lower := strings.ToLower(segment)
		switch lower {
		case ".", "%2e":
			if index == len(segments)-1 {
				normalized = append(normalized, "")
			}
		case "..", ".%2e", "%2e.", "%2e%2e":
			if len(normalized) > 1 {
				normalized = normalized[:len(normalized)-1]
			}
			if index == len(segments)-1 {
				normalized = append(normalized, "")
			}
		default:
			normalized = append(normalized, segment)
		}
	}
	escapedPath = strings.Join(normalized, "/")
	decodedPath, err := url.PathUnescape(escapedPath)
	if err != nil {
		return err
	}
	parsed.Path = decodedPath
	if escapedPath != decodedPath {
		parsed.RawPath = escapedPath
	} else {
		parsed.RawPath = ""
	}
	return nil
}

func validURLScheme(value string) bool {
	for index, r := range value {
		if (r >= 'a' && r <= 'z') || (index > 0 && ((r >= '0' && r <= '9') || r == '+' || r == '-' || r == '.')) {
			continue
		}
		return false
	}
	return value != ""
}

func SourceForURL(value string) (string, bool) {
	parsed, err := parsePolicyURL(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	host = strings.TrimPrefix(host, "www.")
	for _, candidate := range sourceHosts {
		if host == candidate.domain || strings.HasSuffix(host, "."+candidate.domain) {
			return candidate.source, true
		}
	}
	return "", false
}

var trackingKey = regexp.MustCompile(`(?i)^(?:utm_.+|gclid|fbclid|mc_cid|mc_eid)$`)

func NormalizeSourceURL(value string) (string, error) {
	parsed, protections, err := parsePolicyURLDetailed(value)
	requiresHost := parsed != nil && isSpecialURLScheme(parsed.Scheme)
	if err != nil || parsed == nil || parsed.Scheme == "" || (requiresHost && parsed.Host == "") {
		return "", errors.New("invalid source URL")
	}

	parsed.Fragment = ""
	parsed.RawFragment = ""
	if parsed.Host != "" {
		parsed.Host = normalizedHost(parsed)
	}

	escapedPath := parsed.EscapedPath()
	if len(escapedPath) > 1 {
		escapedPath = strings.TrimRight(escapedPath, "/")
		decodedPath, decodeErr := url.PathUnescape(escapedPath)
		if decodeErr != nil {
			return "", errors.New("invalid source URL")
		}
		parsed.Path = decodedPath
		if escapedPath != decodedPath {
			parsed.RawPath = escapedPath
		} else {
			parsed.RawPath = ""
		}
	}
	if parsed.Host != "" && parsed.Path == "" && isSpecialURLScheme(parsed.Scheme) {
		parsed.Path = "/"
		parsed.RawPath = ""
	}

	rawQuery, removed := withoutTrackingParameters(parsed.RawQuery)
	if removed {
		parsed.RawQuery = serializeSearchParameters(rawQuery)
		parsed.ForceQuery = false
	} else {
		parsed.RawQuery = rawQuery
	}
	return restoreURLProtections(parsed.String(), protections), nil
}

func isSpecialURLScheme(scheme string) bool {
	return scheme == "http" || scheme == "https" || scheme == "ftp" || scheme == "ws" || scheme == "wss"
}

func normalizedHost(parsed *url.URL) string {
	hostname := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	defaultPort := (parsed.Scheme == "https" && port == "443") ||
		(parsed.Scheme == "http" && port == "80") ||
		(parsed.Scheme == "wss" && port == "443") ||
		(parsed.Scheme == "ws" && port == "80") ||
		(parsed.Scheme == "ftp" && port == "21")
	if defaultPort {
		port = ""
	}
	if strings.Contains(hostname, ":") {
		hostname = "[" + hostname + "]"
	}
	if port != "" {
		return net.JoinHostPort(strings.Trim(hostname, "[]"), port)
	}
	return hostname
}

func withoutTrackingParameters(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	parts := strings.Split(raw, "&")
	kept := make([]string, 0, len(parts))
	removed := false
	for _, part := range parts {
		key := part
		if index := strings.IndexByte(key, '='); index >= 0 {
			key = key[:index]
		}
		if trackingKey.MatchString(queryDecode(key)) {
			removed = true
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, "&"), removed
}

func serializeSearchParameters(raw string) string {
	if raw == "" {
		return ""
	}
	fields := strings.Split(raw, "&")
	parts := make([]string, 0, len(fields))
	for _, part := range fields {
		if part == "" {
			continue
		}
		key, value, _ := strings.Cut(part, "=")
		parts = append(parts, formEncode(queryDecode(key))+"="+formEncode(queryDecode(value)))
	}
	return strings.Join(parts, "&")
}

func queryDecode(value string) string {
	decoded := make([]byte, 0, len(value))
	for index := 0; index < len(value); {
		if value[index] == '+' {
			decoded = append(decoded, ' ')
			index++
			continue
		}
		if value[index] == '%' && index+2 < len(value) {
			hi, hiOK := hexValue(value[index+1])
			lo, loOK := hexValue(value[index+2])
			if hiOK && loOK {
				decoded = append(decoded, hi<<4|lo)
				index += 3
				continue
			}
		}
		decoded = append(decoded, value[index])
		index++
	}
	return decodeUTF8WithReplacement(decoded)
}

func hexValue(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	default:
		return 0, false
	}
}

func decodeUTF8WithReplacement(value []byte) string {
	var result strings.Builder
	for len(value) > 0 {
		if invalidPrefix := incompleteUTF8PrefixLength(value); invalidPrefix > 0 {
			result.WriteRune(utf8.RuneError)
			value = value[invalidPrefix:]
			continue
		}
		r, size := utf8.DecodeRune(value)
		result.WriteRune(r)
		value = value[size:]
	}
	return result.String()
}

func utf8SequenceLength(first byte) int {
	switch {
	case first >= 0xc2 && first <= 0xdf:
		return 2
	case first >= 0xe0 && first <= 0xef:
		return 3
	case first >= 0xf0 && first <= 0xf4:
		return 4
	default:
		return 1
	}
}

func incompleteUTF8PrefixLength(value []byte) int {
	expected := utf8SequenceLength(value[0])
	if expected == 1 {
		return 0
	}
	for index := 1; index < expected; index++ {
		if index >= len(value) {
			return index
		}
		continuation := value[index]
		if continuation < 0x80 || continuation > 0xbf {
			if index == 1 {
				return 0
			}
			return index
		}
		if index == 1 && ((value[0] == 0xe0 && continuation < 0xa0) ||
			(value[0] == 0xed && continuation > 0x9f) ||
			(value[0] == 0xf0 && continuation < 0x90) ||
			(value[0] == 0xf4 && continuation > 0x8f)) {
			return 0
		}
	}
	return 0
}

func formEncode(value string) string {
	const hex = "0123456789ABCDEF"
	var result strings.Builder
	for _, b := range []byte(value) {
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || strings.ContainsRune("*-._", rune(b)) {
			result.WriteByte(b)
		} else if b == ' ' {
			result.WriteByte('+')
		} else {
			result.WriteByte('%')
			result.WriteByte(hex[b>>4])
			result.WriteByte(hex[b&15])
		}
	}
	return result.String()
}

const (
	javascriptWhitespacePattern    = `[	\n\v\f\r \x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}]`
	javascriptNonWhitespacePattern = `[^	\n\v\f\r \x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}]`
)

var promotionalPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:coupon|discount|sale|buying guide)\b`),
	regexp.MustCompile(`(?i)\b(?:best|top|latest|today'?s|daily|weekly)\b(?:` + javascriptWhitespacePattern + `+` + javascriptNonWhitespacePattern + `+){0,5}` + javascriptWhitespacePattern + `+deals?\b`),
	regexp.MustCompile(`(?i)\bdeals?\b.{0,40}\b(?:save|\d+% off|under \$)\b`),
	regexp.MustCompile(`(?i)\b(?:lowest|best) price\b`),
	regexp.MustCompile(`(?i)\bprice (?:drop|cut)\b`),
	regexp.MustCompile(`(?i)\b(?:save \$?\d+|\d+% off|percent off)\b`),
	regexp.MustCompile(`(?i)\b(?:prime day|black friday|cyber monday)\b`),
	regexp.MustCompile(`(?i)\b(?:last chance|pre-?orders?|pre-?order bonuses?)\b`),
}

func containsPromotionalLanguage(value string) bool {
	for _, pattern := range promotionalPatterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

var rejectedTitles = []struct {
	pattern *regexp.Regexp
	reason  string
}{
	{regexp.MustCompile(`(?i)^show hn:`), "Show HN item"},
	{regexp.MustCompile(`(?i)^state of show hn`), "Show HN item"},
	{regexp.MustCompile(`(?i)\b(?:review|hands-on|buying guide|roundup)\b`), "review, guide, or roundup rather than news"},
	{regexp.MustCompile(`(?i)\[(?:pdf|video)\]`), "PDF or video item"},
	{regexp.MustCompile(`(?i)\b(?:abstract|arxiv)` + javascriptWhitespacePattern + `*:`), "abstract or arXiv-style item"},
	{regexp.MustCompile(`(?i)\barxiv\b`), "arXiv-style item"},
}

var (
	mediaExtension    = regexp.MustCompile(`(?i)\.(?:pdf|mp4|mov|webm)$`)
	mediaSection      = regexp.MustCompile(`(?i)/(?:videos?|deals?)(?:/|$)`)
	reviewURLWords    = regexp.MustCompile(`(?i)\b(?:review|hands on|buying guide|roundup|installer)\b`)
	titleYear         = regexp.MustCompile(`\(((?:19|20)\d{2})\)` + javascriptWhitespacePattern + `*$`)
	pathYear          = regexp.MustCompile(`/((?:19|20)\d{2})/`)
	urlWordSeparators = regexp.MustCompile(`[-_/]+`)
	aiTitle           = regexp.MustCompile(`(?i)\b(?:ai|artificial intelligence|generative ai|machine learning|deep learning|llm|large language model|chatgpt|chatbot|openai|anthropic|gemini|claude|neural network|foundation model|frontier model|apple intelligence|microsoft copilot)\b`)
	aiSection         = regexp.MustCompile(`(?i)/(?:ai|category/ai|ai-artificial-intelligence|artificial-intelligence)(?:/|$)`)
)

func ItemRejectionReason(title, sourceURL, expectedSource string, now time.Time) string {
	source, approved := SourceForURL(sourceURL)
	if !approved {
		return "source is not on the approved publication list"
	}
	if expectedSource != "" && source != expectedSource {
		return "source URL does not match its RSS publication"
	}

	trimmedTitle := trimJavaScriptWhitespace(title)
	for _, rejection := range rejectedTitles {
		if rejection.pattern.MatchString(trimmedTitle) {
			return rejection.reason
		}
	}
	if containsPromotionalLanguage(trimmedTitle) {
		return "deal or promotional item"
	}

	parsed, protections, err := parsePolicyURLDetailed(sourceURL)
	if err != nil {
		return "invalid source URL"
	}
	for _, protection := range protections {
		if protection.replacement == "%" {
			return "invalid source URL"
		}
	}
	encodedPath := restoreURLProtections(parsed.EscapedPath(), protections)
	if mediaExtension.MatchString(encodedPath) {
		return "PDF or video item"
	}
	if mediaSection.MatchString(encodedPath) {
		return "video, deal, or promotional item"
	}
	decodedPath := restoreURLProtections(parsed.Path, protections)
	urlWords := urlWordSeparators.ReplaceAllString(decodedPath, " ")
	if containsPromotionalLanguage(urlWords) {
		return "deal or promotional item"
	}
	if reviewURLWords.MatchString(urlWords) {
		return "review, guide, or roundup rather than news"
	}

	cutoffYear := now.UTC().Year() - 1
	if match := titleYear.FindStringSubmatch(title); len(match) > 1 {
		year, _ := strconv.Atoi(match[1])
		if year < cutoffYear {
			return "obviously old repost"
		}
	}
	if match := pathYear.FindStringSubmatch(encodedPath); len(match) > 1 {
		year, _ := strconv.Atoi(match[1])
		if year < cutoffYear {
			return "obviously old repost"
		}
	}
	if !aiTitle.MatchString(trimmedTitle) && !aiSection.MatchString(encodedPath) {
		return "not clearly AI-related; only AI news may be published"
	}
	return ""
}

func AutomaticItemRejectionReason(title, sourceURL, expectedSource string, now time.Time) string {
	return ItemRejectionReason(title, sourceURL, expectedSource, now)
}

var (
	clickbaitTitle = regexp.MustCompile(`!|\?{2,}`)
	excerptHTML    = regexp.MustCompile(`<[^>]+>`)
	forbiddenCopy  = []struct {
		pattern *regexp.Regexp
		label   string
	}{
		{regexp.MustCompile("\u2014"), "em dash"},
		{regexp.MustCompile(`(?i)\bin a move that\b`), "In a move that"},
		{regexp.MustCompile(`(?i)\bit remains to be seen\b`), "It remains to be seen"},
		{regexp.MustCompile(`(?i)\bgroundbreaking\b`), "groundbreaking"},
		{regexp.MustCompile(`(?i)\brevolutionary\b`), "revolutionary"},
		{regexp.MustCompile(`(?i)\bgame-changing\b`), "game-changing"},
	}
	allTags       = regexp.MustCompile(`(?i)</?([a-z][a-z0-9]*)\b[^>]*>`)
	policyTags    = regexp.MustCompile(`(?i)</?(?:p|h2)\b[^>]*>`)
	literalTag    = regexp.MustCompile(`(?i)^</?(?:p|h2)>$`)
	contentBlocks = regexp.MustCompile(`(?is)<p>.*?</p>|<h2>.*?</h2>`)
	paragraphs    = regexp.MustCompile(`(?is)<p>(.*?)</p>`)
	sourceFooter  = regexp.MustCompile(`(?i)^(?:source|sources)` + javascriptWhitespacePattern + `*:`)
)

func ValidateRewrittenArticle(article RewrittenArticle, options ArticleValidationOptions) []string {
	title := trimJavaScriptWhitespace(article.Title)
	excerpt := trimJavaScriptWhitespace(article.Excerpt)
	content := trimJavaScriptWhitespace(article.Content)
	allCopy := title + "\n" + excerpt + "\n" + content
	errorsFound := make([]string, 0)
	seen := make(map[string]struct{})
	add := func(message string) {
		if _, exists := seen[message]; !exists {
			seen[message] = struct{}{}
			errorsFound = append(errorsFound, message)
		}
	}

	if title == "" {
		add("headline is missing")
	}
	if javascriptLength(title) > 120 {
		add("headline exceeds 120 characters")
	}
	if clickbaitTitle.MatchString(title) {
		add("headline appears clickbait-like")
	}
	if containsPromotionalLanguage(title) {
		add("headline is promotional")
	}
	if excerpt == "" {
		add("excerpt is missing")
	}
	if javascriptLength(excerpt) > 180 {
		add("excerpt exceeds 180 characters")
	}
	if excerptHTML.MatchString(excerpt) {
		add("excerpt contains HTML")
	}
	if strings.ContainsAny(excerpt, "\r\n") {
		add("excerpt is not one plain line")
	}
	if excerpt != "" && sentenceCount(excerpt) != 1 {
		add("excerpt must be exactly one sentence")
	}
	for _, forbidden := range forbiddenCopy {
		if forbidden.pattern.MatchString(allCopy) {
			add("copy contains prohibited " + forbidden.label)
		}
	}

	for _, match := range allTags.FindAllStringSubmatch(content, -1) {
		tag := strings.ToLower(match[1])
		if tag != "p" && tag != "h2" {
			add("article HTML contains tags other than p or h2")
			break
		}
	}
	for _, tag := range policyTags.FindAllString(content, -1) {
		if !literalTag.MatchString(tag) {
			add("article HTML tags contain attributes")
			break
		}
	}
	if trimJavaScriptWhitespace(contentBlocks.ReplaceAllString(content, "")) != "" {
		add("article HTML contains text outside p or h2 blocks")
	}

	paragraphMatches := paragraphs.FindAllStringSubmatch(content, -1)
	if len(paragraphMatches) < 5 || len(paragraphMatches) > 12 {
		add("article must contain 5 to 12 paragraphs")
	}
	for _, paragraph := range paragraphMatches {
		if StripHTML(paragraph[1]) == "" {
			add("article contains an empty paragraph")
			break
		}
	}

	minWords, maxWords := 150, 800
	if options.MinWords != nil {
		minWords = *options.MinWords
	}
	if options.MaxWords != nil {
		maxWords = *options.MaxWords
	}
	words := WordCount(content)
	if words < minWords || words > maxWords {
		add(fmt.Sprintf("article must contain %d to %d words, found %d", minWords, maxWords, words))
	}
	if len(paragraphMatches) > 0 && sourceFooter.MatchString(StripHTML(paragraphMatches[len(paragraphMatches)-1][1])) {
		add("article contains a source footer")
	}
	return errorsFound
}

// JavaScriptLength counts UTF-16 code units, matching JavaScript's String.length.
func JavaScriptLength(s string) int { return javascriptLength(s) }

func javascriptLength(value string) int {
	return len(utf16.Encode([]rune(value)))
}

func sentenceCount(text string) int {
	runes := []rune(text)
	count := 0
	for index, value := range runes {
		if value != '.' && value != '!' && value != '?' {
			continue
		}
		next := index + 1
		for next < len(runes) && strings.ContainsRune(`"')]`, runes[next]) {
			next++
		}
		if next == len(runes) {
			count++
			continue
		}
		if !isJavaScriptWhitespace(runes[next]) {
			continue
		}
		for next < len(runes) && isJavaScriptWhitespace(runes[next]) {
			next++
		}
		if next < len(runes) && ((runes[next] >= 'A' && runes[next] <= 'Z') || (runes[next] >= '0' && runes[next] <= '9')) {
			count++
		}
	}
	return count
}

func isJavaScriptWhitespace(value rune) bool {
	switch value {
	case '	', '\n', '\v', '\f', '\r', ' ', '\u00a0', '\u1680', '\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff':
		return true
	}
	return value >= '\u2000' && value <= '\u200a'
}
