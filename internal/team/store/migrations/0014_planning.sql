-- Flexible work organization for a workspace: shared labels, who does a ticket, planned dates and the
-- order tickets depend on.
--
-- Nothing here changes how an existing ticket moves: every existing ticket becomes work_mode 'agent'
-- (what it always was), with no labels, no planned dates and no dependencies. Team stores these as
-- coordination metadata only, like the rest of the board; none of it reaches anyone's computer.

-- Labels belong to the workspace and are shared by all of its projects. Names are unique within a
-- workspace ignoring case and spacing (name_key is the folded name the application computes).
CREATE TABLE labels (
    id           TEXT    PRIMARY KEY,
    workspace_id TEXT    NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    name_key     TEXT    NOT NULL,
    color        TEXT    NOT NULL CHECK (length(color) = 7 AND substr(color, 1, 1) = '#'),
    description  TEXT    NOT NULL DEFAULT '',
    version      INTEGER NOT NULL DEFAULT 1,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    UNIQUE (workspace_id, name_key),
    UNIQUE (id, workspace_id)
) STRICT;

-- A ticket carries any number of labels of its own workspace: the composite key keeps a ticket from carrying
-- another workspace's label whoever writes the row. The rowid keeps the order they were put on in.
CREATE TABLE ticket_labels (
    ticket_id    TEXT NOT NULL,
    label_id     TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    PRIMARY KEY (ticket_id, label_id),
    FOREIGN KEY (ticket_id, workspace_id) REFERENCES tickets (id, workspace_id) ON DELETE CASCADE,
    FOREIGN KEY (label_id, workspace_id)  REFERENCES labels (id, workspace_id)  ON DELETE CASCADE
) STRICT;
CREATE INDEX ticket_labels_by_label ON ticket_labels (label_id);

-- Who is expected to do the ticket. A fixed classification, not a label.
ALTER TABLE tickets ADD COLUMN work_mode TEXT NOT NULL DEFAULT 'agent' CHECK (work_mode IN ('human', 'agent', 'hybrid'));

-- Planned days for the timeline ('YYYY-MM-DD', or '' when unset).
ALTER TABLE tickets ADD COLUMN plan_start TEXT NOT NULL DEFAULT '';
ALTER TABLE tickets ADD COLUMN plan_end   TEXT NOT NULL DEFAULT '';
ALTER TABLE tickets ADD COLUMN milestone  INTEGER NOT NULL DEFAULT 0 CHECK (milestone IN (0, 1));

-- What a ticket waits for: tickets of the same workspace. (That both are in one project, and that there is no
-- cycle, is the application's rule, checked where a dependency is written; these are the rules the database can
-- state on its own.) These are planning dependencies, separate from the dependencies of a shared request for
-- agent work (schedules), which keep meaning what they meant.
CREATE TABLE ticket_dependencies (
    ticket_id     TEXT NOT NULL,
    depends_on_id TEXT NOT NULL,
    workspace_id  TEXT NOT NULL,
    PRIMARY KEY (ticket_id, depends_on_id),
    CHECK (ticket_id != depends_on_id),
    FOREIGN KEY (ticket_id, workspace_id)     REFERENCES tickets (id, workspace_id) ON DELETE CASCADE,
    FOREIGN KEY (depends_on_id, workspace_id) REFERENCES tickets (id, workspace_id) ON DELETE CASCADE
) STRICT;
CREATE INDEX ticket_dependencies_by_dependency ON ticket_dependencies (depends_on_id);
