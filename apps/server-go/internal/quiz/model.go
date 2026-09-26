// Package quiz owns the daily news quiz: three multiple-choice questions a
// day, written by Gemini from the week's published articles, checked against
// the article text, stored per UTC day, and served to the website.
package quiz

import "errors"

// Article is a published article the quiz may ask about, with its body as
// plain text.
type Article struct {
	ID    int64
	Slug  string
	Title string
	Text  string
}

// Question is one validated quiz question. Evidence is the article sentence
// the answer key was checked against; it is stored but never served.
type Question struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
	Answer   int      `json:"answer"`
	Slug     string   `json:"slug"`
	Title    string   `json:"title"`
	Evidence string   `json:"evidence"`
}

// Quiz is one day's quiz. Number is its row id, shown as the quiz number.
type Quiz struct {
	Number    int64
	Day       string
	Status    string
	Questions []Question
}

const (
	StatusPublished = "published"
	StatusPulled    = "pulled"
)

var (
	// ErrNoQuiz means there is no published quiz to serve.
	ErrNoQuiz = errors.New("no quiz available")
	// ErrInvalidQuiz means the model's quiz failed validation twice; retrying
	// the same articles right away will not help.
	ErrInvalidQuiz = errors.New("quiz failed validation")
	// ErrNotEnoughArticles means fewer than three articles were published in the window.
	ErrNotEnoughArticles = errors.New("not enough recent articles for a quiz")
	// ErrGenerationDisabled means the server runs without a text generator.
	ErrGenerationDisabled = errors.New("quiz generation is not configured")
)
