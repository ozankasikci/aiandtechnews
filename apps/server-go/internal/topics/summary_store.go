package topics

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Topic is a live topic whose hub summary may be due.
type Topic struct {
	ID      int64
	Slug    string
	Name    string
	Kind    string
	Summary string
	Facts   []string
}

// SourceArticle is what the summary is written from: an article's title,
// TL;DR and why-it-matters line. Never its source.
type SourceArticle struct {
	Title        string
	PublishedAt  string
	TLDR         []string
	WhyItMatters string
}

// MinSummaryAge is how long a topic waits between summary refreshes.
const MinSummaryAge = 24 * time.Hour

// Due lists live topics whose summary is missing, or at least MinSummaryAge
// old with new articles tagged since (a tagged article touches the topic's
// updated_at; the refresh stamps summary_updated_at, also when it keeps the
// old summary, so a failing topic waits for the next new article). Topics
// without a summary come first, then the most covered.
func (s *Store) Due(ctx context.Context, limit int) ([]Topic, error) {
	cutoff := s.now().UTC().Add(-MinSummaryAge).Format(sqliteDateTime)
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.slug, t.name, t.kind, t.summary, t.facts_json
		FROM topics t JOIN (SELECT x.topic_id AS id, COUNT(*) AS n FROM article_topics x
			JOIN articles xa ON xa.id = x.article_id AND xa.status = 'published'
			GROUP BY x.topic_id HAVING COUNT(*) >= 3) l ON l.id = t.id
		WHERE t.summary_updated_at IS NULL
			OR (t.summary_updated_at <= ? AND t.updated_at > t.summary_updated_at)
		ORDER BY t.summary_updated_at IS NULL DESC, l.n DESC, t.slug LIMIT ?`, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("list topics due a summary: %w", err)
	}
	defer rows.Close()
	var out []Topic
	for rows.Next() {
		var t Topic
		var facts string
		if err := rows.Scan(&t.ID, &t.Slug, &t.Name, &t.Kind, &t.Summary, &facts); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(facts), &t.Facts)
		out = append(out, t)
	}
	return out, rows.Err()
}

// Latest returns the topic's newest published articles, newest first.
func (s *Store) Latest(ctx context.Context, topicID int64, limit int) ([]SourceArticle, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.title, COALESCE(a.published_at, ''), COALESCE(s.tldr, '[]'), COALESCE(s.why_it_matters, '')
		FROM article_topics x JOIN articles a ON a.id = x.article_id AND a.status = 'published'
		LEFT JOIN article_summaries s ON s.article_id = a.id
		WHERE x.topic_id = ? ORDER BY a.published_at DESC, a.id DESC LIMIT ?`, topicID, limit)
	if err != nil {
		return nil, fmt.Errorf("read topic articles: %w", err)
	}
	defer rows.Close()
	var out []SourceArticle
	for rows.Next() {
		var a SourceArticle
		var points string
		if err := rows.Scan(&a.Title, &a.PublishedAt, &points, &a.WhyItMatters); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(points), &a.TLDR)
		out = append(out, a)
	}
	return out, rows.Err()
}

// SaveSummary stores a verified summary and facts.
func (s *Store) SaveSummary(ctx context.Context, topicID int64, summary string, facts []string) error {
	if facts == nil {
		facts = []string{}
	}
	raw, err := json.Marshal(facts)
	if err != nil {
		return err
	}
	stamp := s.now().UTC().Format(sqliteDateTime)
	if _, err := s.db.ExecContext(ctx, `UPDATE topics SET summary = ?, facts_json = ?, summary_updated_at = ?, updated_at = ? WHERE id = ?`,
		summary, string(raw), stamp, stamp, topicID); err != nil {
		return fmt.Errorf("save topic summary: %w", err)
	}
	return nil
}

// MarkChecked records a refresh attempt that kept the previous summary.
func (s *Store) MarkChecked(ctx context.Context, topicID int64) error {
	stamp := s.now().UTC().Format(sqliteDateTime)
	if _, err := s.db.ExecContext(ctx, `UPDATE topics SET summary_updated_at = ? WHERE id = ?`, stamp, topicID); err != nil {
		return fmt.Errorf("mark topic checked: %w", err)
	}
	return nil
}
