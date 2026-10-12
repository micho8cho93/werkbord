-- Read-only operational views reuse existing rows. Small partial indexes avoid
-- scanning transcripts or adding a second telemetry/write pipeline.
CREATE INDEX analytics_tasks ON tasks(created_at);
CREATE INDEX analytics_runs ON runs(created_at);
CREATE INDEX analytics_events ON events(project_id, seq)
WHERE type IN ('task.created','task.updated','run.state_changed','agent.completed','git.merged');
CREATE INDEX analytics_notices ON events(created_at, project_id)
WHERE type = 'agent.output' AND json_valid(payload) AND json_extract(payload, '$.stream') = 'system';
CREATE INDEX analytics_events_global ON events(seq)
WHERE type IN ('task.created','task.updated','run.state_changed','agent.completed','git.merged');
