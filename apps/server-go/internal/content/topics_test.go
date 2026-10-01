package content_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

func topicFixture(t *testing.T) (*sql.DB, *content.SQLiteStore) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO authors (id, name, email, password_hash, role) VALUES (1, 'Ed', 'ed@example.invalid', 'x', 'editor')`)
	exec(`INSERT INTO categories (id, name, slug, description, color) VALUES (1, 'AI', 'ai', 'd', '#000')`)
	// Articles 1-4 published (day order 1 oldest), 5 draft.
	for i := 1; i <= 5; i++ {
		status := "published"
		if i == 5 {
			status = "draft"
		}
		exec(`INSERT INTO articles (id, title, slug, excerpt, content, category_id, author_id, status, published_at, source, source_url)
			VALUES (?, ?, ?, 'e', '<p>c</p>', 1, 1, ?, ?, 'Secret Source', 'https://secret.example/'||?)`,
			i, fmt.Sprintf("Article %d", i), fmt.Sprintf("article-%d", i), status, fmt.Sprintf("2026-09-0%d 10:00:00", i), i)
	}
	exec(`INSERT INTO topics (id, slug, name, kind, summary, facts_json) VALUES
		(1, 'nvidia', 'Nvidia', 'company', 'Nvidia makes chips.', '["Founded 1993"]'),
		(2, 'rubin', 'Rubin', 'product', '', '[]'),
		(3, 'thin', 'Thin', 'theme', '', '[]'),
		(4, 'drafty', 'Drafty', 'theme', '', '[]')`)
	// nvidia: 1,2,3,4 (4 published); rubin: 1,2,3 (3); thin: 1,2 (2); drafty: 3,4,5 (2 published + draft).
	for _, pair := range [][2]int{{1, 1}, {2, 1}, {3, 1}, {4, 1}, {1, 2}, {2, 2}, {3, 2}, {1, 3}, {2, 3}, {3, 4}, {4, 4}, {5, 4}} {
		exec(`INSERT INTO article_topics (article_id, topic_id) VALUES (?, ?)`, pair[0], pair[1])
	}
	return db, content.NewSQLiteStore(db)
}

func TestLiveTopicsOnly(t *testing.T) {
	_, store := topicFixture(t)
	topics, err := store.ListTopics(context.Background())
	if err != nil || len(topics) != 2 || topics[0].Slug != "nvidia" || topics[0].ArticleCount != 4 || topics[1].Slug != "rubin" {
		t.Fatalf("topics = %#v, %v", topics, err)
	}
	if _, _, err := store.TopicBySlug(context.Background(), "thin"); err != content.ErrTopicNotFound {
		t.Fatalf("thin = %v", err)
	}
	if _, _, err := store.TopicBySlug(context.Background(), "drafty"); err != content.ErrTopicNotFound {
		t.Fatalf("a draft must not make a topic live: %v", err)
	}
	topic, articles, err := store.TopicBySlug(context.Background(), "nvidia")
	if err != nil {
		t.Fatal(err)
	}
	if topic.Summary != "Nvidia makes chips." || len(topic.Facts) != 1 || len(topic.Related) != 1 || topic.Related[0].Slug != "rubin" {
		t.Fatalf("topic = %#v", topic)
	}
	if len(articles) != 4 || articles[0].Slug != "article-4" || len(articles[0].Topics) != 1 || articles[0].Topics[0].Slug != "nvidia" {
		t.Fatalf("articles = %#v", articles)
	}
	// article-2 belongs to nvidia, rubin and thin; thin is not live.
	if len(articles[2].Topics) != 2 {
		t.Fatalf("article-2 topics = %#v", articles[2].Topics)
	}
}

func TestArticleReadsCarryLiveTopics(t *testing.T) {
	_, store := topicFixture(t)
	article, err := store.PublishedBySlug(context.Background(), "article-1")
	if err != nil || len(article.Topics) != 2 || article.Topics[0].Slug != "nvidia" || article.Topics[1].Slug != "rubin" {
		t.Fatalf("by slug = %#v, %v", article.Topics, err)
	}
	list, err := store.List(context.Background(), content.ListQuery{Page: 1, Limit: 12})
	if err != nil || len(list.Articles) != 4 {
		t.Fatalf("list = %v", err)
	}
	for _, a := range list.Articles {
		if a.Slug == "article-4" && (len(a.Topics) != 1 || a.Topics[0].Slug != "nvidia") {
			t.Fatalf("article-4 topics = %#v", a.Topics)
		}
	}
}

func TestTopicHTTPNeverShowsSource(t *testing.T) {
	_, store := topicFixture(t)
	router := chi.NewRouter()
	content.NewTopicPublicHandler(store, slog.New(slog.NewTextHandler(io.Discard, nil))).MountPublic(router)
	get := func(path string) (int, string) {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		return response.Code, response.Body.String()
	}
	code, body := get("/topics/nvidia")
	if code != 200 || strings.Contains(body, "secret") || strings.Contains(body, "Secret") || strings.Contains(body, `"source`) {
		t.Fatalf("%d %s", code, body)
	}
	var parsed struct {
		Topic    map[string]any   `json:"topic"`
		Articles []map[string]any `json:"articles"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil || len(parsed.Articles) != 4 || parsed.Topic["articleCount"].(float64) != 4 {
		t.Fatalf("%v %s", err, body)
	}
	if code, _ := get("/topics/thin"); code != 404 {
		t.Fatalf("thin = %d", code)
	}
	code, body = get("/topics")
	if code != 200 || strings.Contains(body, "thin") || !strings.HasPrefix(body, `{"topics":[{"slug":"nvidia"`) {
		t.Fatalf("%d %s", code, body)
	}
}
