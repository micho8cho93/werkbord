-- The board starts at Available. Backlog is no column: an idea that was not ready is now simply Available, and claiming a
-- ticket moves it straight to In Progress, claimed and waiting for its owner to start it.
-- Only rows change. The table keeps the status values it allows, so a host rolled back to the previous release still reads
-- every row, and no table is rebuilt (the previous migration did that, with every dependent row backed up).
-- A claimed ticket moving from Available to In Progress keeps its assignment counter (both count as active work), so an
-- execution already approved for the claim stays approved. Bumping the version makes a stale editor reload rather than
-- overwrite the new status, and bumping the project revision makes every open board look again.
UPDATE projects SET revision = revision + 1 WHERE id IN (SELECT project_id FROM tickets WHERE status = 'backlog' OR (status = 'available' AND assignee_id IS NOT NULL));
UPDATE tickets SET status = 'in_progress', version = version + 1 WHERE status = 'available' AND assignee_id IS NOT NULL;
UPDATE tickets SET status = 'available', version = version + 1 WHERE status = 'backlog';
