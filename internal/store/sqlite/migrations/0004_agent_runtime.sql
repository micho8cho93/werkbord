-- Agent runtime.
--
-- Runs become interactive sessions, so a run now records what it was asked,
-- what it is waiting for, what it last did, how its process ended, and enough
-- about its process to find it again if the controller dies without cleaning
-- up. Questions gain a kind (a question versus a permission request). Events
-- gain an index so a run's activity can be read without scanning the log.

ALTER TABLE runs ADD COLUMN prompt TEXT NOT NULL DEFAULT '';

-- What a run in 'waiting_for_user' is waiting for; empty in every other state.
ALTER TABLE runs ADD COLUMN waiting TEXT NOT NULL DEFAULT '' CHECK (waiting IN ('', 'question', 'idle'));

ALTER TABLE runs ADD COLUMN activity TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN activity_at INTEGER;
ALTER TABLE runs ADD COLUMN exit_code INTEGER;

-- pid is 0 when no process is attached. process_id is an opaque token that
-- tells the process apart from a later one that reuses the pid.
ALTER TABLE runs ADD COLUMN pid INTEGER NOT NULL DEFAULT 0;
ALTER TABLE runs ADD COLUMN process_id TEXT NOT NULL DEFAULT '';

-- Only a waiting run can be waiting for something, and only a live run can have
-- a process.
CREATE TRIGGER runs_waiting_only_when_waiting_insert BEFORE INSERT ON runs
WHEN (NEW.state = 'waiting_for_user') != (NEW.waiting != '')
BEGIN
  SELECT RAISE(ABORT, 'waiting is set exactly while a run is waiting_for_user');
END;

CREATE TRIGGER runs_waiting_only_when_waiting_update BEFORE UPDATE OF state, waiting ON runs
WHEN (NEW.state = 'waiting_for_user') != (NEW.waiting != '')
BEGIN
  SELECT RAISE(ABORT, 'waiting is set exactly while a run is waiting_for_user');
END;

CREATE TRIGGER runs_no_process_when_ended BEFORE UPDATE OF state, pid ON runs
WHEN NEW.state IN ('completed', 'failed', 'stopped') AND NEW.pid != 0
BEGIN
  SELECT RAISE(ABORT, 'an ended run has no process');
END;

ALTER TABLE questions ADD COLUMN kind TEXT NOT NULL DEFAULT 'ask' CHECK (kind IN ('ask', 'approval'));

CREATE INDEX events_by_run ON events (run_id, seq) WHERE run_id IS NOT NULL;
