-- First-class questions.
--
-- A question now says which task and project it belongs to (so a client or a
-- notification can show it without a lookup), carries the context the user needs
-- to judge it, says whether a typed answer is accepted, and records the whole
-- lifecycle: when it was asked, answered, delivered to the agent and, if it
-- could not be answered, closed and why. Its kinds are the five the interface
-- words differently.
--
-- SQLite cannot change a CHECK constraint or a column's name in place, so the
-- table is rebuilt. Nothing references `questions`, so no foreign key fires.
-- Existing rows keep their id, run, text, options and outcome.

CREATE TABLE questions_new (
    id              TEXT    PRIMARY KEY,
    run_id          TEXT    NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    task_id         TEXT    NOT NULL,
    project_id      TEXT    NOT NULL,
    kind            TEXT    NOT NULL CHECK (kind IN ('clarification', 'decision', 'approval', 'selection', 'instruction')),
    prompt          TEXT    NOT NULL,
    context         TEXT    NOT NULL DEFAULT '',
    options         TEXT    NOT NULL DEFAULT '[]',
    allow_free_text INTEGER NOT NULL DEFAULT 1 CHECK (allow_free_text IN (0, 1)),
    state           TEXT    NOT NULL CHECK (state IN ('pending', 'answered', 'cancelled')),
    answer          TEXT    NOT NULL DEFAULT '',
    cancel_reason   TEXT    NOT NULL DEFAULT '' CHECK (cancel_reason IN ('', 'run_ended', 'interrupted', 'withdrawn')),
    asked_at        INTEGER NOT NULL,
    answered_at     INTEGER,
    delivered_at    INTEGER,
    closed_at       INTEGER,

    -- Each state has exactly the fields it can have.
    CHECK (state != 'pending'   OR (answer = '' AND cancel_reason = '' AND answered_at IS NULL AND delivered_at IS NULL AND closed_at IS NULL)),
    CHECK (state != 'answered'  OR (answered_at IS NOT NULL AND answer != '' AND cancel_reason = '' AND closed_at IS NULL)),
    CHECK (state != 'cancelled' OR (closed_at IS NOT NULL AND cancel_reason != '' AND delivered_at IS NULL)),
    CHECK (delivered_at IS NULL OR answered_at IS NOT NULL)
) STRICT;

INSERT INTO questions_new
    (id, run_id, task_id, project_id, kind, prompt, context, options, allow_free_text,
     state, answer, cancel_reason, asked_at, answered_at, delivered_at, closed_at)
SELECT q.id, q.run_id, r.task_id, r.project_id,
       CASE q.kind WHEN 'approval' THEN 'approval' ELSE 'clarification' END,
       q.prompt, '', q.options,
       CASE q.kind WHEN 'approval' THEN 0 ELSE 1 END,
       q.status,
       CASE WHEN q.status = 'answered' THEN q.answer ELSE '' END,
       CASE WHEN q.status = 'cancelled' THEN 'run_ended' ELSE '' END,
       q.created_at,
       CASE WHEN q.status = 'answered' THEN COALESCE(q.answered_at, q.created_at) END,
       CASE WHEN q.status = 'answered' THEN COALESCE(q.answered_at, q.created_at) END,
       CASE WHEN q.status = 'cancelled' THEN COALESCE(q.answered_at, q.created_at) END
FROM questions q JOIN runs r ON r.id = q.run_id;

DROP TABLE questions;
ALTER TABLE questions_new RENAME TO questions;

CREATE INDEX questions_by_run ON questions (run_id, asked_at);
CREATE INDEX questions_pending ON questions (asked_at) WHERE state = 'pending';
