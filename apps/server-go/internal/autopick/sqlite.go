package autopick

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Settings keys. The dashboard hides "newsroom." settings.
const (
	nextRunKey = "newsroom.autopick_next_run"
	lastRunKey = "newsroom.autopick_last_run"
)

// SQLiteStore reads the newsroom tables and keeps the schedule in settings.
type SQLiteStore struct {
	db *sql.DB
	// skipFeeds are feed URLs whose candidates are left for a human.
	skipFeeds []string
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{db: db} }

// LeavingFeeds makes Pending skip candidates from the given feeds, so they
// stay pending for a human editor.
func (s *SQLiteStore) LeavingFeeds(feedURLs []string) *SQLiteStore {
	s.skipFeeds = feedURLs
	return s
}

// Pending returns the newest pending candidates, oldest first.
func (s *SQLiteStore) Pending(ctx context.Context, limit int) ([]Candidate, error) {
	skip, args := "", make([]any, 0, len(s.skipFeeds)+1)
	if len(s.skipFeeds) > 0 {
		skip = ` AND feed_url NOT IN (` + strings.TrimSuffix(strings.Repeat("?,", len(s.skipFeeds)), ",") + `)`
		for _, feedURL := range s.skipFeeds {
			args = append(args, feedURL)
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, source_name, title, COALESCE(feed_summary, ''), COALESCE(feed_published_at, '')
		FROM (SELECT * FROM candidates WHERE status = 'pending'`+skip+` ORDER BY discovered_at DESC, id DESC LIMIT ?)
		ORDER BY discovered_at, id`, append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("autopick pending: %w", err)
	}
	defer rows.Close()
	var candidates []Candidate
	for rows.Next() {
		var c Candidate
		if err := rows.Scan(&c.ID, &c.Source, &c.Title, &c.Summary, &c.PublishedAt); err != nil {
			return nil, err
		}
		c.Summary = clip(c.Summary, 500)
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// History returns what was published and rejected since, newest first.
func (s *SQLiteStore) History(ctx context.Context, since time.Time, limit int) (History, error) {
	stamp := since.UTC().Format(time.RFC3339)
	published, err := s.stories(ctx, `SELECT title, published_at FROM articles
		WHERE status = 'published' AND published_at >= ? ORDER BY published_at DESC LIMIT ?`, stamp, limit)
	if err != nil {
		return History{}, err
	}
	rejected, err := s.stories(ctx, `SELECT title, updated_at FROM candidates
		WHERE status = 'rejected' AND updated_at >= ? ORDER BY updated_at DESC LIMIT ?`, stamp, limit)
	if err != nil {
		return History{}, err
	}
	return History{Published: published, Rejected: rejected}, nil
}

func (s *SQLiteStore) stories(ctx context.Context, query string, args ...any) ([]Story, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("autopick history: %w", err)
	}
	defer rows.Close()
	stories := []Story{}
	for rows.Next() {
		var story Story
		if err := rows.Scan(&story.Title, &story.At); err != nil {
			return nil, err
		}
		stories = append(stories, story)
	}
	return stories, rows.Err()
}

// NextRun returns when the next run is due, or nil if none was saved.
func (s *SQLiteStore) NextRun(ctx context.Context) (*time.Time, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, nextRunKey).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("autopick next run: %w", err)
	}
	next, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, nil
	}
	return &next, nil
}

// SaveRun records the run and when the next one is due.
func (s *SQLiteStore) SaveRun(ctx context.Context, run Run, next time.Time) error {
	data, err := json.Marshal(run)
	if err != nil {
		return err
	}
	for key, value := range map[string]string{lastRunKey: string(data), nextRunKey: next.UTC().Format(time.RFC3339)} {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
			return fmt.Errorf("autopick save run: %w", err)
		}
	}
	return nil
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
