-- The credit of a featured image that is a real photo: a Wikimedia Commons
-- photo (kind 'commons', with its licence) or the maker's own official image
-- (kind 'official'). A generated illustration has no row. credit is the
-- caption shown on the article page; credit_url is the Commons file page or
-- the maker's page, never the news source. photo_url is the file the photo
-- was copied from, kept so the same photo is not used again soon. image_url
-- is the stored featured image URL this credit describes, so replacing the
-- featured image hides the old credit. A separate table because adoption
-- (internal/database/adopt) can verify only
-- migrations that create new schema objects.
CREATE TABLE article_featured_image_credits (
    article_id INTEGER PRIMARY KEY REFERENCES articles(id) ON DELETE CASCADE,
    image_url TEXT NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('commons', 'official')),
    credit TEXT NOT NULL,
    credit_url TEXT NOT NULL DEFAULT '',
    license TEXT NOT NULL DEFAULT '',
    license_url TEXT NOT NULL DEFAULT '',
    photo_url TEXT NOT NULL DEFAULT '',
    alt TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
