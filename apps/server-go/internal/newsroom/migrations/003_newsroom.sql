CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT OR IGNORE INTO settings (key, value) VALUES
    ('newsroom.publish_delay_min_minutes', '30'),
    ('newsroom.publish_delay_max_minutes', '40');

CREATE TABLE candidates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_url TEXT NOT NULL UNIQUE,
    source_name TEXT NOT NULL,
    feed_url TEXT NOT NULL,
    title TEXT NOT NULL,
    feed_summary TEXT NOT NULL DEFAULT '',
    source_image_url TEXT,
    feed_published_at TEXT,
    discovered_at TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK(status IN ('pending', 'queued', 'processing', 'published', 'failed', 'rejected')),
    scheduled_for TEXT,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    article_id INTEGER,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (article_id) REFERENCES articles(id)
);

CREATE INDEX idx_candidates_status_scheduled ON candidates(status, scheduled_for);
CREATE INDEX idx_candidates_status_discovered ON candidates(status, discovered_at);
