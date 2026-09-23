CREATE TABLE IF NOT EXISTS subscribers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending',
    source_placement TEXT,
    confirmation_sent_at TEXT,
    confirmed_at TEXT,
    unsubscribed_at TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS newsletter_deliveries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    subscriber_id INTEGER NOT NULL,
    edition_key TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'sending',
    provider_message_id TEXT,
    error TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    sent_at TEXT,
    UNIQUE(subscriber_id, edition_key),
    FOREIGN KEY (subscriber_id) REFERENCES subscribers(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS newsletter_editions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    edition_key TEXT NOT NULL UNIQUE,
    subject TEXT NOT NULL,
    articles TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_subscribers_status ON subscribers(status);
CREATE INDEX IF NOT EXISTS idx_newsletter_deliveries_edition ON newsletter_deliveries(edition_key, status);
