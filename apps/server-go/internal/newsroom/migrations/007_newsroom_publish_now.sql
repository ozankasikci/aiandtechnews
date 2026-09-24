-- Candidates an editor asked to publish now. The publisher claims a flagged,
-- due candidate ahead of the rest of the queue and without the minimum gap
-- since the last publish. The flag is a separate table rather than a
-- candidates column because adoption (internal/database/adopt) can verify
-- only migrations that create new schema objects.
CREATE TABLE candidate_publish_now (
    candidate_id INTEGER PRIMARY KEY REFERENCES candidates(id) ON DELETE CASCADE,
    requested_at TEXT NOT NULL
);

-- The flag lives only while the candidate is queued or processing: it is
-- cleared when the candidate is published, failed, returned to pending, or
-- rejected. A transient-failure requeue (processing -> queued) keeps it.
CREATE TRIGGER candidates_clear_publish_now
AFTER UPDATE OF status ON candidates
WHEN NEW.status NOT IN ('queued', 'processing')
BEGIN
    DELETE FROM candidate_publish_now WHERE candidate_id = NEW.id;
END;
