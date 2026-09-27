-- Reads per article per UTC hour ("YYYY-MM-DD HH"), so Trending's "Today"
-- ranks the last 24 hours rather than two calendar days. article_views keeps
-- the daily counts the week window uses. A new table because adoption can
-- verify only migrations that create new schema objects.
CREATE TABLE article_views_hourly (
    article_id INTEGER NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    hour TEXT NOT NULL,
    count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (article_id, hour)
);

CREATE INDEX idx_article_views_hourly_hour ON article_views_hourly(hour);
