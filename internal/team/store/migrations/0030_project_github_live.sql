-- A project's members may see its repository live on GitHub through their own sign-in. Someone who manages projects can turn that off.
ALTER TABLE projects ADD COLUMN github_live_off INTEGER NOT NULL DEFAULT 0;
