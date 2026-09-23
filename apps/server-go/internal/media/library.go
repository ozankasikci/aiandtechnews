package media

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Item is one media row, serialized exactly like Node's SELECT * FROM media.
type Item struct {
	ID         int64  `json:"id"`
	Filename   string `json:"filename"`
	URL        string `json:"url"`
	MIMEType   string `json:"mime_type"`
	Size       int64  `json:"size"`
	UploadedAt string `json:"uploaded_at"`
}

// NewItem is what an upload records: multer's originalname, the generated
// file, and the part's MIME type.
type NewItem struct {
	Filename   string
	URL        string
	MIMEType   string
	Size       int64
	UploadedAt string
}

// ErrMediaNotFound is Node's 404 "Media not found".
var ErrMediaNotFound = errors.New("Media not found")

const mediaColumns = `id, filename, url, mime_type, size, uploaded_at`

// SQLiteLibrary is the media table repository.
type SQLiteLibrary struct{ db *sql.DB }

func NewSQLiteLibrary(db *sql.DB) *SQLiteLibrary { return &SQLiteLibrary{db: db} }

// List ports "SELECT * FROM media ORDER BY uploaded_at DESC" (dashboard.ts:577-582).
func (s *SQLiteLibrary) List(ctx context.Context) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+mediaColumns+` FROM media ORDER BY uploaded_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Item{}
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ID, &item.Filename, &item.URL, &item.MIMEType, &item.Size, &item.UploadedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// Get looks a row up by the raw path id. Like better-sqlite3, the id is bound
// as text, so SQLite's integer affinity applies (" 401" matches 401, "abc"
// matches nothing).
func (s *SQLiteLibrary) Get(ctx context.Context, id string) (Item, error) {
	var item Item
	err := s.db.QueryRowContext(ctx, `SELECT `+mediaColumns+` FROM media WHERE id = ?`, id).
		Scan(&item.ID, &item.Filename, &item.URL, &item.MIMEType, &item.Size, &item.UploadedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrMediaNotFound
	}
	return item, err
}

// Insert ports the INSERT and the SELECT of the new row (dashboard.ts:595-603).
func (s *SQLiteLibrary) Insert(ctx context.Context, item NewItem) (Item, error) {
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO media (filename, url, mime_type, size, uploaded_at) VALUES (?, ?, ?, ?, ?)`,
		item.Filename, item.URL, item.MIMEType, item.Size, item.UploadedAt)
	if err != nil {
		return Item{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Item{}, err
	}
	return s.Get(ctx, fmt.Sprint(id))
}

// Delete ports "DELETE FROM media WHERE id = ?" with the raw path id.
func (s *SQLiteLibrary) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM media WHERE id = ?`, id)
	return err
}

type libraryStore interface {
	List(context.Context) ([]Item, error)
	Get(context.Context, string) (Item, error)
	Insert(context.Context, NewItem) (Item, error)
	Delete(context.Context, string) error
}

type uploadFiles interface {
	Remove(url string) error
	RemoveStored(StoredFile) error
}

// Library holds the dashboard media rules (dashboard.ts:575-628).
type Library struct {
	store libraryStore
	files uploadFiles
	now   func() time.Time
}

func NewLibrary(store libraryStore, files uploadFiles, now func() time.Time) *Library {
	return &Library{store: store, files: files, now: now}
}

func (l *Library) List(ctx context.Context) ([]Item, error) { return l.store.List(ctx) }

// Record inserts the row for a file Save stored. uploaded_at uses Node's
// formatSqliteTimestamp (UTC "YYYY-MM-DD HH:MM:SS"). If the row cannot be
// written the file is removed, so a failed upload leaves no orphan (Node
// leaves the file behind).
func (l *Library) Record(ctx context.Context, file StoredFile, originalName, mimeType string) (Item, error) {
	item, err := l.store.Insert(ctx, NewItem{
		Filename:   originalName,
		URL:        file.URL(),
		MIMEType:   mimeType,
		Size:       file.Size,
		UploadedAt: l.now().UTC().Format(time.DateTime),
	})
	if err != nil {
		if removeErr := l.files.RemoveStored(file); removeErr != nil {
			err = errors.Join(err, fmt.Errorf("remove orphaned upload: %w", removeErr))
		}
		return Item{}, err
	}
	return item, nil
}

// Delete ports dashboard.ts:609-628: 404 for an unknown id, then the file
// (a missing file is fine), then the row. If the file cannot be removed the
// row is kept.
func (l *Library) Delete(ctx context.Context, id string) error {
	item, err := l.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := l.files.Remove(item.URL); err != nil {
		return err
	}
	return l.store.Delete(ctx, id)
}
