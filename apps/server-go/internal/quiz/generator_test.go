package quiz_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/quiz"
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
	if i >= len(s.responses) {
		return "", errors.New("no scripted response")
	}
	return s.responses[i], nil
}

var testArticles = []quiz.Article{
	{ID: 4, Slug: "openai-raises", Title: "OpenAI raises $40 billion", Text: "OpenAI said on Monday it raised $40 billion in new funding. The round was led by SoftBank, the company said."},
	{ID: 3, Slug: "nvidia-chip", Title: "Nvidia shows a new chip", Text: "Nvidia unveiled the Rubin chip at its developer conference. The company said Rubin ships next year."},
	{ID: 2, Slug: "apple-glasses", Title: "Apple delays its glasses", Text: "Apple pushed its smart glasses launch to 2028, according to Bloomberg. The delay follows supply problems."},
	{ID: 1, Slug: "meta-model", Title: "Meta opens a model", Text: "Meta released Llama 5 under an open license on Thursday."},
}

type rawQuestion struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
	Answer   any      `json:"answer"`
	Slug     string   `json:"slug"`
	Evidence string   `json:"evidence"`
}

func validQuestions() []rawQuestion {
	return []rawQuestion{
		{Question: "Which company led OpenAI's $40 billion funding round?", Options: []string{"SoftBank", "Microsoft", "Google", "Amazon"}, Answer: 0,
			Slug: "openai-raises", Evidence: "The round was led by SoftBank, the company said."},
		{Question: "What is the name of the chip Nvidia unveiled?", Options: []string{"Rubin", "Blackwell", "Hopper", "Volta"}, Answer: 0,
			Slug: "nvidia-chip", Evidence: "Nvidia unveiled the Rubin chip at its developer conference."},
		{Question: "To which year did Apple push its smart glasses launch?", Options: []string{"2026", "2027", "2028", "2029"}, Answer: 2,
			Slug: "apple-glasses", Evidence: "Apple pushed its smart glasses launch to 2028, according to Bloomberg."},
	}
}

func encode(t *testing.T, questions []rawQuestion) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"questions": questions})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// correctOption returns the text of the right option in a raw question.
func correctOption(q rawQuestion) string { return q.Options[q.Answer.(int)] }

func TestGenerateBuildsPromptAndReturnsValidatedQuestions(t *testing.T) {
	raw := validQuestions()
	text := &scriptedText{responses: []string{"```json\n" + encode(t, raw) + "\n```"}}
	questions, err := quiz.NewGenerator(text).Generate(context.Background(), "2026-09-27", testArticles)
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) != 3 || len(text.prompts) != 1 {
		t.Fatalf("questions = %d, prompts = %d", len(questions), len(text.prompts))
	}
	for i, question := range questions {
		if question.Question != raw[i].Question || question.Slug != raw[i].Slug || question.Evidence != raw[i].Evidence {
			t.Errorf("question %d = %+v", i, question)
		}
		if question.Title != testArticles[i].Title {
			t.Errorf("question %d title = %q, want %q", i, question.Title, testArticles[i].Title)
		}
		if len(question.Options) != 4 || question.Options[question.Answer] != correctOption(raw[i]) {
			t.Errorf("question %d options = %v answer = %d, want the right option %q", i, question.Options, question.Answer, correctOption(raw[i]))
		}
		if strings.Join(sorted(question.Options), "|") != strings.Join(sorted(raw[i].Options), "|") {
			t.Errorf("question %d options changed: %v", i, question.Options)
		}
	}
	prompt := text.prompts[0]
	for _, want := range []string{
		"exactly 3 multiple-choice questions",
		"Each question must come from a different article.",
		"Never use an em dash.",
		"at most 160 characters",
		"at most 80 characters",
		"copied word for word",
		"Slug: openai-raises\nTitle: OpenAI raises $40 billion\nText:\nOpenAI said on Monday it raised $40 billion in new funding.",
		`{"questions":[{"question":"...","options":["A","B","C","D"],"answer":0,"slug":"article-slug","evidence":"exact sentence copied from that article"}]}`,
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt is missing %q", want)
		}
	}
	if strings.Contains(prompt, "—") {
		t.Error("prompt contains an em dash")
	}
}

func sorted(values []string) []string {
	out := append([]string(nil), values...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func TestGenerateShufflesOptionsDeterministicallyByDay(t *testing.T) {
	generate := func(day string) []quiz.Question {
		t.Helper()
		text := &scriptedText{responses: []string{encode(t, validQuestions())}}
		questions, err := quiz.NewGenerator(text).Generate(context.Background(), day, testArticles)
		if err != nil {
			t.Fatal(err)
		}
		return questions
	}
	first, again := generate("2026-09-27"), generate("2026-09-27")
	for i := range first {
		if strings.Join(first[i].Options, "|") != strings.Join(again[i].Options, "|") || first[i].Answer != again[i].Answer {
			t.Fatalf("question %d shuffled differently for the same day: %v/%d vs %v/%d", i, first[i].Options, first[i].Answer, again[i].Options, again[i].Answer)
		}
	}
	// The model put the right answer first in two of three questions; across
	// a month of days the shuffle must move it to every position.
	positions := map[int]bool{}
	for day := 1; day <= 30; day++ {
		for _, question := range generate(fmt.Sprintf("2026-10-%02d", day)) {
			positions[question.Answer] = true
		}
	}
	if len(positions) != 4 {
		t.Fatalf("answer positions over a month = %v, want all four", positions)
	}
}

func TestGenerateRetriesOnceWithTheProblemsThenFails(t *testing.T) {
	bad := validQuestions()
	bad[0].Answer = 1 // Microsoft: not in the evidence sentence
	text := &scriptedText{responses: []string{encode(t, bad), encode(t, bad)}}
	_, err := quiz.NewGenerator(text).Generate(context.Background(), "2026-09-27", testArticles)
	if !errors.Is(err, quiz.ErrInvalidQuiz) {
		t.Fatalf("err = %v, want ErrInvalidQuiz", err)
	}
	if len(text.prompts) != 2 {
		t.Fatalf("prompts = %d, want 2", len(text.prompts))
	}
	if !strings.Contains(text.prompts[1], "Your previous quiz failed these checks:") ||
		!strings.Contains(text.prompts[1], "question 1: the correct option does not appear in the evidence") {
		t.Errorf("correction prompt = %q", text.prompts[1][len(text.prompts[0]):])
	}
	if !strings.HasPrefix(text.prompts[1], text.prompts[0]) {
		t.Error("correction does not extend the original prompt")
	}
}

func TestGenerateAcceptsACorrectedSecondDraft(t *testing.T) {
	text := &scriptedText{responses: []string{"not json", encode(t, validQuestions())}}
	questions, err := quiz.NewGenerator(text).Generate(context.Background(), "2026-09-27", testArticles)
	if err != nil || len(questions) != 3 {
		t.Fatalf("questions = %v, err = %v", questions, err)
	}
	if !strings.Contains(text.prompts[1], "was not valid JSON") {
		t.Errorf("correction prompt = %q", text.prompts[1][len(text.prompts[0]):])
	}
}

func TestGenerateReturnsTextGeneratorErrorsWithoutRetry(t *testing.T) {
	boom := errors.New("gemini down")
	text := &scriptedText{errs: []error{boom}}
	_, err := quiz.NewGenerator(text).Generate(context.Background(), "2026-09-27", testArticles)
	if !errors.Is(err, boom) || len(text.prompts) != 1 {
		t.Fatalf("err = %v, prompts = %d", err, len(text.prompts))
	}
}

func TestGenerateNeedsThreeArticles(t *testing.T) {
	text := &scriptedText{}
	_, err := quiz.NewGenerator(text).Generate(context.Background(), "2026-09-27", testArticles[:2])
	if !errors.Is(err, quiz.ErrNotEnoughArticles) || len(text.prompts) != 0 {
		t.Fatalf("err = %v, prompts = %d", err, len(text.prompts))
	}
}

func TestGenerateRejectsInvalidQuizzes(t *testing.T) {
	long := strings.Repeat("a", 161)
	tests := []struct {
		name   string
		mutate func([]rawQuestion) []rawQuestion
		want   string
	}{
		{"two questions", func(q []rawQuestion) []rawQuestion { return q[:2] }, "want exactly 3 questions, got 2"},
		{"four questions", func(q []rawQuestion) []rawQuestion {
			extra := rawQuestion{Question: "Which model did Meta release?", Options: []string{"Llama 5", "Llama 4", "Gemma", "Mistral"}, Answer: 0,
				Slug: "meta-model", Evidence: "Meta released Llama 5 under an open license on Thursday."}
			return append(q, extra)
		}, "want exactly 3 questions, got 4"},
		{"repeated article", func(q []rawQuestion) []rawQuestion {
			q[1] = rawQuestion{Question: "Who reported that OpenAI raised money?", Options: []string{"The company", "Reuters", "Bloomberg", "CNBC"}, Answer: 0,
				Slug: "openai-raises", Evidence: "The round was led by SoftBank, the company said."}
			return q
		}, "question 2: article openai-raises is already used"},
		{"unknown article", func(q []rawQuestion) []rawQuestion { q[0].Slug = "made-up"; return q }, "question 1: unknown article slug \"made-up\""},
		{"three options", func(q []rawQuestion) []rawQuestion { q[0].Options = q[0].Options[:3]; return q }, "question 1: want 4 options, got 3"},
		{"empty option", func(q []rawQuestion) []rawQuestion { q[1].Options[3] = "  "; return q }, "question 2: option 4 is empty"},
		{"duplicate options", func(q []rawQuestion) []rawQuestion { q[1].Options[3] = "rubin"; return q }, "question 2: options are not distinct"},
		{"answer out of range", func(q []rawQuestion) []rawQuestion { q[2].Answer = 4; return q }, "question 3: answer must be 0 to 3"},
		{"negative answer", func(q []rawQuestion) []rawQuestion { q[2].Answer = -1; return q }, "question 3: answer must be 0 to 3"},
		{"missing answer", func(q []rawQuestion) []rawQuestion { q[2].Answer = nil; return q }, "question 3: answer must be 0 to 3"},
		{"empty question", func(q []rawQuestion) []rawQuestion { q[0].Question = " "; return q }, "question 1: question is empty"},
		{"long question", func(q []rawQuestion) []rawQuestion { q[0].Question = long; return q }, "question 1: question is longer than 160 characters"},
		{"long option", func(q []rawQuestion) []rawQuestion { q[0].Options[1] = long[:81]; return q }, "question 1: option 2 is longer than 80 characters"},
		{"em dash", func(q []rawQuestion) []rawQuestion { q[0].Question = "Who led the round — and why?"; return q }, "question 1: uses an em dash or angle bracket"},
		{"html", func(q []rawQuestion) []rawQuestion { q[1].Options[2] = "<b>Hopper</b>"; return q }, "question 2: uses an em dash or angle bracket"},
		{"short evidence", func(q []rawQuestion) []rawQuestion { q[1].Evidence = "Rubin chip"; return q }, "question 2: evidence is shorter than 20 characters"},
		{"evidence not in article", func(q []rawQuestion) []rawQuestion {
			q[1].Evidence = "Nvidia unveiled the Rubin chip at CES in Las Vegas."
			return q
		}, "question 2: evidence is not copied from the article"},
		{"evidence from another article", func(q []rawQuestion) []rawQuestion {
			q[2].Evidence = "The round was led by SoftBank, the company said."
			q[2].Options = []string{"SoftBank", "Sony", "Samsung", "Sharp"}
			q[2].Answer = 0
			return q
		}, "question 3: evidence is not copied from the article"},
		{"answer not in evidence", func(q []rawQuestion) []rawQuestion { q[2].Answer = 3; return q }, "question 3: the correct option does not appear in the evidence"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := encode(t, test.mutate(validQuestions()))
			text := &scriptedText{responses: []string{response, response}}
			_, err := quiz.NewGenerator(text).Generate(context.Background(), "2026-09-27", testArticles)
			if !errors.Is(err, quiz.ErrInvalidQuiz) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err = %v, want ErrInvalidQuiz containing %q", err, test.want)
			}
		})
	}
}

// Evidence is matched after collapsing whitespace and straightening curly
// quotes, and the correct option is found in it case-insensitively.
func TestGenerateMatchesEvidenceAcrossWhitespaceQuotesAndCase(t *testing.T) {
	articles := append([]quiz.Article(nil), testArticles...)
	articles[1].Text = "Nvidia unveiled the   Rubin\n chip at its “developer” conference. The company said Rubin ships next year."
	raw := validQuestions()
	raw[1].Evidence = "Nvidia unveiled the Rubin chip at its \"developer\" conference."
	raw[1].Options = []string{"RUBIN", "Blackwell", "Hopper", "Volta"}
	text := &scriptedText{responses: []string{encode(t, raw)}}
	if _, err := quiz.NewGenerator(text).Generate(context.Background(), "2026-09-27", articles); err != nil {
		t.Fatal(err)
	}
}
