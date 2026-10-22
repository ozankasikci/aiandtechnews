package analysis

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// Reader exposes only SELECT queries, also used by analysis-try.
type Reader struct{ db *sql.DB }

func NewReader(db *sql.DB) *Reader { return &Reader{db: db} }

const articleColumns = `a.id, a.slug, a.title, a.content, COALESCE(a.published_at,''), COALESCE(a.source,''), COALESCE(s.tldr,'[]'), COALESCE(s.why_it_matters,'')`
const articleFrom = ` FROM articles a LEFT JOIN article_summaries s ON s.article_id = a.id `

type scanner interface{ Scan(...any) error }

func scanArticle(row scanner) (Article, error) {
	var a Article
	var points string
	err := row.Scan(&a.ID, &a.Slug, &a.Title, &a.Content, &a.PublishedAt, &a.Source, &points, &a.WhyItMatters)
	if err == nil {
		err = json.Unmarshal([]byte(points), &a.TLDR)
	}
	return a, err
}

func (r *Reader) BySlug(ctx context.Context, slug string) (Article, error) {
	return scanArticle(r.db.QueryRowContext(ctx, `SELECT `+articleColumns+articleFrom+` WHERE a.status = 'published' AND a.slug = ?`, slug))
}

func (r *Reader) Latest(ctx context.Context, limit int) ([]Article, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+articleColumns+articleFrom+` WHERE a.status = 'published' ORDER BY datetime(a.published_at) DESC, a.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var articles []Article
	for rows.Next() {
		a, err := scanArticle(rows)
		if err != nil {
			return nil, err
		}
		articles = append(articles, a)
	}
	return articles, rows.Err()
}

// Related returns at most five earlier published articles sharing a topic.
// EXISTS prevents duplicate results when two articles share several topics.
// Outlet-named items are filtered before the five-article cap.
func (r *Reader) Related(ctx context.Context, articleID int64) ([]Related, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT a.slug, a.title, a.excerpt, a.published_at, COALESCE(a.source,'')
 FROM articles a JOIN articles current ON current.id = ?
 WHERE a.status = 'published' AND a.id <> current.id
 AND (datetime(a.published_at) < datetime(current.published_at)
      OR (datetime(a.published_at) = datetime(current.published_at) AND a.id < current.id))
 AND EXISTS (SELECT 1 FROM article_topics x JOIN article_topics y ON y.topic_id = x.topic_id
 WHERE x.article_id = current.id AND y.article_id = a.id)
 ORDER BY datetime(a.published_at) DESC, a.id DESC`, articleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	related := make([]Related, 0)
	for len(related) < 5 && rows.Next() {
		var a Related
		if err := rows.Scan(&a.Slug, &a.Title, &a.Excerpt, &a.PublishedAt, &a.Source); err != nil {
			return nil, err
		}
		if namesOutlet(a) {
			continue
		}
		related = append(related, a)
	}
	return related, rows.Err()
}

// SQLiteStore adds bounded retry bookkeeping to Reader.
type SQLiteStore struct {
	*Reader
	now func() time.Time
}

func NewSQLiteStore(db *sql.DB, now func() time.Time) *SQLiteStore {
	if now == nil {
		now = time.Now
	}
	return &SQLiteStore{Reader: NewReader(db), now: now}
}

// Pending selects one newest eligible article, including failed attempts below the cap.
func (s *SQLiteStore) Pending(ctx context.Context, since time.Time) (Article, error) {
	var id int64
	var attempts int
	err := s.db.QueryRowContext(ctx, `SELECT a.id, COALESCE(n.attempts,0) FROM articles a
 LEFT JOIN article_analysis n ON n.article_id = a.id
 WHERE a.status = 'published' AND datetime(a.published_at) >= datetime(?) AND datetime(a.published_at) <= datetime(?)
 AND (n.article_id IS NULL OR (n.status = 'failed' AND n.attempts < ?))
 ORDER BY datetime(a.published_at) DESC, a.id DESC LIMIT 1`, since.UTC().Format(time.RFC3339), s.now().UTC().Format(time.RFC3339), MaxAttempts).Scan(&id, &attempts)
	if err != nil {
		return Article{}, err
	}
	a, err := scanArticle(s.db.QueryRowContext(ctx, `SELECT `+articleColumns+articleFrom+` WHERE a.id = ?`, id))
	a.Attempts = attempts
	return a, err
}

func (s *SQLiteStore) Save(ctx context.Context, articleID int64, result Result, attempts int) error {
	links := result.Draft.Links
	if links == nil {
		links = []string{}
	}
	data, err := json.Marshal(links)
	if err != nil {
		return err
	}
	stamp := s.now().UTC().Format("2006-01-02 15:04:05")
	_, err = s.db.ExecContext(ctx, `INSERT INTO article_analysis(article_id,status,kind,text,links_json,reason,attempts,created_at,updated_at)
 VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT(article_id) DO UPDATE SET status=excluded.status,kind=excluded.kind,text=excluded.text,
 links_json=excluded.links_json,reason=excluded.reason,attempts=excluded.attempts,updated_at=excluded.updated_at`,
		articleID, result.Status, result.Draft.Kind, result.Draft.Text, string(data), result.Reason, attempts, stamp, stamp)
	return err
}
