CREATE TABLE workspace_licenses (
    workspace_id TEXT PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    document TEXT NOT NULL
);

CREATE TABLE api_replays (
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    device_id TEXT NOT NULL,
    nonce TEXT NOT NULL,
    expires_at INTEGER NOT NULL,
    PRIMARY KEY (workspace_id, device_id, nonce)
);

CREATE INDEX api_replays_expiry ON api_replays(expires_at);
