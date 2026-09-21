package editorial

import (
	"context"
	"database/sql"
	"fmt"
)

type SQLiteStore struct{ db *sql.DB }

func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{db: db} }

func (s *SQLiteStore) AuthorByEmail(ctx context.Context, email string) (LoginAuthor, error) {
	var author LoginAuthor
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, email, role, password_hash FROM authors WHERE email = ?`, email,
	).Scan(&author.ID, &author.Name, &author.Email, &author.Role, &author.PasswordHash)
	if err != nil {
		return LoginAuthor{}, fmt.Errorf("author by email: %w", err)
	}
	return author, nil
}

// ListAuthors intentionally exposes only the six public compatibility columns.
func (s *SQLiteStore) ListAuthors(ctx context.Context) ([]Author, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, email, avatar, bio, role FROM authors ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list authors: %w", err)
	}
	defer rows.Close()
	authors := make([]Author, 0)
	for rows.Next() {
		var author Author
		var avatar, bio sql.NullString
		if err := rows.Scan(&author.ID, &author.Name, &author.Email, &avatar, &bio, &author.Role); err != nil {
			return nil, fmt.Errorf("scan author: %w", err)
		}
		author.Avatar = nullableString(avatar)
		author.Bio = nullableString(bio)
		authors = append(authors, author)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate authors: %w", err)
	}
	return authors, nil
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}
