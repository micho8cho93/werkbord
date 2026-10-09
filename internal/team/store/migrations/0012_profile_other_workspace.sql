-- Werkbord Team: a device that cannot host this workspace because another workspace on the same computer already hosts.
--
-- One computer can belong to several Team workspaces, but a Workspace Host or Connectivity Host listens on ports that
-- are the same for every host of a workspace, so a computer can host for only one of them. A device says so in its
-- profile, and the workspace tells an administrator before they ask it to be a host. Like the rest of the profile it is
-- a yes or no, not an address, a name or a credential.

ALTER TABLE device_profiles ADD COLUMN other_workspace INTEGER NOT NULL DEFAULT 0 CHECK (other_workspace IN (0, 1));
