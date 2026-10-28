CREATE TABLE product_maps (
 workspace_id TEXT NOT NULL,
 project_id TEXT NOT NULL,
 id TEXT NOT NULL,
 state TEXT NOT NULL,
 version INTEGER NOT NULL,
 document TEXT NOT NULL CHECK(json_valid(document) AND length(CAST(document AS BLOB))<=2200000),
 PRIMARY KEY(workspace_id,project_id,id),
 FOREIGN KEY(project_id,workspace_id) REFERENCES projects(id,workspace_id) ON DELETE CASCADE
) STRICT;
CREATE UNIQUE INDEX product_maps_published_version ON product_maps(workspace_id,project_id,version) WHERE state='published';
