-- Which members hold a valid Premium unlock key. The key itself is never stored: the service checks it when it is presented and
-- keeps only its ID, once per team (a key shared between two members of one team is refused), and when it was checked.
CREATE TABLE member_premium (
 workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 member_id    TEXT NOT NULL REFERENCES members(id) ON DELETE CASCADE,
 key_id       TEXT NOT NULL,
 verified_at  INTEGER NOT NULL,
 PRIMARY KEY (workspace_id, member_id),
 UNIQUE (workspace_id, key_id)
) STRICT;

-- What a device presented when it asked to join, until an administrator decides: the same, key ID only.
CREATE TABLE enrollment_premium (
 workspace_id  TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 enrollment_id TEXT NOT NULL,
 key_id        TEXT NOT NULL,
 PRIMARY KEY (workspace_id, enrollment_id)
) STRICT;
