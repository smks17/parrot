CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,
    snapshot   BLOB NOT NULL,
    updated_at INTEGER NOT NULL
);
