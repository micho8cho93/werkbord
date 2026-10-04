-- Execution configuration, settings and the runner.
--
-- * Execution configuration (agent, model, reasoning, interaction, priority) is
--   set at three levels: global defaults, a project, and a task. Each level
--   stores only what it overrides, as JSON in one column, so that a new setting
--   is a new field rather than a migration. An empty object means "inherit
--   everything". See domain.ExecutionConfig.
--
--   `tasks.policy` held the interaction policy and could not say "not set", only
--   "interactive": a task that had never been given a policy and one explicitly
--   set to interactive looked the same. Only a non-default value can be known
--   to have been chosen, so only those are carried over as overrides, and the
--   rest inherit, which is what they did in effect: the global default is
--   "interactive" until someone changes it.
--
--   The column is dropped rather than left to rot. SQLite allows this because the
--   only CHECK that mentions it is the column's own.
--
-- * A run records the model and reasoning level it was started with ('' means the
--   agent's own default), as it already records its policy.
--
-- * `settings` holds the global defaults and the onboarding record: small JSON
--   documents keyed by name, for state the user changes in the app and that does
--   not belong in config.json (which is for the machine, edited by hand).
--
-- * `runners` records the computers that can run agents. Today there is exactly
--   one, this one, registered when the controller first starts; the table exists
--   so that a runner is a thing with an identity rather than an assumption.

ALTER TABLE tasks ADD COLUMN execution TEXT NOT NULL DEFAULT '{}'
    CHECK (json_valid(execution)
       AND (json_extract(execution, '$.interaction') IS NULL OR json_extract(execution, '$.interaction') IN ('interactive', 'autonomous', 'autonomous_stop_if_blocked'))
       AND (json_extract(execution, '$.priority') IS NULL OR json_extract(execution, '$.priority') IN ('low', 'normal', 'high')));

UPDATE tasks SET execution = json_object('interaction', json_extract(policy, '$.interaction'))
WHERE json_extract(policy, '$.interaction') != 'interactive';

ALTER TABLE tasks DROP COLUMN policy;

ALTER TABLE projects ADD COLUMN execution TEXT NOT NULL DEFAULT '{}'
    CHECK (json_valid(execution)
       AND (json_extract(execution, '$.interaction') IS NULL OR json_extract(execution, '$.interaction') IN ('interactive', 'autonomous', 'autonomous_stop_if_blocked'))
       AND (json_extract(execution, '$.priority') IS NULL OR json_extract(execution, '$.priority') IN ('low', 'normal', 'high')));

ALTER TABLE runs ADD COLUMN model     TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN reasoning TEXT NOT NULL DEFAULT '';

CREATE TABLE settings (
    key        TEXT    PRIMARY KEY,
    value      TEXT    NOT NULL CHECK (json_valid(value)),
    updated_at INTEGER NOT NULL
) STRICT;

CREATE TABLE runners (
    id           TEXT    PRIMARY KEY,
    name         TEXT    NOT NULL,
    kind         TEXT    NOT NULL CHECK (kind IN ('local')),
    hostname     TEXT    NOT NULL DEFAULT '',
    os           TEXT    NOT NULL DEFAULT '',
    arch         TEXT    NOT NULL DEFAULT '',
    version      TEXT    NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL
) STRICT;

-- This computer is the one 'local' runner.
CREATE UNIQUE INDEX runners_one_local ON runners (kind) WHERE kind = 'local';
