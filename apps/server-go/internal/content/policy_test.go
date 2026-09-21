package content

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

var policyNow = time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)

func ptrInt(v int) *int { return &v }

func repeatedWords(n int) string {
	words := make([]string, n)
	for i := range words {
		words[i] = "word"
	}
	return strings.Join(words, " ")
}

func articleWith(paragraphs, wordsEach int) RewrittenArticle {
	p := "<p>" + repeatedWords(wordsEach) + "</p>"
	return RewrittenArticle{
		Title:   "OpenAI Updates Its Developer Platform",
		Excerpt: "OpenAI has updated its developer platform with new controls for teams.",
		Content: strings.Repeat(p, paragraphs),
	}
}

func TestPolicyConstantsAndApprovedFeeds(t *testing.T) {
	want := []ApprovedFeed{
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
	}
	if got := ApprovedFeeds(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ApprovedFeeds() = %#v, want %#v", got, want)
	}
	got := ApprovedFeeds()
	got[0].Source = "changed"
	if ApprovedFeeds()[0].Source != "TechCrunch" {
		t.Fatal("ApprovedFeeds exposed mutable policy state")
	}
	wantAuthor := EditorialAuthorDetails{"TechNews Editorial", "editorial@technews.dev", "The TechNews editorial team covers artificial intelligence."}
	if got := EditorialAuthor(); got != wantAuthor {
		t.Fatalf("EditorialAuthor() = %#v, want %#v", got, wantAuthor)
	}
	if MaxArticlesPerRun != 1 {
		t.Fatalf("MaxArticlesPerRun = %d", MaxArticlesPerRun)
	}
}

func TestSourceForURLParity(t *testing.T) {
	tests := []struct {
		url, source string
		ok          bool
	}{
		{"https://techcrunch.com/example", "TechCrunch", true},
		{"https://www.theverge.com/tech/example", "The Verge", true},
		{"https://arstechnica.com/ai/example", "Ars Technica", true},
		{"https://www.wired.com/story/example", "WIRED", true},
		{"https://www.engadget.com/example", "Engadget", true},
		{"https://www.bleepingcomputer.com/news/example", "BleepingComputer", true},
		{"https://www.theregister.com/2026/example", "The Register", true},
		{"https://www.technologyreview.com/2026/example", "MIT Technology Review", true},
		{"https://venturebeat.com/ai/example", "VentureBeat", true},
		{"https://www.404media.co/example", "404 Media", true},
		{"https://restofworld.org/2026/example", "Rest of World", true},
		{"https://decrypt.co/378101/example", "Decrypt", true},
		{"https://labs.techcrunch.com/example", "TechCrunch", true},
		{"https://WWW.THEVERGE.COM/example", "The Verge", true},
		{"http://techcrunch.com:80/example", "TechCrunch", true},
		{"HTTPS://techcrunch.com/story", "TechCrunch", true},
		{"https://%74echcrunch.com/ai/x", "TechCrunch", true},
		{"https:" + strings.Repeat(`\`, 2) + "techcrunch.com" + `\ai\story`, "TechCrunch", true},
		{"https:techcrunch.com/x", "TechCrunch", true},
		{"ftp://techcrunch.com/example", "", false},
		{"https://eviltechcrunch.com/example", "", false},
		{"https://techcrunch.com.evil.test/example", "", false},
		{"https://news.ycombinator.com/item?id=1", "", false},
		{"not a url", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got, ok := SourceForURL(tt.url)
			if got != tt.source || ok != tt.ok {
				t.Fatalf("SourceForURL(%q) = %q, %v, want %q, %v", tt.url, got, ok, tt.source, tt.ok)
			}
		})
	}
}

func TestNormalizeSourceURLParity(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://techcrunch.com/story/?utm_source=rss&ref=home#section", "https://techcrunch.com/story?ref=home"},
		{"https://techcrunch.com/story?utm_source=rss", "https://techcrunch.com/story"},
		{"https://www.techcrunch.com:443/story///?z=1&utm_Source=x&a=2&gclid=y&z=3#frag", "https://www.techcrunch.com/story?z=1&a=2&z=3"},
		{"http://techcrunch.com:80/story/", "http://techcrunch.com/story"},
		{"https://techcrunch.com", "https://techcrunch.com/"},
		{"https://techcrunch.com////", "https://techcrunch.com/"},
		{"ftp://TechCrunch.com:21/story/?utm_source=x&x=%2f", "ftp://techcrunch.com/story?x=%2F"},
		{"mailto:test@example.com?utm_source=x", "mailto:test@example.com"},
		{"https://techcrunch.com:444/story/?b=hello%20world&a=%7E", "https://techcrunch.com:444/story?b=hello%20world&a=%7E"},
		{"https://techcrunch.com/?Ref=one&UTM_X=x&ref=two", "https://techcrunch.com/?Ref=one&ref=two"},
		{"https://techcrunch.com/a%2F///?utm_x=1", "https://techcrunch.com/a%2F"},
		{"https://techcrunch.com/story%2F/?utm_x=1", "https://techcrunch.com/story%2F"},
		{"https://techcrunch.com/x?utm_x=1&flag", "https://techcrunch.com/x?flag="},
		{"https://techcrunch.com/x?utm_x=1&&a=2", "https://techcrunch.com/x?a=2"},
		{"https://techcrunch.com/x?&a=2", "https://techcrunch.com/x?&a=2"},
		{"https://techcrunch.com/x?a=hello world", "https://techcrunch.com/x?a=hello%20world"},
		{"https://techcrunch.com/x?a='é'<x>", "https://techcrunch.com/x?a=%27%C3%A9%27%3Cx%3E"},
		{"mailto:test@example.com?q='é'<x>", "mailto:test@example.com?q='%C3%A9'%3Cx%3E"},
		{"https://techcrunch.com/x?utm_x=1&bad=%FF", "https://techcrunch.com/x?bad=%EF%BF%BD"},
		{"https://techcrunch.com/x?utm_x=1&bad=%E0%A4%A", "https://techcrunch.com/x?bad=%EF%BF%BD%25A"},
		{"https://techcrunch.com/a/../story/?utm_source=x", "https://techcrunch.com/story"},
		{"https://techcrunch.com/a/%2e%2e/story/", "https://techcrunch.com/story"},
		{"https://techcrunch.com/a/.%2E/story/", "https://techcrunch.com/story"},
		{"https://techcrunch.com/a/%2e./story/", "https://techcrunch.com/story"},
		{"https://techcrunch.com/a//../b", "https://techcrunch.com/a/b"},
		{"https://techcrunch.com/a/.", "https://techcrunch.com/a"},
		{"https://techcrunch.com/a/..", "https://techcrunch.com/"},
		{"https://techcrunch.com/a|b", "https://techcrunch.com/a|b"},
		{"https://techcrunch.com/%zz", "https://techcrunch.com/%zz"},
		{"HTTPS://techcrunch.com/story", "https://techcrunch.com/story"},
		{"https:" + strings.Repeat(`\`, 2) + "techcrunch.com" + `\ai\story`, "https://techcrunch.com/ai/story"},
		{"https:" + strings.Repeat(`\`, 2) + "techcrunch.com" + `\ai\story?q=\a#frag`, "https://techcrunch.com/ai/story?q=" + `\a`},
		{"https:techcrunch.com/x", "https://techcrunch.com/x"},
	}
	for _, tt := range tests {
		got, err := NormalizeSourceURL(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("NormalizeSourceURL(%q) = %q, %v, want %q", tt.in, got, err, tt.want)
		}
	}
	for _, in := range []string{"not a url", "://", "https://"} {
		if _, err := NormalizeSourceURL(in); err == nil {
			t.Errorf("NormalizeSourceURL(%q) did not fail", in)
		}
	}
}

func TestTextHelpersParity(t *testing.T) {
	long := strings.Repeat("a", 121)
	tests := []struct{ in, want string }{
		{"OpenAI's New API: What Changed?", "openai-s-new-api-what-changed"},
		{"  Café & AI___News  ", "caf-ai-news"},
		{"---", ""},
		{long, strings.Repeat("a", 120)},
	}
	for _, tt := range tests {
		if got := Slugify(tt.in); got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if got := StripHTML(`<style>x{}</style><p>Hello&nbsp; &amp; &lt;AI&gt; &quot;x&quot; &#39;y&#39;</p><script>alert(1)</script>`); got != `Hello & <AI> "x" 'y'` {
		t.Fatalf("StripHTML() = %q", got)
	}
	if got := StripHTML("<p>one</p>\n\t<p>two &AMP; three</p>"); got != "one two & three" {
		t.Fatalf("StripHTML whitespace = %q", got)
	}
	if got := WordCount("<p>one&nbsp; two</p><h2>three</h2>"); got != 3 {
		t.Fatalf("WordCount = %d", got)
	}
	if got := StripHTML("one\u0085two"); got != "one\u0085two" {
		t.Fatalf("StripHTML JavaScript whitespace parity = %q", got)
	}
	if got := WordCount("one\u0085two"); got != 1 {
		t.Fatalf("WordCount JavaScript whitespace parity = %d", got)
	}
	if got := WordCount("<p> </p>"); got != 0 {
		t.Fatalf("empty WordCount = %d", got)
	}
}

func TestItemRejectionExactParityVectors(t *testing.T) {
	tests := []struct{ name, title, url, expected, want string }{
		{"unapproved", "OpenAI news", "https://news.ycombinator.com/item?id=1", "", "source is not on the approved publication list"},
		{"scheme", "OpenAI news", "ftp://techcrunch.com/news", "TechCrunch", "source is not on the approved publication list"},
		{"expected source", "OpenAI news", "https://techcrunch.com/news", "The Verge", "source URL does not match its RSS publication"},
		{"show hn", " Show HN: My app", "https://techcrunch.com/app", "TechCrunch", "Show HN item"},
		{"state show hn", "State of Show HN projects", "https://techcrunch.com/app", "TechCrunch", "Show HN item"},
		{"review", "AI phone review", "https://techcrunch.com/ai/phone", "TechCrunch", "review, guide, or roundup rather than news"},
		{"hands on title", "AI hands-on report", "https://techcrunch.com/ai/phone", "TechCrunch", "review, guide, or roundup rather than news"},
		{"pdf title", "AI research [PDF]", "https://techcrunch.com/ai/paper", "TechCrunch", "PDF or video item"},
		{"video title", "AI research [video]", "https://techcrunch.com/ai/paper", "TechCrunch", "PDF or video item"},
		{"abstract", "Abstract: AI systems", "https://techcrunch.com/ai/paper", "TechCrunch", "abstract or arXiv-style item"},
		{"abstract unicode whitespace", "Abstract\u00a0: AI systems", "https://techcrunch.com/ai/paper", "TechCrunch", "abstract or arXiv-style item"},
		{"arxiv", "An arXiv AI paper", "https://techcrunch.com/ai/paper", "TechCrunch", "arXiv-style item"},
		{"coupon", "AI coupon codes", "https://techcrunch.com/ai/news", "TechCrunch", "deal or promotional item"},
		{"top deals", "Top five AI laptop deals", "https://techcrunch.com/ai/news", "TechCrunch", "deal or promotional item"},
		{"top deals unicode whitespace", "Top\u00a0five\u00a0AI\u00a0laptop\u00a0deals", "https://techcrunch.com/ai/news", "TechCrunch", "deal or promotional item"},
		{"deal save", "AI deals that save 20% off", "https://techcrunch.com/ai/news", "TechCrunch", "deal or promotional item"},
		{"price", "Lowest price for an AI laptop", "https://techcrunch.com/ai/news", "TechCrunch", "deal or promotional item"},
		{"drop", "AI laptop price drop", "https://techcrunch.com/ai/news", "TechCrunch", "deal or promotional item"},
		{"save", "Save $20 on this AI app", "https://techcrunch.com/ai/news", "TechCrunch", "deal or promotional item"},
		{"holiday", "AI offers for Black Friday", "https://techcrunch.com/ai/news", "TechCrunch", "deal or promotional item"},
		{"preorder", "Last Chance For AI Phone Preorder Bonuses", "https://techcrunch.com/ai/news", "TechCrunch", "deal or promotional item"},
		{"pdf path", "OpenAI paper", "https://techcrunch.com/files/paper.PDF", "TechCrunch", "PDF or video item"},
		{"video path", "OpenAI update", "https://techcrunch.com/videos/update", "TechCrunch", "video, deal, or promotional item"},
		{"deal section", "OpenAI update", "https://techcrunch.com/deal/update", "TechCrunch", "video, deal, or promotional item"},
		{"encoded promo", "OpenAI update", "https://techcrunch.com/ai/last%20chance", "TechCrunch", "deal or promotional item"},
		{"installer", "OpenAI update", "https://techcrunch.com/tech/apps-hardware-installer", "TechCrunch", "review, guide, or roundup rather than news"},
		{"collapsed review separators", "OpenAI update", "https://techcrunch.com/ai/buying--guide", "TechCrunch", "deal or promotional item"},
		{"collapsed promotion separators", "OpenAI update", "https://techcrunch.com/ai/deal-----------------------------------------save", "TechCrunch", "deal or promotional item"},
		{"literal pipe promotion boundary", "OpenAI update", "https://techcrunch.com/foo/deal|save", "TechCrunch", "deal or promotional item"},
		{"old title", "AI classic (2024)", "https://techcrunch.com/ai/classic", "TechCrunch", "obviously old repost"},
		{"old title unicode whitespace", "AI classic (2024)\u00a0", "https://techcrunch.com/ai/classic", "TechCrunch", "obviously old repost"},
		{"old url", "OpenAI classic", "https://techcrunch.com/2024/08/classic", "TechCrunch", "obviously old repost"},
		{"non ai", "Apple releases a security update", "https://techcrunch.com/tech/update", "TechCrunch", "not clearly AI-related; only AI news may be published"},
		{"accepted title", "OpenAI updates ChatGPT", "https://www.theverge.com/tech/openai", "The Verge", ""},
		{"accepted section", "New safeguards arrive", "https://www.technologyreview.com/ai/2026/09/11/safeguards/", "MIT Technology Review", ""},
		{"transaction deal", "Anthropic continues compute streak in $45B deal with Nscale", "https://techcrunch.com/2026/08/26/anthropic-45-billion-deal-with-nscale/", "TechCrunch", ""},
		{"Samsung preorder retained vector", "Last Chance For Samsung Galaxy Z Fold 8 And Flip 8 Preorder Bonuses", "https://www.theverge.com/gadgets/976103/samsung-galaxy-z-fold-flip-8-preorder-airpods-pro-3-deal-sale", "The Verge", "deal or promotional item"},
		{"corporate agreement retained vector", "Anthropic continues compute-gobbling streak in $45B deal with Nscale", "https://techcrunch.com/2026/08/26/anthropic-continues-compute-gobbling-streak-in-45-billion-deal-with-nscale/", "TechCrunch", ""},
		{"Spider-Man box office retained vector", "Spider-Man: Brand New Day Smashes Box Office Records With $1 Billion Worldwide Opening", "https://www.theverge.com/entertainment/975297/spider-man-brand-new-day-marvel-sony-xmen-doomsday", "The Verge", "not clearly AI-related; only AI news may be published"},
		{"Apple security retained vector", "Apple releases a macOS security update", "https://www.theverge.com/tech/975300/apple-macos-security-update", "The Verge", "not clearly AI-related; only AI news may be published"},
		{"OpenAI launch retained vector", "OpenAI launches a new model for developers", "https://techcrunch.com/2026/08/05/openai-launches-a-new-model-for-developers/", "TechCrunch", ""},
		{"frontier model retained vector", "New safeguards arrive for frontier models", "https://www.technologyreview.com/ai/2026/09/11/frontier-model-safeguards/", "MIT Technology Review", ""},
		{"summer fail closed retained vector", "Summer travel destinations attracting record crowds", "https://techcrunch.com/2026/08/05/summer-travel-destinations/", "TechCrunch", "not clearly AI-related; only AI news may be published"},
		{"Ted Lasso retained vector", "Ted Lasso Returns for Season Four Alongside New Tech and App Releases", "https://www.theverge.com/tech/977084/ted-lasso-bose-tony-installer", "The Verge", "review, guide, or roundup rather than news"},
		{"NASA retained vector", "NASA Perseverance Rover Nears Mars Distance Record", "https://arstechnica.com/space/2026/08/the-first-self-driving-vehicle-on-mars-has-proven-to-be-a-smashing-success", "Ars Technica", "not clearly AI-related; only AI news may be published"},
		{"Spider-Man app retained vector", "Spider-Man Season Four Arrives With a New Mobile App", "https://www.theverge.com/tech/975297/spider-man-season-four-mobile-app", "The Verge", "not clearly AI-related; only AI news may be published"},
		{"Chuwi review retained vector", "Review: The $450 Chuwi UniBook Laptop Falls Short", "https://www.theverge.com/tech/977031/chuwi-unibook-laptop-intel-wildcat-lake-review", "The Verge", "review, guide, or roundup rather than news"},
		{"Installer retained vector", "New apps and hardware to try this weekend", "https://www.theverge.com/tech/977084/apps-hardware-installer", "The Verge", "review, guide, or roundup rather than news"},
		{"cutoff title", "AI classic (2025)", "https://techcrunch.com/ai/classic", "TechCrunch", ""},
		{"future title", "AI classic (2027)", "https://techcrunch.com/ai/classic", "TechCrunch", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ItemRejectionReason(tt.title, tt.url, tt.expected, policyNow); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			if got := AutomaticItemRejectionReason(tt.title, tt.url, tt.expected, policyNow); got != tt.want {
				t.Fatalf("automatic got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEveryAITitleAndURLSignal(t *testing.T) {
	titles := []string{"AI", "artificial intelligence", "generative AI", "machine learning", "deep learning", "LLM", "large language model", "ChatGPT", "chatbot", "OpenAI", "Anthropic", "Gemini", "Claude", "neural network", "foundation model", "frontier model", "Apple Intelligence", "Microsoft Copilot"}
	for _, signal := range titles {
		t.Run("title "+signal, func(t *testing.T) {
			if got := ItemRejectionReason(signal+" update", "https://techcrunch.com/news/update", "TechCrunch", policyNow); got != "" {
				t.Fatalf("got %q", got)
			}
		})
	}
	paths := []string{"/ai/story", "/category/ai/story", "/ai-artificial-intelligence/story", "/artificial-intelligence/story"}
	for _, path := range paths {
		if got := ItemRejectionReason("New safeguards arrive", "https://techcrunch.com"+path, "TechCrunch", policyNow); got != "" {
			t.Errorf("path %q got %q", path, got)
		}
	}
	for _, weak := range []string{"software", "cybersecurity", "chips", "apps", "startups", "robotics"} {
		if got := ItemRejectionReason("New "+weak+" update", "https://techcrunch.com/tech/update", "TechCrunch", policyNow); !strings.Contains(got, "not clearly AI-related") {
			t.Errorf("weak signal %q got %q", weak, got)
		}
	}
}

func TestValidateRewrittenArticleExactErrorOrderAndDedup(t *testing.T) {
	article := RewrittenArticle{
		Title:   strings.Repeat("A", 121) + "?! Last chance sale Groundbreaking",
		Excerpt: "<b>First revolutionary sentence</b>.\nSecond sentence.",
		Content: `<div game="changing">In a move that it remains to be seen ` + "\u2014" + ` game-changing</div><p class="x"></p><p></p><p></p><p></p><p>Source: Example</p>outside`,
	}
	want := []string{
		"headline exceeds 120 characters",
		"headline appears clickbait-like",
		"headline is promotional",
		"excerpt contains HTML",
		"excerpt is not one plain line",
		"excerpt must be exactly one sentence",
		"copy contains prohibited em dash",
		"copy contains prohibited In a move that",
		"copy contains prohibited It remains to be seen",
		"copy contains prohibited groundbreaking",
		"copy contains prohibited revolutionary",
		"copy contains prohibited game-changing",
		"article HTML contains tags other than p or h2",
		"article HTML tags contain attributes",
		"article HTML contains text outside p or h2 blocks",
		"article must contain 5 to 12 paragraphs",
		"article contains an empty paragraph",
		"article must contain 150 to 800 words, found 14",
		"article contains a source footer",
	}
	if got := ValidateRewrittenArticle(article, ArticleValidationOptions{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("errors = %#v\nwant   = %#v", got, want)
	}
}

func TestValidateTitleAndExcerptEdges(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RewrittenArticle)
		want   []string
	}{
		{"missing", func(a *RewrittenArticle) { a.Title = " "; a.Excerpt = " " }, []string{"headline is missing", "excerpt is missing"}},
		{"missing byte order mark", func(a *RewrittenArticle) { a.Title = "\ufeff"; a.Excerpt = "\ufeff" }, []string{"headline is missing", "excerpt is missing"}},
		{"title 120", func(a *RewrittenArticle) { a.Title = strings.Repeat("a", 120) }, []string{}},
		{"single question", func(a *RewrittenArticle) { a.Title = "OpenAI asks a question?" }, []string{}},
		{"double question", func(a *RewrittenArticle) { a.Title = "OpenAI asks??" }, []string{"headline appears clickbait-like"}},
		{"excerpt 180", func(a *RewrittenArticle) { a.Excerpt = strings.Repeat("a", 179) + "." }, []string{}},
		{"excerpt 181", func(a *RewrittenArticle) { a.Excerpt = strings.Repeat("a", 180) + "." }, []string{"excerpt exceeds 180 characters"}},
		{"no sentence", func(a *RewrittenArticle) { a.Excerpt = "OpenAI updated the service" }, []string{"excerpt must be exactly one sentence"}},
		{"two sentences", func(a *RewrittenArticle) { a.Excerpt = "OpenAI updated the service. Teams can use it." }, []string{"excerpt must be exactly one sentence"}},
		{"two sentences unicode whitespace", func(a *RewrittenArticle) { a.Excerpt = "OpenAI updated the service.\u00a0Teams can use it." }, []string{"excerpt must be exactly one sentence"}},
		{"two sentences byte order mark", func(a *RewrittenArticle) { a.Excerpt = "OpenAI updated the service.\ufeffTeams can use it." }, []string{"excerpt must be exactly one sentence"}},
		{"quoted sentence", func(a *RewrittenArticle) { a.Excerpt = `OpenAI called it "ready."` }, []string{}},
		{"trailing backslash is not sentence punctuation", func(a *RewrittenArticle) { a.Excerpt = "OpenAI updated.\\" }, []string{"excerpt must be exactly one sentence"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := articleWith(5, 30)
			tt.mutate(&a)
			if got := ValidateRewrittenArticle(a, ArticleValidationOptions{}); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestValidateHTMLRules(t *testing.T) {
	tests := []struct{ name, content, want string }{
		{"h2 allowed", "<h2>Context</h2>" + strings.Repeat("<p>"+repeatedWords(30)+"</p>", 5), ""},
		{"outer byte order mark", "\ufeff" + strings.Repeat("<p>"+repeatedWords(30)+"</p>", 5) + "\ufeff", ""},
		{"invalid tag", strings.Repeat("<p>"+repeatedWords(30)+"</p>", 5) + "<h3>More</h3>", "article HTML contains tags other than p or h2"},
		{"attribute", "<p class=\"x\">" + repeatedWords(30) + "</p>" + strings.Repeat("<p>"+repeatedWords(30)+"</p>", 4), "article HTML tags contain attributes"},
		{"outside", "outside" + strings.Repeat("<p>"+repeatedWords(30)+"</p>", 5), "article HTML contains text outside p or h2 blocks"},
		{"empty", "<p>&nbsp;</p>" + strings.Repeat("<p>"+repeatedWords(38)+"</p>", 4), "article contains an empty paragraph"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := articleWith(5, 30)
			a.Content = tt.content
			got := ValidateRewrittenArticle(a, ArticleValidationOptions{})
			if tt.want == "" && len(got) != 0 {
				t.Fatalf("got %#v", got)
			}
			if tt.want != "" && !containsString(got, tt.want) {
				t.Fatalf("got %#v, missing %q", got, tt.want)
			}
		})
	}
}

func TestValidateRetainedAcceptedArticleVector(t *testing.T) {
	paragraph := "The company shared a detailed product update with customers and developers today. The release changes how teams use the service while keeping its existing tools available. Executives described the update without announcing new pricing or making unsupported performance claims. Customers can review the published documentation before deciding whether the changes fit their work."
	article := RewrittenArticle{
		Title:   "OpenAI Updates Its Developer Platform",
		Excerpt: "OpenAI has updated its developer platform with new controls for teams.",
		Content: strings.Repeat("<p>"+paragraph+"</p>", 5),
	}
	if got := ValidateRewrittenArticle(article, ArticleValidationOptions{}); len(got) != 0 {
		t.Fatalf("retained accepted article got %#v", got)
	}
}

func TestValidateParagraphAndWordBoundaries(t *testing.T) {
	tests := []struct {
		name                      string
		paragraphs, words         int
		paragraphError, wordError bool
	}{
		{"four paragraphs", 4, 38, true, false},
		{"five paragraphs", 5, 30, false, false},
		{"twelve paragraphs", 12, 40, false, false},
		{"thirteen paragraphs", 13, 40, true, false},
		{"149 words", 1, 149, true, true},
		{"150 words", 5, 30, false, false},
		{"800 words", 10, 80, false, false},
		{"mid-length 480 words retained ceiling regression", 8, 60, false, false},
		{"801 words", 9, 89, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateRewrittenArticle(articleWith(tt.paragraphs, tt.words), ArticleValidationOptions{})
			if containsString(got, "article must contain 5 to 12 paragraphs") != tt.paragraphError {
				t.Fatalf("paragraph errors %#v", got)
			}
			hasWord := false
			for _, e := range got {
				if strings.HasPrefix(e, "article must contain 150 to 800 words") {
					hasWord = true
				}
			}
			if hasWord != tt.wordError {
				t.Fatalf("word errors %#v", got)
			}
		})
	}
}

func TestValidationOptionsDistinguishZeroAndFooter(t *testing.T) {
	a := articleWith(5, 1)
	if got := ValidateRewrittenArticle(a, ArticleValidationOptions{MinWords: ptrInt(0), MaxWords: ptrInt(0)}); !containsString(got, "article must contain 0 to 0 words, found 5") {
		t.Fatalf("zero options got %#v", got)
	}
	a = articleWith(5, 30)
	a.Content = strings.Repeat("<p>"+repeatedWords(37)+"</p>", 4) + "<p>Sources: Example</p>"
	if got := ValidateRewrittenArticle(a, ArticleValidationOptions{}); !containsString(got, "article contains a source footer") {
		t.Fatalf("footer got %#v", got)
	}
	a.Content = strings.Repeat("<p>"+repeatedWords(37)+"</p>", 4) + "<p>Sources\u00a0: Example</p>"
	if got := ValidateRewrittenArticle(a, ArticleValidationOptions{}); !containsString(got, "article contains a source footer") {
		t.Fatalf("unicode whitespace footer got %#v", got)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
