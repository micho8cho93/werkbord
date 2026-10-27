CREATE TABLE device_operations (
 id TEXT PRIMARY KEY,
 runner_id TEXT NOT NULL,
 actor TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 operation TEXT NOT NULL,
 result TEXT NOT NULL,
 state TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL
);
CREATE INDEX device_operations_pending ON device_operations(runner_id,state,created_at);
