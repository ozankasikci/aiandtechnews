package publisher_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

var primaryInput = publisher.RewriteInput{
	Source: "Ollama releases", Title: "Ollama v1.2.0", CanonicalURL: "https://github.com/ollama/ollama/releases/tag/v1.2.0",
	SourceText: "Release notes text.", Primary: true,
}

func TestReportIsWrittenFromTheDocumentAndFactChecked(t *testing.T) {
	text := &scriptedText{responses: []string{validArticleJSON(), `{"unsupported":[]}`}}
	article, err := publisher.NewRewriter(text).Rewrite(context.Background(), primaryInput)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title == "" || len(text.prompts) != 2 {
		t.Fatalf("article %+v after %d prompts", article, len(text.prompts))
	}
	if !strings.Contains(text.prompts[0], "published by Ollama itself") || !strings.Contains(text.prompts[0], "Release notes text.") {
		t.Errorf("report prompt: %s", text.prompts[0])
	}
	if !strings.Contains(text.prompts[1], "fact checker") || !strings.Contains(text.prompts[1], "Release notes text.") {
		t.Errorf("fact check prompt: %s", text.prompts[1])
	}
}

func TestReportIsRewrittenOnceWhenTheFactCheckFindsUnsupportedClaims(t *testing.T) {
	text := &scriptedText{responses: []string{validArticleJSON(), `{"unsupported":["Early testers said it followed instructions closely."]}`, validArticleJSON(), `{"unsupported":[]}`}}
	if _, err := publisher.NewRewriter(text).Rewrite(context.Background(), primaryInput); err != nil {
		t.Fatal(err)
	}
	if len(text.prompts) != 4 || !strings.Contains(text.prompts[2], "Early testers said it followed instructions closely.") {
		t.Fatalf("prompts: %d", len(text.prompts))
	}
}

func TestReportFailsPermanentlyWhenUnsupportedClaimsRemain(t *testing.T) {
	bad := `{"unsupported":["An invented number."]}`
	text := &scriptedText{responses: []string{validArticleJSON(), bad, validArticleJSON(), bad, validArticleJSON(), bad}}
	_, err := publisher.NewRewriter(text).Rewrite(context.Background(), primaryInput)
	if !publisher.IsPermanent(err) || !strings.Contains(err.Error(), "An invented number.") {
		t.Fatalf("err = %v", err)
	}
}

func TestReportFailsWhenTheFactCheckAnswerIsUnreadable(t *testing.T) {
	text := &scriptedText{responses: []string{validArticleJSON(), `not json`}}
	_, err := publisher.NewRewriter(text).Rewrite(context.Background(), primaryInput)
	if err == nil || publisher.IsPermanent(err) {
		t.Fatalf("an unreadable fact check must fail and be retried, got %v", err)
	}
}

func TestExtractDocumentTextKeepsListsAndHeadings(t *testing.T) {
	html := `<html><body><nav><li>Skip to content</li></nav><main><h2>What's changed</h2><ul>` +
		strings.Repeat(`<li>Added support for a new vision model with tool calling in the API</li><li>Fixed a crash when loading very large context windows on Apple silicon</li>`, 1) +
		`<li>Improved memory estimates for models split across several GPUs and cards</li><li>The scheduler now unloads idle models sooner when memory is low on the host</li>` +
		`<li>New environment variable to set the default context length per model family</li><li>Windows installer no longer requires administrator rights for user installs</li></ul>` +
		`<p>Thanks to all the new contributors.</p></main></body></html>`
	text := publisher.ExtractDocumentText(html)
	for _, want := range []string{"What's changed", "tool calling in the API", "administrator rights"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
	if strings.Contains(text, "Skip to content") {
		t.Errorf("navigation kept: %q", text)
	}
}

func TestReportExplainsWhyAnExcerptIsNotOneSentence(t *testing.T) {
	bad := strings.Replace(validArticleJSON(), "The new model handles longer tasks at a lower price.", "The model goes on sale Oct. 23 at a lower price.", 1)
	text := &scriptedText{responses: []string{bad, validArticleJSON(), `{"unsupported":[]}`}}
	if _, err := publisher.NewRewriter(text).Rewrite(context.Background(), primaryInput); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.prompts[1], "The model goes on sale Oct. 23 at a lower price.") || !strings.Contains(text.prompts[1], "write the words out") {
		t.Fatalf("correction: %s", text.prompts[1][len(text.prompts[1])-500:])
	}
}
