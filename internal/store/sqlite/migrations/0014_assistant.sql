-- The conversational assistant's private state (internal/assistant, internal/appops).
--
-- It is kept small on purpose. A session is only the handle needed to continue the provider's own conversation
-- and to recover after a restart: there is no transcript here and no history to browse. An action is a change
-- the assistant proposed, held until the person confirms or declines it. The audit is an append-only, chained
-- record of every application operation the assistant (or, later, an MCP client) performed or was refused.
--
-- Nothing here is read by, or changes how, tasks, runs or questions behave.

CREATE TABLE assistant_sessions (
    id           TEXT    PRIMARY KEY,
    provider     TEXT    NOT NULL,
    model        TEXT    NOT NULL DEFAULT '',
    reasoning    TEXT    NOT NULL DEFAULT '',
    provider_ref TEXT    NOT NULL DEFAULT '',
    state        TEXT    NOT NULL CHECK (state IN ('idle', 'running', 'awaiting_confirmation')),
    turns        INTEGER NOT NULL DEFAULT 0 CHECK (turns >= 0),
    last_error   TEXT    NOT NULL DEFAULT '',
    reported_at  INTEGER NOT NULL DEFAULT 0,
    version      INTEGER NOT NULL DEFAULT 1,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
) STRICT;

CREATE TABLE assistant_actions (
    id          TEXT    PRIMARY KEY,
    session_id  TEXT    NOT NULL REFERENCES assistant_sessions(id) ON DELETE CASCADE,
    principal   TEXT    NOT NULL,
    operation   TEXT    NOT NULL,
    args        TEXT    NOT NULL CHECK (json_valid(args)),
    args_hash   TEXT    NOT NULL,
    summary     TEXT    NOT NULL,
    state       TEXT    NOT NULL CHECK (state IN ('pending', 'executing', 'executed', 'failed', 'rejected', 'expired', 'withdrawn')),
    outcome     TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL,
    resolved_at INTEGER
) STRICT;
CREATE INDEX assistant_actions_by_session ON assistant_actions (session_id, created_at);
CREATE INDEX assistant_actions_open ON assistant_actions (state) WHERE state IN ('pending', 'executing');

-- seq orders the trail and is never reused (AUTOINCREMENT). An entry is never changed or removed: the triggers
-- refuse both, so a bug or a stray statement cannot rewrite what the assistant did. hash chains each entry to the
-- one before it, so a line taken out of a copy of the database is detectable.
CREATE TABLE assistant_audit (
    seq        INTEGER PRIMARY KEY AUTOINCREMENT,
    at         INTEGER NOT NULL,
    session_id TEXT    NOT NULL DEFAULT '',
    action_id  TEXT    NOT NULL DEFAULT '',
    actor      TEXT    NOT NULL,
    operation  TEXT    NOT NULL,
    kind       TEXT    NOT NULL CHECK (kind IN ('read', 'mutation', 'system')),
    outcome    TEXT    NOT NULL,
    args_hash  TEXT    NOT NULL DEFAULT '',
    detail     TEXT    NOT NULL DEFAULT '',
    prev_hash  TEXT    NOT NULL,
    hash       TEXT    NOT NULL
) STRICT;
CREATE INDEX assistant_audit_by_session ON assistant_audit (session_id, seq);

CREATE TRIGGER assistant_audit_no_update BEFORE UPDATE ON assistant_audit
BEGIN
  SELECT RAISE(ABORT, 'the assistant audit is append-only');
END;

CREATE TRIGGER assistant_audit_no_delete BEFORE DELETE ON assistant_audit
BEGIN
  SELECT RAISE(ABORT, 'the assistant audit is append-only');
END;
