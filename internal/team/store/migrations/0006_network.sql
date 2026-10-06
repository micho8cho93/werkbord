-- Werkbord Team: the customer-owned private network, and how a device joins it.
--
-- Everything here is public or hashed. A workspace's private keys (its own, and the
-- network authority's) are never in the database: they live sealed in the key vault of
-- each Workspace Host, and the database may be copied or replicated without them. What
-- is here is what the hosts and the administrators need to know: which address each
-- device has, which certificates were issued (by fingerprint, so a revoked device's can
-- be refused), the invitations made (by a hash of their one-time credential, never the
-- credential), and the requests to join. A test lists every column and fails if one is
-- added that could hold a secret.

CREATE TABLE network_settings (
    workspace_id        TEXT    PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    workspace_key       TEXT    NOT NULL,
    fingerprint         TEXT    NOT NULL,
    network_prefix      TEXT    NOT NULL,
    ca_certificate      TEXT    NOT NULL,
    enrollment_approval TEXT    NOT NULL DEFAULT 'auto' CHECK (enrollment_approval IN ('auto', 'admin')),
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
) STRICT;

CREATE TABLE device_network (
    device_id           TEXT    PRIMARY KEY,
    workspace_id        TEXT    NOT NULL,
    overlay_addr        TEXT    NOT NULL,
    network_public_key  TEXT    NOT NULL,
    sealing_key         TEXT    NOT NULL DEFAULT '',
    groups              TEXT    NOT NULL,
    discovery           INTEGER NOT NULL DEFAULT 0 CHECK (discovery IN (0, 1)),
    relay               INTEGER NOT NULL DEFAULT 0 CHECK (relay IN (0, 1)),
    bootstrap_endpoints TEXT    NOT NULL DEFAULT '[]',
    network_endpoints   TEXT    NOT NULL DEFAULT '[]',
    reachability        TEXT    NOT NULL DEFAULT 'unknown' CHECK (reachability IN ('unknown', 'public', 'private_only', 'unreachable')),
    last_check_at       INTEGER,
    last_external_ok_at INTEGER,
    -- Workspace secrets sealed (encrypted) to this device's sealing key, waiting for it
    -- to collect them. Ciphertext only: nothing but that device can open it.
    provision_sealed    BLOB,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    UNIQUE (workspace_id, overlay_addr),
    FOREIGN KEY (device_id, workspace_id) REFERENCES devices (id, workspace_id) ON DELETE CASCADE
) STRICT;

-- Every certificate the workspace issued, by fingerprint. A revoked device's are
-- marked, and every host tells its network program to refuse them. These rows
-- deliberately do not go when the device's registry row does (a removed member's
-- devices are deleted): the refusal must outlive the device it is for, until the
-- certificate expires on its own.
CREATE TABLE network_certificates (
    fingerprint  TEXT    PRIMARY KEY,
    workspace_id TEXT    NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    device_id    TEXT    NOT NULL,
    issued_at    INTEGER NOT NULL,
    not_after    INTEGER NOT NULL,
    revoked_at   INTEGER
) STRICT;

CREATE INDEX network_certificates_by_device ON network_certificates (workspace_id, device_id);
CREATE INDEX network_certificates_revoked ON network_certificates (workspace_id, revoked_at) WHERE revoked_at IS NOT NULL;

-- A device's credential for the workspace's API: the hash of a random token, one per
-- device. Revoking the device deletes it, which is what makes revocation immediate.
CREATE TABLE device_credentials (
    device_id    TEXT    PRIMARY KEY,
    workspace_id TEXT    NOT NULL,
    token_hash   TEXT    NOT NULL UNIQUE,
    created_at   INTEGER NOT NULL,
    FOREIGN KEY (device_id, workspace_id) REFERENCES devices (id, workspace_id) ON DELETE CASCADE
) STRICT;

CREATE TABLE enrollment_invitations (
    id               TEXT    PRIMARY KEY,
    workspace_id     TEXT    NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    -- A hash of the one-time credential. The credential is in the invitation, which
    -- is shown once and not stored.
    credential_hash  BLOB    NOT NULL,
    created_by       TEXT    NOT NULL,
    label            TEXT    NOT NULL DEFAULT '',
    for_member_id    TEXT    NOT NULL DEFAULT '',
    role             TEXT    NOT NULL,
    capabilities     TEXT    NOT NULL DEFAULT '',
    require_approval INTEGER NOT NULL DEFAULT 0 CHECK (require_approval IN (0, 1)),
    state            TEXT    NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'used', 'withdrawn')),
    created_at       INTEGER NOT NULL,
    expires_at       INTEGER NOT NULL,
    used_at          INTEGER
) STRICT;

CREATE INDEX enrollment_invitations_by_workspace ON enrollment_invitations (workspace_id, created_at);

CREATE TABLE enrollments (
    id                 TEXT    PRIMARY KEY,
    workspace_id       TEXT    NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    -- One request per invitation: it is single use, so a second is refused by the schema too.
    invitation_id      TEXT    NOT NULL UNIQUE REFERENCES enrollment_invitations(id) ON DELETE CASCADE,
    member_id          TEXT    NOT NULL DEFAULT '',
    member_name        TEXT    NOT NULL DEFAULT '',
    device_id          TEXT    NOT NULL,
    device_name        TEXT    NOT NULL,
    device_key         TEXT    NOT NULL,
    network_public_key TEXT    NOT NULL,
    sealing_key        TEXT    NOT NULL DEFAULT '',
    capabilities       TEXT    NOT NULL DEFAULT '',
    state              TEXT    NOT NULL CHECK (state IN ('pending', 'approved', 'denied')),
    delivered_at       INTEGER,
    decided_by         TEXT    NOT NULL DEFAULT '',
    decided_at         INTEGER,
    remote_addr        TEXT    NOT NULL DEFAULT '',
    created_at         INTEGER NOT NULL
) STRICT;

CREATE INDEX enrollments_by_workspace ON enrollments (workspace_id, state, created_at);
