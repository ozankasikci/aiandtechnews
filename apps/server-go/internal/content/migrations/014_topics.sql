-- Topic hubs: a company, product, person or theme that several articles are
-- about. New tables only (adoption can verify only migrations that create
-- new schema objects). A topic is "live" (public) once three published
-- articles belong to it.
CREATE TABLE topics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('company', 'product', 'person', 'theme')),
    summary TEXT NOT NULL DEFAULT '',
    facts_json TEXT NOT NULL DEFAULT '[]',
    summary_updated_at TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- alias_norm is the normalised form of a name (see internal/topics.Normalize).
CREATE TABLE topic_aliases (
    alias_norm TEXT NOT NULL UNIQUE,
    topic_id INTEGER NOT NULL REFERENCES topics(id) ON DELETE CASCADE
);

CREATE TABLE article_topics (
    article_id INTEGER NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    topic_id INTEGER NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
    PRIMARY KEY (article_id, topic_id)
);
CREATE INDEX article_topics_topic ON article_topics(topic_id);
