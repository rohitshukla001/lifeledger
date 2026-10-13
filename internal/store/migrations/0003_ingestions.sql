CREATE TABLE ingestions (
    id           INTEGER PRIMARY KEY,
    source_kind  TEXT    NOT NULL CHECK (source_kind IN ('text', 'pdf')),
    source_name  TEXT    NOT NULL DEFAULT '',
    content_hash TEXT    NOT NULL UNIQUE,
    summary      TEXT    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL
);

CREATE TABLE proposals (
    id            INTEGER PRIMARY KEY,
    ingestion_id  INTEGER NOT NULL REFERENCES ingestions (id) ON DELETE CASCADE,
    title         TEXT    NOT NULL CHECK (title <> ''),
    category      TEXT    NOT NULL CHECK (category IN ('bill', 'subscription', 'renewal', 'warranty', 'document', 'deadline', 'other')),
    amount_minor  INTEGER CHECK (amount_minor IS NULL OR amount_minor >= 0),
    currency      TEXT    CHECK (currency IS NULL OR length(currency) = 3),
    due_on        TEXT    CHECK (due_on IS NULL OR date(due_on) = due_on),
    recurrence    TEXT    NOT NULL CHECK (recurrence IN ('none', 'weekly', 'monthly', 'quarterly', 'yearly')),
    notes         TEXT    NOT NULL DEFAULT '',
    evidence      TEXT    NOT NULL DEFAULT '',
    confidence    REAL    NOT NULL CHECK (confidence BETWEEN 0 AND 1),
    duplicate_of  INTEGER REFERENCES obligations (id) ON DELETE SET NULL,
    status        TEXT    NOT NULL CHECK (status IN ('pending', 'accepted', 'rejected')),
    obligation_id INTEGER REFERENCES obligations (id) ON DELETE SET NULL,
    created_at    INTEGER NOT NULL,
    decided_at    INTEGER
);

CREATE INDEX proposals_status ON proposals (status, ingestion_id);
