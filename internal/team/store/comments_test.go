package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"werkbord/internal/sqlitekit"
	"werkbord/internal/team/domain"
)

// conv is a workspace with a project, two tickets and three people on the project (Ada, Bo, Cy), in a real database.
type conv struct {
	t             *testing.T
	db            *DB
	ws, project   string
	ticket, other string
	ada, bo, cy   string
}

func newConv(t *testing.T) *conv {
	t.Helper()
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "team.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	c := &conv{t: t, db: db, ws: "w1", project: "p1", ticket: "ttk_1", other: "ttk_2", ada: "tmb_ada", bo: "tmb_bo", cy: "tmb_cy"}
	c.update(func(tx Tx) error {
		if err := tx.InsertWorkspace(ctx, domain.Workspace{ID: c.ws, Name: "Acme", CreatedAt: t0}); err != nil {
			return err
		}
		for i, id := range []string{c.ada, c.bo, c.cy} {
			role := domain.RoleMember
			if i == 0 {
				role = domain.RoleOwner
			}
			if err := tx.InsertMember(ctx, domain.Member{ID: id, WorkspaceID: c.ws, Name: fmt.Sprintf("Person %d", i), Role: role, CreatedAt: t0}, "hash-"+id); err != nil {
				return err
			}
		}
		if err := tx.InsertProject(ctx, domain.Project{ID: c.project, WorkspaceID: c.ws, Name: "Shop", CreatedAt: t0, UpdatedAt: t0}); err != nil {
			return err
		}
		for _, id := range []string{c.ada, c.bo, c.cy} {
			if err := tx.AddProjectMember(ctx, c.ws, domain.ProjectMember{ProjectID: c.project, MemberID: id, AddedBy: c.ada, AddedAt: t0}); err != nil {
				return err
			}
		}
		for i, id := range []string{c.ticket, c.other} {
			if err := tx.InsertTicket(ctx, c.ws, domain.Ticket{ID: id, ProjectID: c.project, Number: i + 1, Title: "T", Status: domain.TicketBacklog, CreatorID: c.ada, CreatedAt: t0, UpdatedAt: t0}); err != nil {
				return err
			}
		}
		return nil
	})
	return c
}

func (c *conv) update(fn func(Tx) error) {
	c.t.Helper()
	if err := c.db.Update(context.Background(), fn); err != nil {
		c.t.Fatal(err)
	}
}

func (c *conv) view(fn func(Tx) error) {
	c.t.Helper()
	if err := c.db.View(context.Background(), fn); err != nil {
		c.t.Fatal(err)
	}
}

func (c *conv) revision() (n int64) {
	c.t.Helper()
	c.view(func(tx Tx) (err error) { n, err = tx.WorkspaceRevision(context.Background(), c.ws); return })
	return n
}

var commentN int

// post stores a comment by author on the ticket, mentioning the given people, and notifies them as the service does.
func (c *conv) post(ticket, author string, mentions ...string) domain.Comment {
	c.t.Helper()
	commentN++
	segs := []domain.Segment{{Type: domain.SegmentText, Text: fmt.Sprintf("comment %d ", commentN)}}
	for _, m := range mentions {
		segs = append(segs, domain.Segment{Type: domain.SegmentMention, MemberID: m, Name: "x"})
	}
	var out domain.Comment
	c.update(func(tx Tx) (err error) {
		out, err = tx.InsertComment(context.Background(), c.ws, domain.Comment{ID: domain.NewID(domain.PrefixComment), ProjectID: c.project, TicketID: ticket, AuthorID: author, AuthorName: "A",
			Segments: segs, ClientID: fmt.Sprintf("client-%04d", commentN), ClientDigest: domain.SegmentsDigest(segs), CreatedAt: t0.Add(time.Duration(commentN) * time.Second)})
		if err != nil {
			return err
		}
		for _, m := range mentions {
			if _, err := tx.NotifyMention(context.Background(), c.ws, domain.Notification{ID: domain.NewID(domain.PrefixNotification), ProjectID: c.project, TicketID: ticket, CommentID: out.ID,
				RecipientID: m, AuthorID: author, CreatedAt: out.CreatedAt}); err != nil {
				return err
			}
		}
		return nil
	})
	return out
}

func (c *conv) unread(member string) (n int) {
	c.t.Helper()
	c.view(func(tx Tx) error {
		counts, err := tx.UnreadNotifications(context.Background(), c.ws, member)
		for _, v := range counts {
			n += v
		}
		return err
	})
	return n
}

func seqs(cs []domain.Comment) (out []int64) {
	for _, c := range cs {
		out = append(out, c.Seq)
	}
	return out
}

func TestCommentsTakeTheNextPlaceInTheirTicketsConversation(t *testing.T) {
	c := newConv(t)
	a, b, other := c.post(c.ticket, c.ada), c.post(c.ticket, c.bo), c.post(c.other, c.ada)
	if a.Seq != 1 || b.Seq != 2 || other.Seq != 1 {
		t.Fatalf("places %d %d %d: each ticket counts from 1", a.Seq, b.Seq, other.Seq)
	}
	// The same author's client posting under the same creation id again is refused by the schema, and found by its id first.
	c.view(func(tx Tx) error {
		got, found, err := tx.CommentByClientID(context.Background(), c.ws, c.ticket, c.ada, a.ClientID)
		if err != nil || !found || got.ID != a.ID {
			t.Errorf("a posted comment was not found by its creation id: %v %v %v", got.ID, found, err)
		}
		if _, found, _ := tx.CommentByClientID(context.Background(), c.ws, c.ticket, c.bo, a.ClientID); found {
			t.Error("a creation id found somebody else's comment")
		}
		return nil
	})
	err := c.db.Update(context.Background(), func(tx Tx) error {
		_, err := tx.InsertComment(context.Background(), c.ws, domain.Comment{ID: domain.NewID(domain.PrefixComment), ProjectID: c.project, TicketID: c.ticket, AuthorID: c.ada, AuthorName: "A",
			Segments: a.Segments, ClientID: a.ClientID, ClientDigest: a.ClientDigest, CreatedAt: t0})
		return err
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a duplicate creation id: %v", err)
	}
}

func TestReadingAPageOfComments(t *testing.T) {
	c := newConv(t)
	for i := 0; i < 12; i++ {
		c.post(c.ticket, c.ada)
	}
	page := func(q CommentPageQuery) (p CommentPage) {
		c.view(func(tx Tx) (err error) { p, err = tx.CommentPage(context.Background(), c.ws, c.ticket, q); return })
		return p
	}
	latest := page(CommentPageQuery{Limit: 5})
	if !reflect.DeepEqual(seqs(latest.Comments), []int64{8, 9, 10, 11, 12}) || !latest.HasOlder || latest.HasNewer || latest.Live != 12 {
		t.Fatalf("latest: %v older=%v newer=%v live=%d", seqs(latest.Comments), latest.HasOlder, latest.HasNewer, latest.Live)
	}
	older := page(CommentPageQuery{Limit: 5, Before: 8})
	if !reflect.DeepEqual(seqs(older.Comments), []int64{3, 4, 5, 6, 7}) || !older.HasOlder || !older.HasNewer {
		t.Fatalf("before 8: %v older=%v newer=%v", seqs(older.Comments), older.HasOlder, older.HasNewer)
	}
	oldest := page(CommentPageQuery{Limit: 5, Before: 3})
	if !reflect.DeepEqual(seqs(oldest.Comments), []int64{1, 2}) || oldest.HasOlder || !oldest.HasNewer {
		t.Fatalf("before 3: %v older=%v newer=%v", seqs(oldest.Comments), oldest.HasOlder, oldest.HasNewer)
	}
	newer := page(CommentPageQuery{Limit: 5, After: 4, HasAfter: true})
	if !reflect.DeepEqual(seqs(newer.Comments), []int64{5, 6, 7, 8, 9}) || !newer.HasOlder || !newer.HasNewer {
		t.Fatalf("after 4: %v older=%v newer=%v", seqs(newer.Comments), newer.HasOlder, newer.HasNewer)
	}
	end := page(CommentPageQuery{Limit: 5, After: 12, HasAfter: true})
	if len(end.Comments) != 0 || !end.HasOlder || end.HasNewer {
		t.Fatalf("after the last: %v older=%v newer=%v", seqs(end.Comments), end.HasOlder, end.HasNewer)
	}
	// Zero is a place too: after nothing is the start of the conversation (not the latest comments).
	if start := page(CommentPageQuery{Limit: 5, After: 0, HasAfter: true}); !reflect.DeepEqual(seqs(start.Comments), []int64{1, 2, 3, 4, 5}) || start.HasOlder || !start.HasNewer {
		t.Fatalf("after 0: %v older=%v newer=%v", seqs(start.Comments), start.HasOlder, start.HasNewer)
	}
	around := page(CommentPageQuery{Limit: 5, Around: 6})
	if !reflect.DeepEqual(seqs(around.Comments), []int64{4, 5, 6, 7, 8}) || !around.HasOlder || !around.HasNewer {
		t.Fatalf("around 6: %v older=%v newer=%v", seqs(around.Comments), around.HasOlder, around.HasNewer)
	}
	// Near an end the window is filled from the other side.
	if got := page(CommentPageQuery{Limit: 5, Around: 12}); !reflect.DeepEqual(seqs(got.Comments), []int64{8, 9, 10, 11, 12}) || got.HasNewer || !got.HasOlder {
		t.Fatalf("around the last: %v older=%v newer=%v", seqs(got.Comments), got.HasOlder, got.HasNewer)
	}
	if got := page(CommentPageQuery{Limit: 5, Around: 1}); !reflect.DeepEqual(seqs(got.Comments), []int64{1, 2, 3, 4, 5}) || got.HasOlder || !got.HasNewer {
		t.Fatalf("around the first: %v older=%v newer=%v", seqs(got.Comments), got.HasOlder, got.HasNewer)
	}
	// A page larger than the maximum, or none asked for, is the default size, and a short conversation is all there is.
	if all := page(CommentPageQuery{}); len(all.Comments) != 12 || all.HasOlder {
		t.Fatalf("default page: %v", seqs(all.Comments))
	}
}

func TestEditingCommentsIsCompareAndSwapAndDeletingLeavesATombstone(t *testing.T) {
	ctx := context.Background()
	c := newConv(t)
	first := c.post(c.ticket, c.ada, c.bo)
	second := c.post(c.ticket, c.ada)
	edited := t0.Add(time.Hour)
	first.Segments = []domain.Segment{{Type: domain.SegmentText, Text: "changed"}}
	first.EditedAt = &edited
	c.update(func(tx Tx) error {
		saved, err := tx.SaveComment(ctx, c.ws, first, 1)
		if err != nil || saved.Version != 2 {
			t.Fatalf("save: %v %+v", err, saved)
		}
		// The same version again is a conflict: somebody else's edit came first.
		if _, err := tx.SaveComment(ctx, c.ws, first, 1); !errors.Is(err, domain.ErrConflict) {
			t.Errorf("a stale edit: %v", err)
		}
		// Who it mentions follows what it says.
		if got, _ := tx.CommentMentions(ctx, c.ws, first.ID); len(got) != 0 {
			t.Errorf("an edit that took out the mention left it: %v", got)
		}
		gone := first
		gone.ID = "tcm_unknown"
		if _, err := tx.SaveComment(ctx, c.ws, gone, 1); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("editing what is not there: %v", err)
		}
		return nil
	})
	c.update(func(tx Tx) error {
		t1, changed, err := tx.TombstoneComment(ctx, c.ws, second, domain.DeletedByManager, 1, t0.Add(2*time.Hour))
		if err != nil || !changed || !t1.Deleted() || t1.DeletedBy != domain.DeletedByManager || len(t1.Segments) != 0 || t1.Version != 2 {
			t.Fatalf("tombstone: %+v %v %v", t1, changed, err)
		}
		again, changed, err := tx.TombstoneComment(ctx, c.ws, second, domain.DeletedByAuthor, 2, t0.Add(3*time.Hour))
		if err != nil || changed || again.DeletedBy != domain.DeletedByManager {
			t.Errorf("deleting twice changed something: %+v %v %v", again, changed, err)
		}
		// A deleted comment can no longer be edited.
		second.EditedAt = &edited
		if _, err := tx.SaveComment(ctx, c.ws, second, 2); !errors.Is(err, domain.ErrConflict) {
			t.Errorf("editing a tombstone: %v", err)
		}
		return nil
	})
	c.view(func(tx Tx) error {
		p, err := tx.CommentPage(ctx, c.ws, c.ticket, CommentPageQuery{})
		if err != nil || len(p.Comments) != 2 || p.Live != 1 || !p.Comments[1].Deleted() || p.Comments[1].Seq != 2 {
			t.Errorf("the tombstone must keep its place: %+v %v", seqs(p.Comments), err)
		}
		return nil
	})
}

func TestOneNotificationPerCommentAndPersonAndAReadOneStaysRead(t *testing.T) {
	ctx := context.Background()
	c := newConv(t)
	k := c.post(c.ticket, c.ada, c.bo)
	if c.unread(c.bo) != 1 || c.unread(c.cy) != 0 || c.unread(c.ada) != 0 {
		t.Fatalf("who is waiting: bo %d cy %d ada %d", c.unread(c.bo), c.unread(c.cy), c.unread(c.ada))
	}
	var id string
	c.view(func(tx Tx) error {
		rows, err := tx.Notifications(ctx, c.ws, c.bo, NotificationQuery{})
		if err != nil || len(rows) != 1 || rows[0].CommentID != k.ID || rows[0].TicketNumber != 1 || rows[0].ProjectName != "Shop" {
			t.Fatalf("notifications: %+v %v", rows, err)
		}
		id = rows[0].ID
		return nil
	})
	// Notifying again, as an edit that mentions the person once more would, makes no second one.
	c.update(func(tx Tx) error {
		made, err := tx.NotifyMention(ctx, c.ws, domain.Notification{ID: domain.NewID(domain.PrefixNotification), ProjectID: c.project, TicketID: c.ticket, CommentID: k.ID, RecipientID: c.bo, AuthorID: c.ada, CreatedAt: t0.Add(time.Hour)})
		if err != nil || made {
			t.Errorf("a second notification for the same comment and person: made=%v %v", made, err)
		}
		return nil
	})
	// Reading it is idempotent, moves the workspace revision once, and the second call changes nothing.
	before := c.revision()
	c.update(func(tx Tx) error {
		n, changed, err := tx.MarkNotificationRead(ctx, c.ws, c.bo, id, t0.Add(2*time.Hour))
		if err != nil || !changed || n.ReadAt == nil {
			t.Fatalf("mark read: %+v %v %v", n, changed, err)
		}
		return nil
	})
	after := c.revision()
	if after <= before {
		t.Fatalf("marking a notification read did not move the workspace revision (%d -> %d)", before, after)
	}
	c.update(func(tx Tx) error {
		n, changed, err := tx.MarkNotificationRead(ctx, c.ws, c.bo, id, t0.Add(3*time.Hour))
		if err != nil || changed || n.ReadAt == nil || !n.ReadAt.Equal(t0.Add(2*time.Hour)) {
			t.Errorf("reading it again: %+v %v %v (it must keep the time it was first read)", n, changed, err)
		}
		return nil
	})
	if got := c.revision(); got != after {
		t.Errorf("reading what was already read moved the revision (%d -> %d)", after, got)
	}
	if c.unread(c.bo) != 0 {
		t.Error("a read notification is still counted")
	}
	// Mentioning them again does not reopen it.
	c.update(func(tx Tx) error {
		if made, _ := tx.NotifyMention(ctx, c.ws, domain.Notification{ID: domain.NewID(domain.PrefixNotification), ProjectID: c.project, TicketID: c.ticket, CommentID: k.ID, RecipientID: c.bo, AuthorID: c.ada, CreatedAt: t0}); made {
			t.Error("a read notification was reopened")
		}
		return nil
	})
	if c.unread(c.bo) != 0 {
		t.Error("a read notification came back")
	}
	// Somebody else cannot read, or even find, it.
	err := c.db.Update(ctx, func(tx Tx) error { _, _, err := tx.MarkNotificationRead(ctx, c.ws, c.cy, id, t0); return err })
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("another member marking it read: %v", err)
	}
	c.view(func(tx Tx) error {
		if _, err := tx.Notification(ctx, c.ws, c.cy, id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("another member found it: %v", err)
		}
		if got, _ := tx.Notifications(ctx, c.ws, c.cy, NotificationQuery{}); len(got) != 0 {
			t.Errorf("another member lists it: %v", got)
		}
		return nil
	})
	// The unread filter leaves out what is read; asking for everything does not.
	c.view(func(tx Tx) error {
		unread, _ := tx.Notifications(ctx, c.ws, c.bo, NotificationQuery{UnreadOnly: true})
		all, _ := tx.Notifications(ctx, c.ws, c.bo, NotificationQuery{})
		if len(unread) != 0 || len(all) != 1 {
			t.Errorf("unread %d, all %d", len(unread), len(all))
		}
		return nil
	})
}

func TestAWithdrawnNotificationIsGoneUntilTheMentionIsMadeAgain(t *testing.T) {
	ctx := context.Background()
	c := newConv(t)
	k := c.post(c.ticket, c.ada, c.bo, c.cy)
	c.update(func(tx Tx) error { return tx.WithdrawNotifications(ctx, c.ws, k.ID, []string{c.bo}) })
	if c.unread(c.bo) != 0 || c.unread(c.cy) != 1 {
		t.Fatalf("withdrawing Bo's: bo %d cy %d", c.unread(c.bo), c.unread(c.cy))
	}
	// Reading a withdrawn notification is as if it never existed.
	c.view(func(tx Tx) error {
		if got, _ := tx.Notifications(ctx, c.ws, c.bo, NotificationQuery{}); len(got) != 0 {
			t.Errorf("a withdrawn notification is listed: %v", got)
		}
		return nil
	})
	// An empty list withdraws nothing; nil withdraws everything.
	c.update(func(tx Tx) error { return tx.WithdrawNotifications(ctx, c.ws, k.ID, []string{}) })
	if c.unread(c.cy) != 1 {
		t.Error("withdrawing nobody took Cy's away")
	}
	// Put the mention back: it comes back, unread, as new.
	c.update(func(tx Tx) error {
		made, err := tx.NotifyMention(ctx, c.ws, domain.Notification{ID: domain.NewID(domain.PrefixNotification), ProjectID: c.project, TicketID: c.ticket, CommentID: k.ID, RecipientID: c.bo, AuthorID: c.ada, CreatedAt: t0.Add(time.Hour)})
		if err != nil || !made {
			t.Errorf("a mention made again: %v %v", made, err)
		}
		return nil
	})
	if c.unread(c.bo) != 1 {
		t.Error("the renewed notification is not waiting")
	}
	c.update(func(tx Tx) error { return tx.WithdrawNotifications(ctx, c.ws, k.ID, nil) })
	if c.unread(c.bo) != 0 || c.unread(c.cy) != 0 {
		t.Error("withdrawing for everyone left some")
	}
}

// Losing access to a project takes the person's notifications on it away for good, however they leave, and being added
// again does not bring them back. The database does this, not the code that happens to remove the person.
func TestLeavingAProjectWithdrawsItsNotificationsForGood(t *testing.T) {
	ctx := context.Background()
	c := newConv(t)
	c.post(c.ticket, c.ada, c.bo)
	// A project of the same workspace that Bo is also on keeps its own.
	var otherProject string = "p2"
	c.update(func(tx Tx) error {
		if err := tx.InsertProject(ctx, domain.Project{ID: otherProject, WorkspaceID: c.ws, Name: "Blog", CreatedAt: t0, UpdatedAt: t0}); err != nil {
			return err
		}
		for _, id := range []string{c.ada, c.bo} {
			if err := tx.AddProjectMember(ctx, c.ws, domain.ProjectMember{ProjectID: otherProject, MemberID: id, AddedBy: c.ada, AddedAt: t0}); err != nil {
				return err
			}
		}
		return tx.InsertTicket(ctx, c.ws, domain.Ticket{ID: "ttk_3", ProjectID: otherProject, Number: 3, Title: "B", Status: domain.TicketBacklog, CreatorID: c.ada, CreatedAt: t0, UpdatedAt: t0})
	})
	commentN++
	c.update(func(tx Tx) error {
		segs := []domain.Segment{{Type: domain.SegmentMention, MemberID: c.bo, Name: "x"}}
		k, err := tx.InsertComment(ctx, c.ws, domain.Comment{ID: domain.NewID(domain.PrefixComment), ProjectID: otherProject, TicketID: "ttk_3", AuthorID: c.ada, AuthorName: "A", Segments: segs,
			ClientID: fmt.Sprintf("client-%04d", commentN), ClientDigest: "d", CreatedAt: t0})
		if err != nil {
			return err
		}
		_, err = tx.NotifyMention(ctx, c.ws, domain.Notification{ID: domain.NewID(domain.PrefixNotification), ProjectID: otherProject, TicketID: "ttk_3", CommentID: k.ID, RecipientID: c.bo, AuthorID: c.ada, CreatedAt: t0})
		return err
	})
	if c.unread(c.bo) != 2 {
		t.Fatalf("Bo waits for %d, want 2", c.unread(c.bo))
	}
	c.update(func(tx Tx) error {
		_, err := tx.RemoveProjectMember(ctx, c.ws, c.project, c.bo)
		return err
	})
	if c.unread(c.bo) != 1 {
		t.Fatalf("after leaving one project Bo waits for %d, want only the other project's", c.unread(c.bo))
	}
	c.update(func(tx Tx) error {
		return tx.AddProjectMember(ctx, c.ws, domain.ProjectMember{ProjectID: c.project, MemberID: c.bo, AddedBy: c.ada, AddedAt: t0.Add(time.Hour)})
	})
	if c.unread(c.bo) != 1 {
		t.Fatalf("coming back to the project brought %d notifications back", c.unread(c.bo)-1)
	}
	c.view(func(tx Tx) error {
		rows, _ := tx.Notifications(ctx, c.ws, c.bo, NotificationQuery{ProjectID: c.project})
		if len(rows) != 0 {
			t.Errorf("old notifications resurfaced on rejoining: %+v", rows)
		}
		return nil
	})
}

// A notification that exists for a person who is not on the project is not shown, even if nothing withdrew it.
func TestANotificationIsOnlyShownWhileItsRecipientIsOnTheProject(t *testing.T) {
	ctx := context.Background()
	c := newConv(t)
	c.post(c.ticket, c.ada, c.bo)
	raw := func(q string, args ...any) {
		c.t.Helper()
		c.update(func(tx Tx) error { _, err := tx.(*sqlTx).q.ExecContext(ctx, q, args...); return err })
	}
	raw(`DELETE FROM project_members WHERE member_id = ?`, c.bo)
	raw(`UPDATE comment_notifications SET withdrawn = 0`) // as if nothing had withdrawn it
	if c.unread(c.bo) != 0 {
		t.Error("an unread notification of a project its recipient is not on is counted")
	}
	c.view(func(tx Tx) error {
		rows, err := tx.Notifications(ctx, c.ws, c.bo, NotificationQuery{})
		if err != nil || len(rows) != 0 {
			t.Errorf("listed: %+v %v", rows, err)
		}
		if _, err := tx.Notification(ctx, c.ws, c.bo, rows0(tx, c)); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("found: %v", err)
		}
		return nil
	})
}

// rows0 is the ID of the one notification there is.
func rows0(tx Tx, c *conv) string {
	var id string
	_ = tx.(*sqlTx).q.QueryRowContext(context.Background(), `SELECT id FROM comment_notifications LIMIT 1`).Scan(&id)
	return id
}

func TestRemovingAMemberRemovesTheirNotificationsButNotTheCommentsTheyWrote(t *testing.T) {
	ctx := context.Background()
	c := newConv(t)
	byBo := c.post(c.ticket, c.bo, c.cy)
	c.post(c.ticket, c.ada, c.bo)
	c.update(func(tx Tx) error { return tx.DeleteMember(ctx, c.ws, c.bo) })
	c.view(func(tx Tx) error {
		got, err := tx.Comment(ctx, c.ws, c.project, c.ticket, byBo.ID)
		if err != nil || got.AuthorID != c.bo || got.AuthorName != "A" {
			t.Errorf("the comment of a member who left must stay, with its author: %+v %v", got, err)
		}
		var n int
		if err := tx.(*sqlTx).q.QueryRowContext(ctx, `SELECT COUNT(*) FROM comment_notifications WHERE recipient_id = ?`, c.bo).Scan(&n); err != nil || n != 0 {
			t.Errorf("notifications of a member who left: %d %v", n, err)
		}
		if mentions, _ := tx.CommentMentions(ctx, c.ws, byBo.ID); len(mentions) != 1 || mentions[0] != c.cy {
			t.Errorf("mentions: %v", mentions)
		}
		return nil
	})
	if c.unread(c.cy) != 1 {
		t.Error("Cy's notification went with Bo")
	}
}

func TestTheSchemaRefusesImpossibleComments(t *testing.T) {
	ctx := context.Background()
	c := newConv(t)
	k := c.post(c.ticket, c.ada)
	exec := func(q string, a ...any) error {
		return c.db.pool.Update(ctx, func(tx *sql.Tx) error { _, err := tx.ExecContext(ctx, q, a...); return err })
	}
	insert := func(id, project, ticket string, seq int, deletedAt any, deletedBy string) error {
		return exec(`INSERT INTO ticket_comments (id, workspace_id, project_id, ticket_id, seq, author_id, author_name, client_id, client_digest, created_at, deleted_at, deleted_by)
			VALUES (?, 'w1', ?, ?, ?, 'tmb_ada', 'A', ?, 'd', 1, ?, ?)`, id, project, ticket, seq, "client-"+id, deletedAt, deletedBy)
	}
	for name, err := range map[string]error{
		"a place already taken":       insert("tcm_a", c.project, c.ticket, int(k.Seq), nil, ""),
		"place zero":                  insert("tcm_b", c.project, c.ticket, 0, nil, ""),
		"a ticket that is not there":  insert("tcm_c", c.project, "ttk_none", 1, nil, ""),
		"a project that is not there": insert("tcm_d", "p_none", c.ticket, 9, nil, ""),
		"deleted by nobody":           insert("tcm_e", c.project, c.ticket, 9, 5, ""),
		"deleted by somebody, never":  insert("tcm_f", c.project, c.ticket, 9, nil, "author"),
		"deleted by an unknown party": insert("tcm_g", c.project, c.ticket, 9, 5, "stranger"),
	} {
		if err == nil {
			t.Errorf("accepted: %s", name)
		}
	}
	if err := exec(`INSERT INTO comment_notifications (id, workspace_id, project_id, ticket_id, comment_id, recipient_id, author_id, created_at) VALUES ('tnt_x', 'w1', ?, ?, ?, 'tmb_nobody', 'tmb_ada', 1)`, c.project, c.ticket, k.ID); err == nil {
		t.Error("a notification for somebody who is not in the workspace")
	}
	if err := exec(`INSERT INTO comment_notifications (id, workspace_id, project_id, ticket_id, comment_id, recipient_id, author_id, created_at, withdrawn) VALUES ('tnt_y', 'w1', ?, ?, ?, 'tmb_bo', 'tmb_ada', 1, 2)`, c.project, c.ticket, k.ID); err == nil {
		t.Error("a notification withdrawn two ways")
	}
}

// Deleting a ticket or a project takes its conversation, mentions and notifications with it.
func TestTheConversationLeavesWithItsTicket(t *testing.T) {
	ctx := context.Background()
	c := newConv(t)
	c.post(c.ticket, c.ada, c.bo)
	c.update(func(tx Tx) error {
		_, err := tx.(*sqlTx).q.ExecContext(ctx, `DELETE FROM tickets WHERE id = ?`, c.ticket)
		return err
	})
	c.view(func(tx Tx) error {
		for _, table := range []string{"ticket_comments", "comment_mentions", "comment_notifications"} {
			var n int
			if err := tx.(*sqlTx).q.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil || n != 0 {
				t.Errorf("%s keeps %d rows of a deleted ticket (%v)", table, n, err)
			}
		}
		return nil
	})
}

// The migration is additive: a database from before it keeps everything, gains the tables, and works.
func TestTheCommentsMigrationUpgradesADatabaseInPlace(t *testing.T) {
	ctx := context.Background()
	ms, _ := Migrations()
	target := migrationNamed(t, ms, "ticket_comments")
	path := filepath.Join(t.TempDir(), "team.db")
	old, err := sqlitekit.Open(ctx, path, sqlitekit.Options{Migrations: ms[:target], Product: productName})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO workspaces(id,name,created_at) VALUES('w','Team',1)`,
		`INSERT INTO members(id,workspace_id,name,role,token_hash,created_at) VALUES('m','w','Ada','owner','hash',1)`,
		`INSERT INTO projects(id,workspace_id,name,created_at,updated_at) VALUES('p','w','Repo',1,1)`,
		`INSERT INTO project_members(project_id,member_id,workspace_id,added_by,added_at,role) VALUES('p','m','w','m',1,'owner')`,
		`INSERT INTO tickets(id,workspace_id,project_id,number,title,status,creator_id,created_at,updated_at) VALUES('t','w','p',1,'Work','available','m',1,1)`,
	} {
		if _, err := old.Writer.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	var revision int64
	_ = old.Writer.QueryRowContext(ctx, `SELECT revision FROM workspaces WHERE id='w'`).Scan(&revision)
	_ = old.Close()

	db, err := Open(ctx, path, nil)
	if err != nil {
		t.Fatal("the upgrade failed:", err)
	}
	defer db.Close()
	if v, _ := db.SchemaVersion(ctx); v != len(ms) {
		t.Fatalf("schema version %d, want %d", v, len(ms))
	}
	err = db.Update(ctx, func(tx Tx) error {
		k, err := tx.Ticket(ctx, "w", "p", "t")
		if err != nil || k.Title != "Work" {
			t.Fatalf("the ticket did not survive: %+v %v", k, err)
		}
		if p, err := tx.CommentPage(ctx, "w", "t", CommentPageQuery{}); err != nil || len(p.Comments) != 0 || p.Live != 0 {
			t.Fatalf("a ticket from before has comments: %+v %v", p, err)
		}
		n, err := tx.UnreadNotifications(ctx, "w", "m")
		if err != nil || len(n) != 0 {
			t.Fatalf("notifications from nowhere: %v %v", n, err)
		}
		c, err := tx.InsertComment(ctx, "w", domain.Comment{ID: "tcm_1", ProjectID: "p", TicketID: "t", AuthorID: "m", AuthorName: "Ada",
			Segments: []domain.Segment{{Type: domain.SegmentText, Text: "first"}}, ClientID: "client-0001", ClientDigest: "d", CreatedAt: t0})
		if err != nil || c.Seq != 1 {
			t.Fatalf("the first comment on an upgraded ticket: %+v %v", c, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if rev := func() (n int64) {
		_ = db.View(ctx, func(tx Tx) (err error) { n, err = tx.WorkspaceRevision(ctx, "w"); return })
		return n
	}(); rev <= revision {
		t.Errorf("a comment did not move the workspace revision (%d -> %d)", revision, rev)
	}
	err = db.View(ctx, func(tx Tx) error {
		rows, err := tx.(*sqlTx).q.QueryContext(ctx, `PRAGMA foreign_key_check`)
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			t.Error("the upgrade left a broken foreign key")
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
}
