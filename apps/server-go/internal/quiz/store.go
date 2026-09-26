package quiz

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"
)

type SQLiteStore struct{ db *sql.DB }

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{db: db} }

// RecentArticles returns up to limit published articles whose published_at
// is on or after since, newest first, with their bodies as plain text.
// published_at is compared through datetime() because Node and Go store it
// in different formats.
func (s *SQLiteStore) RecentArticles(ctx context.Context, since time.Time, limit int) ([]Article, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, slug, title, content FROM articles
		WHERE status = 'published' AND datetime(published_at) >= datetime(?)
		ORDER BY datetime(published_at) DESC, id DESC LIMIT ?`, since.UTC().Format(time.DateTime), limit)
	if err != nil {
		return nil, fmt.Errorf("list recent articles: %w", err)
	}
	defer rows.Close()
	articles := make([]Article, 0)
	for rows.Next() {
		var article Article
		var body string
		if err := rows.Scan(&article.ID, &article.Slug, &article.Title, &body); err != nil {
			return nil, fmt.Errorf("scan recent article: %w", err)
		}
		article.Text = plainText(body)
		articles = append(articles, article)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent articles: %w", err)
	}
	return articles, nil
}

var (
	scriptOrStyle          = regexp.MustCompile(`(?is)<(script|style)\b.*?</(script|style)>`)
	htmlTag                = regexp.MustCompile(`<[^>]+>`)
	spaceBeforePunctuation = regexp.MustCompile(`\s+([.,;:!?)])`)
)

// plainText turns an article body into the text the quiz's evidence is
// checked against: tags become spaces, entities are decoded, and whitespace
// collapses. A tag right before punctuation ("<em>time</em>.") would leave
// "time ."; that space is removed so a sentence the model copies matches.
func plainText(body string) string {
	text := htmlTag.ReplaceAllString(scriptOrStyle.ReplaceAllString(body, " "), " ")
	text = strings.Join(strings.Fields(html.UnescapeString(text)), " ")
	return spaceBeforePunctuation.ReplaceAllString(text, "$1")
}

// Save stores day's quiz as published and returns it. Saving a day that
// already has a quiz replaces its questions and republishes it, keeping its
// number.
func (s *SQLiteStore) Save(ctx context.Context, day string, questions []Question, at time.Time) (Quiz, error) {
	encoded, err := json.Marshal(questions)
	if err != nil {
		return Quiz{}, fmt.Errorf("encode quiz questions: %w", err)
	}
	var id int64
	if err := s.db.QueryRowContext(ctx, `INSERT INTO quizzes (day, questions, status, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(day) DO UPDATE SET questions = excluded.questions, status = excluded.status, created_at = excluded.created_at
		RETURNING id`, day, string(encoded), StatusPublished, at.UTC().Format(time.RFC3339)).Scan(&id); err != nil {
		return Quiz{}, fmt.Errorf("save quiz for %s: %w", day, err)
	}
	return Quiz{Number: id, Day: day, Status: StatusPublished, Questions: questions}, nil
}

// Latest returns the most recent published quiz, or ErrNoQuiz.
func (s *SQLiteStore) Latest(ctx context.Context) (Quiz, error) {
	var quiz Quiz
	var encoded string
	err := s.db.QueryRowContext(ctx, `SELECT id, day, status, questions FROM quizzes
		WHERE status = ? ORDER BY day DESC LIMIT 1`, StatusPublished).Scan(&quiz.Number, &quiz.Day, &quiz.Status, &encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return Quiz{}, ErrNoQuiz
	}
	if err != nil {
		return Quiz{}, fmt.Errorf("read latest quiz: %w", err)
	}
	if err := json.Unmarshal([]byte(encoded), &quiz.Questions); err != nil {
		return Quiz{}, fmt.Errorf("decode quiz %d questions: %w", quiz.Number, err)
	}
	return quiz, nil
}

// Pull hides day's quiz and reports whether day had one.
func (s *SQLiteStore) Pull(ctx context.Context, day string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE quizzes SET status = ? WHERE day = ?`, StatusPulled, day)
	if err != nil {
		return false, fmt.Errorf("pull quiz for %s: %w", day, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("pull quiz for %s: %w", day, err)
	}
	return changed > 0, nil
}

// Delete removes day's quiz, if any.
func (s *SQLiteStore) Delete(ctx context.Context, day string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM quizzes WHERE day = ?`, day); err != nil {
		return fmt.Errorf("delete quiz for %s: %w", day, err)
	}
	return nil
}

// Exists reports whether day has a quiz row, published or pulled.
func (s *SQLiteStore) Exists(ctx context.Context, day string) (bool, error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM quizzes WHERE day = ?)`, day).Scan(&exists); err != nil {
		return false, fmt.Errorf("check quiz for %s: %w", day, err)
	}
	return exists, nil
}
