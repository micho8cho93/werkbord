-- Werkbord Team: the registry of devices.
--
-- A device is a machine that takes part in a workspace, recorded by what other
-- members and devices may know about it: its ID, its owner, its name, its PUBLIC
-- signing key, what it does for the workspace, and when it was last seen. There is
-- deliberately no column that could hold anything else: no private key, no Git, API
-- or model credential, no filesystem path, no environment variable. A test lists
-- every column of these tables and fails if one is added that could.
--
-- Capabilities are a device's, not its owner's: nothing here refers to a member's
-- role, and no member's role is changed by owning a device.

CREATE TABLE devices (
    id                  TEXT    PRIMARY KEY,
    workspace_id        TEXT    NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    member_id           TEXT    NOT NULL,
    name                TEXT    NOT NULL,
    -- The device's public Ed25519 key, base64 (raw URL alphabet).
    public_key          TEXT    NOT NULL,
    -- The state of each host role; 'none' when the device does not hold it.
    host_status         TEXT    NOT NULL DEFAULT 'none' CHECK (host_status         IN ('none', 'joining', 'active', 'unavailable')),
    connectivity_status TEXT    NOT NULL DEFAULT 'none' CHECK (connectivity_status IN ('none', 'joining', 'active', 'unavailable')),
    last_seen_at        INTEGER,
    -- Revocation is permanent: once set it is never cleared (the queries only ever set it where it is null).
    revoked_at          INTEGER,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    UNIQUE (id, workspace_id),
    -- A key is registered once per workspace, so one key cannot stand for two devices.
    UNIQUE (workspace_id, public_key),
    FOREIGN KEY (member_id, workspace_id) REFERENCES members (id, workspace_id) ON DELETE CASCADE
) STRICT;

CREATE INDEX devices_by_member ON devices (workspace_id, member_id);

CREATE TABLE device_capabilities (
    device_id    TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    capability   TEXT NOT NULL CHECK (capability IN ('workspace_host', 'connectivity_host', 'runner')),
    PRIMARY KEY (device_id, capability),
    FOREIGN KEY (device_id, workspace_id) REFERENCES devices (id, workspace_id) ON DELETE CASCADE
) STRICT;

CREATE INDEX device_capabilities_by_capability ON device_capabilities (workspace_id, capability);
