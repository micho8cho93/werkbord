-- Werkbord Team: comments on tickets, the people they mention, and what each mention tells the person it is for.
-- Additive: no existing table is changed. Timestamps are Unix milliseconds (UTC).
--
-- Everything here is coordination metadata. A comment is words and member references; nothing in it is a path on
-- anyone's computer, a credential or markup (the content is stored as a list of text and mention segments, which the
-- console only ever shows as text).

CREATE TABLE ticket_comments (
    id            TEXT    PRIMARY KEY,
    workspace_id  TEXT    NOT NULL,
    project_id    TEXT    NOT NULL,
    ticket_id     TEXT    NOT NULL REFERENCES tickets (id) ON DELETE CASCADE,
    -- Where the comment sits in its ticket's conversation: 1, 2, 3, ... in the order posted. Never reused, never moved by an edit.
    seq           INTEGER NOT NULL CHECK (seq >= 1),
    -- Who wrote it. A plain id, not a foreign key: the conversation must outlive a member who leaves, and still say who spoke.
    author_id     TEXT    NOT NULL,
    -- The name they had when they wrote it: what is shown for a member who is no longer in the workspace.
    author_name   TEXT    NOT NULL,
    -- The creation id the author's client chose, and a digest of what was posted: posting the same comment twice (a retry)
    -- finds the first one; the same id with other words is refused.
    client_id     TEXT    NOT NULL,
    client_digest TEXT    NOT NULL,
    -- A JSON list of {"type":"text","text":...} and {"type":"mention","memberId":...,"name":...} in order. A tombstone's is [].
    body          TEXT    NOT NULL DEFAULT '[]',
    version       INTEGER NOT NULL DEFAULT 1,
    created_at    INTEGER NOT NULL,
    edited_at     INTEGER,
    -- A deleted comment stays as a tombstone, so the comments around it keep their order.
    deleted_at    INTEGER,
    deleted_by    TEXT    NOT NULL DEFAULT '',
    UNIQUE (ticket_id, seq),
    UNIQUE (ticket_id, author_id, client_id),
    UNIQUE (id, workspace_id),
    FOREIGN KEY (project_id, workspace_id) REFERENCES projects (id, workspace_id) ON DELETE CASCADE,
    CHECK (deleted_by IN ('', 'author', 'manager')),
    CHECK ((deleted_at IS NULL) = (deleted_by = ''))
) STRICT;

-- Who a comment mentions now: one row per person, however many times the comment names them. The member is a plain id,
-- so a mention of someone who has left stays where it was written.
CREATE TABLE comment_mentions (
    comment_id   TEXT NOT NULL REFERENCES ticket_comments (id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL,
    member_id    TEXT NOT NULL,
    PRIMARY KEY (comment_id, member_id)
) STRICT;

CREATE INDEX comment_mentions_by_member ON comment_mentions (workspace_id, member_id);

-- What a mention tells its recipient. One row for each comment and person, ever: a second mention of them in the same
-- comment, or an edit that does not touch their mention, makes no second row and never reopens one they have read.
-- Taking a notification away (the mention is removed, the comment is deleted, the recipient leaves the project) sets
-- withdrawn: it is then gone for them, and it does not come back by their joining the project again. Only a new
-- mention of them in that comment, made by an edit after the old one was taken out, gives it back, unread.
CREATE TABLE comment_notifications (
    id           TEXT    PRIMARY KEY,
    workspace_id TEXT    NOT NULL,
    project_id   TEXT    NOT NULL,
    ticket_id    TEXT    NOT NULL,
    comment_id   TEXT    NOT NULL REFERENCES ticket_comments (id) ON DELETE CASCADE,
    recipient_id TEXT    NOT NULL,
    author_id    TEXT    NOT NULL,
    created_at   INTEGER NOT NULL,
    read_at      INTEGER,
    withdrawn    INTEGER NOT NULL DEFAULT 0 CHECK (withdrawn IN (0, 1)),
    UNIQUE (comment_id, recipient_id),
    FOREIGN KEY (project_id, workspace_id) REFERENCES projects (id, workspace_id) ON DELETE CASCADE,
    -- A notification is nothing without the person it is for.
    FOREIGN KEY (recipient_id, workspace_id) REFERENCES members (id, workspace_id) ON DELETE CASCADE
) STRICT;

CREATE INDEX comment_notifications_by_recipient ON comment_notifications (workspace_id, recipient_id, created_at DESC, id DESC);
CREATE INDEX comment_notifications_by_project ON comment_notifications (project_id, recipient_id);

-- Losing access to a project takes its notifications away, whoever removes the person and however: the database does it, so
-- that no other way of leaving a project can forget to, and a person who is added again finds none of them.
CREATE TRIGGER comment_notifications_leave_project AFTER DELETE ON project_members
BEGIN
    UPDATE comment_notifications SET withdrawn = 1
     WHERE workspace_id = OLD.workspace_id AND project_id = OLD.project_id AND recipient_id = OLD.member_id AND withdrawn = 0;
END;

-- As in migration 0003: whatever writes these tables moves the workspace's revision in the same transaction, so a read-state
-- change (nothing else changes) still reaches every open session, and a write that forgets to bump a revision cannot hide.
CREATE TRIGGER workspace_rev_comment_insert AFTER INSERT ON ticket_comments
BEGIN UPDATE workspaces SET revision = revision + 1 WHERE id = NEW.workspace_id; END;

CREATE TRIGGER workspace_rev_comment_update AFTER UPDATE ON ticket_comments
BEGIN UPDATE workspaces SET revision = revision + 1 WHERE id = NEW.workspace_id; END;

CREATE TRIGGER workspace_rev_comment_delete AFTER DELETE ON ticket_comments
BEGIN UPDATE workspaces SET revision = revision + 1 WHERE id = OLD.workspace_id; END;

CREATE TRIGGER workspace_rev_notification_insert AFTER INSERT ON comment_notifications
BEGIN UPDATE workspaces SET revision = revision + 1 WHERE id = NEW.workspace_id; END;

CREATE TRIGGER workspace_rev_notification_update AFTER UPDATE ON comment_notifications
BEGIN UPDATE workspaces SET revision = revision + 1 WHERE id = NEW.workspace_id; END;

CREATE TRIGGER workspace_rev_notification_delete AFTER DELETE ON comment_notifications
BEGIN UPDATE workspaces SET revision = revision + 1 WHERE id = OLD.workspace_id; END;
