-- Credits for inline images that are real photos rather than our own
-- illustrations: a Wikimedia Commons photo (kind 'commons', with its licence)
-- or the maker's own official image (kind 'official'). Generated
-- illustrations have no row. credit is the caption text shown on the site;
-- credit_url is the Commons file page or the maker's official page, never the
-- news source. One row per article_images row. A separate table because
-- adoption (internal/database/adopt) can verify only migrations that create
-- new schema objects.
CREATE TABLE article_image_credits (
    article_image_id INTEGER PRIMARY KEY REFERENCES article_images(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK(kind IN ('commons', 'official')),
    credit TEXT NOT NULL,
    credit_url TEXT NOT NULL DEFAULT '',
    license TEXT NOT NULL DEFAULT '',
    license_url TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
