package content

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// A topic (company, product, person or theme) is public, "live", once at
// least MinLiveTopicArticles published articles belong to it, so there are no
// thin hub pages. Everything below reads only published articles.
const (
	MinLiveTopicArticles = 3
	// MaxArticleTopics caps the topic chips on one article.
	MaxArticleTopics = 4
	// MaxTopicArticles caps the articles on one topic page.
	MaxTopicArticles = 100
	// MaxRelatedTopics caps a topic's related list.
	MaxRelatedTopics = 6
)

// ErrTopicNotFound is a topic that does not exist or is not live.
var ErrTopicNotFound = errors.New("topic not found")

// TopicRef is the short form of a topic: chips on an article and the
// related list of a topic page.
type TopicRef struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// TopicSummary is one row of the topic index.
type TopicSummary struct {
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	ArticleCount int64  `json:"articleCount"`
	UpdatedAt    string `json:"updatedAt"`
}

// Topic is a topic page's header.
type Topic struct {
	Slug         string     `json:"slug"`
	Name         string     `json:"name"`
	Kind         string     `json:"kind"`
	Summary      string     `json:"summary"`
	Facts        []string   `json:"facts"`
	ArticleCount int64      `json:"articleCount"`
	UpdatedAt    string     `json:"updatedAt"`
	Related      []TopicRef `json:"related"`
}

// liveTopics selects the live topics with their published article count.
const liveTopics = `SELECT x.topic_id AS id, COUNT(*) AS n FROM article_topics x
	JOIN articles xa ON xa.id = x.article_id AND xa.status = 'published'
	GROUP BY x.topic_id HAVING COUNT(*) >= 3`

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// loadTopics sets Topics on each article: its live topics in the order the
// extraction listed them, at most MaxArticleTopics.
func loadTopics(ctx context.Context, q queryer, articles []Article) error {
	if len(articles) == 0 {
		return nil
	}
	index := make(map[int64][]int, len(articles))
	args := make([]any, 0, len(articles))
	marks := make([]string, 0, len(articles))
	for i, article := range articles {
		if _, seen := index[article.ID]; !seen {
			args = append(args, article.ID)
			marks = append(marks, "?")
		}
		index[article.ID] = append(index[article.ID], i)
	}
	rows, err := q.QueryContext(ctx, `SELECT at.article_id, t.slug, t.name FROM article_topics at
		JOIN topics t ON t.id = at.topic_id JOIN (`+liveTopics+`) l ON l.id = t.id
		WHERE at.article_id IN (`+strings.Join(marks, ",")+`) ORDER BY at.article_id, at.rowid`, args...)
	if err != nil {
		return fmt.Errorf("read article topics: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var articleID int64
		var ref TopicRef
		if err := rows.Scan(&articleID, &ref.Slug, &ref.Name); err != nil {
			return fmt.Errorf("scan article topic: %w", err)
		}
		for _, i := range index[articleID] {
			if len(articles[i].Topics) < MaxArticleTopics {
				articles[i].Topics = append(articles[i].Topics, ref)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate article topics: %w", err)
	}
	return nil
}

// ListTopics returns the live topics, the most covered first.
func (s *SQLiteStore) ListTopics(ctx context.Context) ([]TopicSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.slug, t.name, t.kind, l.n, t.updated_at
		FROM topics t JOIN (`+liveTopics+`) l ON l.id = t.id ORDER BY l.n DESC, t.slug`)
	if err != nil {
		return nil, fmt.Errorf("list topics: %w", err)
	}
	defer rows.Close()
	topics := make([]TopicSummary, 0)
	for rows.Next() {
		var topic TopicSummary
		if err := rows.Scan(&topic.Slug, &topic.Name, &topic.Kind, &topic.ArticleCount, &topic.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan topic: %w", err)
		}
		topics = append(topics, topic)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate topics: %w", err)
	}
	return topics, nil
}

// TopicBySlug returns a live topic and its newest published articles.
func (s *SQLiteStore) TopicBySlug(ctx context.Context, slug string) (Topic, []Article, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Topic{}, nil, fmt.Errorf("begin topic read transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var topic Topic
	var topicID int64
	var facts string
	err = tx.QueryRowContext(ctx, `SELECT t.id, t.slug, t.name, t.kind, t.summary, t.facts_json, l.n, t.updated_at
		FROM topics t JOIN (`+liveTopics+`) l ON l.id = t.id WHERE t.slug = ?`, slug).
		Scan(&topicID, &topic.Slug, &topic.Name, &topic.Kind, &topic.Summary, &facts, &topic.ArticleCount, &topic.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Topic{}, nil, ErrTopicNotFound
	}
	if err != nil {
		return Topic{}, nil, fmt.Errorf("read topic: %w", err)
	}
	if err := json.Unmarshal([]byte(facts), &topic.Facts); err != nil || topic.Facts == nil {
		topic.Facts = []string{}
	}

	topic.Related = []TopicRef{}
	related, err := tx.QueryContext(ctx, `SELECT t2.slug, t2.name FROM article_topics a1
		JOIN articles a ON a.id = a1.article_id AND a.status = 'published'
		JOIN article_topics a2 ON a2.article_id = a1.article_id AND a2.topic_id <> a1.topic_id
		JOIN topics t2 ON t2.id = a2.topic_id JOIN (`+liveTopics+`) l ON l.id = t2.id
		WHERE a1.topic_id = ? GROUP BY t2.id ORDER BY COUNT(*) DESC, l.n DESC, t2.slug LIMIT ?`, topicID, MaxRelatedTopics)
	if err != nil {
		return Topic{}, nil, fmt.Errorf("read related topics: %w", err)
	}
	for related.Next() {
		var ref TopicRef
		if err := related.Scan(&ref.Slug, &ref.Name); err != nil {
			related.Close()
			return Topic{}, nil, fmt.Errorf("scan related topic: %w", err)
		}
		topic.Related = append(topic.Related, ref)
	}
	related.Close()
	if err := related.Err(); err != nil {
		return Topic{}, nil, fmt.Errorf("iterate related topics: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `SELECT `+articleColumns+articleJoins+`
		JOIN article_topics at ON at.article_id = a.id
		WHERE at.topic_id = ? AND a.status = 'published'
		ORDER BY a.published_at DESC, a.id DESC LIMIT ?`, topicID, MaxTopicArticles)
	if err != nil {
		return Topic{}, nil, fmt.Errorf("list topic articles: %w", err)
	}
	articles := make([]Article, 0)
	for rows.Next() {
		article, err := scanArticle(rows)
		if err != nil {
			rows.Close()
			return Topic{}, nil, fmt.Errorf("scan topic article: %w", err)
		}
		articles = append(articles, article)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Topic{}, nil, fmt.Errorf("iterate topic articles: %w", err)
	}
	if err := loadTopics(ctx, tx, articles); err != nil {
		return Topic{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return Topic{}, nil, fmt.Errorf("commit topic read transaction: %w", err)
	}
	return topic, articles, nil
}
