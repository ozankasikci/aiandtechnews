package publisher_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

type scriptedText struct {
	responses []string
	errs      []error
	prompts   []string
}

func (s *scriptedText) GenerateJSON(_ context.Context, prompt string) (string, error) {
	i := len(s.prompts)
	s.prompts = append(s.prompts, prompt)
	if i < len(s.errs) && s.errs[i] != nil {
		return "", s.errs[i]
	}
	return s.responses[i], nil
}

func validArticleJSON() string {
	var body strings.Builder
	for i := 0; i < 6; i++ {
		body.WriteString("<p>Anthropic released a new model for developers on Tuesday. It handles longer tasks and costs less to run. Early testers said it followed instructions closely. The company plans a wider rollout next month.</p>")
	}
	return `{"title":"Anthropic releases a cheaper model for developers","excerpt":"The new model handles longer tasks at a lower price.","content":"` + body.String() + `"}`
}

var input = publisher.RewriteInput{
	Source: "TechCrunch", Title: "Anthropic launches new model", CanonicalURL: "https://techcrunch.com/2026/09/23/anthropic", SourceText: "Source reporting text.",
}

func TestRewriteBuildsPromptAndAcceptsValidArticle(t *testing.T) {
	text := &scriptedText{responses: []string{"```json\n" + validArticleJSON() + "\n```"}}
	article, err := publisher.NewRewriter(text).Rewrite(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title != "Anthropic releases a cheaper model for developers" {
		t.Fatalf("article = %+v", article)
	}
	prompt := text.prompts[0]
	for _, want := range []string{
		"You are TechNews Editorial.",
		"Write 150 to 800 words in 5 to 12 paragraphs.",
		"Publication: TechCrunch\nOriginal headline: Anthropic launches new model\nCanonical source URL: https://techcrunch.com/2026/09/23/anthropic",
		"Source reporting:\nSource reporting text.",
		`{"title":"Short factual headline","excerpt":"One sentence.","content":"<p>Article body.</p>"}`,
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestRewriteRetriesOnceWithCorrection(t *testing.T) {
	text := &scriptedText{responses: []string{"not json", validArticleJSON()}}
	if _, err := publisher.NewRewriter(text).Rewrite(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(text.prompts[1], "\n\nYour previous response was not valid JSON. Return only the required JSON object.") {
		t.Fatalf("second prompt suffix = %q", text.prompts[1][len(text.prompts[1])-120:])
	}
}

func TestRewriteFailsPermanentlyAfterTwoInvalidDrafts(t *testing.T) {
	short := `{"title":"Short","excerpt":"One.","content":"<p>Too short.</p>"}`
	text := &scriptedText{responses: []string{short, short}}
	_, err := publisher.NewRewriter(text).Rewrite(context.Background(), input)
	if err == nil || !publisher.IsPermanent(err) || len(text.prompts) != 2 {
		t.Fatalf("err=%v permanent=%v prompts=%d", err, publisher.IsPermanent(err), len(text.prompts))
	}
	if !strings.Contains(text.prompts[1], "Your previous draft failed these checks:") {
		t.Fatalf("missing validation correction")
	}
}

func TestRewriteClassifiesGeminiErrors(t *testing.T) {
	transient := &scriptedText{errs: []error{&gemini.Error{Status: 503}}}
	if _, err := publisher.NewRewriter(transient).Rewrite(context.Background(), input); err == nil || publisher.IsPermanent(err) {
		t.Fatalf("503 should be transient: %v", err)
	}
	permanent := &scriptedText{errs: []error{&gemini.Error{Status: 400}}}
	if _, err := publisher.NewRewriter(permanent).Rewrite(context.Background(), input); !publisher.IsPermanent(err) {
		t.Fatalf("400 should be permanent: %v", err)
	}
	missing := &scriptedText{errs: []error{gemini.ErrMissingAPIKey}}
	if _, err := publisher.NewRewriter(missing).Rewrite(context.Background(), input); !errors.Is(err, gemini.ErrMissingAPIKey) || publisher.IsPermanent(err) {
		t.Fatalf("missing key should stay transient (config fix, then retry): %v", err)
	}
}
