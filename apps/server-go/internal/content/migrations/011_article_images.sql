-- Illustrations placed inside an article's body, generated after the article
-- is published. after_paragraph is how many <p> blocks of the body come
-- before the image. A failed row keeps its attempts and last error so the
-- worker gives up after a few tries. One row per article for now. A separate
-- table rather than columns on articles because adoption
-- (internal/database/adopt) can verify only migrations that create new
-- schema objects.
CREATE TABLE article_images (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    article_id INTEGER NOT NULL UNIQUE REFERENCES articles(id) ON DELETE CASCADE,
    url TEXT NOT NULL DEFAULT '',
    alt TEXT NOT NULL DEFAULT '',
    after_paragraph INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL CHECK(status IN ('ready', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
