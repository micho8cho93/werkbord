ALTER TABLE assistant_sessions ADD COLUMN selection_mode TEXT NOT NULL DEFAULT '';
ALTER TABLE assistant_sessions ADD COLUMN selection_json TEXT NOT NULL DEFAULT 'null';
