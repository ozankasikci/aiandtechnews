-- Retries for the featured-image replacement job (internal/featuredreimage),
-- which gives articles whose featured image is a news source's photo an
-- image of our own. One row per article that failed at least once; nothing
-- is recorded for a success, since the article then stops matching. A new
-- table only, because adoption (internal/database/adopt) can verify only
-- migrations that create new schema objects.
CREATE TABLE featured_reimage (
    article_id INTEGER PRIMARY KEY REFERENCES articles(id) ON DELETE CASCADE,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    updated_at TEXT NOT NULL
);
