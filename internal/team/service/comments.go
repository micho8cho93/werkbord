package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// Comments on tickets, and the notifications their mentions make.
//
// The conversation on a ticket is shared: everyone who may see the ticket reads it, and everyone with comments.post
// writes in it. A comment may mention people on the project; only a mention makes a notification, and the notification
// is its recipient's alone. Nothing here starts a process or reaches a computer: a comment is words and member IDs, stored
// with the ticket's other coordination metadata, and the clients that show it are told of it through the same project and
// workspace revisions as every other change.

// CommentNotice is the viewer's own notification on a comment.
type CommentNotice struct {
	ID     string     `json:"id"`
	Read   bool       `json:"read"`
	ReadAt *time.Time `json:"readAt,omitempty"`
}

// CommentView is a comment as one viewer sees it: with the names as they are now (or as they were, for someone who has
// left), what the viewer's own mention of it is, and what the viewer may do to it.
type CommentView struct {
	domain.Comment
	// AuthorFormer: the author is no longer in the workspace, and AuthorName is the name they had.
	AuthorFormer bool `json:"authorFormer,omitempty"`
	// MentionsMe and Notification: the comment mentions the viewer, and the viewer's notification for it, if it is still theirs.
	MentionsMe   bool           `json:"mentionsMe,omitempty"`
	Notification *CommentNotice `json:"notification,omitempty"`
	CanEdit      bool           `json:"canEdit"`
	CanDelete    bool           `json:"canDelete"`
}

// CommentList is some of a ticket's comments, oldest first.
type CommentList struct {
	Comments []CommentView `json:"comments"`
	// Total counts the comments that are not deleted.
	Total    int  `json:"total"`
	HasOlder bool `json:"hasOlder"`
	HasNewer bool `json:"hasNewer"`
	// OlderBefore and NewerAfter are what to ask for to read the comments before and after these (as before and after).
	OlderBefore int64 `json:"olderBefore,omitempty"`
	NewerAfter  int64 `json:"newerAfter,omitempty"`
	// Focus is the comment a read around one was asked for, so a client knows what to highlight.
	Focus string `json:"focus,omitempty"`
	// UnreadMentions is how many unread notifications the viewer has on this ticket, on this page or not.
	UnreadMentions int `json:"unreadMentions"`
	// CanPost: the viewer may write here now. ReadOnly says the ticket (or its project) is archived, which is why not.
	CanPost  bool `json:"canPost"`
	ReadOnly bool `json:"readOnly"`
}

// CommentListQuery says which comments to read. Limit is capped at domain.MaxCommentPage. At most one of Before, After
// and Around is set: Before and After are positions (as an earlier CommentList gave them; After counts only when HasAfter
// is set, because 0 is a position: the start of the conversation), Around the ID of a comment the page must contain.
type CommentListQuery struct {
	Limit    int
	Before   int64
	After    int64
	HasAfter bool
	Around   string
}

// nameBook names members for the length of one transaction.
type nameBook struct {
	ctx   context.Context
	tx    store.Tx
	ws    string
	known map[string]*domain.Member
}

func newNameBook(ctx context.Context, tx store.Tx, ws string) *nameBook {
	return &nameBook{ctx: ctx, tx: tx, ws: ws, known: map[string]*domain.Member{}}
}

// member is the member, or nil when they are no longer in the workspace.
func (p *nameBook) member(id string) (*domain.Member, error) {
	if m, ok := p.known[id]; ok {
		return m, nil
	}
	m, err := p.tx.Member(p.ctx, p.ws, id)
	if err != nil {
		if isNotFound(err) {
			p.known[id] = nil
			return nil, nil
		}
		return nil, err
	}
	p.known[id] = &m
	return &m, nil
}

// name is what to call someone: their name now, or the one they had (and whether they have left).
func (p *nameBook) name(id, was string) (name string, former bool, err error) {
	m, err := p.member(id)
	if err != nil {
		return "", false, err
	}
	switch {
	case m != nil:
		return m.Name, false, nil
	case was != "":
		return was, true, nil
	}
	return "a former member", true, nil
}

func isNotFound(err error) bool { return errors.Is(err, domain.ErrNotFound) }

// commentWritable says whether a ticket's conversation may be changed: not while the ticket or its project is archived.
func commentWritable(x access, k domain.Ticket) error {
	switch {
	case k.ArchivedAt != nil:
		return fmt.Errorf("%w: this ticket is archived, so its comments are read-only; restore the ticket to change them", domain.ErrConflict)
	case x.Project.Archived:
		return fmt.Errorf("%w: the project is archived, so its comments are read-only", domain.ErrConflict)
	}
	return nil
}

func commentView(a Actor, x access, k domain.Ticket, ppl *nameBook, c domain.Comment, notice *domain.Notification) (CommentView, error) {
	v := CommentView{Comment: c}
	var err error
	if v.AuthorName, v.AuthorFormer, err = ppl.name(c.AuthorID, c.AuthorName); err != nil {
		return v, err
	}
	v.Segments = make([]domain.Segment, 0, len(c.Segments))
	for _, seg := range c.Segments {
		if seg.Type == domain.SegmentMention {
			if seg.Name, _, err = ppl.name(seg.MemberID, seg.Name); err != nil {
				return v, err
			}
			if seg.MemberID == a.Member.ID {
				v.MentionsMe = true
			}
		}
		v.Segments = append(v.Segments, seg)
	}
	if notice != nil {
		v.MentionsMe = true
		v.Notification = &CommentNotice{ID: notice.ID, Read: notice.ReadAt != nil, ReadAt: notice.ReadAt}
	}
	if commentWritable(x, k) == nil && !c.Deleted() {
		mine := c.AuthorID == a.Member.ID && x.can(domain.PPCommentsPost)
		v.CanEdit = mine
		v.CanDelete = mine || x.can(domain.PPCommentsModerate)
	}
	return v, nil
}

// ListComments reads a ticket's comments, oldest first: the latest ones by default, or those before or after a position,
// or around a comment (what opening a notification asks for).
func (s *Service) ListComments(ctx context.Context, a Actor, projectID, ticketID string, q CommentListQuery) (CommentList, error) {
	out := CommentList{Comments: []CommentView{}}
	if (q.Before != 0 && q.HasAfter) || (q.Around != "" && (q.Before != 0 || q.HasAfter)) || q.Before < 0 || q.After < 0 || q.Limit < 0 {
		return out, fmt.Errorf("%w: ask for the comments before, after or around one, not several of those", domain.ErrInvalid)
	}
	err := s.view(ctx, a, projectID, func(tx store.Tx, x access) error {
		if err := x.require(domain.PPTicketsView, "see this project's tickets"); err != nil {
			return err
		}
		k, err := tx.Ticket(ctx, a.Workspace.ID, projectID, ticketID)
		if err != nil {
			return err
		}
		pq := store.CommentPageQuery{Limit: q.Limit, Before: q.Before, After: q.After, HasAfter: q.HasAfter}
		if q.Around != "" {
			focus, err := tx.Comment(ctx, a.Workspace.ID, projectID, ticketID, q.Around)
			if err != nil {
				return err
			}
			pq.Around, out.Focus = focus.Seq, focus.ID
		}
		page, err := tx.CommentPage(ctx, a.Workspace.ID, ticketID, pq)
		if err != nil {
			return err
		}
		ids := make([]string, len(page.Comments))
		for i, c := range page.Comments {
			ids[i] = c.ID
		}
		notices, err := tx.CommentNotifications(ctx, a.Workspace.ID, a.Member.ID, ids)
		if err != nil {
			return err
		}
		ppl := newNameBook(ctx, tx, a.Workspace.ID)
		for _, c := range page.Comments {
			var notice *domain.Notification
			if n, ok := notices[c.ID]; ok {
				notice = &n
			}
			v, err := commentView(a, x, k, ppl, c, notice)
			if err != nil {
				return err
			}
			out.Comments = append(out.Comments, v)
		}
		if n := len(page.Comments); n > 0 {
			out.OlderBefore, out.NewerAfter = page.Comments[0].Seq, page.Comments[n-1].Seq
		}
		out.Total, out.HasOlder, out.HasNewer = page.Live, page.HasOlder, page.HasNewer
		if out.UnreadMentions, err = tx.UnreadNotificationsOnTicket(ctx, a.Workspace.ID, a.Member.ID, ticketID); err != nil {
			return err
		}
		out.ReadOnly = commentWritable(x, k) != nil
		out.CanPost = x.can(domain.PPCommentsPost) && !out.ReadOnly
		return nil
	})
	return out, err
}

// mentionable resolves who a comment may mention: the people on the project now, and, when a comment is edited, the people
// it already mentions (who may have left since: their mention stays where it was written, only a new one must be of a
// person who is on the project).
func mentionable(ctx context.Context, tx store.Tx, ws, projectID string, in []domain.Segment, existing []domain.Segment) (func(string) (string, bool), error) {
	ppl := newNameBook(ctx, tx, ws)
	had := map[string]string{}
	for _, seg := range existing {
		if seg.Type == domain.SegmentMention {
			had[seg.MemberID] = seg.Name
		}
	}
	names := map[string]string{}
	for _, seg := range in {
		if seg.Type != domain.SegmentMention {
			continue
		}
		id := strings.TrimSpace(seg.MemberID)
		if _, done := names[id]; done || id == "" {
			continue
		}
		name, was := had[id]
		if !was {
			on, err := tx.IsProjectMember(ctx, ws, projectID, id)
			if err != nil {
				return nil, err
			}
			if !on {
				continue
			}
		}
		n, _, err := ppl.name(id, name)
		if err != nil {
			return nil, err
		}
		names[id] = n
	}
	return func(id string) (string, bool) { n, ok := names[id]; return n, ok }, nil
}

// CommentInput is a comment a member posts. ClientID is a creation ID their client makes up for it: posting it again, as a
// client does when it does not know whether the first request arrived, finds the comment already posted rather than
// making a second.
type CommentInput struct {
	ClientID string
	Segments []domain.Segment
}

// PostComment adds a comment to a ticket. The second result is false when it was already posted (the same creation ID,
// and the same words): the comment returned is the one posted before and nothing is notified a second time.
func (s *Service) PostComment(ctx context.Context, a Actor, projectID, ticketID string, in CommentInput) (CommentView, bool, error) {
	clientID, err := domain.CleanClientID(in.ClientID)
	if err != nil {
		return CommentView{}, false, err
	}
	var view CommentView
	created := true
	err = s.mutate(ctx, a, projectID, func(tx store.Tx, x access) error {
		if err := x.require(domain.PPTicketsView, "see this project's tickets"); err != nil {
			return err
		}
		if err := x.require(domain.PPCommentsPost, "comment on tickets"); err != nil {
			return err
		}
		k, err := tx.Ticket(ctx, a.Workspace.ID, projectID, ticketID)
		if err != nil {
			return err
		}
		ppl := newNameBook(ctx, tx, a.Workspace.ID)
		// A retry finds what it posted before, whatever has happened to the people it mentions since.
		if prior, found, err := tx.CommentByClientID(ctx, a.Workspace.ID, ticketID, a.Member.ID, clientID); err != nil {
			return err
		} else if found {
			segs, _, err := domain.CleanComment(in.Segments, func(id string) (string, bool) { return "", strings.TrimSpace(id) != "" })
			if err != nil || domain.SegmentsDigest(segs) != prior.ClientDigest {
				return fmt.Errorf("%w: this creation id was already used for another comment; make a new one", domain.ErrConflict)
			}
			created = false
			view, err = s.commentViewFor(ctx, tx, a, x, k, ppl, prior)
			return err
		}
		if err := commentWritable(x, k); err != nil {
			return err
		}
		resolve, err := mentionable(ctx, tx, a.Workspace.ID, projectID, in.Segments, nil)
		if err != nil {
			return err
		}
		segs, mentioned, err := domain.CleanComment(in.Segments, resolve)
		if err != nil {
			return err
		}
		c := domain.Comment{ID: domain.NewID(domain.PrefixComment), ProjectID: projectID, TicketID: ticketID, AuthorID: a.Member.ID, AuthorName: a.Member.Name,
			Segments: segs, ClientID: clientID, ClientDigest: domain.SegmentsDigest(segs), CreatedAt: s.stamp()}
		if c, err = tx.InsertComment(ctx, a.Workspace.ID, c); err != nil {
			return err
		}
		if err := s.notifyMentioned(ctx, tx, a, c, mentioned); err != nil {
			return err
		}
		view, err = s.commentViewFor(ctx, tx, a, x, k, ppl, c)
		return err
	})
	return view, created, err
}

// commentViewFor is a comment as the actor sees it, with their own notification on it.
func (s *Service) commentViewFor(ctx context.Context, tx store.Tx, a Actor, x access, k domain.Ticket, ppl *nameBook, c domain.Comment) (CommentView, error) {
	notices, err := tx.CommentNotifications(ctx, a.Workspace.ID, a.Member.ID, []string{c.ID})
	if err != nil {
		return CommentView{}, err
	}
	var notice *domain.Notification
	if n, ok := notices[c.ID]; ok {
		notice = &n
	}
	return commentView(a, x, k, ppl, c, notice)
}

// notifyMentioned tells the people a comment mentions, except its author: you do not notify yourself.
func (s *Service) notifyMentioned(ctx context.Context, tx store.Tx, a Actor, c domain.Comment, mentioned []string) error {
	for _, id := range mentioned {
		if id == c.AuthorID {
			continue
		}
		_, err := tx.NotifyMention(ctx, a.Workspace.ID, domain.Notification{ID: domain.NewID(domain.PrefixNotification), ProjectID: c.ProjectID, TicketID: c.TicketID,
			CommentID: c.ID, RecipientID: id, AuthorID: c.AuthorID, CreatedAt: s.stamp()})
		if err != nil {
			return err
		}
	}
	return nil
}

// EditComment replaces the content of a comment its author wrote, if it is still at the version the author read. The
// people it newly mentions are notified, once; those whose mention was taken out lose their notification; the others,
// whether or not they have read theirs, are left exactly as they were.
func (s *Service) EditComment(ctx context.Context, a Actor, projectID, ticketID, commentID string, version int64, segments []domain.Segment) (CommentView, error) {
	var view CommentView
	err := s.mutate(ctx, a, projectID, func(tx store.Tx, x access) error {
		if err := x.require(domain.PPTicketsView, "see this project's tickets"); err != nil {
			return err
		}
		if err := x.require(domain.PPCommentsPost, "edit comments"); err != nil {
			return err
		}
		k, err := tx.Ticket(ctx, a.Workspace.ID, projectID, ticketID)
		if err != nil {
			return err
		}
		c, err := tx.Comment(ctx, a.Workspace.ID, projectID, ticketID, commentID)
		if err != nil {
			return err
		}
		if c.AuthorID != a.Member.ID {
			return forbidden("edit a comment someone else wrote")
		}
		if err := commentWritable(x, k); err != nil {
			return err
		}
		switch {
		case c.Deleted():
			return fmt.Errorf("%w: this comment was deleted", domain.ErrConflict)
		case version != c.Version:
			return fmt.Errorf("%w: this comment was changed by someone else; reload it and try again", domain.ErrConflict)
		}
		resolve, err := mentionable(ctx, tx, a.Workspace.ID, projectID, segments, c.Segments)
		if err != nil {
			return err
		}
		segs, mentioned, err := domain.CleanComment(segments, resolve)
		if err != nil {
			return err
		}
		ppl := newNameBook(ctx, tx, a.Workspace.ID)
		if domain.SameSegments(segs, c.Segments) { // nothing to say again
			view, err = s.commentViewFor(ctx, tx, a, x, k, ppl, c)
			return err
		}
		before, err := tx.CommentMentions(ctx, a.Workspace.ID, c.ID)
		if err != nil {
			return err
		}
		edited := s.stamp()
		c.Segments, c.EditedAt = segs, &edited
		if c, err = tx.SaveComment(ctx, a.Workspace.ID, c, version); err != nil {
			return err
		}
		had := map[string]bool{}
		for _, id := range before {
			had[id] = true
		}
		var added []string
		for _, id := range mentioned {
			if !had[id] {
				added = append(added, id)
			}
			delete(had, id)
		}
		if err := s.notifyMentioned(ctx, tx, a, c, added); err != nil {
			return err
		}
		removed := make([]string, 0, len(had))
		for _, id := range before { // in the order stored, not the map's
			if had[id] {
				removed = append(removed, id)
			}
		}
		if len(removed) > 0 {
			if err := tx.WithdrawNotifications(ctx, a.Workspace.ID, c.ID, removed); err != nil {
				return err
			}
		}
		view, err = s.commentViewFor(ctx, tx, a, x, k, ppl, c)
		return err
	})
	return view, err
}

// DeleteComment turns a comment into a tombstone and takes away every notification it made. Its author may delete it, and
// so may a project manager (comments.moderate); an edit is never a manager's. A version, if given, must be the comment's.
// Deleting what is already deleted changes nothing and says so: the tombstone is returned.
func (s *Service) DeleteComment(ctx context.Context, a Actor, projectID, ticketID, commentID string, version *int64) (CommentView, error) {
	var view CommentView
	err := s.mutate(ctx, a, projectID, func(tx store.Tx, x access) error {
		if err := x.require(domain.PPTicketsView, "see this project's tickets"); err != nil {
			return err
		}
		k, err := tx.Ticket(ctx, a.Workspace.ID, projectID, ticketID)
		if err != nil {
			return err
		}
		c, err := tx.Comment(ctx, a.Workspace.ID, projectID, ticketID, commentID)
		if err != nil {
			return err
		}
		ppl := newNameBook(ctx, tx, a.Workspace.ID)
		own := c.AuthorID == a.Member.ID && x.can(domain.PPCommentsPost)
		if !own && !x.can(domain.PPCommentsModerate) {
			return forbidden("delete a comment someone else wrote")
		}
		if c.Deleted() {
			view, err = s.commentViewFor(ctx, tx, a, x, k, ppl, c)
			return err
		}
		if err := commentWritable(x, k); err != nil {
			return err
		}
		if version != nil && *version != c.Version {
			return fmt.Errorf("%w: this comment was changed by someone else; reload it and try again", domain.ErrConflict)
		}
		by := domain.DeletedByManager
		if own {
			by = domain.DeletedByAuthor
		}
		if c, _, err = tx.TombstoneComment(ctx, a.Workspace.ID, c, by, c.Version, s.stamp()); err != nil {
			return err
		}
		if err := tx.WithdrawNotifications(ctx, a.Workspace.ID, c.ID, nil); err != nil {
			return err
		}
		view, err = s.commentViewFor(ctx, tx, a, x, k, ppl, c)
		return err
	})
	return view, err
}

// ---- notifications ----

// NotificationItem is a mention that waits for its recipient: who wrote it, where, and the start of what it says.
type NotificationItem struct {
	ID           string     `json:"id"`
	ProjectID    string     `json:"projectId"`
	ProjectName  string     `json:"projectName"`
	TicketID     string     `json:"ticketId"`
	TicketKey    string     `json:"ticketKey"`
	TicketTitle  string     `json:"ticketTitle"`
	CommentID    string     `json:"commentId"`
	AuthorID     string     `json:"authorId"`
	AuthorName   string     `json:"authorName"`
	AuthorFormer bool       `json:"authorFormer,omitempty"`
	Excerpt      string     `json:"excerpt"`
	CreatedAt    time.Time  `json:"createdAt"`
	ReadAt       *time.Time `json:"readAt,omitempty"`
	// Archived: the ticket, or its project, is archived. The comment can still be opened; it cannot be answered there.
	Archived bool `json:"archived,omitempty"`
}

// NotificationList is a page of the viewer's notifications, newest first.
type NotificationList struct {
	Notifications []NotificationItem `json:"notifications"`
	// Unread counts the viewer's unread notifications (on the project asked for, if one was), whatever page this is.
	Unread  int  `json:"unread"`
	HasMore bool `json:"hasMore"`
	// Next is what to ask for (as cursor) to read the page after this one.
	Next     string `json:"next,omitempty"`
	Revision int64  `json:"revision"`
}

// NotificationListQuery says which notifications to read. Unread ones, unless IncludeRead.
type NotificationListQuery struct {
	ProjectID   string
	IncludeRead bool
	Limit       int
	Cursor      string
}

func parseNotificationCursor(s string) (store.NotificationCursor, error) {
	if s == "" {
		return store.NotificationCursor{}, nil
	}
	at, id, ok := strings.Cut(s, ".")
	n, err := strconv.ParseInt(at, 10, 64)
	if !ok || err != nil || id == "" || n < 0 {
		return store.NotificationCursor{}, fmt.Errorf("%w: that is not a place in the notifications", domain.ErrInvalid)
	}
	return store.NotificationCursor{CreatedAt: time.UnixMilli(n).UTC(), ID: id}, nil
}

func notificationCursor(n store.NotificationRow) string {
	return strconv.FormatInt(n.CreatedAt.UnixMilli(), 10) + "." + n.ID
}

func (s *Service) notificationItem(ppl *nameBook, r store.NotificationRow) (NotificationItem, error) {
	name, former, err := ppl.name(r.AuthorID, r.AuthorName)
	if err != nil {
		return NotificationItem{}, err
	}
	segs := make([]domain.Segment, 0, len(r.Segments))
	for _, seg := range r.Segments {
		if seg.Type == domain.SegmentMention {
			if seg.Name, _, err = ppl.name(seg.MemberID, seg.Name); err != nil {
				return NotificationItem{}, err
			}
		}
		segs = append(segs, seg)
	}
	return NotificationItem{ID: r.ID, ProjectID: r.ProjectID, ProjectName: r.ProjectName, TicketID: r.TicketID, TicketKey: domain.TicketKey(r.TicketNumber), TicketTitle: r.TicketTitle,
		CommentID: r.CommentID, AuthorID: r.AuthorID, AuthorName: name, AuthorFormer: former, Excerpt: domain.Excerpt(segs, domain.ExcerptLen),
		CreatedAt: r.CreatedAt, ReadAt: r.ReadAt, Archived: r.TicketArchived || r.ProjectArchived}, nil
}

// ListNotifications reads the viewer's own notifications, newest first, across the projects they are on or in the one
// they ask for. A notification of a project the viewer has left is not listed, and does not come back if they rejoin.
func (s *Service) ListNotifications(ctx context.Context, a Actor, q NotificationListQuery) (NotificationList, error) {
	out := NotificationList{Notifications: []NotificationItem{}}
	cursor, err := parseNotificationCursor(q.Cursor)
	if err != nil {
		return out, err
	}
	limit := q.Limit
	if limit <= 0 || limit > domain.MaxNotificationPage {
		limit = domain.DefaultNotificationPage
	}
	err = s.db.View(ctx, func(tx store.Tx) error {
		if q.ProjectID != "" {
			if _, err := s.visibleProject(ctx, tx, a, q.ProjectID); err != nil {
				return err
			}
		}
		rows, err := tx.Notifications(ctx, a.Workspace.ID, a.Member.ID, store.NotificationQuery{ProjectID: q.ProjectID, UnreadOnly: !q.IncludeRead, Limit: limit + 1, Before: cursor})
		if err != nil {
			return err
		}
		if len(rows) > limit {
			rows, out.HasMore = rows[:limit], true
		}
		ppl := newNameBook(ctx, tx, a.Workspace.ID)
		for _, r := range rows {
			item, err := s.notificationItem(ppl, r)
			if err != nil {
				return err
			}
			out.Notifications = append(out.Notifications, item)
		}
		if out.HasMore {
			out.Next = notificationCursor(rows[len(rows)-1])
		}
		counts, err := tx.UnreadNotifications(ctx, a.Workspace.ID, a.Member.ID)
		if err != nil {
			return err
		}
		for project, n := range counts {
			if q.ProjectID == "" || q.ProjectID == project {
				out.Unread += n
			}
		}
		out.Revision, err = tx.WorkspaceRevision(ctx, a.Workspace.ID)
		return err
	})
	return out, err
}

// MarkNotificationRead records that the viewer has read one of their notifications. It is idempotent: reading what is
// already read changes nothing, moves no revision, and returns the notification as it was first read. A notification of
// someone else's, or one that is no longer the viewer's to see, is not found.
func (s *Service) MarkNotificationRead(ctx context.Context, a Actor, id string) (NotificationItem, error) {
	var item NotificationItem
	moved := false
	err := s.db.Update(ctx, func(tx store.Tx) error {
		var err error
		if _, moved, err = tx.MarkNotificationRead(ctx, a.Workspace.ID, a.Member.ID, id, s.stamp()); err != nil {
			return err
		}
		row, err := tx.Notification(ctx, a.Workspace.ID, a.Member.ID, id)
		if err != nil {
			return err
		}
		item, err = s.notificationItem(newNameBook(ctx, tx, a.Workspace.ID), row)
		return err
	})
	if moved {
		s.changed(a.Workspace.ID, err)
	}
	return item, err
}
