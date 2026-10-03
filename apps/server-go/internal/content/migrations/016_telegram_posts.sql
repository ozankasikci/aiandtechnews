-- Telegram channel posts (internal/telegram): one row per article the
-- channel worker tried. A 'sent' row is never posted again; a 'failed' row is
-- retried until attempts reaches the worker's cap. A new table only, because
-- adoption (internal/database/adopt) can verify only migrations that create
-- new schema objects.
CREATE TABLE telegram_posts (
    article_id INTEGER PRIMARY KEY REFERENCES articles(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('sent', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    message_id INTEGER,
    last_error TEXT,
    posted_at TEXT,
    updated_at TEXT NOT NULL
);
