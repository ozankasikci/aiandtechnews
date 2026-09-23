package newsletter

import (
	"strconv"
	"strings"
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
// (field order and names are Node's).
type DigestArticle struct {
	Title          string `json:"title"`
	Slug           string `json:"slug"`
	Excerpt        string `json:"excerpt"`
	Category       string `json:"category"`
	ReadingMinutes int    `json:"readingMinutes"`
}

// The templates below are apps/server/src/newsletter/email.ts verbatim,
// including every newline and indentation run inside the template literals.

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

type digestSection struct {
	category string
	articles []DigestArticle
}

// groupByCategory is Node's groupByCategory: sections in order of first
// appearance, articles in selection order, categories compared exactly.
func groupByCategory(articles []DigestArticle) []digestSection {
	var sections []digestSection
	for _, article := range articles {
		found := false
		for i := range sections {
			if sections[i].category == article.Category {
				sections[i].articles = append(sections[i].articles, article)
				found = true
				break
			}
		}
		if !found {
			sections = append(sections, digestSection{category: article.Category, articles: []DigestArticle{article}})
		}
	}
	return sections
}

func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}
	return many
}

// DigestEmail is Node's digestEmail.
func DigestEmail(to string, articles []DigestArticle, siteURL, unsubscribeURL string) Email {
	sections := groupByCategory(articles)

	var sectionsHTML strings.Builder
	textSections := make([]string, 0, len(sections))
	for _, section := range sections {
		var items strings.Builder
		textItems := make([]string, 0, len(section.articles))
		for _, article := range section.articles {
			url := siteURL + "/article/" + encodeURIComponent(article.Slug)
			minutes := strconv.Itoa(article.ReadingMinutes)
			items.WriteString(`<div style="padding:0 0 18px">
        <a href="` + escapeHTML(url) + `" style="color:#f5f5f7;text-decoration:none;font-size:19px;line-height:1.3;font-weight:800">` + escapeHTML(article.Title) + `</a>
        <span style="color:#8f8f9c;font-size:13px;white-space:nowrap"> (` + minutes + ` minute read)</span>
        <p style="color:#b8b8c2;line-height:1.55;margin:7px 0 0">` + escapeHTML(article.Excerpt) + `</p>
      </div>`)
			textItems = append(textItems, article.Title+" ("+minutes+" minute read)\n"+article.Excerpt+"\n"+url)
		}
		sectionsHTML.WriteString(`<div style="padding:18px 0 0;border-top:1px solid #292936">
        <div style="color:#9b87f5;font-size:11px;font-weight:800;letter-spacing:.12em;text-transform:uppercase;margin-bottom:12px">` + escapeHTML(section.category) + `</div>
        ` + items.String() + `
      </div>`)
		textSections = append(textSections, jsToUpper(section.category)+"\n\n"+strings.Join(textItems, "\n\n"))
	}

	totalMinutes := 0
	for _, article := range articles {
		totalMinutes += article.ReadingMinutes
	}
	content := `
    <h1 style="margin:0 0 8px;font-size:26px;line-height:1.2">Today in AI and technology</h1>
    <p style="color:#c8c8d0;line-height:1.65;margin:0 0 18px">` + strconv.Itoa(len(articles)) + ` ` + plural(len(articles), "story", "stories") +
		` worth knowing, about ` + strconv.Itoa(totalMinutes) + ` ` + plural(totalMinutes, "minute", "minutes") + ` of reading.</p>
    ` + sectionsHTML.String() + `
    <div style="padding-top:14px;border-top:1px solid #292936">` + button("See all stories", siteURL) + `</div>`
	return Email{
		To:      to,
		Subject: DigestSubject(articles),
		HTML:    emailLayout(content, `You subscribed at aiandtech.news. <a href="`+escapeHTML(unsubscribeURL)+`" style="color:#b7a7ff">Unsubscribe</a>.`),
		Text:    "Today in AI and technology\n\n" + strings.Join(textSections, "\n\n") + "\n\nSee all stories: " + siteURL + "\n\nUnsubscribe: " + unsubscribeURL,
		Headers: unsubscribeHeaders(unsubscribeURL),
		Tags:    []Tag{{Name: "email_type", Value: "daily_digest"}},
	}
}
