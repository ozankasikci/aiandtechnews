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
	{Source: "Google", URL: "https://blog.google/rss/"},
	{Source: "Google Cloud", URL: "https://cloudblog.withgoogle.com/rss/"},
	{Source: "Meta Engineering", URL: "https://engineering.fb.com/feed/"},
	{Source: "Microsoft Azure", URL: "https://azure.microsoft.com/en-us/blog/feed/"},
	{Source: "Microsoft Security", URL: "https://www.microsoft.com/en-us/security/blog/feed/"},
	{Source: "Microsoft Foundry", URL: "https://devblogs.microsoft.com/foundry/feed/"},
	{Source: "AWS", URL: "https://aws.amazon.com/blogs/aws/feed/"},
	{Source: "Amazon Science", URL: "https://www.amazon.science/index.rss"},
	{Source: "Amazon", URL: "https://www.aboutamazon.com/news/rss"},
	{Source: "IBM", URL: "https://newsroom.ibm.com/press-releases-artificial-intelligence?pagetemplate=rss"},
	{Source: "Salesforce", URL: "https://www.salesforce.com/news/feed/"},
	{Source: "Figma", URL: "https://www.figma.com/blog/feed/atom.xml"},
	{Source: "Stripe", URL: "https://stripe.com/blog/feed.rss"},
	{Source: "Vercel", URL: "https://vercel.com/atom"},
	{Source: "Docker", URL: "https://www.docker.com/feed/"},
	{Source: "JetBrains", URL: "https://blog.jetbrains.com/ai/feed/"},
	// Cohere has no feed; its sitemap is watched instead.
	{Source: "Cohere", URL: "https://cohere.com/sitemap.xml", SitemapPaths: []string{"/blog/", "/research/"}},
	// Chip makers.
	{Source: "AMD", URL: "https://ir.amd.com/rss/news-releases.xml"},
	{Source: "Arm", URL: "https://newsroom.arm.com/feed"},
	{Source: "SK hynix", URL: "https://news.skhynix.com/feed/"},
	// Regulators and governments.
	{Source: "FTC", URL: "https://www.ftc.gov/feeds/press-release.xml"},
	{Source: "SEC", URL: "https://www.sec.gov/news/pressreleases.rss"},
	{Source: "NIST", URL: "https://www.nist.gov/news-events/news/rss.xml"},
	{Source: "CISA", URL: "https://www.cisa.gov/news.xml"},
	{Source: "European Commission", URL: "https://digital-strategy.ec.europa.eu/en/rss.xml"},
	{Source: "UK Government", URL: "https://www.gov.uk/government/organisations/ai-security-institute.atom"},
	{Source: "UK Government", URL: "https://www.gov.uk/government/organisations/department-for-science-innovation-and-technology.atom"},
	// Universities and research groups.
	{Source: "MIT News", URL: "https://news.mit.edu/topic/mitartificial-intelligence2-rss.xml"},
	{Source: "Berkeley AI Research", URL: "https://bair.berkeley.edu/blog/feed.xml"},
	{Source: "Carnegie Mellon University", URL: "https://www.cs.cmu.edu/news/feed"},
	// Release feeds: titles are bare version numbers, so the project name is added.
	{Source: "Ollama releases", URL: "https://github.com/ollama/ollama/releases.atom", TitlePrefix: "Ollama"},
	{Source: "Transformers releases", URL: "https://github.com/huggingface/transformers/releases.atom", TitlePrefix: "Hugging Face Transformers"},
	{Source: "OpenAI Codex releases", URL: "https://github.com/openai/codex/releases.atom", TitlePrefix: "OpenAI Codex"},
	{Source: "Claude Code releases", URL: "https://github.com/anthropics/claude-code/releases.atom", TitlePrefix: "Claude Code"},
	{Source: "Gemini CLI releases", URL: "https://github.com/google-gemini/gemini-cli/releases.atom", TitlePrefix: "Gemini CLI"},
	{Source: "vLLM releases", URL: "https://github.com/vllm-project/vllm/releases.atom", TitlePrefix: "vLLM"},
	{Source: "Open WebUI releases", URL: "https://github.com/open-webui/open-webui/releases.atom", TitlePrefix: "Open WebUI"},
	{Source: "ComfyUI releases", URL: "https://github.com/comfyanonymous/ComfyUI/releases.atom", TitlePrefix: "ComfyUI"},
	{Source: "VS Code releases", URL: "https://github.com/microsoft/vscode/releases.atom", TitlePrefix: "Visual Studio Code"},
	{Source: "Zed releases", URL: "https://github.com/zed-industries/zed/releases.atom", TitlePrefix: "Zed"},
	{Source: "Cline releases", URL: "https://github.com/cline/cline/releases.atom", TitlePrefix: "Cline"},
	{Source: "OpenHands releases", URL: "https://github.com/All-Hands-AI/OpenHands/releases.atom", TitlePrefix: "OpenHands"},
	{Source: "Dify releases", URL: "https://github.com/langgenius/dify/releases.atom", TitlePrefix: "Dify"},
	{Source: "Diffusers releases", URL: "https://github.com/huggingface/diffusers/releases.atom", TitlePrefix: "Hugging Face Diffusers"},
	{Source: "MLX releases", URL: "https://github.com/ml-explore/mlx/releases.atom", TitlePrefix: "MLX"},
	{Source: "Unsloth releases", URL: "https://github.com/unslothai/unsloth/releases.atom", TitlePrefix: "Unsloth"},
	{Source: "CrewAI releases", URL: "https://github.com/crewAIInc/crewAI/releases.atom", TitlePrefix: "CrewAI"},
	{Source: "AutoGen releases", URL: "https://github.com/microsoft/autogen/releases.atom", TitlePrefix: "AutoGen"},
	{Source: "OpenAI Agents SDK releases", URL: "https://github.com/openai/openai-agents-python/releases.atom", TitlePrefix: "OpenAI Agents SDK"},
	{Source: "Google ADK releases", URL: "https://github.com/google/adk-python/releases.atom", TitlePrefix: "Google Agent Development Kit"},
	{Source: "Model Context Protocol releases", URL: "https://github.com/modelcontextprotocol/modelcontextprotocol/releases.atom", TitlePrefix: "Model Context Protocol"},
	{Source: "LangGraph releases", URL: "https://github.com/langchain-ai/langgraph/releases.atom", TitlePrefix: "LangGraph"},
	{Source: "InvokeAI releases", URL: "https://github.com/invoke-ai/InvokeAI/releases.atom", TitlePrefix: "InvokeAI"},
	{Source: "Whisper releases", URL: "https://github.com/openai/whisper/releases.atom", TitlePrefix: "OpenAI Whisper"},
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
	{"Google Cloud", "cloud.google.com", "/blog/"},
	{"Meta Engineering", "engineering.fb.com", ""},
	{"Microsoft Azure", "azure.microsoft.com", ""},
	{"Microsoft Security", "microsoft.com", "/en-us/security/"},
	{"Microsoft Foundry", "devblogs.microsoft.com", "/foundry/"},
	{"Amazon Science", "amazon.science", ""},
	{"Amazon", "aboutamazon.com", ""},
	{"IBM", "newsroom.ibm.com", ""},
	{"Salesforce", "salesforce.com", "/news/"},
	{"Figma", "figma.com", "/blog/"},
	{"Stripe", "stripe.com", "/blog/"},
	{"Vercel", "vercel.com", ""},
	{"Docker", "docker.com", ""},
	{"JetBrains", "blog.jetbrains.com", ""},
	{"Cohere", "cohere.com", ""},
	{"AMD", "ir.amd.com", ""},
	{"Arm", "newsroom.arm.com", ""},
	{"SK hynix", "news.skhynix.com", ""},
	{"FTC", "ftc.gov", ""},
	{"SEC", "sec.gov", ""},
	{"NIST", "nist.gov", ""},
	{"CISA", "cisa.gov", ""},
	{"European Commission", "digital-strategy.ec.europa.eu", ""},
	{"UK Government", "gov.uk", "/government/"},
	{"MIT News", "news.mit.edu", ""},
	{"Berkeley AI Research", "bair.berkeley.edu", ""},
	{"Carnegie Mellon University", "cmu.edu", ""},
	{"Ollama releases", "github.com", "/ollama/ollama/"},
	{"Transformers releases", "github.com", "/huggingface/transformers/"},
	{"OpenAI Codex releases", "github.com", "/openai/codex/"},
	{"Claude Code releases", "github.com", "/anthropics/claude-code/"},
	{"Gemini CLI releases", "github.com", "/google-gemini/gemini-cli/"},
	{"vLLM releases", "github.com", "/vllm-project/vllm/"},
	{"Open WebUI releases", "github.com", "/open-webui/open-webui/"},
	{"ComfyUI releases", "github.com", "/comfyanonymous/comfyui/"},
	{"VS Code releases", "github.com", "/microsoft/vscode/"},
	{"Zed releases", "github.com", "/zed-industries/zed/"},
	{"Cline releases", "github.com", "/cline/cline/"},
	{"OpenHands releases", "github.com", "/all-hands-ai/openhands/"},
	{"Dify releases", "github.com", "/langgenius/dify/"},
	{"Diffusers releases", "github.com", "/huggingface/diffusers/"},
	{"MLX releases", "github.com", "/ml-explore/mlx/"},
	{"Unsloth releases", "github.com", "/unslothai/unsloth/"},
	{"CrewAI releases", "github.com", "/crewaiinc/crewai/"},
	{"AutoGen releases", "github.com", "/microsoft/autogen/"},
	{"OpenAI Agents SDK releases", "github.com", "/openai/openai-agents-python/"},
	{"Google ADK releases", "github.com", "/google/adk-python/"},
	{"Model Context Protocol releases", "github.com", "/modelcontextprotocol/modelcontextprotocol/"},
	{"LangGraph releases", "github.com", "/langchain-ai/langgraph/"},
	{"InvokeAI releases", "github.com", "/invoke-ai/invokeai/"},
	{"Whisper releases", "github.com", "/openai/whisper/"},
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
	"Google Cloud":                    "Google",
	"Meta Engineering":                "Meta",
	"Microsoft Azure":                 "Microsoft",
	"Microsoft Security":              "Microsoft",
	"Microsoft Foundry":               "Microsoft",
	"Microsoft Research":              "Microsoft",
	"AWS":                             "Amazon Web Services",
	"Amazon Science":                  "Amazon",
	"FTC":                             "the US Federal Trade Commission",
	"SEC":                             "the US Securities and Exchange Commission",
	"NIST":                            "the US National Institute of Standards and Technology",
	"CISA":                            "the US Cybersecurity and Infrastructure Security Agency",
	"European Commission":             "the European Commission",
	"UK Government":                   "the UK government",
	"MIT News":                        "MIT",
	"Berkeley AI Research":            "the Berkeley AI Research lab",
	"VS Code releases":                "Microsoft",
	"Zed releases":                    "Zed Industries",
	"Cline releases":                  "the Cline project",
	"OpenHands releases":              "All Hands AI",
	"Dify releases":                   "LangGenius",
	"Diffusers releases":              "Hugging Face",
	"MLX releases":                    "Apple",
	"Unsloth releases":                "Unsloth AI",
	"CrewAI releases":                 "CrewAI",
	"AutoGen releases":                "Microsoft",
	"OpenAI Agents SDK releases":      "OpenAI",
	"Google ADK releases":             "Google",
	"Model Context Protocol releases": "the Model Context Protocol project",
	"LangGraph releases":              "LangChain",
	"InvokeAI releases":               "the InvokeAI project",
	"Whisper releases":                "OpenAI",
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
