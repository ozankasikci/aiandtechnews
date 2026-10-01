package topics

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
)

const sqliteDateTime = "2006-01-02 15:04:05"

// Store reads and writes the topic tables.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(db *sql.DB, now func() time.Time) *Store { return &Store{db: db, now: now} }

// Assignment is what tagging an article changed, for revalidating the site.
type Assignment struct {
	// Topics are the slugs of the live topics the article now belongs to
	// (the only ones with a public page).
	Topics []string
	// Articles are slugs of articles whose chips changed: the article itself
	// and, for a topic that just reached three articles, all its articles.
	Articles []string
}

// Article reads what extraction needs about a published article.
func (s *Store) Article(ctx context.Context, id int64) (ArticleInput, error) {
	return s.article(ctx, `a.id = ?`, id)
}

func (s *Store) article(ctx context.Context, where string, arg any) (ArticleInput, error) {
	var a ArticleInput
	var body, points, why sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT a.id, a.slug, a.title, a.excerpt, a.content, s.tldr, s.why_it_matters
		FROM articles a LEFT JOIN article_summaries s ON s.article_id = a.id
		WHERE a.status = 'published' AND `+where, arg).Scan(&a.ID, &a.Slug, &a.Title, &a.Excerpt, &body, &points, &why)
	if err != nil {
		return ArticleInput{}, fmt.Errorf("read article %v: %w", arg, err)
	}
	a.Body = content.StripHTML(body.String)
	a.Body = strings.Join(strings.Fields(a.Body), " ")
	a.WhyItMatters = why.String
	if points.Valid {
		_ = json.Unmarshal([]byte(points.String), &a.TLDR)
	}
	return a, nil
}

// Untagged lists published articles without topics, newest first. slug and
// limit narrow it (limit 0: all).
func (s *Store) Untagged(ctx context.Context, slug string, limit int) ([]ArticleInput, error) {
	query := `SELECT a.id FROM articles a WHERE a.status = 'published'
		AND NOT EXISTS (SELECT 1 FROM article_topics t WHERE t.article_id = a.id)`
	var args []any
	if slug != "" {
		query += ` AND a.slug = ?`
		args = append(args, slug)
	}
	query += ` ORDER BY a.published_at DESC, a.id DESC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list untagged articles: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	articles := make([]ArticleInput, 0, len(ids))
	for _, id := range ids {
		a, err := s.Article(ctx, id)
		if err != nil {
			return nil, err
		}
		articles = append(articles, a)
	}
	return articles, nil
}

// Assign links the article to the topic of each entity, creating topics that
// no alias matches. It runs in one transaction. A topic that reaches
// content.MinLiveTopicArticles with this article is "newly live": its
// articles are all listed in the result because their pages gain chips.
func (s *Store) Assign(ctx context.Context, articleID int64, entities []Entity) (Assignment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Assignment{}, fmt.Errorf("begin topic assignment: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stamp := s.now().UTC().Format(sqliteDateTime)
	var result Assignment
	var topicIDs []int64
	var topicSlugs []string
	for _, entity := range entities {
		id, slug, err := resolve(ctx, tx, entity, stamp)
		if err != nil {
			return Assignment{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO article_topics (article_id, topic_id) VALUES (?, ?)`, articleID, id); err != nil {
			return Assignment{}, fmt.Errorf("link article to topic %s: %w", slug, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE topics SET updated_at = ? WHERE id = ?`, stamp, id); err != nil {
			return Assignment{}, fmt.Errorf("touch topic %s: %w", slug, err)
		}
		topicIDs = append(topicIDs, id)
		topicSlugs = append(topicSlugs, slug)
	}
	var own string
	if err := tx.QueryRowContext(ctx, `SELECT slug FROM articles WHERE id = ?`, articleID).Scan(&own); err != nil {
		return Assignment{}, fmt.Errorf("read article slug: %w", err)
	}
	result.Articles = append(result.Articles, own)
	for i, id := range topicIDs {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM article_topics x JOIN articles a ON a.id = x.article_id
			AND a.status = 'published' WHERE x.topic_id = ?`, id).Scan(&count); err != nil {
			return Assignment{}, fmt.Errorf("count topic articles: %w", err)
		}
		if count < content.MinLiveTopicArticles {
			continue
		}
		result.Topics = append(result.Topics, topicSlugs[i])
		if count != content.MinLiveTopicArticles {
			continue
		}
		rows, err := tx.QueryContext(ctx, `SELECT a.slug FROM article_topics x JOIN articles a ON a.id = x.article_id
			AND a.status = 'published' WHERE x.topic_id = ?`, id)
		if err != nil {
			return Assignment{}, fmt.Errorf("list newly live topic articles: %w", err)
		}
		for rows.Next() {
			var slug string
			if err := rows.Scan(&slug); err != nil {
				rows.Close()
				return Assignment{}, err
			}
			result.Articles = append(result.Articles, slug)
		}
		rows.Close()
	}
	if err := tx.Commit(); err != nil {
		return Assignment{}, fmt.Errorf("commit topic assignment: %w", err)
	}
	return result, nil
}

// resolve finds the topic an entity belongs to or creates it, and records
// the entity's names as aliases. The entity's own name decides first; its
// other aliases are tried only when the name is new, so an alias such as the
// maker's name cannot pull a product into the maker's topic when the product
// already exists.
func resolve(ctx context.Context, tx *sql.Tx, entity Entity, stamp string) (int64, string, error) {
	nameKey := Key(entity.Name)
	keys := []string{nameKey}
	for _, alias := range entity.Aliases {
		if key := Key(alias); len(key) >= 3 && key != nameKey {
			keys = append(keys, key)
		}
	}
	var id int64
	var slug string
	found := false
	for _, key := range keys {
		err := tx.QueryRowContext(ctx, `SELECT t.id, t.slug FROM topic_aliases a JOIN topics t ON t.id = a.topic_id WHERE a.alias_norm = ?`, key).Scan(&id, &slug)
		if err == nil {
			found = true
			break
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, "", fmt.Errorf("find topic alias: %w", err)
		}
	}
	if !found {
		slug = content.Slugify(entity.Name)
		var taken int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM topics WHERE slug = ?`, slug).Scan(&taken)
		if err == nil {
			slug = content.Slugify(entity.Name + " " + entity.Kind)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return 0, "", fmt.Errorf("check topic slug: %w", err)
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO topics (slug, name, kind, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			slug, entity.Name, entity.Kind, stamp, stamp)
		if err != nil {
			return 0, "", fmt.Errorf("create topic %s: %w", slug, err)
		}
		if id, err = res.LastInsertId(); err != nil {
			return 0, "", err
		}
	}
	for _, key := range keys {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO topic_aliases (alias_norm, topic_id) VALUES (?, ?)`, key, id); err != nil {
			return 0, "", fmt.Errorf("record topic alias: %w", err)
		}
	}
	return id, slug, nil
}

// TopicCount is a topic with its published article count.
type TopicCount struct {
	Slug, Name, Kind string
	Articles         int
}

// Counts lists every topic (live or not) with its published article count,
// the most covered first.
func (s *Store) Counts(ctx context.Context) ([]TopicCount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.slug, t.name, t.kind, COUNT(a.id) FROM topics t
		LEFT JOIN article_topics x ON x.topic_id = t.id
		LEFT JOIN articles a ON a.id = x.article_id AND a.status = 'published'
		GROUP BY t.id ORDER BY COUNT(a.id) DESC, t.slug`)
	if err != nil {
		return nil, fmt.Errorf("count topics: %w", err)
	}
	defer rows.Close()
	var out []TopicCount
	for rows.Next() {
		var c TopicCount
		if err := rows.Scan(&c.Slug, &c.Name, &c.Kind, &c.Articles); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
