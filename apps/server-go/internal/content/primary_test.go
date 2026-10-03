package content

import "testing"

func TestPrimarySourcesAreApprovedButNotNewsPublications(t *testing.T) {
	cases := map[string]string{
		"https://openai.com/index/some-launch/":                       "OpenAI",
		"https://www.anthropic.com/news/claude-frontier-academy":      "Anthropic",
		"https://github.com/ollama/ollama/releases/tag/v1.2.0":        "Ollama releases",
		"https://github.com/comfyanonymous/ComfyUI/releases/tag/v0.9": "ComfyUI releases",
		"https://www.microsoft.com/en-us/research/blog/a-result/":     "Microsoft Research",
		"https://blogs.microsoft.com/blog/2026/10/01/x/":              "Microsoft",
		"https://machinelearning.apple.com/research/x":                "Apple Machine Learning Research",
		"https://www.apple.com/newsroom/2026/10/x/":                   "Apple",
		"https://huggingface.co/blog/some-post":                       "Hugging Face",
	}
	for url, want := range cases {
		if got, ok := SourceForURL(url); !ok || got != want {
			t.Errorf("SourceForURL(%s) = %q, %v; want %q", url, got, ok, want)
		}
		if _, ok := NewsSourceForURL(url); ok {
			t.Errorf("%s must not be a news publication", url)
		}
		if !IsPrimarySource(want) {
			t.Errorf("%s is not a primary source", want)
		}
	}
	for _, url := range []string{"https://github.com/someone/else/releases/tag/v1", "https://www.microsoft.com/en-us/windows", "https://www.apple.com/iphone/", "https://huggingface.co/some/model"} {
		if source, ok := SourceForURL(url); ok {
			t.Errorf("SourceForURL(%s) = %q, want none", url, source)
		}
	}
	if source, ok := SourceForURL("https://techcrunch.com/2026/10/01/x/"); !ok || source != "TechCrunch" || IsPrimarySource(source) {
		t.Errorf("TechCrunch = %q, %v", source, ok)
	}
}

func TestEveryPrimaryFeedHasAHostAndIsMarkedPrimary(t *testing.T) {
	for _, feed := range PrimaryFeeds() {
		if !feed.Primary || !IsPrimarySource(feed.Source) || !IsPrimaryFeed(feed.URL) {
			t.Errorf("feed %+v", feed)
		}
	}
	if IsPrimaryFeed("https://techcrunch.com/feed/") {
		t.Error("TechCrunch is not a primary feed")
	}
}

func TestPrereleaseTitles(t *testing.T) {
	for _, title := range []string{"0.162.0-alpha.10", "v0.35.1-rc2", "v0.31.0rc5", "Release v0.64.0-nightly.20261003.gfb972b2f8", "viable/strict/1791023242", "v2.0.0-beta.1"} {
		if !IsPrerelease(title) {
			t.Errorf("%q should be a pre-release", title)
		}
	}
	for _, title := range []string{"v0.35.1", "v2.1.287", "v0.31.0: [Misc] Add Transformers version upper bound", "Release v0.63.0", "0.161.0"} {
		if IsPrerelease(title) {
			t.Errorf("%q should be a stable release", title)
		}
	}
}
