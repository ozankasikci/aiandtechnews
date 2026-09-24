package newsroom

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
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
	c.attempts, c.last_error, a.slug,
	EXISTS (SELECT 1 FROM candidate_publish_now p WHERE p.candidate_id = c.id)
	FROM candidates c LEFT JOIN articles a ON a.id = c.article_id`

// listOrder groups the queue view (processing, queued, failed) ahead of the
// review view, then orders within each status as the app displays it.
const listOrder = ` ORDER BY CASE c.status WHEN 'processing' THEN 0 WHEN 'queued' THEN 1 WHEN 'failed' THEN 2
	WHEN 'pending' THEN 3 WHEN 'published' THEN 4 ELSE 5 END,
	CASE WHEN c.status = 'queued' THEN c.scheduled_for END ASC,
	CASE WHEN c.status = 'pending' THEN c.discovered_at END DESC,
	CASE WHEN c.status = 'published' THEN c.published_at END DESC,
	c.updated_at DESC, c.id DESC`

type rowScanner interface{ Scan(...any) error }

func scanCandidate(row rowScanner) (Candidate, error) {
	var c Candidate
	err := row.Scan(&c.ID, &c.Title, &c.FeedSummary, &c.SourceName, &c.SourceURL,
		&c.SourceImageURL, &c.FeedPublishedAt, &c.DiscoveredAt, &c.Status, &c.ScheduledFor,
		&c.Attempts, &c.LastError, &c.ArticleSlug, &c.PublishNow)
	return c, err
}

// Insert stores a new pending candidate. It reports false without error when
// the source URL is already a candidate (including rejected ones) or the
// story was already published as an article (same source URL or title slug).
func (s *SQLiteStore) Insert(ctx context.Context, candidate NewCandidate, now time.Time) (int64, bool, error) {
	var feedPublished *string
	if candidate.FeedPublishedAt != nil {
		value := formatTime(*candidate.FeedPublishedAt)
		feedPublished = &value
	}
	stamp := formatTime(now)
	result, err := s.db.ExecContext(ctx, `INSERT INTO candidates
		(source_url, source_name, feed_url, title, feed_summary, source_image_url, feed_published_at, discovered_at, status, updated_at)
		SELECT ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?
		WHERE NOT EXISTS (SELECT 1 FROM articles WHERE source_url = ? OR slug = ?)
		ON CONFLICT(source_url) DO NOTHING`,
		candidate.SourceURL, candidate.SourceName, candidate.FeedURL, candidate.Title, candidate.FeedSummary,
		candidate.SourceImageURL, feedPublished, stamp, stamp,
		candidate.SourceURL, content.Slugify(candidate.Title))
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
	if page < 1 || limit < 1 || len(statuses) == 0 {
		return Page{}, fmt.Errorf("list candidates: invalid page=%d limit=%d statuses=%d", page, limit, len(statuses))
	}
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
	if from != StatusPending && from != StatusFailed {
		return fmt.Errorf("mark queued from %q: %w", from, ErrStaleTransition)
	}
	return s.transition(ctx, id, `UPDATE candidates SET status = 'queued', scheduled_for = ?, attempts = 0,
		last_error = NULL, updated_at = ? WHERE id = ? AND status = ?`,
		formatTime(scheduledFor), formatTime(now), id, string(from))
}

// MarkPublishNow queues a pending, queued, or failed candidate due at now and
// flags it so ClaimDue takes it first, ignoring the minimum gap. Like
// MarkQueued, a pending or failed candidate starts with no attempts and no
// last error; an already queued one keeps them. Other queue entries keep
// their times.
func (s *SQLiteStore) MarkPublishNow(ctx context.Context, id int64, from Status, now time.Time) (err error) {
	if from != StatusPending && from != StatusQueued && from != StatusFailed {
		return fmt.Errorf("mark publish now from %q: %w", from, ErrStaleTransition)
	}
	stamp := formatTime(now)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin publish now %d: %w", id, err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE candidates SET status = 'queued', scheduled_for = ?,
		attempts = CASE WHEN status = 'queued' THEN attempts ELSE 0 END,
		last_error = CASE WHEN status = 'queued' THEN last_error ELSE NULL END,
		updated_at = ? WHERE id = ? AND status = ?`, stamp, stamp, id, string(from))
	if err != nil {
		return fmt.Errorf("publish now %d: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("publish now %d: %w", id, err)
	}
	if affected == 0 {
		return ErrStaleTransition
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO candidate_publish_now (candidate_id, requested_at) VALUES (?, ?)
		ON CONFLICT(candidate_id) DO UPDATE SET requested_at = excluded.requested_at`, id, stamp); err != nil {
		return fmt.Errorf("flag publish now %d: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit publish now %d: %w", id, err)
	}
	return nil
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

// ShiftQueue moves every queued candidate that is not flagged "publish now"
// by the same number of minutes, keeping their relative spacing, in one
// transaction. A positive shift postpones. A negative shift brings the queue
// forward but is clamped so the earliest shifted candidate is not scheduled
// before now; when nothing can move (empty queue, or the earliest candidate
// is already due) the result applies 0 minutes and changes nothing. Updates
// are guarded by status = 'queued', so a candidate the publisher claims
// concurrently is left alone. The result lists all queued candidates,
// including flagged ones, in schedule order.
func (s *SQLiteStore) ShiftQueue(ctx context.Context, minutes int, now time.Time) (QueueShift, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return QueueShift{}, fmt.Errorf("begin queue shift: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `SELECT c.id, c.scheduled_for FROM candidates c
		WHERE c.status = 'queued'
		  AND NOT EXISTS (SELECT 1 FROM candidate_publish_now p WHERE p.candidate_id = c.id)
		ORDER BY c.scheduled_for, c.id`)
	if err != nil {
		return QueueShift{}, fmt.Errorf("read queue for shift: %w", err)
	}
	type entry struct {
		id    int64
		stamp string
		at    time.Time
	}
	entries := make([]entry, 0)
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.stamp); err != nil {
			_ = rows.Close()
			return QueueShift{}, fmt.Errorf("scan queue entry: %w", err)
		}
		if e.at, err = parseTime(e.stamp); err != nil {
			_ = rows.Close()
			return QueueShift{}, fmt.Errorf("parse scheduled_for %q of candidate %d: %w", e.stamp, e.id, err)
		}
		entries = append(entries, e)
	}
	if err := rows.Close(); err != nil {
		return QueueShift{}, fmt.Errorf("read queue for shift: %w", err)
	}
	if err := rows.Err(); err != nil {
		return QueueShift{}, fmt.Errorf("read queue for shift: %w", err)
	}

	applied := 0
	if len(entries) > 0 {
		applied = clampShift(minutes, entries[0].at, now)
	}
	var shifted int64
	if applied != 0 {
		stamp := formatTime(now)
		delta := time.Duration(applied) * time.Minute
		for _, e := range entries {
			result, err := tx.ExecContext(ctx, `UPDATE candidates SET scheduled_for = ?, updated_at = ?
				WHERE id = ? AND status = 'queued' AND scheduled_for = ?`,
				formatTime(e.at.Add(delta)), stamp, e.id, e.stamp)
			if err != nil {
				return QueueShift{}, fmt.Errorf("shift candidate %d: %w", e.id, err)
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return QueueShift{}, fmt.Errorf("shift candidate %d: %w", e.id, err)
			}
			shifted += affected
		}
	}
	if shifted == 0 {
		applied = 0
	}

	candidates, err := queuedCandidates(ctx, tx)
	if err != nil {
		return QueueShift{}, err
	}
	if err := tx.Commit(); err != nil {
		return QueueShift{}, fmt.Errorf("commit queue shift: %w", err)
	}
	return QueueShift{Shifted: shifted, Minutes: applied, Candidates: candidates}, nil
}

// clampShift returns the minutes to apply: a positive request as is, and a
// negative one no further forward than would put earliest before now (and
// never positive, so an already overdue queue is left where it is).
func clampShift(minutes int, earliest, now time.Time) int {
	if minutes >= 0 {
		return minutes
	}
	limit := int(math.Ceil(now.Sub(earliest).Minutes()))
	return min(max(minutes, limit), 0)
}

// queuedCandidates lists every queued candidate in schedule order.
func queuedCandidates(ctx context.Context, tx *sql.Tx) ([]Candidate, error) {
	rows, err := tx.QueryContext(ctx, candidateSelect+` WHERE c.status = 'queued' ORDER BY c.scheduled_for, c.id`)
	if err != nil {
		return nil, fmt.Errorf("list queued candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]Candidate, 0)
	for rows.Next() {
		candidate, err := scanCandidate(rows)
		if err != nil {
			return nil, fmt.Errorf("scan queued candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate queued candidates: %w", err)
	}
	return candidates, nil
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
		COALESCE(SUM(status = 'published' AND published_at >= ?), 0),
		MIN(CASE WHEN status = 'queued' THEN scheduled_for END),
		MAX(CASE WHEN status = 'published' THEN published_at END)
		FROM candidates`, formatTime(publishedSince)).Scan(
		&overview.Pending, &overview.Queued, &overview.Processing, &overview.Failed,
		&overview.PublishedToday, &overview.NextPublishAt, &overview.lastPublishedAt)
	if err != nil {
		return Overview{}, fmt.Errorf("count candidates by status: %w", err)
	}
	overview.LastCollectedAt, err = s.setting(ctx, settingLastCollected)
	if err != nil {
		return Overview{}, err
	}
	return overview, nil
}

// SetLastCollected records when the collector last completed a run.
func (s *SQLiteStore) SetLastCollected(ctx context.Context, at time.Time) error {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, settingLastCollected, formatTime(at)); err != nil {
		return fmt.Errorf("record last collection: %w", err)
	}
	return nil
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
	if err := delay.Validate(); err != nil {
		return PublishDelay{}, fmt.Errorf("stored publish delay %+v: %w", delay, err)
	}
	return delay, nil
}

// ClaimDue moves the next due queued candidate to processing (attempts+1)
// and returns it. A due candidate flagged "publish now" comes first and
// ignores minGap; otherwise the earliest due candidate is taken, but only
// when the most recent publish is older than minGap, which keeps spacing
// after transient retries, crash recovery or downtime. Nothing is claimed
// while another candidate is processing.
func (s *SQLiteStore) ClaimDue(ctx context.Context, now time.Time, minGap time.Duration) (Candidate, bool, error) {
	stamp := formatTime(now)
	var id int64
	err := s.db.QueryRowContext(ctx, `UPDATE candidates
		SET status = 'processing', attempts = attempts + 1, updated_at = ?
		WHERE id = (SELECT c.id FROM candidates c
		            LEFT JOIN candidate_publish_now p ON p.candidate_id = c.id
		            WHERE c.status = 'queued' AND c.scheduled_for <= ?
		              AND (p.candidate_id IS NOT NULL OR NOT EXISTS
		                   (SELECT 1 FROM candidates WHERE status = 'published' AND published_at > ?))
		            ORDER BY p.candidate_id IS NULL, c.scheduled_for, c.id LIMIT 1)
		  AND status = 'queued'
		  AND NOT EXISTS (SELECT 1 FROM candidates WHERE status = 'processing')
		RETURNING id`, stamp, stamp, formatTime(now.Add(-minGap))).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Candidate{}, false, nil
	}
	if err != nil {
		return Candidate{}, false, fmt.Errorf("claim due candidate: %w", err)
	}
	candidate, err := s.Get(ctx, id)
	if err != nil {
		return Candidate{}, false, err
	}
	return candidate, true, nil
}

// MarkPublished records the article created from a processing candidate.
func (s *SQLiteStore) MarkPublished(ctx context.Context, id, articleID int64, now time.Time) error {
	stamp := formatTime(now)
	return s.transition(ctx, id, `UPDATE candidates SET status = 'published', article_id = ?, published_at = ?,
		scheduled_for = NULL, last_error = NULL, updated_at = ? WHERE id = ? AND status = 'processing'`,
		articleID, stamp, stamp, id)
}

// MarkFailed stops retrying a processing candidate and records why.
func (s *SQLiteStore) MarkFailed(ctx context.Context, id int64, reason string, now time.Time) error {
	return s.transition(ctx, id, `UPDATE candidates SET status = 'failed', scheduled_for = NULL, last_error = ?,
		updated_at = ? WHERE id = ? AND status = 'processing'`, reason, formatTime(now), id)
}

// Requeue returns a processing candidate to the queue after a transient failure.
func (s *SQLiteStore) Requeue(ctx context.Context, id int64, reason string, at, now time.Time) error {
	return s.transition(ctx, id, `UPDATE candidates SET status = 'queued', scheduled_for = ?, last_error = ?,
		updated_at = ? WHERE id = ? AND status = 'processing'`, formatTime(at), reason, formatTime(now), id)
}

// RequeueWithoutAttempt returns a processing candidate to the queue and gives
// back the attempt its claim consumed. It is for failures that are not the
// candidate's fault: shutdown and system configuration faults.
func (s *SQLiteStore) RequeueWithoutAttempt(ctx context.Context, id int64, reason string, at, now time.Time) error {
	return s.transition(ctx, id, `UPDATE candidates SET status = 'queued', scheduled_for = ?, last_error = ?,
		attempts = MAX(attempts - 1, 0), updated_at = ? WHERE id = ? AND status = 'processing'`,
		formatTime(at), reason, formatTime(now), id)
}

// interruptedReason is recorded when a candidate keeps getting interrupted mid-publish.
const interruptedReason = "interrupted repeatedly while publishing"

// ResetProcessing recovers candidates left processing by a crash. Those that
// already used maxAttempts claims are marked failed (so a candidate that
// crashes the process cannot loop forever); the rest are requeued, due now.
// It returns how many were requeued and how many were failed.
func (s *SQLiteStore) ResetProcessing(ctx context.Context, now time.Time, maxAttempts int) (requeued, failed int64, err error) {
	stamp := formatTime(now)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin processing reset: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE candidates SET status = 'failed', scheduled_for = NULL, last_error = ?,
		updated_at = ? WHERE status = 'processing' AND attempts >= ?`, interruptedReason, stamp, maxAttempts)
	if err != nil {
		return 0, 0, fmt.Errorf("fail interrupted candidates: %w", err)
	}
	if failed, err = result.RowsAffected(); err != nil {
		return 0, 0, fmt.Errorf("fail interrupted candidates: %w", err)
	}
	result, err = tx.ExecContext(ctx, `UPDATE candidates SET status = 'queued', scheduled_for = ?, updated_at = ?
		WHERE status = 'processing'`, stamp, stamp)
	if err != nil {
		return 0, 0, fmt.Errorf("reset processing candidates: %w", err)
	}
	if requeued, err = result.RowsAffected(); err != nil {
		return 0, 0, fmt.Errorf("reset processing candidates: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit processing reset: %w", err)
	}
	return requeued, failed, nil
}

func (s *SQLiteStore) SetPublishDelay(ctx context.Context, delay PublishDelay) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delay update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
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
