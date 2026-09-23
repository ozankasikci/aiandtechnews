package newsroom

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	settingDelayMin      = "newsroom.publish_delay_min_minutes"
	settingDelayMax      = "newsroom.publish_delay_max_minutes"
	settingLastCollected = "newsroom.last_collected_at"
)

var defaultDelay = PublishDelay{MinMinutes: 30, MaxMinutes: 40}

type SQLiteStore struct{ db *sql.DB }

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{db: db} }

const candidateSelect = `SELECT c.id, c.title, c.feed_summary, c.source_name, c.source_url,
	c.source_image_url, c.feed_published_at, c.discovered_at, c.status, c.scheduled_for,
	c.attempts, c.last_error, a.slug
	FROM candidates c LEFT JOIN articles a ON a.id = c.article_id`

// listOrder groups the queue view (processing, queued, failed) ahead of the
// review view, then orders within each status as the app displays it.
const listOrder = ` ORDER BY CASE c.status WHEN 'processing' THEN 0 WHEN 'queued' THEN 1 WHEN 'failed' THEN 2
	WHEN 'pending' THEN 3 WHEN 'published' THEN 4 ELSE 5 END,
	CASE WHEN c.status = 'queued' THEN c.scheduled_for END ASC,
	CASE WHEN c.status = 'pending' THEN c.discovered_at END DESC,
	c.updated_at DESC, c.id DESC`

type rowScanner interface{ Scan(...any) error }

func scanCandidate(row rowScanner) (Candidate, error) {
	var c Candidate
	err := row.Scan(&c.ID, &c.Title, &c.FeedSummary, &c.SourceName, &c.SourceURL,
		&c.SourceImageURL, &c.FeedPublishedAt, &c.DiscoveredAt, &c.Status, &c.ScheduledFor,
		&c.Attempts, &c.LastError, &c.ArticleSlug)
	return c, err
}

// Insert stores a new pending candidate. It reports false without error when
// the source URL is already known (including rejected candidates).
func (s *SQLiteStore) Insert(ctx context.Context, candidate NewCandidate, now time.Time) (int64, bool, error) {
	var feedPublished *string
	if candidate.FeedPublishedAt != nil {
		value := formatTime(*candidate.FeedPublishedAt)
		feedPublished = &value
	}
	stamp := formatTime(now)
	result, err := s.db.ExecContext(ctx, `INSERT INTO candidates
		(source_url, source_name, feed_url, title, feed_summary, source_image_url, feed_published_at, discovered_at, status, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?)
		ON CONFLICT(source_url) DO NOTHING`,
		candidate.SourceURL, candidate.SourceName, candidate.FeedURL, candidate.Title, candidate.FeedSummary,
		candidate.SourceImageURL, feedPublished, stamp, stamp)
	if err != nil {
		return 0, false, fmt.Errorf("insert candidate: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, false, fmt.Errorf("insert candidate: %w", err)
	}
	if affected == 0 {
		return 0, false, nil
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, false, fmt.Errorf("insert candidate id: %w", err)
	}
	return id, true, nil
}

func (s *SQLiteStore) Get(ctx context.Context, id int64) (Candidate, error) {
	candidate, err := scanCandidate(s.db.QueryRowContext(ctx, candidateSelect+` WHERE c.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Candidate{}, ErrNotFound
	}
	if err != nil {
		return Candidate{}, fmt.Errorf("get candidate %d: %w", id, err)
	}
	return candidate, nil
}

// List returns one page of candidates in any of statuses. statuses must be non-empty.
func (s *SQLiteStore) List(ctx context.Context, statuses []Status, page, limit int) (Page, error) {
	args := make([]any, 0, len(statuses)+2)
	for _, status := range statuses {
		args = append(args, string(status))
	}
	where := ` WHERE c.status IN (` + strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",") + `)`

	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM candidates c`+where, args...).Scan(&total); err != nil {
		return Page{}, fmt.Errorf("count candidates: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, candidateSelect+where+listOrder+` LIMIT ? OFFSET ?`,
		append(args, limit, (page-1)*limit)...)
	if err != nil {
		return Page{}, fmt.Errorf("list candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]Candidate, 0)
	for rows.Next() {
		candidate, err := scanCandidate(rows)
		if err != nil {
			return Page{}, fmt.Errorf("scan candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("iterate candidates: %w", err)
	}
	return Page{
		Candidates: candidates,
		Total:      total,
		Page:       page,
		TotalPages: (total + int64(limit) - 1) / int64(limit),
	}, nil
}

// transition runs a guarded UPDATE; zero affected rows means the candidate
// was missing or no longer in the expected status.
func (s *SQLiteStore) transition(ctx context.Context, id int64, query string, args ...any) error {
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update candidate %d: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update candidate %d: %w", id, err)
	}
	if affected == 0 {
		return ErrStaleTransition
	}
	return nil
}

// MarkQueued moves a candidate from `from` (pending or failed) into the queue.
func (s *SQLiteStore) MarkQueued(ctx context.Context, id int64, from Status, scheduledFor, now time.Time) error {
	return s.transition(ctx, id, `UPDATE candidates SET status = 'queued', scheduled_for = ?, attempts = 0,
		last_error = NULL, updated_at = ? WHERE id = ? AND status = ?`,
		formatTime(scheduledFor), formatTime(now), id, string(from))
}

// MarkPending returns a queued candidate to review.
func (s *SQLiteStore) MarkPending(ctx context.Context, id int64, now time.Time) error {
	return s.transition(ctx, id, `UPDATE candidates SET status = 'pending', scheduled_for = NULL, updated_at = ?
		WHERE id = ? AND status = 'queued'`, formatTime(now), id)
}

// MarkRejected rejects a pending or failed candidate. Rejected rows are kept
// so the collector never re-adds the URL.
func (s *SQLiteStore) MarkRejected(ctx context.Context, id int64, now time.Time) error {
	return s.transition(ctx, id, `UPDATE candidates SET status = 'rejected', scheduled_for = NULL, updated_at = ?
		WHERE id = ? AND status IN ('pending', 'failed')`, formatTime(now), id)
}

// LatestScheduled returns the tail of the queue, or nil when nothing is queued or processing.
func (s *SQLiteStore) LatestScheduled(ctx context.Context) (*time.Time, error) {
	var value sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(scheduled_for) FROM candidates
		WHERE status IN ('queued', 'processing')`).Scan(&value); err != nil {
		return nil, fmt.Errorf("read queue tail: %w", err)
	}
	if !value.Valid {
		return nil, nil
	}
	latest, err := parseTime(value.String)
	if err != nil {
		return nil, fmt.Errorf("parse queue tail %q: %w", value.String, err)
	}
	return &latest, nil
}

// Overview counts candidates by status; publishedSince bounds published_today.
func (s *SQLiteStore) Overview(ctx context.Context, publishedSince time.Time) (Overview, error) {
	var overview Overview
	err := s.db.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(status = 'pending'), 0),
		COALESCE(SUM(status = 'queued'), 0),
		COALESCE(SUM(status = 'processing'), 0),
		COALESCE(SUM(status = 'failed'), 0),
		COALESCE(SUM(status = 'published' AND updated_at >= ?), 0),
		MIN(CASE WHEN status = 'queued' THEN scheduled_for END)
		FROM candidates`, formatTime(publishedSince)).Scan(
		&overview.Pending, &overview.Queued, &overview.Processing, &overview.Failed,
		&overview.PublishedToday, &overview.NextPublishAt)
	if err != nil {
		return Overview{}, fmt.Errorf("count candidates by status: %w", err)
	}
	overview.LastCollectedAt, err = s.setting(ctx, settingLastCollected)
	if err != nil {
		return Overview{}, err
	}
	return overview, nil
}

func (s *SQLiteStore) setting(ctx context.Context, key string) (*string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read setting %s: %w", key, err)
	}
	return &value, nil
}

func (s *SQLiteStore) PublishDelay(ctx context.Context) (PublishDelay, error) {
	delay := defaultDelay
	for key, target := range map[string]*int{settingDelayMin: &delay.MinMinutes, settingDelayMax: &delay.MaxMinutes} {
		value, err := s.setting(ctx, key)
		if err != nil {
			return PublishDelay{}, err
		}
		if value == nil {
			continue
		}
		parsed, err := strconv.Atoi(*value)
		if err != nil {
			return PublishDelay{}, fmt.Errorf("parse setting %s=%q: %w", key, *value, err)
		}
		*target = parsed
	}
	return delay, nil
}

func (s *SQLiteStore) SetPublishDelay(ctx context.Context, delay PublishDelay) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delay update: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	for _, pair := range [][2]string{
		{settingDelayMin, strconv.Itoa(delay.MinMinutes)},
		{settingDelayMax, strconv.Itoa(delay.MaxMinutes)},
	} {
		if _, err = tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, pair[0], pair[1]); err != nil {
			return fmt.Errorf("write setting %s: %w", pair[0], err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit delay update: %w", err)
	}
	return nil
}
