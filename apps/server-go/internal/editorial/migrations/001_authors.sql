CREATE TABLE authors (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    avatar TEXT,
    bio TEXT,
    role TEXT NOT NULL DEFAULT 'editor' CHECK(role IN ('admin', 'editor'))
);
