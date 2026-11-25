-- Only a featured photo of a stand-in has a row. Its credit records when
-- the photo was used. Keep this in a separate table for schema adoption.
CREATE TABLE article_featured_stand_ins (
    article_id INTEGER PRIMARY KEY REFERENCES article_featured_image_credits(article_id) ON DELETE CASCADE,
    subject TEXT NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('person', 'place', 'product_in_use')),
    maker TEXT NOT NULL
);
