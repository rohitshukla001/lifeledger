CREATE TABLE actions (
    id            INTEGER PRIMARY KEY,
    kind          TEXT    NOT NULL CHECK (kind IN ('mark_paid', 'snooze', 'calendar_event', 'send_email')),
    obligation_id INTEGER REFERENCES obligations (id) ON DELETE SET NULL,
    payload       TEXT    NOT NULL CHECK (json_valid(payload)),
    summary       TEXT    NOT NULL,
    status        TEXT    NOT NULL CHECK (status IN ('pending', 'approved', 'rejected', 'done', 'failed')),
    requested_by  TEXT    NOT NULL,
    source_ref    TEXT    NOT NULL DEFAULT '',
    result        TEXT    NOT NULL DEFAULT '',
    error         TEXT    NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL,
    decided_at    INTEGER,
    executed_at   INTEGER
);

CREATE INDEX actions_status ON actions (status, id);

CREATE TABLE audit_log (
    id         INTEGER PRIMARY KEY,
    actor      TEXT    NOT NULL,
    event      TEXT    NOT NULL,
    subject    TEXT    NOT NULL,
    detail     TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);
