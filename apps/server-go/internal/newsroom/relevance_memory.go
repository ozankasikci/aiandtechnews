package newsroom

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// relevanceRejectedKey holds the URLs the AI relevance judge turned down,
// newest last, so each feed item is judged once. The dashboard hides
// "newsroom." settings.
const (
	relevanceRejectedKey  = "newsroom.relevance_rejected"
	relevanceRejectedKeep = 3000
)

type rejectedItem struct {
	URL    string `json:"url"`
	Reason string `json:"reason"`
}

// KnownURLs reports which source URLs already exist as candidates or articles.
func (s *SQLiteStore) KnownURLs(ctx context.Context, urls []string) (map[string]bool, error) {
	known := map[string]bool{}
	for start := 0; start < len(urls); start += 200 {
		batch := urls[start:min(start+200, len(urls))]
		marks := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		args := make([]any, 0, 2*len(batch))
		for _, u := range batch {
			args = append(args, u)
		}
		args = append(args, args...)
		rows, err := s.db.QueryContext(ctx, `SELECT source_url FROM candidates WHERE source_url IN (`+marks+`)
			UNION SELECT source_url FROM articles WHERE source_url IN (`+marks+`)`, args...)
		if err != nil {
			return nil, fmt.Errorf("known urls: %w", err)
		}
		for rows.Next() {
			var u sql.NullString
			if err := rows.Scan(&u); err != nil {
				rows.Close()
				return nil, err
			}
			known[u.String] = true
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return known, nil
}

func (s *SQLiteStore) rejectedItems(ctx context.Context) ([]rejectedItem, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, relevanceRejectedKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read relevance memory: %w", err)
	}
	var items []rejectedItem
	if json.Unmarshal([]byte(raw), &items) != nil {
		return nil, nil // a damaged memory only means judging again
	}
	return items, nil
}

// RejectedURLs reports which URLs the judge rejected before.
func (s *SQLiteStore) RejectedURLs(ctx context.Context, urls []string) (map[string]bool, error) {
	items, err := s.rejectedItems(ctx)
	if err != nil {
		return nil, err
	}
	remembered := make(map[string]bool, len(items))
	for _, item := range items {
		remembered[item.URL] = true
	}
	out := map[string]bool{}
	for _, u := range urls {
		if remembered[u] {
			out[u] = true
		}
	}
	return out, nil
}

// RememberRejected adds judged rejections, keeping the newest few thousand.
func (s *SQLiteStore) RememberRejected(ctx context.Context, reasons map[string]string) error {
	items, err := s.rejectedItems(ctx)
	if err != nil {
		return err
	}
	for u, reason := range reasons {
		items = append(items, rejectedItem{URL: u, Reason: reason})
	}
	if len(items) > relevanceRejectedKeep {
		items = items[len(items)-relevanceRejectedKeep:]
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, relevanceRejectedKey, string(raw))
	if err != nil {
		return fmt.Errorf("record relevance memory: %w", err)
	}
	return nil
}
