package newsletter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// SQLiteStore holds the newsletter SQL. Every statement is Node's
// (apps/server/src/newsletter/service.ts) with the same parameters. The
// check-then-write pairs Node runs back to back on its single thread run
// in one transaction here, so concurrent Go requests cannot interleave them.
type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{db: db} }

type subscriber struct {
	id     int64
	email  string
	status string
}

func (s *SQLiteStore) inTx(ctx context.Context, run func(*sql.Tx) error) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()
	if err = run(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func scanSubscriber(row *sql.Row) (subscriber, bool, error) {
	var found subscriber
	var status sql.NullString
	err := row.Scan(&found.id, &found.email, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return subscriber{}, false, nil
	}
	if err != nil {
		return subscriber{}, false, err
	}
	found.status = status.String
	return found, true, nil
}

// Subscribe is requestSubscription's SQL: an active address is left alone,
// any other existing row (pending from the old double opt-in, or
// unsubscribed) is reactivated, and a new address is inserted as active.
func (s *SQLiteStore) Subscribe(ctx context.Context, email, source, timestamp string) (SubscriptionState, error) {
	state := StateSubscribed
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		existing, found, err := scanSubscriber(tx.QueryRowContext(ctx, `SELECT id, email, status FROM subscribers WHERE lower(email) = ?`, email))
		if err != nil {
			return fmt.Errorf("find subscriber: %w", err)
		}
		if found && existing.status == "active" {
			state = StateAlreadyActive
			return nil
		}
		if found {
			_, err = tx.ExecContext(ctx, `UPDATE subscribers
           SET status = 'active',
               source_placement = ?,
               confirmed_at = COALESCE(confirmed_at, ?),
               unsubscribed_at = NULL,
               updated_at = ?
           WHERE id = ?`, source, timestamp, timestamp, existing.id)
			if err != nil {
				return fmt.Errorf("reactivate subscriber: %w", err)
			}
			return nil
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO subscribers (email, status, source_placement, confirmed_at, created_at, updated_at)
         VALUES (?, 'active', ?, ?, ?, ?)`, email, source, timestamp, timestamp, timestamp)
		if err != nil {
			return fmt.Errorf("insert subscriber: %w", err)
		}
		return nil
	})
	return state, err
}

// Confirm is confirmSubscription's SQL. It returns the subscriber's email
// when this call confirmed it (the caller then sends the welcome email).
func (s *SQLiteStore) Confirm(ctx context.Context, id int64, timestamp string) (ConfirmationState, string, error) {
	state := ConfirmationInvalid
	var email string
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		existing, found, err := scanSubscriber(tx.QueryRowContext(ctx, `SELECT id, email, status FROM subscribers WHERE id = ?`, id))
		if err != nil {
			return fmt.Errorf("find subscriber: %w", err)
		}
		switch {
		case !found || existing.status == "unsubscribed":
			return nil
		case existing.status == "active":
			state = ConfirmationAlreadyConfirmed
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE subscribers
         SET status = 'active', confirmed_at = ?, unsubscribed_at = NULL, updated_at = ?
         WHERE id = ?`, timestamp, timestamp, existing.id); err != nil {
			return fmt.Errorf("confirm subscriber: %w", err)
		}
		state, email = ConfirmationConfirmed, existing.email
		return nil
	})
	return state, email, err
}

// Unsubscribe is unsubscribe's SQL.
func (s *SQLiteStore) Unsubscribe(ctx context.Context, id int64, timestamp string) (UnsubscribeState, error) {
	state := UnsubscribeInvalid
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		existing, found, err := scanSubscriber(tx.QueryRowContext(ctx, `SELECT id, email, status FROM subscribers WHERE id = ?`, id))
		if err != nil {
			return fmt.Errorf("find subscriber: %w", err)
		}
		switch {
		case !found:
			return nil
		case existing.status == "unsubscribed":
			state = UnsubscribeAlreadyUnsubscribed
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE subscribers
         SET status = 'unsubscribed', unsubscribed_at = ?, updated_at = ?
         WHERE id = ?`, timestamp, timestamp, existing.id); err != nil {
			return fmt.Errorf("unsubscribe: %w", err)
		}
		state = UnsubscribeUnsubscribed
		return nil
	})
	return state, err
}

// ListEditions is listEditions' SQL; limit is already clamped to 1..100.
func (s *SQLiteStore) ListEditions(ctx context.Context, limit int) ([]Edition, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT edition_key, subject, articles, created_at
         FROM newsletter_editions
         ORDER BY edition_key DESC
         LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list editions: %w", err)
	}
	defer rows.Close()
	editions := []Edition{}
	for rows.Next() {
		var key, subject, articles, createdAt string
		if err := rows.Scan(&key, &subject, &articles, &createdAt); err != nil {
			return nil, fmt.Errorf("scan edition: %w", err)
		}
		editions = append(editions, toEdition(key, subject, articles, createdAt))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list editions: %w", err)
	}
	return editions, nil
}

// Edition is getEdition's SQL.
func (s *SQLiteStore) Edition(ctx context.Context, key string) (Edition, bool, error) {
	var subject, articles, createdAt string
	err := s.db.QueryRowContext(ctx, `SELECT edition_key, subject, articles, created_at FROM newsletter_editions WHERE edition_key = ?`, key).
		Scan(&key, &subject, &articles, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Edition{}, false, nil
	}
	if err != nil {
		return Edition{}, false, fmt.Errorf("get edition: %w", err)
	}
	return toEdition(key, subject, articles, createdAt), true, nil
}

// toEdition is Node's toEdition: the stored articles JSON is passed through
// when it is an array, whatever its elements; anything else (malformed JSON,
// an object) becomes [] so a bad row cannot take the archive down.
func toEdition(key, subject, articles, createdAt string) Edition {
	edition := Edition{Edition: key, Subject: subject, Articles: json.RawMessage("[]"), CreatedAt: createdAt}
	trimmed := strings.TrimLeft(articles, " \t\r\n")
	if strings.HasPrefix(trimmed, "[") && json.Valid([]byte(articles)) {
		edition.Articles = json.RawMessage(strings.TrimSpace(articles))
	}
	return edition
}
