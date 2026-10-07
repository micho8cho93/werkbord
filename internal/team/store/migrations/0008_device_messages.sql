-- Werkbord Team: the mailbox of signed requests between a person's own devices.
--
-- A person can ask one of their devices (a runner) to do something from another of their devices (their phone, their
-- laptop). The asking device SIGNS the request, with a key only it holds, and a Workspace Host stores it here and hands it
-- to the device it is for. The host never makes, changes or signs one: a request that arrives is stored exactly as the
-- sender signed it, and the device it is for checks the signature itself, against the sender's public key, before it does
-- anything. A host that routes a request cannot forge one, because it has no sender's key.
--
-- What is stored is what the sender signed: identifiers, an action from a closed list, an expiry, a nonce and a payload of
-- identifiers (envelope/actions.go: no command, no path, no environment, no free-form text beyond a short reply to a
-- question). There is no column that could hold anything else. Messages are kept a few days and then removed.

CREATE TABLE device_messages (
    -- The envelope's own message ID: random, signed by the sender, so a message cannot be stored twice.
    id             TEXT    NOT NULL PRIMARY KEY,
    workspace_id   TEXT    NOT NULL,
    from_device_id TEXT    NOT NULL,
    to_device_id   TEXT    NOT NULL,
    -- The person who asked: the owner of both devices.
    member_id      TEXT    NOT NULL,
    action         TEXT    NOT NULL CHECK (action IN ('open_ticket_on_runner', 'start_approved_run', 'cancel_run', 'respond_to_agent_question', 'fetch_runner_status')),
    -- The signed envelope, as the sender made it.
    envelope       TEXT    NOT NULL,
    state          TEXT    NOT NULL CHECK (state IN ('queued', 'done', 'refused')),
    -- What the device it was for said back: a small JSON object, empty until it has.
    result         TEXT    NOT NULL DEFAULT '',
    created_at     INTEGER NOT NULL,
    expires_at     INTEGER NOT NULL,
    decided_at     INTEGER,
    FOREIGN KEY (from_device_id, workspace_id) REFERENCES devices (id, workspace_id) ON DELETE CASCADE,
    FOREIGN KEY (to_device_id, workspace_id)   REFERENCES devices (id, workspace_id) ON DELETE CASCADE,
    FOREIGN KEY (member_id, workspace_id)      REFERENCES members (id, workspace_id) ON DELETE CASCADE
) STRICT;

CREATE INDEX device_messages_inbox ON device_messages (workspace_id, to_device_id, state, created_at);
CREATE INDEX device_messages_by_person ON device_messages (workspace_id, member_id, created_at);
