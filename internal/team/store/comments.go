package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"werkbord/internal/team/domain"
)

// Comments on tickets, the people they mention, and the notification each mention makes. See migration 0021.

// CommentPageQuery says which comments of a ticket to read. They come back oldest first. With no cursor it is the
// latest ones; Before is the position of the oldest comment the caller already has, After (when HasAfter is set: zero is
// a position too, the start of the conversation) the newest, and Around the position of a comment the page must contain.
type CommentPageQuery struct {
	Limit    int
	Before   int64
	After    int64
	HasAfter bool
	Around   int64
}

// CommentPage is some of a ticket's comments, oldest first, and whether there are more on either side.
type CommentPage struct {
	Comments []domain.Comment
	HasOlder bool
	HasNewer bool
	// Live is how many comments the ticket has that are not deleted.
	Live int
}

// NotificationQuery says which of a member's notifications to read, newest first. A zero Before is the newest.
type NotificationQuery struct {
	ProjectID  string
	UnreadOnly bool
	Limit      int
	Before     NotificationCursor
}

// NotificationCursor is where the previous page of notifications ended.
type NotificationCursor struct {
	CreatedAt time.Time
	ID        string
}

// Zero reports whether there is no cursor.
func (c NotificationCursor) Zero() bool { return c.ID == "" }

// NotificationRow is a notification with what it is about, as one read finds it: the comment's current content, the ticket
// and the project. It only exists for a notification that is still the recipient's to see.
type NotificationRow struct {
	domain.Notification
	AuthorName      string
	Segments        []domain.Segment
	TicketNumber    int
	TicketTitle     string
	TicketArchived  bool
	ProjectName     string
	ProjectArchived bool
}

const commentCols = `id, project_id, ticket_id, seq, author_id, author_name, client_id, client_digest, body, version, created_at, edited_at, deleted_at, deleted_by`

func scanComment(s interface{ Scan(...any) error }) (domain.Comment, error) {
	var c domain.Comment
	var body string
	var created int64
	var edited, deleted sql.NullInt64
	err := s.Scan(&c.ID, &c.ProjectID, &c.TicketID, &c.Seq, &c.AuthorID, &c.AuthorName, &c.ClientID, &c.ClientDigest, &body, &c.Version, &created, &edited, &deleted, &c.DeletedBy)
	if err != nil {
		return c, err
	}
	c.CreatedAt, c.EditedAt, c.DeletedAt = fromMS(created), optMS(edited), optMS(deleted)
	if c.Segments, err = domain.ParseSegments(body); err != nil {
		return c, fmt.Errorf("comment %s has unreadable content: %w", c.ID, err)
	}
	return c, nil
}

// InsertComment stores a new comment as the next one of its ticket, with the people it mentions, and returns it as
// stored (its position filled in). Posting what an author's client already posted (same creation ID) is
// domain.ErrConflict: the use case asks for the first one with CommentByClientID before it gets here.
func (t *sqlTx) InsertComment(ctx context.Context, workspaceID string, c domain.Comment) (domain.Comment, error) {
	if err := t.q.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM ticket_comments WHERE ticket_id = ?`, c.TicketID).Scan(&c.Seq); err != nil {
		return c, err
	}
	_, err := t.q.ExecContext(ctx, `INSERT INTO ticket_comments (id, workspace_id, project_id, ticket_id, seq, author_id, author_name, client_id, client_digest, body, version, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)`,
		c.ID, workspaceID, c.ProjectID, c.TicketID, c.Seq, c.AuthorID, c.AuthorName, c.ClientID, c.ClientDigest, domain.EncodeSegments(c.Segments), ms(c.CreatedAt))
	if isUnique(err) {
		return c, fmt.Errorf("%w: this comment was already posted", domain.ErrConflict)
	}
	if err != nil {
		return c, err
	}
	c.Version = 1
	return c, t.SetCommentMentions(ctx, workspaceID, c.ID, domain.MentionedIDs(c.Segments))
}

// Comment returns one comment of a ticket.
func (t *sqlTx) Comment(ctx context.Context, workspaceID, projectID, ticketID, id string) (domain.Comment, error) {
	c, err := scanComment(t.q.QueryRowContext(ctx, `SELECT `+commentCols+` FROM ticket_comments WHERE workspace_id = ? AND project_id = ? AND ticket_id = ? AND id = ?`,
		workspaceID, projectID, ticketID, id))
	return c, notFound(err, "comment")
}

// CommentByClientID finds the comment a client already posted under a creation ID.
func (t *sqlTx) CommentByClientID(ctx context.Context, workspaceID, ticketID, authorID, clientID string) (domain.Comment, bool, error) {
	c, err := scanComment(t.q.QueryRowContext(ctx, `SELECT `+commentCols+` FROM ticket_comments WHERE workspace_id = ? AND ticket_id = ? AND author_id = ? AND client_id = ?`,
		workspaceID, ticketID, authorID, clientID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Comment{}, false, nil
	}
	return c, err == nil, err
}

func (t *sqlTx) queryComments(ctx context.Context, where string, args []any, order string, limit int) ([]domain.Comment, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT `+commentCols+` FROM ticket_comments WHERE `+where+` ORDER BY seq `+order+` LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Comment{}
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func reverseComments(c []domain.Comment) {
	for i, j := 0, len(c)-1; i < j; i, j = i+1, j-1 {
		c[i], c[j] = c[j], c[i]
	}
}

// CommentPage reads some of a ticket's comments (see CommentPageQuery).
func (t *sqlTx) CommentPage(ctx context.Context, workspaceID, ticketID string, q CommentPageQuery) (CommentPage, error) {
	var page CommentPage
	limit := q.Limit
	if limit <= 0 || limit > domain.MaxCommentPage {
		limit = domain.DefaultCommentPage
	}
	base := `workspace_id = ? AND ticket_id = ?`
	args := func(extra ...any) []any { return append([]any{workspaceID, ticketID}, extra...) }
	exists := func(cond string, extra ...any) (bool, error) {
		var n int
		err := t.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT 1 FROM ticket_comments WHERE `+base+` AND `+cond+` LIMIT 1)`, args(extra...)...).Scan(&n)
		return n > 0, err
	}
	var err error
	switch {
	case q.Around > 0:
		// Half the page from before the comment and the rest from after it; near an end of the conversation, the other side fills up.
		older, err := t.queryComments(ctx, base+` AND seq < ?`, args(q.Around), "DESC", limit/2)
		if err != nil {
			return page, err
		}
		newer, err := t.queryComments(ctx, base+` AND seq > ?`, args(q.Around), "ASC", limit-1-len(older))
		if err != nil {
			return page, err
		}
		if want := limit - 1 - len(newer); len(older) < want {
			if older, err = t.queryComments(ctx, base+` AND seq < ?`, args(q.Around), "DESC", want); err != nil {
				return page, err
			}
		}
		self, err := t.queryComments(ctx, base+` AND seq = ?`, args(q.Around), "ASC", 1)
		if err != nil {
			return page, err
		}
		reverseComments(older)
		page.Comments = append(append(older, self...), newer...)
	case q.Before > 0:
		page.Comments, err = t.queryComments(ctx, base+` AND seq < ?`, args(q.Before), "DESC", limit)
		reverseComments(page.Comments)
	case q.HasAfter:
		page.Comments, err = t.queryComments(ctx, base+` AND seq > ?`, args(q.After), "ASC", limit)
	default:
		page.Comments, err = t.queryComments(ctx, base, args(), "DESC", limit)
		reverseComments(page.Comments)
	}
	if err != nil {
		return page, err
	}
	if n := len(page.Comments); n > 0 {
		first, last := page.Comments[0].Seq, page.Comments[n-1].Seq
		if page.HasOlder, err = exists(`seq < ?`, first); err != nil {
			return page, err
		}
		if page.HasNewer, err = exists(`seq > ?`, last); err != nil {
			return page, err
		}
	} else {
		// Nothing came back: say which side the rest is on, from where the caller was.
		switch {
		case q.Before > 0:
			page.HasNewer, err = exists(`seq >= ?`, q.Before)
		case q.HasAfter:
			page.HasOlder, err = exists(`seq <= ?`, q.After)
		}
		if err != nil {
			return page, err
		}
	}
	err = t.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM ticket_comments WHERE `+base+` AND deleted_at IS NULL`, args()...).Scan(&page.Live)
	return page, err
}

// CommentMentions are the people a comment mentions now, in a stable order.
func (t *sqlTx) CommentMentions(ctx context.Context, workspaceID, commentID string) ([]string, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT member_id FROM comment_mentions WHERE workspace_id = ? AND comment_id = ? ORDER BY member_id`, workspaceID, commentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetCommentMentions replaces the people a comment mentions.
func (t *sqlTx) SetCommentMentions(ctx context.Context, workspaceID, commentID string, memberIDs []string) error {
	if _, err := t.q.ExecContext(ctx, `DELETE FROM comment_mentions WHERE workspace_id = ? AND comment_id = ?`, workspaceID, commentID); err != nil {
		return err
	}
	for _, id := range memberIDs {
		if _, err := t.q.ExecContext(ctx, `INSERT INTO comment_mentions (comment_id, workspace_id, member_id) VALUES (?, ?, ?) ON CONFLICT (comment_id, member_id) DO NOTHING`,
			commentID, workspaceID, id); err != nil {
			return err
		}
	}
	return nil
}

// SaveComment writes an edit if the comment is still at the version the editor read (compare-and-swap), moves its
// version and records when it was edited, and replaces who it mentions. A comment that was deleted meanwhile, or
// changed by someone else, is domain.ErrConflict; one that is not there is domain.ErrNotFound.
func (t *sqlTx) SaveComment(ctx context.Context, workspaceID string, c domain.Comment, version int64) (domain.Comment, error) {
	res, err := t.q.ExecContext(ctx, `UPDATE ticket_comments SET body = ?, edited_at = ?, version = version + 1
		WHERE workspace_id = ? AND project_id = ? AND ticket_id = ? AND id = ? AND version = ? AND deleted_at IS NULL`,
		domain.EncodeSegments(c.Segments), nullMS(c.EditedAt), workspaceID, c.ProjectID, c.TicketID, c.ID, version)
	if err != nil {
		return c, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return c, t.commentRefusal(ctx, workspaceID, c)
	}
	c.Version = version + 1
	return c, t.SetCommentMentions(ctx, workspaceID, c.ID, domain.MentionedIDs(c.Segments))
}

func (t *sqlTx) commentRefusal(ctx context.Context, workspaceID string, c domain.Comment) error {
	cur, err := t.Comment(ctx, workspaceID, c.ProjectID, c.TicketID, c.ID)
	switch {
	case err != nil:
		return err
	case cur.Deleted():
		return fmt.Errorf("%w: this comment was deleted", domain.ErrConflict)
	}
	return fmt.Errorf("%w: this comment was changed by someone else; reload it and try again", domain.ErrConflict)
}

// TombstoneComment deletes a comment's content and mentions and leaves a tombstone in its place, if the comment is
// still at the version the caller read. It reports false when the comment was already deleted.
func (t *sqlTx) TombstoneComment(ctx context.Context, workspaceID string, c domain.Comment, deletedBy string, version int64, now time.Time) (domain.Comment, bool, error) {
	res, err := t.q.ExecContext(ctx, `UPDATE ticket_comments SET body = '[]', deleted_at = ?, deleted_by = ?, version = version + 1
		WHERE workspace_id = ? AND project_id = ? AND ticket_id = ? AND id = ? AND version = ? AND deleted_at IS NULL`,
		ms(now), deletedBy, workspaceID, c.ProjectID, c.TicketID, c.ID, version)
	if err != nil {
		return c, false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		cur, err := t.Comment(ctx, workspaceID, c.ProjectID, c.TicketID, c.ID)
		if err != nil {
			return c, false, err
		}
		if cur.Deleted() {
			return cur, false, nil
		}
		return c, false, t.commentRefusal(ctx, workspaceID, c)
	}
	deleted := now
	c.Segments, c.Version, c.DeletedAt, c.DeletedBy = []domain.Segment{}, version+1, &deleted, deletedBy
	return c, true, t.SetCommentMentions(ctx, workspaceID, c.ID, nil)
}

// ---- notifications ----

// NotifyMention makes the notification for a person a comment mentions. There is one for each comment and person,
// ever: a notification that exists is left as it is, read or not, and one that was withdrawn (their mention was taken
// out and is now put back) is given back, unread. It reports whether the person has a new, or renewed, notification.
func (t *sqlTx) NotifyMention(ctx context.Context, workspaceID string, n domain.Notification) (bool, error) {
	res, err := t.q.ExecContext(ctx, `INSERT INTO comment_notifications (id, workspace_id, project_id, ticket_id, comment_id, recipient_id, author_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (comment_id, recipient_id) DO UPDATE SET created_at = excluded.created_at, author_id = excluded.author_id, read_at = NULL, withdrawn = 0
		WHERE comment_notifications.withdrawn = 1`,
		n.ID, workspaceID, n.ProjectID, n.TicketID, n.CommentID, n.RecipientID, n.AuthorID, ms(n.CreatedAt))
	if containsFK(err) {
		return false, fmt.Errorf("%w: that person is not in this workspace", domain.ErrInvalid)
	}
	if err != nil {
		return false, err
	}
	rows, _ := res.RowsAffected()
	return rows > 0, nil
}

// WithdrawNotifications takes away what a comment's mentions told people: those of the given people, or of everyone when
// none are named. A withdrawn notification is no longer listed or counted, and stays gone.
func (t *sqlTx) WithdrawNotifications(ctx context.Context, workspaceID, commentID string, recipients []string) error {
	q := `UPDATE comment_notifications SET withdrawn = 1 WHERE workspace_id = ? AND comment_id = ? AND withdrawn = 0`
	args := []any{workspaceID, commentID}
	if recipients != nil {
		if len(recipients) == 0 {
			return nil
		}
		q += ` AND recipient_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(recipients)), ",") + `)`
		for _, r := range recipients {
			args = append(args, r)
		}
	}
	_, err := t.q.ExecContext(ctx, q, args...)
	return err
}

// CommentNotifications are the notifications a person has on some comments that are still theirs to see, by comment.
func (t *sqlTx) CommentNotifications(ctx context.Context, workspaceID, recipientID string, commentIDs []string) (map[string]domain.Notification, error) {
	out := map[string]domain.Notification{}
	if len(commentIDs) == 0 {
		return out, nil
	}
	args := []any{workspaceID, recipientID}
	for _, id := range commentIDs {
		args = append(args, id)
	}
	rows, err := t.q.QueryContext(ctx, `SELECT `+notificationCols+` FROM comment_notifications
		WHERE workspace_id = ? AND recipient_id = ? AND withdrawn = 0 AND comment_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(commentIDs)), ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out[n.CommentID] = n
	}
	return out, rows.Err()
}

const notificationCols = `id, project_id, ticket_id, comment_id, recipient_id, author_id, created_at, read_at, withdrawn`

func scanNotification(s interface{ Scan(...any) error }) (domain.Notification, error) {
	var n domain.Notification
	var created int64
	var read sql.NullInt64
	var withdrawn int
	err := s.Scan(&n.ID, &n.ProjectID, &n.TicketID, &n.CommentID, &n.RecipientID, &n.AuthorID, &created, &read, &withdrawn)
	n.CreatedAt, n.ReadAt, n.Withdrawn = fromMS(created), optMS(read), withdrawn == 1
	return n, err
}

// What a notification is shown with, and the conditions for it to be shown at all: the person is on the project now, the
// notification was not withdrawn, and the comment still has its words.
const notificationRowSelect = `SELECT n.id, n.project_id, n.ticket_id, n.comment_id, n.recipient_id, n.author_id, n.created_at, n.read_at, n.withdrawn,
		c.author_name, c.body, k.number, k.title, k.archived_at, p.name, p.archived
	FROM comment_notifications n
	JOIN ticket_comments c ON c.id = n.comment_id AND c.workspace_id = n.workspace_id
	JOIN tickets k ON k.id = c.ticket_id AND k.workspace_id = n.workspace_id
	JOIN projects p ON p.id = n.project_id AND p.workspace_id = n.workspace_id
	JOIN project_members pm ON pm.project_id = n.project_id AND pm.member_id = n.recipient_id AND pm.workspace_id = n.workspace_id
	WHERE n.workspace_id = ? AND n.recipient_id = ? AND n.withdrawn = 0 AND c.deleted_at IS NULL`

func scanNotificationRow(s interface{ Scan(...any) error }) (NotificationRow, error) {
	var r NotificationRow
	var created int64
	var read, archived sql.NullInt64
	var withdrawn, projectArchived int
	var body string
	err := s.Scan(&r.ID, &r.ProjectID, &r.TicketID, &r.CommentID, &r.RecipientID, &r.AuthorID, &created, &read, &withdrawn,
		&r.AuthorName, &body, &r.TicketNumber, &r.TicketTitle, &archived, &r.ProjectName, &projectArchived)
	if err != nil {
		return r, err
	}
	r.CreatedAt, r.ReadAt, r.Withdrawn = fromMS(created), optMS(read), withdrawn == 1
	r.TicketArchived, r.ProjectArchived = archived.Valid, projectArchived == 1
	r.Segments, err = domain.ParseSegments(body)
	return r, err
}

// Notifications lists a person's notifications, newest first (see NotificationQuery).
func (t *sqlTx) Notifications(ctx context.Context, workspaceID, recipientID string, q NotificationQuery) ([]NotificationRow, error) {
	limit := q.Limit
	if limit <= 0 || limit > domain.MaxNotificationPage {
		limit = domain.DefaultNotificationPage
	}
	query, args := notificationRowSelect, []any{workspaceID, recipientID}
	if q.UnreadOnly {
		query += ` AND n.read_at IS NULL`
	}
	if q.ProjectID != "" {
		query += ` AND n.project_id = ?`
		args = append(args, q.ProjectID)
	}
	if !q.Before.Zero() {
		query += ` AND (n.created_at < ? OR (n.created_at = ? AND n.id < ?))`
		args = append(args, ms(q.Before.CreatedAt), ms(q.Before.CreatedAt), q.Before.ID)
	}
	rows, err := t.q.QueryContext(ctx, query+` ORDER BY n.created_at DESC, n.id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NotificationRow{}
	for rows.Next() {
		r, err := scanNotificationRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Notification returns one notification of a person, if it is still theirs to see.
func (t *sqlTx) Notification(ctx context.Context, workspaceID, recipientID, id string) (NotificationRow, error) {
	r, err := scanNotificationRow(t.q.QueryRowContext(ctx, notificationRowSelect+` AND n.id = ?`, workspaceID, recipientID, id))
	return r, notFound(err, "notification")
}

// UnreadNotifications counts a person's unread notifications on each project, the ones they are shown (see Notifications).
func (t *sqlTx) UnreadNotifications(ctx context.Context, workspaceID, recipientID string) (map[string]int, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT n.project_id, COUNT(*)
		FROM comment_notifications n
		JOIN ticket_comments c ON c.id = n.comment_id AND c.workspace_id = n.workspace_id
		JOIN project_members pm ON pm.project_id = n.project_id AND pm.member_id = n.recipient_id AND pm.workspace_id = n.workspace_id
		WHERE n.workspace_id = ? AND n.recipient_id = ? AND n.withdrawn = 0 AND n.read_at IS NULL AND c.deleted_at IS NULL
		GROUP BY n.project_id`, workspaceID, recipientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var project string
		var n int
		if err := rows.Scan(&project, &n); err != nil {
			return nil, err
		}
		out[project] = n
	}
	return out, rows.Err()
}

// UnreadNotificationsOnTicket counts a person's unread notifications about one ticket, the ones they are shown.
func (t *sqlTx) UnreadNotificationsOnTicket(ctx context.Context, workspaceID, recipientID, ticketID string) (int, error) {
	var n int
	err := t.q.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM comment_notifications n
		JOIN ticket_comments c ON c.id = n.comment_id AND c.workspace_id = n.workspace_id
		JOIN project_members pm ON pm.project_id = n.project_id AND pm.member_id = n.recipient_id AND pm.workspace_id = n.workspace_id
		WHERE n.workspace_id = ? AND n.recipient_id = ? AND n.ticket_id = ? AND n.withdrawn = 0 AND n.read_at IS NULL AND c.deleted_at IS NULL`,
		workspaceID, recipientID, ticketID).Scan(&n)
	return n, err
}

// MarkNotificationRead records that its recipient has read a notification. Doing it again changes nothing, and says so:
// the returned notification is the same one, with the time it was first read. A notification that is not the person's, or
// is no longer theirs to see, is domain.ErrNotFound.
func (t *sqlTx) MarkNotificationRead(ctx context.Context, workspaceID, recipientID, id string, now time.Time) (domain.Notification, bool, error) {
	row, err := t.Notification(ctx, workspaceID, recipientID, id)
	if err != nil {
		return domain.Notification{}, false, err
	}
	if row.ReadAt != nil {
		return row.Notification, false, nil
	}
	res, err := t.q.ExecContext(ctx, `UPDATE comment_notifications SET read_at = ? WHERE workspace_id = ? AND recipient_id = ? AND id = ? AND read_at IS NULL AND withdrawn = 0`,
		ms(now), workspaceID, recipientID, id)
	if err != nil {
		return domain.Notification{}, false, err
	}
	changed, _ := res.RowsAffected()
	row.ReadAt = &now
	if changed == 0 { // read by another of the person's sessions in the same instant
		fresh, err := t.Notification(ctx, workspaceID, recipientID, id)
		return fresh.Notification, false, err
	}
	return row.Notification, true, nil
}
