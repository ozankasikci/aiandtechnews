package publisher_test

import (
	"context"
	"errors"
	"fmt"
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
	cases := []struct {
		name              string
		err               error
		permanent, system bool
	}{
		{"503 is transient", &gemini.Error{Status: 503}, false, false},
		{"429 is transient", &gemini.Error{Status: 429}, false, false},
		{"400 is permanent", &gemini.Error{Status: 400}, true, false},
		{"401 is a system fault", &gemini.Error{Status: 401}, false, true},
		{"403 is a system fault", &gemini.Error{Status: 403}, false, true},
		{"404 is a system fault", &gemini.Error{Status: 404}, false, true},
		{"missing key is a system fault", gemini.ErrMissingAPIKey, false, true},
		{"safety block is permanent", fmt.Errorf("%w: SAFETY", gemini.ErrBlocked), true, false},
		{"network error is transient", errors.New("connection reset"), false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := publisher.NewRewriter(&scriptedText{errs: []error{tc.err}}).Rewrite(context.Background(), input)
			if !errors.Is(err, tc.err) || publisher.IsPermanent(err) != tc.permanent || publisher.IsSystemFault(err) != tc.system {
				t.Fatalf("err=%v permanent=%v system=%v", err, publisher.IsPermanent(err), publisher.IsSystemFault(err))
			}
		})
	}
}
