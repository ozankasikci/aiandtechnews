package newsletter

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Email is Node's NewsletterEmail. Headers keep Node's object key order,
// which is the order they appear in the Resend request body.
type Email struct {
	To      string
	Subject string
	HTML    string
	Text    string
	Headers []Header
	Tags    []Tag
}

type Header struct {
	Name  string
	Value string
}

type Tag struct {
	Name  string
	Value string
}

// DigestArticle is one story in a digest and in the stored edition JSON
// (field order and names are Node's). The fields tagged "-" are only used to
// render the email; the stored edition keeps Node's five fields.
type DigestArticle struct {
	Title          string   `json:"title"`
	Slug           string   `json:"slug"`
	Excerpt        string   `json:"excerpt"`
	Category       string   `json:"category"`
	ReadingMinutes int      `json:"readingMinutes"`
	Image          string   `json:"-"`
	Source         string   `json:"-"`
	WhyItMatters   string   `json:"-"`
	TLDR           []string `json:"-"`
}

// The welcome templates below are apps/server/src/newsletter/email.ts
// verbatim, including every newline and indentation run inside the template
// literals. The digest has its own email-safe table layout further down.

func emailLayout(content, footer string) string {
	return `<!doctype html>
<html>
  <body style="margin:0;background:#0b0b10;color:#f5f5f7;font-family:Arial,sans-serif">
    <div style="max-width:620px;margin:0 auto;padding:36px 20px">
      <div style="font-size:12px;font-weight:800;letter-spacing:.16em;text-transform:uppercase;color:#9b87f5;margin-bottom:24px">AI &amp; Tech News</div>
      <div style="background:#15151d;border:1px solid #292936;border-radius:8px;padding:28px">` + content + `</div>
      <div style="color:#8f8f9c;font-size:12px;line-height:1.6;padding:20px 4px">` + footer + `</div>
    </div>
  </body>
</html>`
}

func button(label, url string) string {
	return `<a href="` + escapeHTML(url) + `" style="display:inline-block;background:#7c5cff;color:#fff;text-decoration:none;font-weight:700;border-radius:5px;padding:13px 20px">` + escapeHTML(label) + `</a>`
}

func unsubscribeHeaders(unsubscribeURL string) []Header {
	return []Header{
		{Name: "List-Unsubscribe", Value: "<" + unsubscribeURL + ">"},
		{Name: "List-Unsubscribe-Post", Value: "List-Unsubscribe=One-Click"},
	}
}

// WelcomeEmail is Node's welcomeEmail, sent when a pending subscriber confirms.
func WelcomeEmail(to, siteURL, unsubscribeURL string) Email {
	content := `
    <h1 style="margin:0 0 14px;font-size:26px;line-height:1.2">Welcome to AI &amp; Tech News</h1>
    <p style="color:#c8c8d0;line-height:1.65;margin:0 0 22px">Your subscription is confirmed. We will send you a concise daily digest of the AI and technology stories worth knowing.</p>
    ` + button("Read the latest stories", siteURL)
	return Email{
		To:      to,
		Subject: "Welcome to AI & Tech News",
		HTML:    emailLayout(content, `You can <a href="`+escapeHTML(unsubscribeURL)+`" style="color:#b7a7ff">unsubscribe at any time</a>.`),
		Text:    "Welcome to AI & Tech News. Your subscription is confirmed.\n\nRead the latest stories: " + siteURL + "\n\nUnsubscribe: " + unsubscribeURL,
		Headers: unsubscribeHeaders(unsubscribeURL),
		Tags:    []Tag{{Name: "email_type", Value: "welcome"}},
	}
}

// DigestSubject is Node's digestSubject: the lead story's title, with the
// number of further stories.
func DigestSubject(articles []DigestArticle) string {
	if len(articles) == 0 {
		return "Today in AI and technology"
	}
	if rest := len(articles) - 1; rest > 0 {
		return articles[0].Title + " (+" + strconv.Itoa(rest) + " more)"
	}
	return articles[0].Title
}

const (
	digestFont   = "-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif"
	digestAccent = "#7c5cff"
)

// digestStyles switches the light layout to the site's dark palette in
// clients that honor prefers-color-scheme, and narrows it on phones.
const digestStyles = `@media (max-width:600px){.px{padding-left:20px!important;padding-right:20px!important}.thumb{width:96px!important}.thumb img{width:96px!important;height:64px!important}.lead-title{font-size:24px!important}}
@media (prefers-color-scheme:dark){.bg{background:#0b0b10!important}.card{background:#15151d!important;border-color:#292936!important}.title{color:#f5f5f7!important}.body{color:#c8c8d0!important}.rule{border-color:#292936!important}.why{background:#1d1a2e!important}}`

func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}
	return many
}

// trackedURL adds the newsletter's UTM parameters to a site link.
func trackedURL(link, edition string) string {
	separator := "?"
	if strings.Contains(link, "?") {
		separator = "&"
	}
	return link + separator + "utm_source=newsletter&utm_medium=email&utm_campaign=daily_" + url.QueryEscape(edition)
}

// imageURL resolves a featured image to an absolute http(s) URL, or "" when
// there is no usable image.
func imageURL(image, siteURL string) string {
	image = strings.TrimSpace(image)
	switch {
	case strings.HasPrefix(image, "https://"), strings.HasPrefix(image, "http://"):
		return image
	case strings.HasPrefix(image, "/") && !strings.HasPrefix(image, "//"):
		return strings.TrimRight(siteURL, "/") + image
	}
	return ""
}

// editionLabel is the masthead date, e.g. "Thu, Oct 1".
func editionLabel(edition string) string {
	day, err := time.Parse("2006-01-02", edition)
	if err != nil {
		return ""
	}
	return day.Format("Mon, Jan 2")
}

// summary is the story's "Why it matters" line, or its excerpt for older
// articles that have no summary.
func (article DigestArticle) summary() string {
	if why := strings.TrimSpace(article.WhyItMatters); why != "" {
		return why
	}
	return article.Excerpt
}

func digestMeta(article DigestArticle) string {
	details := strconv.Itoa(article.ReadingMinutes) + " min"
	if article.Source != "" {
		details = escapeHTML(article.Source) + " · " + details
	}
	return `<div style="font-size:12px;font-weight:700;letter-spacing:.08em;text-transform:uppercase;color:` + digestAccent + `">` +
		escapeHTML(article.Category) + ` <span style="color:#9a9aa6">· ` + details + `</span></div>`
}

func digestLead(article DigestArticle, link, siteURL string) string {
	var html strings.Builder
	if image := imageURL(article.Image, siteURL); image != "" {
		html.WriteString(`<a href="` + escapeHTML(link) + `"><img src="` + escapeHTML(image) + `" width="600" alt="" style="display:block;width:100%;height:auto;border:0"></a>`)
	}
	html.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr><td class="px" style="padding:24px 32px 8px">`)
	return html.String()
}

func digestStory(article DigestArticle, link, siteURL string) string {
	var html strings.Builder
	html.WriteString(`<tr><td class="rule" style="padding:20px 0;border-top:1px solid #ececf1"><table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr>`)
	if image := imageURL(article.Image, siteURL); image != "" {
		html.WriteString(`<td class="thumb" width="132" valign="top" style="padding-right:16px"><a href="` + escapeHTML(link) + `"><img src="` + escapeHTML(image) +
			`" width="132" height="88" alt="" style="display:block;width:132px;height:88px;object-fit:cover;border-radius:6px;border:0"></a></td>`)
	}
	html.WriteString(`<td valign="top">` + digestMeta(article) +
		`<a href="` + escapeHTML(link) + `" class="title" style="display:block;margin:6px 0;color:#14141a;text-decoration:none;font-size:18px;line-height:1.3;font-weight:800">` + escapeHTML(article.Title) + `</a>` +
		`<div class="body" style="font-size:14px;line-height:1.5;color:#55555f">` + escapeHTML(article.summary()) + `</div></td></tr></table></td></tr>`)
	return html.String()
}

// DigestEmail renders the daily digest: the first article leads with its
// image, "Why it matters" and TL;DR, and the rest follow as compact stories.
// edition is the YYYY-MM-DD edition key.
func DigestEmail(to string, articles []DigestArticle, siteURL, unsubscribeURL, edition string) Email {
	totalMinutes := 0
	for _, article := range articles {
		totalMinutes += article.ReadingMinutes
	}
	intro := strconv.Itoa(len(articles)) + " " + plural(len(articles), "story", "stories") +
		" · about " + strconv.Itoa(totalMinutes) + " " + plural(totalMinutes, "minute", "minutes") + " of reading"
	link := func(article DigestArticle) string {
		return trackedURL(siteURL+"/article/"+encodeURIComponent(article.Slug), edition)
	}
	home := trackedURL(siteURL, edition)

	var content, text strings.Builder
	text.WriteString("AI & Tech News · " + editionLabel(edition) + "\n" + intro + "\n\n")
	if len(articles) > 0 {
		lead := articles[0]
		leadLink := link(lead)
		content.WriteString(digestLead(lead, leadLink, siteURL))
		content.WriteString(`<div class="body" style="font-size:13px;color:#6a6a76;margin-bottom:14px">` + intro + `</div>` + digestMeta(lead) +
			`<a href="` + escapeHTML(leadLink) + `" class="title lead-title" style="display:block;margin:8px 0 14px;color:#14141a;text-decoration:none;font-size:27px;line-height:1.2;font-weight:900">` + escapeHTML(lead.Title) + `</a>`)
		if why := strings.TrimSpace(lead.WhyItMatters); why != "" {
			content.WriteString(`<div class="why" style="background:#f3f0ff;border-left:3px solid ` + digestAccent + `;border-radius:4px;padding:12px 14px;margin:0 0 16px">` +
				`<div style="font-size:11px;font-weight:800;letter-spacing:.1em;text-transform:uppercase;color:` + digestAccent + `;margin-bottom:4px">Why it matters</div>` +
				`<div class="body" style="font-size:15px;line-height:1.5;color:#2a2a33">` + escapeHTML(why) + `</div></div>`)
		} else {
			content.WriteString(`<div class="body" style="font-size:15px;line-height:1.55;color:#3a3a44;margin:0 0 16px">` + escapeHTML(lead.Excerpt) + `</div>`)
		}
		text.WriteString(strings.ToUpper(lead.Category) + "\n" + lead.Title + " (" + strconv.Itoa(lead.ReadingMinutes) + " min)\n" + lead.summary() + "\n")
		if len(lead.TLDR) > 0 {
			content.WriteString(`<table role="presentation" cellpadding="0" cellspacing="0">`)
			for i, point := range lead.TLDR {
				number := strconv.Itoa(i + 1)
				content.WriteString(`<tr><td valign="top" style="padding:0 10px 8px 0;color:` + digestAccent + `;font-weight:800;font-size:14px">` + number +
					`</td><td class="body" style="padding:0 0 8px;font-size:15px;line-height:1.5;color:#3a3a44">` + escapeHTML(point) + `</td></tr>`)
				text.WriteString(number + ". " + point + "\n")
			}
			content.WriteString(`</table>`)
		}
		text.WriteString(leadLink + "\n")
		content.WriteString(`<a href="` + escapeHTML(leadLink) + `" style="display:inline-block;margin:8px 0 6px;background:` + digestAccent +
			`;color:#ffffff;text-decoration:none;font-weight:700;font-size:15px;border-radius:6px;padding:12px 18px">Read the story &rarr;</a></td></tr>`)
		if rest := articles[1:]; len(rest) > 0 {
			content.WriteString(`<tr><td class="px" style="padding:12px 32px 4px"><div style="font-size:12px;font-weight:800;letter-spacing:.12em;text-transform:uppercase;color:#9a9aa6">Also today</div></td></tr>` +
				`<tr><td class="px" style="padding:0 32px 8px"><table role="presentation" width="100%" cellpadding="0" cellspacing="0">`)
			text.WriteString("\nALSO TODAY\n")
			for _, article := range rest {
				content.WriteString(digestStory(article, link(article), siteURL))
				text.WriteString("\n" + article.Title + " (" + article.Category + ", " + strconv.Itoa(article.ReadingMinutes) + " min)\n" + article.summary() + "\n" + link(article) + "\n")
			}
			content.WriteString(`</table></td></tr>`)
		}
		content.WriteString(`<tr><td class="px rule" style="padding:20px 32px 28px;border-top:1px solid #ececf1"><a href="` + escapeHTML(home) +
			`" style="color:` + digestAccent + `;font-weight:700;text-decoration:none;font-size:15px">See all of today's stories &rarr;</a></td></tr></table>`)
	}
	text.WriteString("\nSee all stories: " + home + "\n\nGot this from a friend? Subscribe: " + siteURL + "\nUnsubscribe: " + unsubscribeURL)

	preheader := intro
	if len(articles) > 1 {
		preheader = articles[0].Title + ", plus " + strconv.Itoa(len(articles)-1) + " more " + plural(len(articles)-1, "story", "stories")
	}
	html := `<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="light dark"><meta name="supported-color-schemes" content="light dark"><title>` + escapeHTML(DigestSubject(articles)) + `</title><style>` + digestStyles + `</style></head>
<body class="bg" style="margin:0;padding:0;background:#f4f4f7">
<div style="display:none;max-height:0;overflow:hidden;opacity:0">` + escapeHTML(preheader) + strings.Repeat("&#847;&zwnj;&nbsp;", 30) + `</div>
<table role="presentation" class="bg" width="100%" cellpadding="0" cellspacing="0" style="background:#f4f4f7"><tr><td align="center" style="padding:28px 12px">
<table role="presentation" width="600" cellpadding="0" cellspacing="0" style="max-width:600px;width:100%;font-family:` + digestFont + `">
<tr><td class="px" style="padding:0 8px 18px"><table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr>
<td style="font-size:17px;font-weight:900;color:` + digestAccent + `">AI <span style="color:#9a9aa6">&amp;</span> Tech News</td>
<td align="right" style="font-size:12px;color:#8a8a96">` + escapeHTML(editionLabel(edition)) + ` · Daily Brief</td></tr></table></td></tr>
<tr><td class="card" style="background:#ffffff;border:1px solid #e6e6ec;border-radius:12px;overflow:hidden">` + content.String() + `</td></tr>
<tr><td class="px" style="padding:22px 8px;font-size:12px;line-height:1.7;color:#8a8a96;text-align:center">Got this from a friend? <a href="` + escapeHTML(siteURL) + `" style="color:` + digestAccent + `">Subscribe for the daily brief</a>.<br>You subscribed at aiandtech.news. <a href="` + escapeHTML(unsubscribeURL) + `" style="color:#8a8a96">Unsubscribe</a>.</td></tr>
</table></td></tr></table>
</body></html>`
	return Email{
		To:      to,
		Subject: DigestSubject(articles),
		HTML:    html,
		Text:    text.String(),
		Headers: unsubscribeHeaders(unsubscribeURL),
		Tags:    []Tag{{Name: "email_type", Value: "daily_digest"}},
	}
}
