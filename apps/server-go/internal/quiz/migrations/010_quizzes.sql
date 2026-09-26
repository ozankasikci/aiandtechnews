-- One daily news quiz per UTC day. questions is a JSON array of three
-- multiple-choice questions, each with the evidence sentence the answer key
-- was checked against. A pulled quiz keeps its row so the daily loop does not
-- generate that day again.
CREATE TABLE quizzes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    day TEXT NOT NULL UNIQUE,
    questions TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('published','pulled')),
    created_at TEXT NOT NULL
);
