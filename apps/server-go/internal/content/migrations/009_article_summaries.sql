-- The TL;DR (a JSON array of three sentences) and "Why it matters" line the
-- rewrite writes for each article. Optional: older articles have no row. A
-- separate table rather than columns on articles because adoption
-- (internal/database/adopt) can verify only migrations that create new
-- schema objects, and the article body itself may hold only <p> and <h2>.
CREATE TABLE article_summaries (
    article_id INTEGER PRIMARY KEY REFERENCES articles(id) ON DELETE CASCADE,
    tldr TEXT NOT NULL DEFAULT '[]',
    why_it_matters TEXT NOT NULL DEFAULT ''
);
