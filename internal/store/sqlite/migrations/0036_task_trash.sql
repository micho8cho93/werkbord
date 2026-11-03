-- A task goes Done -> archived (archived_at) -> deleted (deleted_at). A deleted task is kept, out of every list, for the trash
-- retention period so it can be restored; after that it is erased. A deleted task is always an archived one.
ALTER TABLE tasks ADD COLUMN deleted_at INTEGER;
CREATE INDEX tasks_trash ON tasks(deleted_at) WHERE deleted_at IS NOT NULL;
