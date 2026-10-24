-- The finite, approved Lucide catalogue replaces generated pixel glyphs. Live
-- legacy identities are upgraded atomically by Open, preserving their history.
CREATE UNIQUE INDEX personal_workspace_library_icon_unique
ON personal_workspaces(json_extract(document, '$.icon'))
WHERE json_extract(document, '$.icon') GLOB 'lucide-*';
