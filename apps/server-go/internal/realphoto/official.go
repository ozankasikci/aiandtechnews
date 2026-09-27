package realphoto

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/net/html"
	"golang.org/x/net/publicsuffix"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
)

const (
	// maxLinks caps the outbound links offered to the model.
	maxLinks = 25
	// maxLinksPerSite keeps one site from crowding out the others.
	maxLinksPerSite = 2
	// maxTitleFetches caps the linked pages whose <title> is read.
	maxTitleFetches = 12
	// maxImageDownloads caps the official page's images downloaded to measure.
	maxImageDownloads = 16
)

// Link is an outbound link of the news source's page.
type Link struct {
	URL    string `json:"url"`
	Domain string `json:"domain"`
	Text   string `json:"text"`
	Title  string `json:"title,omitempty"`
}

// official finds the maker's own site among the source page's links and
// verifies the big images on it. It returns the candidates, the official
// page URL and the maker's name for the credit.
func (f *Finder) official(ctx context.Context, request Request, plan Plan) ([]Candidate, string, string, error) {
	if strings.TrimSpace(request.SourceURL) == "" {
		return nil, "", "", errors.New("the article has no source page")
	}
	source, err := f.getPage(ctx, request.SourceURL)
	if err != nil {
		return nil, "", "", fmt.Errorf("source page: %w", err)
	}
	links := OutboundLinks(source.Body, source.FinalURL)
	if len(links) == 0 {
		return nil, "", "", errors.New("the source page links to no candidate official site")
	}
	f.readTitles(ctx, links)
	choice, err := f.chooseOfficial(ctx, request, plan, links)
	if err != nil {
		return nil, "", "", fmt.Errorf("choose official link: %w", err)
	}
	if choice.Index < 0 {
		return nil, "", "", fmt.Errorf("no official site among %d links: %s", len(links), choice.Reason)
	}
	link := links[choice.Index]
	maker := oneLine(choice.Maker, 60)
	if maker == "" {
		maker = plan.Maker
	}
	if maker == "" {
		maker = link.Domain
	}
	page, err := f.getPage(ctx, link.URL)
	if err != nil {
		return nil, link.URL, maker, fmt.Errorf("official page: %w", err)
	}
	pageURL := page.FinalURL
	if sameSite(pageURL, request.SourceURL) || blockedSite(pageURL) {
		return nil, pageURL, maker, errors.New("official link redirected to a news, social or shop site")
	}
	var candidates []Candidate
	for _, imageURL := range PageImages(page.Body, pageURL) {
		candidates = append(candidates, Candidate{Source: KindOfficial, ImageURL: imageURL, PageURL: pageURL})
	}
	if len(candidates) == 0 {
		return nil, pageURL, maker, errors.New("the official page has no usable image")
	}
	// Download to measure; drop the small or wrongly shaped ones before any
	// vision check.
	downloads, fitting := 0, 0
	for i := range candidates {
		candidate := &candidates[i]
		if downloads >= maxImageDownloads || fitting >= MaxVerified || ctx.Err() != nil {
			candidate.Reason = "not downloaded (cap reached)"
			continue
		}
		downloads++
		data, err := f.getImage(ctx, candidate.ImageURL)
		if err != nil {
			candidate.Reason = "download: " + err.Error()
			continue
		}
		width, height, err := measure(data)
		if err != nil {
			candidate.Reason = "unreadable image: " + err.Error()
			continue
		}
		candidate.Width, candidate.Height = width, height
		if ok, why := fitsSlot(width, height); !ok {
			candidate.Reason = why
			continue
		}
		candidate.data = data
		fitting++
	}
	f.verifyAll(ctx, plan, request, KindOfficial, candidates)
	return candidates, pageURL, maker, nil
}

// readTitles fills in the linked pages' titles, a few at a time; a page
// that cannot be read keeps an empty title.
func (f *Finder) readTitles(ctx context.Context, links []Link) {
	var wg sync.WaitGroup
	limit := make(chan struct{}, 4)
	for i := range links {
		if i >= maxTitleFetches {
			break
		}
		wg.Add(1)
		go func(link *Link) {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			page, err := f.get(ctx, link.URL, "text/html", maxTitleBytes, true, isHTML)
			if err == nil {
				link.Title = oneLine(pageTitle(page.Body), 120)
			}
		}(&links[i])
	}
	wg.Wait()
}

type officialChoice struct {
	Index  int    `json:"index"`
	Maker  string `json:"maker"`
	Reason string `json:"reason"`
}

const officialPrompt = `A technology news article is about: %s (made or owned by: %s).
Headline: %s

Below are the outbound links of the article's source page. Pick the one that is the OFFICIAL website of the subject's maker or owner, ideally its page about this exact subject (a product page, a press page or the home page). Not a news site, blog, review, shop, marketplace, crowdfunding page, social network, investor, partner, accelerator programme, or a different company.

Links:
%s
Return only JSON with exactly this shape:
{"index":0,"maker":"...","reason":"..."}
index: the link's number, or -1 if none is the maker's official site.
maker: the maker's short brand name as it should appear in a photo credit (e.g. "WiCi"), empty if index is -1.
reason: one short sentence.`

func (f *Finder) chooseOfficial(ctx context.Context, request Request, plan Plan, links []Link) (officialChoice, error) {
	var list strings.Builder
	for i, link := range links {
		fmt.Fprintf(&list, "%d. domain: %s | link text: %s | page title: %s | url: %s\n", i, link.Domain, orNone(link.Text), orNone(link.Title), link.URL)
	}
	maker := plan.Maker
	if maker == "" {
		maker = "unknown"
	}
	raw, err := f.text.GenerateJSON(ctx, fmt.Sprintf(officialPrompt, plan.Subject, maker, request.Title, list.String()))
	if err != nil {
		return officialChoice{}, err
	}
	return ParseOfficialChoice(raw, len(links))
}

// ParseOfficialChoice reads the model's pick among count links.
func ParseOfficialChoice(raw string, count int) (officialChoice, error) {
	var choice struct {
		Index  *int   `json:"index"`
		Maker  string `json:"maker"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(cleanJSON(raw)), &choice); err != nil || choice.Index == nil {
		return officialChoice{}, errors.New("unparsable answer")
	}
	index := *choice.Index
	if index < -1 || index >= count {
		return officialChoice{}, fmt.Errorf("index %d out of range", index)
	}
	return officialChoice{Index: index, Maker: strings.TrimSpace(choice.Maker), Reason: oneLine(choice.Reason, 200)}, nil
}

func orNone(text string) string {
	if text == "" {
		return "-"
	}
	return text
}

// OutboundLinks lists the page's links to other sites that could be a
// maker's official site: not the publisher, not a news site, social
// network, shop, marketplace or link shortener. At most maxLinksPerSite per
// site and maxLinks in all, in page order.
func OutboundLinks(page []byte, pageURL string) []Link {
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}
	var links []Link
	perSite := map[string]int{}
	seen := map[string]bool{}
	tokenizer := html.NewTokenizer(bytes.NewReader(page))
	var current *Link
	var text strings.Builder
	skip := 0 // inside <script>/<style>
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			break
		}
		token := tokenizer.Token()
		switch tokenType {
		case html.StartTagToken, html.SelfClosingTagToken:
			switch token.Data {
			case "script", "style", "noscript":
				if tokenType == html.StartTagToken {
					skip++
				}
			case "a":
				current, text = nil, strings.Builder{}
				href := attr(token, "href")
				resolved := resolve(base, href)
				if resolved == "" || sameSite(resolved, pageURL) || blockedSite(resolved) {
					continue
				}
				domain := registrableDomain(resolved)
				if seen[resolved] || perSite[domain] >= maxLinksPerSite {
					continue
				}
				current = &Link{URL: resolved, Domain: domain}
				if label := attr(token, "aria-label"); label != "" {
					text.WriteString(label)
				}
			case "img":
				if current != nil && text.Len() == 0 {
					text.WriteString(attr(token, "alt"))
				}
			}
		case html.EndTagToken:
			switch token.Data {
			case "script", "style", "noscript":
				skip = max(skip-1, 0)
			case "a":
				if current != nil && len(links) < maxLinks {
					current.Text = oneLine(text.String(), 100)
					seen[current.URL] = true
					perSite[current.Domain]++
					links = append(links, *current)
				}
				current = nil
			}
		case html.TextToken:
			if current != nil && skip == 0 {
				text.WriteString(" " + token.Data)
			}
		}
	}
	return links
}

// PageImages lists the page's likely hero or product images, og:image and
// twitter:image first, then <img>/<source> in page order (the largest
// srcset entry). Logos, icons, badges and vector images are left out.
func PageImages(page []byte, pageURL string) []string {
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}
	var meta, body []string
	seen := map[string]bool{}
	add := func(into *[]string, raw string) {
		resolved := resolve(base, raw)
		if resolved == "" || seen[resolved] || !likelyPhotoURL(resolved) {
			return
		}
		seen[resolved] = true
		*into = append(*into, resolved)
	}
	tokenizer := html.NewTokenizer(bytes.NewReader(page))
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			break
		}
		if tokenType != html.StartTagToken && tokenType != html.SelfClosingTagToken {
			continue
		}
		token := tokenizer.Token()
		switch token.Data {
		case "meta":
			key := strings.ToLower(attr(token, "property") + attr(token, "name"))
			switch key {
			case "og:image", "og:image:url", "og:image:secure_url", "twitter:image", "twitter:image:src":
				add(&meta, attr(token, "content"))
			}
		case "link":
			if strings.EqualFold(attr(token, "rel"), "preload") && strings.EqualFold(attr(token, "as"), "image") {
				if set := largestSrcset(attr(token, "imagesrcset")); set != "" {
					add(&body, set)
				} else {
					add(&body, attr(token, "href"))
				}
			}
		case "img", "source":
			if logoLike(attr(token, "alt")+" "+attr(token, "class")+" "+attr(token, "id")) || declaredSmall(token) {
				continue
			}
			if set := largestSrcset(attr(token, "srcset") + attr(token, "data-srcset")); set != "" {
				add(&body, set)
				continue
			}
			for _, key := range []string{"data-src", "data-lazy-src", "data-original", "src"} {
				if value := attr(token, key); value != "" {
					add(&body, value)
					break
				}
			}
		}
	}
	return append(meta, body...)
}

func attr(token html.Token, key string) string {
	for _, attribute := range token.Attr {
		if strings.EqualFold(attribute.Key, key) {
			return strings.TrimSpace(attribute.Val)
		}
	}
	return ""
}

// resolve makes href absolute against base; only http(s) URLs survive.
func resolve(base *url.URL, href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(strings.ToLower(href), "data:") {
		return ""
	}
	parsed, err := base.Parse(href)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}
	parsed.Fragment = ""
	return parsed.String()
}

var srcsetWidth = regexp.MustCompile(`^(\d+)w$`)

// largestSrcset is the srcset candidate with the biggest width descriptor
// (or the last one when none has a width).
func largestSrcset(srcset string) string {
	best, bestWidth := "", -1
	for _, entry := range strings.Split(srcset, ",") {
		fields := strings.Fields(entry)
		if len(fields) == 0 {
			continue
		}
		width := 0
		if len(fields) > 1 {
			if match := srcsetWidth.FindStringSubmatch(fields[1]); match != nil {
				width, _ = strconv.Atoi(match[1])
			}
		}
		if width >= bestWidth {
			best, bestWidth = fields[0], width
		}
	}
	return best
}

var logoWords = regexp.MustCompile(`(?i)(^|[^a-z])(logo|logos|icon|icons|favicon|badge|sprite|avatar|emoji|flag|award|partner|partners|inception|spinner|loader|placeholder|qr|arrow|payment|social)([^a-z]|$)`)

func logoLike(text string) bool { return logoWords.MatchString(text) }

// likelyPhotoURL leaves out vector images and file names that say logo,
// icon, badge and the like.
func likelyPhotoURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	path := strings.ToLower(parsed.Path)
	if strings.HasSuffix(path, ".svg") || strings.HasSuffix(path, ".ico") || strings.HasSuffix(path, ".gif") {
		return false
	}
	name := path[strings.LastIndex(path, "/")+1:]
	return !logoLike(name) && !logoLike(parsed.RawQuery)
}

// declaredSmall is an <img> whose width attribute says it is small.
func declaredSmall(token html.Token) bool {
	width, err := strconv.Atoi(strings.TrimSuffix(attr(token, "width"), "px"))
	return err == nil && width > 0 && width < 400
}

func pageTitle(page []byte) string {
	tokenizer := html.NewTokenizer(bytes.NewReader(page))
	inTitle := false
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return ""
		case html.StartTagToken:
			if name, _ := tokenizer.TagName(); string(name) == "title" {
				inTitle = true
			}
		case html.TextToken:
			if inTitle {
				return strings.TrimSpace(string(tokenizer.Text()))
			}
		case html.EndTagToken:
			if name, _ := tokenizer.TagName(); string(name) == "head" {
				return ""
			}
		}
	}
}

// registrableDomain is the URL's eTLD+1 ("wici.ai", "bbc.co.uk").
func registrableDomain(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	domain, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return host
	}
	return domain
}

func sameSite(a, b string) bool {
	domainA, domainB := registrableDomain(a), registrableDomain(b)
	return domainA != "" && domainA == domainB
}

// blockedSites are never a maker's official site: news and magazine sites,
// social networks, video, shops and marketplaces, crowdfunding, link
// shorteners and affiliate redirects, app stores, reference sites.
var blockedSites = map[string]bool{
	// social and video
	"twitter.com": true, "x.com": true, "t.co": true, "facebook.com": true, "fb.com": true, "instagram.com": true,
	"linkedin.com": true, "lnkd.in": true, "youtube.com": true, "youtu.be": true, "tiktok.com": true, "reddit.com": true,
	"threads.net": true, "threads.com": true, "bsky.app": true, "mastodon.social": true, "pinterest.com": true,
	"discord.gg": true, "discord.com": true, "t.me": true, "telegram.org": true, "whatsapp.com": true, "vimeo.com": true,
	"twitch.tv": true, "medium.com": true, "substack.com": true, "flipboard.com": true, "news.ycombinator.com": true,
	"ycombinator.com": true, "producthunt.com": true, "tumblr.com": true, "snapchat.com": true, "quora.com": true,
	// news and magazines
	"theverge.com": true, "engadget.com": true, "techcrunch.com": true, "wired.com": true, "arstechnica.com": true,
	"reuters.com": true, "bloomberg.com": true, "cnbc.com": true, "cnn.com": true, "bbc.co.uk": true, "bbc.com": true,
	"nytimes.com": true, "wsj.com": true, "ft.com": true, "theguardian.com": true, "washingtonpost.com": true,
	"forbes.com": true, "businessinsider.com": true, "insider.com": true, "axios.com": true, "zdnet.com": true,
	"cnet.com": true, "gizmodo.com": true, "mashable.com": true, "venturebeat.com": true, "theinformation.com": true,
	"techradar.com": true, "tomshardware.com": true, "tomsguide.com": true, "9to5mac.com": true, "9to5google.com": true,
	"macrumors.com": true, "androidauthority.com": true, "androidcentral.com": true, "electrek.co": true,
	"theregister.com": true, "apnews.com": true, "yahoo.com": true, "msn.com": true, "news.google.com": true,
	"semafor.com": true, "fortune.com": true, "time.com": true, "economist.com": true, "newscientist.com": true,
	"technologyreview.com": true, "spectrum.ieee.org": true, "theatlantic.com": true, "vox.com": true, "vice.com": true,
	"digitaltrends.com": true, "pcmag.com": true, "pcworld.com": true, "anandtech.com": true, "servethehome.com": true,
	"the-decoder.com": true, "siliconangle.com": true, "404media.co": true, "platformer.news": true, "wccftech.com": true,
	"notebookcheck.net": true, "gsmarena.com": true, "slashgear.com": true, "bgr.com": true, "neowin.net": true,
	"thenextweb.com": true, "fastcompany.com": true, "npr.org": true, "politico.com": true, "latimes.com": true,
	"nbcnews.com": true, "cbsnews.com": true, "abcnews.go.com": true, "foxnews.com": true, "scmp.com": true,
	"nikkei.com": true, "asia.nikkei.com": true, "techmeme.com": true, "biztoc.com": true, "crunchbase.com": true,
	// shops, marketplaces, crowdfunding, app stores
	"bestbuy.com": true, "walmart.com": true, "target.com": true, "aliexpress.com": true, "alibaba.com": true,
	"etsy.com": true, "newegg.com": true, "bhphotovideo.com": true, "kickstarter.com": true, "indiegogo.com": true,
	"apps.apple.com": true, "play.google.com": true, "costco.com": true, "adorama.com": true,
	"temu.com": true, "shein.com": true, "rakuten.com": true, "flipkart.com": true, "jd.com": true, "taobao.com": true,
	// link shorteners, affiliate and tracking redirects
	"bit.ly": true, "geni.us": true, "amzn.to": true, "go.skimresources.com": true, "skimresources.com": true,
	"viglink.com": true, "shareasale.com": true, "howl.me": true, "howl.link": true, "tinyurl.com": true,
	"ow.ly": true, "buff.ly": true, "dlvr.it": true, "shop-links.co": true, "fave.co": true, "avantlink.com": true,
	"awin1.com": true, "anrdoezrs.net": true, "dpbolvw.net": true, "jdoqocy.com": true, "kqzyfj.com": true,
	"tkqlhce.com": true, "pntrs.com": true, "prf.hn": true, "linksynergy.com": true, "click.linksynergy.com": true,
	// reference, archives, papers, code
	"wikipedia.org": true, "wikimedia.org": true, "wikidata.org": true, "archive.org": true, "archive.ph": true,
	"doi.org": true, "arxiv.org": true, "github.com": true, "goo.gl": true, "apple.news": true,
	"sec.gov": true, "prnewswire.com": true, "businesswire.com": true, "globenewswire.com": true,
}

// blockedLabels block a brand under any country domain (amazon.co.uk, ebay.de).
var blockedLabels = map[string]bool{"amazon": true, "ebay": true, "aliexpress": true, "walmart": true, "rakuten": true, "mercadolibre": true}

// blockedSite reports whether a URL is on a site that cannot be a maker's
// official site, including every approved news source.
func blockedSite(raw string) bool {
	if _, ok := content.SourceForURL(raw); ok {
		return true
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return true
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	domain := registrableDomain(raw)
	if blockedSites[host] || blockedSites[domain] {
		return true
	}
	label, _, _ := strings.Cut(domain, ".")
	return blockedLabels[label]
}
