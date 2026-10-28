CREATE TABLE product_map_snapshots (
 project_id TEXT NOT NULL REFERENCES projects(id),
 version INTEGER NOT NULL,
 snapshot TEXT NOT NULL CHECK(json_valid(snapshot)),
 PRIMARY KEY(project_id,version)
);
CREATE TABLE product_map_jobs (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id),
 body TEXT NOT NULL CHECK(json_valid(body))
);
CREATE INDEX product_map_jobs_project ON product_map_jobs(project_id);
