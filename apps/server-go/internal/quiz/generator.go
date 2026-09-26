package quiz

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	questionCount    = 3
	optionCount      = 4
	maxQuestionChars = 160
	maxOptionChars   = 80
	minEvidenceChars = 20
	generateAttempts = 2
)

// TextGenerator is the JSON text model; *gemini.Client implements it.
type TextGenerator interface {
	GenerateJSON(ctx context.Context, prompt string) (string, error)
}

// Generator asks the text model for a quiz and accepts it only when every
// answer key is backed by a sentence copied from its article.
type Generator struct {
	text TextGenerator
}

func NewGenerator(text TextGenerator) *Generator { return &Generator{text: text} }

var (
	leadingFence  = regexp.MustCompile("(?i)^```(?:json)?\\s*")
	trailingFence = regexp.MustCompile("\\s*```$")
	jsonObject    = regexp.MustCompile(`(?s)\{.*\}`)
)

// Generate writes day's quiz from articles (newest first). Like the
// publisher's rewriter it makes up to two attempts, the second carrying a
// correction that lists what failed; a second failure is ErrInvalidQuiz.
// Each accepted question's options are shuffled, seeded by day and question
// index, because models tend to put the right answer first.
func (g *Generator) Generate(ctx context.Context, day string, articles []Article) ([]Question, error) {
	if len(articles) < questionCount {
		return nil, fmt.Errorf("%w: %d published in the last week", ErrNotEnoughArticles, len(articles))
	}
	base := quizPrompt(articles)
	correction := ""
	var lastProblem string
	for attempt := 1; attempt <= generateAttempts; attempt++ {
		raw, err := g.text.GenerateJSON(ctx, base+correction)
		if err != nil {
			return nil, fmt.Errorf("generate quiz: %w", err)
		}
		drafts, ok := parseQuiz(raw)
		if !ok {
			lastProblem = "response was not valid JSON"
			correction = "\n\nYour previous response was not valid JSON. Return only the required JSON object."
			continue
		}
		questions, problems := validate(drafts, articles)
		if len(problems) == 0 {
			for i := range questions {
				shuffle(&questions[i], day, i)
			}
			return questions, nil
		}
		lastProblem = strings.Join(problems, "; ")
		correction = "\n\nYour previous quiz failed these checks: " + lastProblem +
			". Write the quiz again from the same articles and satisfy every requirement."
	}
	return nil, fmt.Errorf("%w: %s", ErrInvalidQuiz, lastProblem)
}

type draftQuestion struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
	Answer   *int     `json:"answer"`
	Slug     string   `json:"slug"`
	Evidence string   `json:"evidence"`
}

func parseQuiz(value string) ([]draftQuestion, bool) {
	cleaned := strings.TrimSpace(trailingFence.ReplaceAllString(leadingFence.ReplaceAllString(value, ""), ""))
	candidate := cleaned
	if match := jsonObject.FindString(cleaned); match != "" {
		candidate = match
	}
	var parsed struct {
		Questions *[]draftQuestion `json:"questions"`
	}
	if err := json.Unmarshal([]byte(candidate), &parsed); err != nil || parsed.Questions == nil {
		return nil, false
	}
	return *parsed.Questions, true
}

// validate checks the whole quiz and returns the trimmed questions, titled
// from their articles, or every problem found.
func validate(drafts []draftQuestion, articles []Article) ([]Question, []string) {
	if len(drafts) != questionCount {
		return nil, []string{fmt.Sprintf("want exactly %d questions, got %d", questionCount, len(drafts))}
	}
	bySlug := make(map[string]Article, len(articles))
	for _, article := range articles {
		bySlug[article.Slug] = article
	}
	used := make(map[string]bool, questionCount)
	var problems []string
	questions := make([]Question, 0, questionCount)
	for i, draft := range drafts {
		problem := func(format string, args ...any) {
			problems = append(problems, fmt.Sprintf("question %d: ", i+1)+fmt.Sprintf(format, args...))
		}
		question := Question{
			Question: strings.TrimSpace(draft.Question),
			Slug:     strings.TrimSpace(draft.Slug),
			Evidence: strings.TrimSpace(draft.Evidence),
		}
		switch {
		case question.Question == "":
			problem("question is empty")
		case utf8.RuneCountInString(question.Question) > maxQuestionChars:
			problem("question is longer than %d characters", maxQuestionChars)
		}
		article, known := bySlug[question.Slug]
		switch {
		case !known:
			problem("unknown article slug %q", question.Slug)
		case used[question.Slug]:
			problem("article %s is already used by another question", question.Slug)
		}
		used[question.Slug] = true
		question.Title = article.Title

		optionsOK := len(draft.Options) == optionCount
		if !optionsOK {
			problem("want %d options, got %d", optionCount, len(draft.Options))
		}
		seen := make(map[string]bool, len(draft.Options))
		for j, option := range draft.Options {
			option = strings.TrimSpace(option)
			question.Options = append(question.Options, option)
			switch {
			case option == "":
				problem("option %d is empty", j+1)
				optionsOK = false
			case utf8.RuneCountInString(option) > maxOptionChars:
				problem("option %d is longer than %d characters", j+1, maxOptionChars)
			}
			key := strings.ToLower(option)
			if option != "" && seen[key] {
				problem("options are not distinct")
				optionsOK = false
			}
			seen[key] = true
		}
		answerOK := draft.Answer != nil && *draft.Answer >= 0 && *draft.Answer < optionCount
		if answerOK {
			question.Answer = *draft.Answer
		} else {
			problem("answer must be 0 to %d", optionCount-1)
		}

		fields := append([]string{question.Question, question.Evidence}, question.Options...)
		if strings.ContainsAny(strings.Join(fields, "\n"), "—<>") {
			problem("uses an em dash or angle bracket")
		}

		evidence := matchable(question.Evidence)
		switch {
		case utf8.RuneCountInString(question.Evidence) < minEvidenceChars:
			problem("evidence is shorter than %d characters", minEvidenceChars)
		case known && !strings.Contains(matchable(article.Text), evidence):
			problem("evidence is not copied from the article")
		case optionsOK && answerOK &&
			!strings.Contains(strings.ToLower(evidence), strings.ToLower(matchable(question.Options[question.Answer]))):
			problem("the correct option does not appear in the evidence")
		}
		questions = append(questions, question)
	}
	if len(problems) > 0 {
		return nil, problems
	}
	return questions, nil
}

// quoteStraightener turns curly quotes into straight ones, because a model
// copying a sentence often straightens them.
var quoteStraightener = strings.NewReplacer("‘", "'", "’", "'", "“", `"`, "”", `"`)

// matchable prepares text for the verbatim evidence check: whitespace runs
// collapse to one space and curly quotes become straight.
func matchable(text string) string {
	return quoteStraightener.Replace(strings.Join(strings.Fields(text), " "))
}

// shuffle reorders question's options with a permutation seeded by day and
// index, so the same quiz always shuffles the same way, and remaps Answer.
func shuffle(question *Question, day string, index int) {
	hash := fnv.New64a()
	_, _ = fmt.Fprintf(hash, "%s#%d", day, index)
	seed := hash.Sum64()
	random := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	order := random.Perm(len(question.Options))
	options := make([]string, len(order))
	answer := 0
	for position, original := range order {
		options[position] = question.Options[original]
		if original == question.Answer {
			answer = position
		}
	}
	question.Options, question.Answer = options, answer
}

func quizPrompt(articles []Article) string {
	var list strings.Builder
	for i, article := range articles {
		fmt.Fprintf(&list, "Article %d\nSlug: %s\nTitle: %s\nText:\n%s\n\n", i+1, article.Slug, article.Title, article.Text)
	}
	return fmt.Sprintf(`You are TechNews Editorial. Write today's news quiz for readers of the articles below.

Requirements:
- Write exactly %d multiple-choice questions.
- Each question must come from a different article.
- Test facts a reader of the story would know: names, numbers, companies, and what happened.
- Never write trick questions or opinion questions.
- Give each question %d short, distinct options, exactly one of them correct.
- "answer" is the zero-based index of the correct option.
- Keep each question at most %d characters and each option at most %d characters.
- Use plain text with no HTML. Never use an em dash.
- "slug" is the slug of the article the question comes from.
- "evidence" must be one sentence copied word for word from that article that contains the correct answer.

Articles:

%sReturn only JSON with this exact shape:
{"questions":[{"question":"...","options":["A","B","C","D"],"answer":0,"slug":"article-slug","evidence":"exact sentence copied from that article"}]}`,
		questionCount, optionCount, maxQuestionChars, maxOptionChars, list.String())
}
