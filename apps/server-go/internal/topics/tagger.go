package topics

import (
	"context"
	"log/slog"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/siterevalidate"
)

// TagTimeout bounds tagging one article, so publishing is never held up long.
const TagTimeout = 45 * time.Second

// Tagger extracts and stores an article's topics, best effort.
type Tagger struct {
	Store     *Store
	Extractor Extractor
	// Site, when set, is asked to refresh the pages the tagging changed.
	Site   siterevalidate.Notifier
	Logger *slog.Logger
}

// TagArticle never fails the caller: any error is logged and the article
// simply stays without topics until the backfill picks it up.
func (t *Tagger) TagArticle(ctx context.Context, articleID int64) {
	ctx, cancel := context.WithTimeout(ctx, TagTimeout)
	defer cancel()
	assignment, err := t.Tag(ctx, articleID)
	if err != nil {
		t.Logger.WarnContext(ctx, "topic tagging failed; the backfill will retry", "article", articleID, "error", err)
		return
	}
	t.Logger.InfoContext(ctx, "article tagged", "article", articleID, "topics", assignment.Topics)
}

// Tag extracts the topics of one published article, stores them and asks
// the site to refresh what changed.
func (t *Tagger) Tag(ctx context.Context, articleID int64) (Assignment, error) {
	article, err := t.Store.Article(ctx, articleID)
	if err != nil {
		return Assignment{}, err
	}
	entities, err := t.Extractor.Extract(ctx, article)
	if err != nil {
		return Assignment{}, err
	}
	if len(entities) == 0 {
		return Assignment{}, nil
	}
	assignment, err := t.Store.Assign(ctx, articleID, entities)
	if err != nil {
		return Assignment{}, err
	}
	if t.Site != nil && len(assignment.Topics) > 0 {
		siterevalidate.NotifyWithTopics(t.Site, assignment.Articles, assignment.Topics)
	}
	return assignment, nil
}
