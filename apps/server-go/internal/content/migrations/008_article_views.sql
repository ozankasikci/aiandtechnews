-- Reads per article per UTC day, so "Trending" can rank by recent reads
-- instead of all-time view_count. A separate table rather than a column on
-- articles because adoption (internal/database/adopt) can verify only
-- migrations that create new schema objects.
CREATE TABLE article_views (
    article_id INTEGER NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    day TEXT NOT NULL,
    count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (article_id, day)
);

CREATE INDEX idx_article_views_day ON article_views(day);
