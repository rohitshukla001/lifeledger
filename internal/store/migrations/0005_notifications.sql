CREATE TABLE notifications (
    id            INTEGER PRIMARY KEY,
    kind          TEXT    NOT NULL CHECK (kind IN ('reminder', 'review')),
    dedupe_key    TEXT    NOT NULL UNIQUE CHECK (dedupe_key <> ''),
    obligation_id INTEGER REFERENCES obligations (id) ON DELETE CASCADE,
    title         TEXT    NOT NULL CHECK (title <> ''),
    body          TEXT    NOT NULL,
    attempts      INTEGER NOT NULL DEFAULT 0,
    last_error    TEXT    NOT NULL DEFAULT '',
    delivered_at  INTEGER,
    created_at    INTEGER NOT NULL
);

CREATE INDEX notifications_pending ON notifications (delivered_at, created_at);
