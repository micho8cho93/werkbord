-- Werkbord Team: what a device says about the kind of machine it is.
--
-- A device reports, about itself and with its own credential, what kind of computer it is and whether it has
-- been seen to go to sleep. The workspace uses it to tell an administrator that a laptop that sleeps is not a good
-- Workspace Host, before they make it one. Like the registry itself it is public, non-secret coordination
-- metadata: no path, no hostname, no address, no environment, no credential, and no column that could hold one.
--
-- It is kept apart from the registry (the devices table) so that the registry keeps exactly the columns it has had.

CREATE TABLE device_profiles (
    device_id    TEXT    NOT NULL PRIMARY KEY,
    workspace_id TEXT    NOT NULL,
    -- darwin, linux, windows or other: what the device's own Go runtime says.
    platform     TEXT    NOT NULL CHECK (platform IN ('darwin', 'linux', 'windows', 'other')),
    -- What kind of computer its owner says it is, or the device could tell: unknown until someone knows.
    form         TEXT    NOT NULL CHECK (form IN ('desktop', 'laptop', 'server', 'unknown')),
    -- The device has been seen to go to sleep, or its owner says it sleeps automatically.
    sleeps       INTEGER NOT NULL CHECK (sleeps IN (0, 1)),
    -- How many times it was seen to wake in the last seven days.
    sleep_events INTEGER NOT NULL CHECK (sleep_events >= 0),
    -- The version of the Team software on the device.
    version      TEXT    NOT NULL,
    reported_at  INTEGER NOT NULL,
    FOREIGN KEY (device_id, workspace_id) REFERENCES devices (id, workspace_id) ON DELETE CASCADE
) STRICT;

CREATE INDEX device_profiles_by_workspace ON device_profiles (workspace_id);
