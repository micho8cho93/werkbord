-- Owner offers are descriptive metadata; they never authorize a host role.
ALTER TABLE device_profiles ADD COLUMN offer_workspace_role INTEGER NOT NULL DEFAULT 0 CHECK (offer_workspace_role IN (0, 1));
ALTER TABLE device_profiles ADD COLUMN offer_connectivity_role INTEGER NOT NULL DEFAULT 0 CHECK (offer_connectivity_role IN (0, 1));
