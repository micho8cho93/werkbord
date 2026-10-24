-- Dedicated generated glyphs cannot be assigned to two live workspaces.
CREATE UNIQUE INDEX personal_workspace_icon_unique
ON personal_workspaces(json_extract(document, '$.icon'))
WHERE json_extract(document, '$.icon') GLOB 'workspace-*';
