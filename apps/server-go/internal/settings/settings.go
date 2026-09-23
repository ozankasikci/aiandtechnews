// Package settings ports the dashboard site-settings endpoints
// (GET/PUT /api/dashboard/settings). It owns no migration: the settings
// table is created, with Node's exact schema, by newsroom migration 3
// (CREATE TABLE IF NOT EXISTS), and newsroom stores its own "newsroom.*"
// keys in the same table.
//
// Deviation from Node (reviewed contract change, see
// docs/superpowers/plans/2026-09-24-go-admin-content.md, "Known Node
// behaviors kept" item 1 and its accompanying user decision): Node upserts a
// blank social/webhook field as SQL NULL, which violates settings.value NOT
// NULL and rolls the whole update back with a 500 -- the dashboard sends
// null for every blank field on every save, so today Node cannot save
// settings while any of those fields is blank. Go stores an empty string for
// a null value instead of failing.
//
// Deviation from Node (reviewed contract change): GET /api/dashboard/settings
// hides keys with the "newsroom." prefix (they are newsroom's own state, not
// dashboard site settings), and PUT can never write them because they are
// not in the allowlist below.
package settings

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/jsonbody"
)

// allowedKeys is Node's validKeys allowlist, in the order Node upserts them.
var allowedKeys = [...]string{
	"site_name",
	"site_description",
	"social_twitter",
	"social_linkedin",
	"social_github",
	"newsletter_enabled",
	"newsletter_provider",
	"newsletter_webhook_url",
}

// newsroomKeyPrefix marks settings rows that belong to newsroom, not the
// dashboard site settings surface. Hidden from GET, never written by PUT.
const newsroomKeyPrefix = "newsroom."

// Entry is one settings row.
type Entry struct {
	Key   string
	Value string
}

// Values is every settings row in SQLite's row order. It encodes as one JSON
// object in that order, with newsletter_enabled decoded to a boolean
// (value === "true") and every other value kept as its stored string.
type Values []Entry

func (v Values) MarshalJSON() ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	for i, entry := range v {
		if i > 0 {
			buffer.WriteByte(',')
		}
		key, err := json.Marshal(entry.Key)
		if err != nil {
			return nil, err
		}
		buffer.Write(key)
		buffer.WriteByte(':')
		if entry.Key == "newsletter_enabled" {
			buffer.WriteString(strconv.FormatBool(entry.Value == "true"))
			continue
		}
		value, err := json.Marshal(entry.Value)
		if err != nil {
			return nil, err
		}
		buffer.Write(value)
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

// Update is one allowlisted upsert, already bound like better-sqlite3 would.
type Update struct {
	Key   string
	Value any
}

type SQLiteStore struct{ db *sql.DB }

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{db: db} }

// All reads every row with Node's unordered query (SQLite row order),
// including newsroom's own keys; callers that serve the dashboard filter
// those out.
func (s *SQLiteStore) All(ctx context.Context) (Values, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, fmt.Errorf("read settings: %w", err)
	}
	defer rows.Close()
	values := make(Values, 0)
	for rows.Next() {
		var entry Entry
		if err := rows.Scan(&entry.Key, &entry.Value); err != nil {
			return nil, fmt.Errorf("scan setting: %w", err)
		}
		values = append(values, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate settings: %w", err)
	}
	return values, nil
}

// Upsert applies every update in one transaction; any failure rolls all of
// them back, like Node's db.transaction.
func (s *SQLiteStore) Upsert(ctx context.Context, updates []Update) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin settings transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	for _, update := range updates {
		if _, err = tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, update.Key, update.Value); err != nil {
			return fmt.Errorf("upsert setting %s: %w", update.Key, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit settings transaction: %w", err)
	}
	return nil
}

type store interface {
	All(context.Context) (Values, error)
	Upsert(context.Context, []Update) error
}

type Service struct{ store store }

func NewService(store store) *Service { return &Service{store: store} }

// Get ports dashboard.ts:632-648, with the reviewed deviation that
// newsroom.* keys never appear on the dashboard settings surface.
func (s *Service) Get(ctx context.Context) (Values, error) {
	values, err := s.store.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("get settings: %w", err)
	}
	visible := make(Values, 0, len(values))
	for _, entry := range values {
		if strings.HasPrefix(entry.Key, newsroomKeyPrefix) {
			continue
		}
		visible = append(visible, entry)
	}
	return visible, nil
}

// Update ports dashboard.ts:650-696: every defined allowlisted key is
// upserted (booleans as "true"/"false", other values bound like
// better-sqlite3), unknown keys -- including any "newsroom." key, which is
// never in the allowlist -- are ignored, and the visible rows are returned.
//
// Reviewed deviation: a null value is stored as an empty string instead of
// SQL NULL, so the dashboard can save settings with a blank social or
// webhook field. See the package doc.
func (s *Service) Update(ctx context.Context, body jsonbody.Object) (Values, error) {
	updates := make([]Update, 0, len(allowedKeys))
	for _, key := range allowedKeys {
		value := body.Get(key)
		if !value.Defined() {
			continue
		}
		if value.IsNull() {
			updates = append(updates, Update{Key: key, Value: ""})
			continue
		}
		if b, ok := value.Bool(); ok {
			updates = append(updates, Update{Key: key, Value: strconv.FormatBool(b)})
			continue
		}
		bound, err := value.Bind()
		if err != nil {
			return nil, fmt.Errorf("bind setting %s: %w", key, err)
		}
		updates = append(updates, Update{Key: key, Value: bound})
	}
	if err := s.store.Upsert(ctx, updates); err != nil {
		return nil, fmt.Errorf("update settings: %w", err)
	}
	return s.Get(ctx)
}
