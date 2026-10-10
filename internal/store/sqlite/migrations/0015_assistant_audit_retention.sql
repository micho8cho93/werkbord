-- Retention for the assistant's audit.
--
-- The audit is append-only and hash-chained. Old entries can now be removed, but only as a prefix: the oldest entries up
-- to a point, never one from the middle. The last removed entry's hash is kept as a checkpoint, so the entries that remain
-- still verify (the first of them must follow the checkpoint), and a line taken out of the middle or the end is still
-- detected. A prefix can only be removed inside a window the pruning declares first, in the same transaction.

CREATE TABLE assistant_audit_checkpoint (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    through_seq INTEGER NOT NULL,
    hash        TEXT    NOT NULL,
    pruned      INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
) STRICT;

CREATE TABLE assistant_audit_window (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    through_seq INTEGER NOT NULL
) STRICT;
INSERT INTO assistant_audit_window (id, through_seq) VALUES (1, 0);

DROP TRIGGER assistant_audit_no_delete;
CREATE TRIGGER assistant_audit_no_delete BEFORE DELETE ON assistant_audit
WHEN OLD.seq > (SELECT through_seq FROM assistant_audit_window WHERE id = 1)
BEGIN
  SELECT RAISE(ABORT, 'the assistant audit is append-only');
END;
