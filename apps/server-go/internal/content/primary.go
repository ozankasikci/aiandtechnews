package content

import (
	"regexp"
	"strings"
	"time"
)

// Primary sources are the places news comes from: company blogs, newsrooms,
// changelogs and release feeds. An item from one is reported from the
// document itself (an "original"), not rewritten from another outlet.

// PrimaryItemMaxAge is how old a primary feed item may be. Company feeds
// carry years of posts, and only what is new is news.
const PrimaryItemMaxAge = 48 * time.Hour

// StalePrimaryItemReason is the rejection reason for older or undated items.
const StalePrimaryItemReason = "primary source item is undated or older than 2 days"

var primaryFeeds = [...]ApprovedFeed{
	{Source: "OpenAI", URL: "https://openai.com/news/rss.xml"},
	// Anthropic has no feed; its sitemap is watched instead.
	{Source: "Anthropic", URL: "https://www.anthropic.com/sitemap.xml", SitemapPaths: []string{"/news/", "/research/", "/engineering/"}},
	{Source: "Google", URL: "https://blog.google/technology/ai/rss/"},
	{Source: "Google DeepMind", URL: "https://deepmind.google/blog/rss.xml"},
	{Source: "Google Research", URL: "https://research.google/blog/rss/"},
	{Source: "Google Developers", URL: "https://developers.googleblog.com/feeds/posts/default"},
	{Source: "Meta", URL: "https://about.fb.com/news/feed/"},
	{Source: "Microsoft", URL: "https://blogs.microsoft.com/feed/"},
	{Source: "Microsoft Research", URL: "https://www.microsoft.com/en-us/research/feed/"},
	{Source: "Nvidia", URL: "https://blogs.nvidia.com/feed/"},
	{Source: "Nvidia Newsroom", URL: "https://nvidianews.nvidia.com/releases.xml"},
	{Source: "Mistral AI", URL: "https://mistral.ai/rss.xml"},
	{Source: "Hugging Face", URL: "https://huggingface.co/blog/feed.xml"},
	{Source: "AWS", URL: "https://aws.amazon.com/blogs/machine-learning/feed/"},
	{Source: "Apple Machine Learning Research", URL: "https://machinelearning.apple.com/rss.xml"},
	{Source: "Apple", URL: "https://www.apple.com/newsroom/rss-feed.rss"},
	{Source: "GitHub", URL: "https://github.blog/feed/"},
	{Source: "GitHub", URL: "https://github.blog/changelog/feed/"},
	{Source: "Cloudflare", URL: "https://blog.cloudflare.com/rss/"},
	{Source: "Together AI", URL: "https://www.together.ai/blog/rss.xml"},
	{Source: "Ollama", URL: "https://ollama.com/blog/rss.xml"},
	{Source: "Databricks", URL: "https://www.databricks.com/feed"},
	{Source: "Cursor", URL: "https://cursor.com/changelog/rss.xml"},
	{Source: "Replicate", URL: "https://replicate.com/blog/rss"},
	{Source: "Waymo", URL: "https://waymo.com/blog/rss.xml"},
	{Source: "IBM Research", URL: "https://research.ibm.com/rss"},
	// Release feeds: titles are bare version numbers, so the project name is added.
	{Source: "Ollama releases", URL: "https://github.com/ollama/ollama/releases.atom", TitlePrefix: "Ollama"},
	{Source: "Transformers releases", URL: "https://github.com/huggingface/transformers/releases.atom", TitlePrefix: "Hugging Face Transformers"},
	{Source: "OpenAI Codex releases", URL: "https://github.com/openai/codex/releases.atom", TitlePrefix: "OpenAI Codex"},
	{Source: "Claude Code releases", URL: "https://github.com/anthropics/claude-code/releases.atom", TitlePrefix: "Claude Code"},
	{Source: "Gemini CLI releases", URL: "https://github.com/google-gemini/gemini-cli/releases.atom", TitlePrefix: "Gemini CLI"},
	{Source: "vLLM releases", URL: "https://github.com/vllm-project/vllm/releases.atom", TitlePrefix: "vLLM"},
	{Source: "Open WebUI releases", URL: "https://github.com/open-webui/open-webui/releases.atom", TitlePrefix: "Open WebUI"},
	{Source: "ComfyUI releases", URL: "https://github.com/comfyanonymous/ComfyUI/releases.atom", TitlePrefix: "ComfyUI"},
}

// primaryHosts maps a primary source to its domain and, where one domain
// hosts several sources, a lowercase path prefix. More specific entries come
// first.
var primaryHosts = [...]struct {
	source string
	domain string
	path   string
}{
	{"OpenAI", "openai.com", ""},
	{"Anthropic", "anthropic.com", ""},
	{"Google", "blog.google", ""},
	{"Google DeepMind", "deepmind.google", ""},
	{"Google Research", "research.google", ""},
	{"Google Developers", "developers.googleblog.com", ""},
	{"Meta", "about.fb.com", ""},
	{"Microsoft", "blogs.microsoft.com", ""},
	{"Microsoft Research", "microsoft.com", "/en-us/research/"},
	{"Nvidia Newsroom", "nvidianews.nvidia.com", ""},
	{"Nvidia", "blogs.nvidia.com", ""},
	{"Mistral AI", "mistral.ai", ""},
	{"Hugging Face", "huggingface.co", "/blog/"},
	{"AWS", "aws.amazon.com", "/blogs/"},
	{"Apple Machine Learning Research", "machinelearning.apple.com", ""},
	{"Apple", "apple.com", "/newsroom/"},
	{"GitHub", "github.blog", ""},
	{"Cloudflare", "blog.cloudflare.com", ""},
	{"Together AI", "together.ai", ""},
	{"Ollama", "ollama.com", ""},
	{"Databricks", "databricks.com", ""},
	{"Cursor", "cursor.com", ""},
	{"Replicate", "replicate.com", ""},
	{"Waymo", "waymo.com", ""},
	{"IBM Research", "research.ibm.com", ""},
	{"Ollama releases", "github.com", "/ollama/ollama/"},
	{"Transformers releases", "github.com", "/huggingface/transformers/"},
	{"OpenAI Codex releases", "github.com", "/openai/codex/"},
	{"Claude Code releases", "github.com", "/anthropics/claude-code/"},
	{"Gemini CLI releases", "github.com", "/google-gemini/gemini-cli/"},
	{"vLLM releases", "github.com", "/vllm-project/vllm/"},
	{"Open WebUI releases", "github.com", "/open-webui/open-webui/"},
	{"ComfyUI releases", "github.com", "/comfyanonymous/comfyui/"},
}

// PrereleaseReason is the rejection reason for alpha, beta, release
// candidate, nightly and per-commit builds in release feeds.
const PrereleaseReason = "pre-release or nightly build"

var prereleaseTitle = regexp.MustCompile(`(?i)\b(?:alpha|beta|nightly|preview|canary|dev)\b|\d[-.]?rc[-.]?\d*\b|viable/|trunk/|/`)

// IsPrerelease reports whether a release feed title names a pre-release,
// nightly or per-commit build rather than a stable release.
func IsPrerelease(title string) bool { return prereleaseTitle.MatchString(title) }

// PrimaryFeeds returns the primary source feeds, each marked Primary.
func PrimaryFeeds() []ApprovedFeed {
	feeds := make([]ApprovedFeed, len(primaryFeeds))
	for i, feed := range primaryFeeds {
		feed.Primary = true
		feeds[i] = feed
	}
	return feeds
}

// PrimaryFeedURLs returns the URLs of the primary source feeds.
func PrimaryFeedURLs() []string {
	urls := make([]string, len(primaryFeeds))
	for i, feed := range primaryFeeds {
		urls[i] = feed.URL
	}
	return urls
}

// IsPrimaryFeed reports whether feedURL is a primary source feed.
func IsPrimaryFeed(feedURL string) bool {
	for _, feed := range primaryFeeds {
		if feed.URL == feedURL {
			return true
		}
	}
	return false
}

// primaryPublishers names the organisation behind a source whose name is
// not the organisation itself.
var primaryPublishers = map[string]string{
	"Google Developers":               "Google",
	"Nvidia Newsroom":                 "Nvidia",
	"Apple Machine Learning Research": "Apple",
	"Ollama releases":                 "Ollama",
	"Transformers releases":           "Hugging Face",
	"OpenAI Codex releases":           "OpenAI",
	"Claude Code releases":            "Anthropic",
	"Gemini CLI releases":             "Google",
	"vLLM releases":                   "the vLLM project",
	"Open WebUI releases":             "the Open WebUI project",
	"ComfyUI releases":                "the ComfyUI project",
}

// PrimaryPublisher returns the organisation that publishes a primary source.
func PrimaryPublisher(source string) string {
	if publisher, ok := primaryPublishers[source]; ok {
		return publisher
	}
	return source
}

// IsPrimarySource reports whether source names a primary source.
func IsPrimarySource(source string) bool {
	for _, candidate := range primaryHosts {
		if candidate.source == source {
			return true
		}
	}
	return false
}

func primarySourceFor(host, path string) (string, bool) {
	path = strings.ToLower(path)
	for _, candidate := range primaryHosts {
		if host != candidate.domain && !strings.HasSuffix(host, "."+candidate.domain) {
			continue
		}
		if candidate.path == "" || strings.HasPrefix(path, candidate.path) {
			return candidate.source, true
		}
	}
	return "", false
}
