CREATE TABLE obligations (
    id            INTEGER PRIMARY KEY,
    title         TEXT    NOT NULL CHECK (title <> ''),
    category      TEXT    NOT NULL CHECK (category IN ('bill', 'subscription', 'renewal', 'warranty', 'document', 'deadline', 'other')),
    amount_minor  INTEGER CHECK (amount_minor IS NULL OR amount_minor >= 0),
    currency      TEXT    CHECK (currency IS NULL OR length(currency) = 3),
    due_on        TEXT    CHECK (due_on IS NULL OR date(due_on) = due_on),
    recurrence    TEXT    NOT NULL CHECK (recurrence IN ('none', 'weekly', 'monthly', 'quarterly', 'yearly')),
    status        TEXT    NOT NULL CHECK (status IN ('open', 'done', 'snoozed', 'cancelled')),
    snoozed_until TEXT    CHECK (snoozed_until IS NULL OR date(snoozed_until) = snoozed_until),
    notes         TEXT    NOT NULL DEFAULT '',
    source        TEXT    NOT NULL,
    source_ref    TEXT    NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);

CREATE INDEX obligations_status_due ON obligations (status, due_on);

CREATE TABLE memories (
    id         INTEGER PRIMARY KEY,
    kind       TEXT    NOT NULL CHECK (kind IN ('fact', 'preference', 'note')),
    content    TEXT    NOT NULL CHECK (content <> ''),
    source     TEXT    NOT NULL,
    source_ref TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE conversations (
    id         INTEGER PRIMARY KEY,
    title      TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE messages (
    id              INTEGER PRIMARY KEY,
    conversation_id INTEGER NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    role            TEXT    NOT NULL CHECK (role IN ('system', 'user', 'assistant', 'tool')),
    content         TEXT    NOT NULL DEFAULT '',
    tool_calls      TEXT,
    tool_call_id    TEXT    NOT NULL DEFAULT '',
    created_at      INTEGER NOT NULL
);

CREATE INDEX messages_conversation ON messages (conversation_id, id);
