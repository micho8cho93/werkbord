package service

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devboard/internal/team/domain"
)

// report records a pull request (and a commit) on a ticket the actor holds.
func (tm *team) report(a Actor, k domain.Ticket, pr domain.PullRequest) domain.Ticket {
	tm.t.Helper()
	commits := []domain.Commit{{SHA: shaOne, Subject: "Fix the thing", Author: a.Member.Name, CommittedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)}}
	ahead, behind := 1, pr.Behind
	if behind < 0 {
		behind = 0
	}
	got, err := tm.svc.ReportGit(bg, a, tm.pid(), k.ID, GitReport{Commits: &commits, PullRequest: &pr,
		State: &BranchState{HeadSHA: shaOne, BaseBranch: "main", Ahead: &ahead, Behind: &behind, LastCommitAt: time.Now(), Files: []string{"auth/token.go"}}})
	if err != nil {
		tm.t.Fatal(err)
	}
	return got
}

func openPR() domain.PullRequest {
	return domain.PullRequest{Number: 7, URL: prURL, State: domain.PROpen, Mergeable: domain.MergeClean, BaseBranch: "main", Behind: 0}
}

func actionKinds(w MyWork) map[string]Action {
	out := map[string]Action{}
	for _, a := range w.NeedsAction {
		out[a.Kind+":"+a.TicketKey] = a
	}
	return out
}

// ---- My Work ----

func TestMyWorkShowsWhatTheMemberIsDoingAcrossProjects(t *testing.T) {
	tm := newTeam(t)
	other := tm.project2Named("Docs")
	if err := tm.svc.AddProjectMember(bg, tm.owner, other.ID, tm.bo.Member.ID, ""); err != nil {
		t.Fatal(err)
	}
	// Bo claims one ticket here, is given one by Ada, has submitted a third, and has one in the other project.
	claimed := tm.claimed(tm.bo, "Fix login")
	given := tm.ticket(tm.owner, "Add audit log", domain.TicketAvailable)
	if _, err := tm.svc.AssignTicket(bg, tm.owner, tm.pid(), given.ID, tm.bo.Member.ID); err != nil {
		t.Fatal(err)
	}
	sub := tm.claimed(tm.bo, "Rotate keys")
	tm.report(tm.bo, sub, openPR())
	if _, err := tm.svc.SubmitTicket(bg, tm.bo, tm.pid(), sub.ID, SubmitInput{}); err != nil {
		t.Fatal(err)
	}
	dk, err := tm.svc.CreateTicket(bg, tm.owner, other.ID, TicketInput{Title: "Write the guide", Status: domain.TicketAvailable})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.ClaimTicket(bg, tm.bo, other.ID, dk.ID); err != nil {
		t.Fatal(err)
	}
	// Cy's work must not appear.
	cyK := tm.claimed(tm.cy, "Cy's thing")

	w, err := tm.svc.MyWork(bg, tm.bo)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.InProgress) != 3 || len(w.Submitted) != 1 {
		t.Fatalf("in progress %d, submitted %d", len(w.InProgress), len(w.Submitted))
	}
	byKey := map[string]WorkItem{}
	for _, it := range append(append([]WorkItem{}, w.InProgress...), w.Submitted...) {
		byKey[it.Ticket.Key] = it
		if it.Ticket.AssigneeID != tm.bo.Member.ID || it.Ticket.Key == cyK.Key {
			t.Fatalf("someone else's work in My Work: %+v", it.Ticket)
		}
	}
	if it := byKey[given.Key]; it.AssignedBy != "Ada" || it.Author != "Bo" {
		t.Fatalf("assigned item: %+v", it)
	}
	if it := byKey[claimed.Key]; it.AssignedBy != "" || it.Merge != MergeNone {
		t.Fatalf("claimed item: %+v", it)
	}
	if it := byKey[dk.Key]; it.Project.Name != "Docs" {
		t.Fatalf("project not carried: %+v", it.Project)
	}
	if len(w.PullRequests) != 1 || w.PullRequests[0].Ticket.Key != sub.Key || w.PullRequests[0].Merge != MergeMergeable || len(w.PullRequests[0].Ticket.Commits) != 1 {
		t.Fatalf("pull requests: %+v", w.PullRequests)
	}
	if len(w.Repositories) != 2 {
		t.Fatalf("repositories: %+v", w.Repositories)
	}
	for _, r := range w.Repositories {
		if r.Project.ID != tm.pid() {
			continue
		}
		if len(r.Branches) == 0 {
			t.Fatalf("no branches for the member's own tickets: %+v", r)
		}
		for _, b := range r.Branches {
			if b.TicketKey == cyK.Key {
				t.Fatalf("Cy's branch shown in Bo's work: %+v", b)
			}
		}
	}
	// Cy sees only Cy's.
	cw, _ := tm.svc.MyWork(bg, tm.cy)
	if len(cw.InProgress) != 1 || cw.InProgress[0].Ticket.Key != cyK.Key || len(cw.Submitted) != 0 {
		t.Fatalf("%+v", cw)
	}
	// Nobody else's work shows through a project the viewer is not on.
	outsider, _ := tm.member(tm.owner, "Eve")
	if ew, _ := tm.svc.MyWork(bg, outsider); len(ew.InProgress)+len(ew.Submitted)+len(ew.Repositories) != 0 {
		t.Fatalf("%+v", ew)
	}
}

func TestMyWorkSaysWhatWaitsForTheMember(t *testing.T) {
	tm := newTeam(t)
	k := tm.claimed(tm.bo, "Fix login")
	// Open pull request, conflicts, behind: the worst first.
	pr := openPR()
	pr.Mergeable, pr.Behind = domain.MergeConflicting, 4
	tm.report(tm.bo, k, pr)
	w, _ := tm.svc.MyWork(bg, tm.bo)
	got := actionKinds(w)
	for _, want := range []string{"conflict:" + k.Key, "behind:" + k.Key} {
		if _, ok := got[want]; !ok {
			t.Fatalf("missing %s in %+v", want, w.NeedsAction)
		}
	}
	if _, ok := got["submit:"+k.Key]; ok {
		t.Fatal("told to submit a pull request that has conflicts")
	}
	if w.NeedsAction[0].Level != "problem" {
		t.Fatalf("the problem does not come first: %+v", w.NeedsAction)
	}
	// Once the conflicts are resolved it is ready to submit.
	tm.report(tm.bo, k, openPR())
	w, _ = tm.svc.MyWork(bg, tm.bo)
	if _, ok := actionKinds(w)["submit:"+k.Key]; !ok {
		t.Fatalf("a clean open pull request should suggest submitting: %+v", w.NeedsAction)
	}
	// Submitted, then sent back with a note: the note reaches the author.
	if _, err := tm.svc.SubmitTicket(bg, tm.bo, tm.pid(), k.ID, SubmitInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.RequestChanges(bg, tm.di, tm.pid(), k.ID, "Handle expired tokens"); err != nil {
		t.Fatal(err)
	}
	w, _ = tm.svc.MyWork(bg, tm.bo)
	a, ok := actionKinds(w)["changes_requested:"+k.Key]
	if !ok || !strings.Contains(a.Message, "Handle expired tokens") {
		t.Fatalf("changes requested not surfaced: %+v", w.NeedsAction)
	}
	if _, ok := actionKinds(w)["submit:"+k.Key]; ok {
		t.Fatal("told to submit a ticket that was just sent back")
	}
	// Resubmitted: nothing of that kind waits any more.
	if _, err := tm.svc.SubmitTicket(bg, tm.bo, tm.pid(), k.ID, SubmitInput{}); err != nil {
		t.Fatal(err)
	}
	w, _ = tm.svc.MyWork(bg, tm.bo)
	if _, ok := actionKinds(w)["changes_requested:"+k.Key]; ok || len(w.Submitted) != 1 {
		t.Fatalf("%+v", w)
	}

	// The reviewer is asked, and then is told when the pull request is merged.
	k2 := tm.claimed(tm.cy, "Another")
	tm.report(tm.cy, k2, openPR2())
	if _, err := tm.svc.SubmitTicket(bg, tm.cy, tm.pid(), k2.ID, SubmitInput{ReviewerID: tm.di.Member.ID}); err != nil {
		t.Fatal(err)
	}
	dw, _ := tm.svc.MyWork(bg, tm.di)
	if _, ok := actionKinds(dw)["review_requested:"+k2.Key]; !ok || dw.ReviewsWaiting != 2 {
		t.Fatalf("%d waiting, %+v", dw.ReviewsWaiting, dw.NeedsAction)
	}
	merged := openPR2()
	merged.State = domain.PRMerged
	if _, err := tm.svc.ReportGit(bg, tm.di, tm.pid(), k2.ID, GitReport{PullRequest: &merged}); err != nil {
		t.Fatal(err)
	}
	dw, _ = tm.svc.MyWork(bg, tm.di)
	if _, ok := actionKinds(dw)["ready_to_complete:"+k2.Key]; !ok {
		t.Fatalf("%+v", dw.NeedsAction)
	}
	cw, _ := tm.svc.MyWork(bg, tm.cy)
	if _, ok := actionKinds(cw)["merged:"+k2.Key]; !ok {
		t.Fatalf("%+v", cw.NeedsAction)
	}
}

func openPR2() domain.PullRequest {
	pr := openPR()
	pr.Number, pr.URL = 8, "https://github.com/acme/shop/pull/8"
	return pr
}

func TestAStaleTicketAsksToBeReleased(t *testing.T) {
	tm := newTeam(t)
	k := tm.claimed(tm.bo, "Slow one")
	tm.svc.now = func() time.Time { return time.Now().Add(StaleAfter + 24*time.Hour) }
	w, _ := tm.svc.MyWork(bg, tm.bo)
	if _, ok := actionKinds(w)["stale:"+k.Key]; !ok {
		t.Fatalf("%+v", w.NeedsAction)
	}
}

// ---- Reviews ----

func TestReviewsListsWhatAReviewerMayDecide(t *testing.T) {
	tm := newTeam(t)
	k1 := tm.claimed(tm.bo, "First")
	tm.report(tm.bo, k1, openPR())
	k2 := tm.claimed(tm.cy, "Second")
	conf := openPR2()
	conf.Mergeable = domain.MergeConflicting
	tm.report(tm.cy, k2, conf)
	time.Sleep(3 * time.Millisecond) // distinct submission times
	for _, s := range []struct {
		a   Actor
		k   domain.Ticket
		rev string
	}{{tm.cy, k2, tm.di.Member.ID}, {tm.bo, k1, ""}} {
		if _, err := tm.svc.SubmitTicket(bg, s.a, tm.pid(), s.k.ID, SubmitInput{ReviewerID: s.rev}); err != nil {
			t.Fatal(err)
		}
	}
	q, err := tm.svc.Reviews(bg, tm.di)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Items) != 2 {
		t.Fatalf("%d items", len(q.Items))
	}
	// Asked-of-me first, even though it was submitted second to last.
	first := q.Items[0]
	if first.Ticket.Key != k2.Key || !first.RequestedOfMe || first.Merge != MergeConflicting || first.CanComplete || first.Blocker == "" {
		t.Fatalf("%+v", first)
	}
	second := q.Items[1]
	if second.Ticket.Key != k1.Key || second.Author != "Bo" || second.Ticket.Branch == "" || len(second.Ticket.Commits) != 1 || second.Ticket.PullRequest == nil ||
		second.Merge != MergeMergeable || second.Mine || !second.CanReview || second.CanComplete ||
		!strings.Contains(second.Blocker, "still open") || second.Links.PullRequest != prURL {
		t.Fatalf("%+v", second)
	}
	// Board uses the identical gate, including open PRs and self-review.
	for _, actor := range []Actor{tm.di, tm.bo, tm.cy} {
		board, err := tm.svc.Board(bg, actor, tm.pid())
		if err != nil {
			t.Fatal(err)
		}
		reviews, err := tm.svc.Reviews(bg, actor)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range reviews.Items {
			reason, exists := board.Completion[item.Ticket.ID]
			if !exists || reason != item.Blocker || (reason == "") != item.CanComplete {
				t.Fatalf("Board/Reviews disagree for %s: board=%q review=%+v", actor.Member.ID, reason, item)
			}
		}
	}
	// Once the pull request is merged on the Git host and the merge is recorded, the reviewer may finish it.
	merged := openPR()
	merged.State = domain.PRMerged
	if _, err := tm.svc.ReportGit(bg, tm.di, tm.pid(), k1.ID, GitReport{PullRequest: &merged}); err != nil {
		t.Fatal(err)
	}
	q, _ = tm.svc.Reviews(bg, tm.di)
	for _, it := range q.Items {
		if it.Ticket.Key == k1.Key && (!it.CanComplete || it.Blocker != "" || it.Merge != MergeMerged) {
			t.Fatalf("%+v", it)
		}
	}
	// The author sees their own submission, flagged as theirs and not theirs to approve.
	bq, _ := tm.svc.Reviews(bg, tm.bo)
	if len(bq.Items) != 1 || !bq.Items[0].Mine || bq.Items[0].CanReview || bq.Items[0].CanComplete {
		t.Fatalf("%+v", bq.Items)
	}
	// A plain member sees reviews they can do nothing about only if they are theirs.
	cq, _ := tm.svc.Reviews(bg, tm.cy)
	if len(cq.Items) != 1 || cq.Items[0].Ticket.Key != k2.Key {
		t.Fatalf("%+v", cq.Items)
	}
	// What the queue says matches what completing does.
	if _, err := tm.svc.CompleteTicket(bg, tm.di, tm.pid(), k2.ID); err == nil {
		t.Fatal("completed a ticket whose pull request is open")
	}
	if _, err := tm.svc.CompleteTicket(bg, tm.di, tm.pid(), k1.ID); err != nil {
		t.Fatal(err)
	}
	if q, _ = tm.svc.Reviews(bg, tm.di); len(q.Items) != 1 {
		t.Fatalf("a finished ticket is still in the queue: %d", len(q.Items))
	}
}

func TestReviewsAcrossProjectsStayWithinWhatTheViewerCanSee(t *testing.T) {
	tm := newTeam(t)
	secret := tm.project2Named("Secret")
	k, _ := tm.svc.CreateTicket(bg, tm.owner, secret.ID, TicketInput{Title: "Hidden", Status: domain.TicketAvailable})
	if _, err := tm.svc.ClaimTicket(bg, tm.owner, secret.ID, k.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.SubmitTicket(bg, tm.owner, secret.ID, k.ID, SubmitInput{}); err != nil {
		t.Fatal(err)
	}
	for _, a := range []Actor{tm.bo, tm.cy, tm.di} {
		q, _ := tm.svc.Reviews(bg, a)
		w, _ := tm.svc.MyWork(bg, a)
		o, _ := tm.svc.Overview(bg, a)
		if len(q.Items) != 0 || len(w.InProgress)+len(w.Submitted) != 0 || len(o.Working) != 0 || len(o.Projects) != 1 {
			t.Fatalf("%s sees a project they are not on: %+v %+v %+v", a.Member.Name, q, w, o)
		}
	}
	oq, _ := tm.svc.Reviews(bg, tm.owner)
	if len(oq.Items) != 1 || !oq.Items[0].Mine || !oq.Items[0].CanComplete {
		t.Fatalf("a project owner may sign off their own work: %+v", oq.Items)
	}
}

// ---- Overview ----

func TestTheOverviewShowsWhatIsAvailableAndWhoIsOnWhat(t *testing.T) {
	tm := newTeam(t)
	tm.ticket(tm.owner, "Idea", domain.TicketBacklog)
	tm.ticket(tm.owner, "Open 1", domain.TicketAvailable)
	tm.ticket(tm.owner, "Open 2", domain.TicketAvailable)
	k := tm.claimed(tm.bo, "Bo's")
	conf := openPR()
	conf.Mergeable = domain.MergeConflicting
	tm.report(tm.bo, k, conf)
	r := tm.claimed(tm.cy, "Cy's")
	tm.report(tm.cy, r, openPR2())
	if _, err := tm.svc.SubmitTicket(bg, tm.cy, tm.pid(), r.ID, SubmitInput{}); err != nil {
		t.Fatal(err)
	}
	o, err := tm.svc.Overview(bg, tm.di)
	if err != nil {
		t.Fatal(err)
	}
	if o.Available != 2 || len(o.Projects) != 1 || len(o.Working) != 2 {
		t.Fatalf("%+v", o)
	}
	ps := o.Projects[0]
	if ps.Counts["backlog"] != 1 || ps.Counts["available"] != 2 || ps.Counts["in_progress"] != 1 || ps.Counts["review"] != 1 || ps.Counts["done"] != 0 ||
		ps.People != 4 || ps.ToReview != 1 || ps.Mine != 0 || ps.Problems != 1 || ps.Role != domain.ProjectReviewer || !ps.Member {
		t.Fatalf("%+v", ps)
	}
	who := map[string]string{}
	for _, it := range o.Working {
		who[it.Ticket.Key] = it.Author
	}
	if who[k.Key] != "Bo" || who[r.Key] != "Cy" {
		t.Fatalf("%v", who)
	}
	if bo, _ := tm.svc.Overview(bg, tm.bo); bo.Projects[0].Mine != 1 || bo.Projects[0].ToReview != 0 {
		t.Fatalf("%+v", bo.Projects[0])
	}
}

// ---- keeping clients in step ----

func TestWorkspaceSyncReportsEveryImportantEvent(t *testing.T) {
	tm := newTeam(t)
	start, err := tm.svc.WaitForWorkspaceChange(bg, tm.bo, 0, -1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(start.Events) != 0 || len(start.Projects) != 1 || start.Projects[0].ID != tm.pid() {
		t.Fatalf("%+v", start)
	}
	rev, cursor := start.Revision, start.Cursor

	k := tm.ticket(tm.owner, "Authentication error", domain.TicketAvailable)
	got, _ := tm.svc.ClaimTicket(bg, tm.bo, tm.pid(), k.ID)
	tm.report(tm.bo, got, openPR())
	if _, err := tm.svc.ReleaseTicket(bg, tm.bo, tm.pid(), k.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.AssignTicket(bg, tm.owner, tm.pid(), k.ID, tm.cy.Member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.AssignTicket(bg, tm.owner, tm.pid(), k.ID, tm.bo.Member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.SubmitTicket(bg, tm.bo, tm.pid(), k.ID, SubmitInput{}); err != nil {
		t.Fatal(err)
	}
	merged := openPR()
	merged.State = domain.PRMerged
	if _, err := tm.svc.ReportGit(bg, tm.di, tm.pid(), k.ID, GitReport{PullRequest: &merged}); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.CompleteTicket(bg, tm.di, tm.pid(), k.ID); err != nil {
		t.Fatal(err)
	}

	res, err := tm.svc.WaitForWorkspaceChange(bg, tm.cy, rev, cursor, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Revision <= rev || res.Truncated || res.Reset || res.Cursor <= cursor {
		t.Fatalf("%+v", res)
	}
	var kinds []domain.ActivityKind
	for _, e := range res.Events {
		kinds = append(kinds, e.Kind)
		if e.ProjectName != "Shop" || e.ActorName == "" {
			t.Fatalf("%+v", e)
		}
	}
	want := []domain.ActivityKind{
		domain.ActTicketCreated, domain.ActTicketClaimed, domain.ActPullRequestMade, domain.ActTicketReleased, domain.ActTicketReassigned, domain.ActTicketReassigned,
		domain.ActWorkSubmitted, domain.ActReviewRequested, domain.ActPullRequestMerged, domain.ActTicketCompleted,
	}
	if fmt.Sprint(kinds) != fmt.Sprint(want) {
		t.Fatalf("events\n got %v\nwant %v", kinds, want)
	}
	// Project revisions come with it, so a client knows which board to reload.
	board, _ := tm.svc.Board(bg, tm.cy, tm.pid())
	if res.Projects[0].Revision != board.Revision {
		t.Fatalf("%+v vs %d", res.Projects, board.Revision)
	}
	// Asking again from where it left off: nothing new.
	again, _ := tm.svc.WaitForWorkspaceChange(bg, tm.cy, res.Revision, res.Cursor, 0)
	if again.Changed || len(again.Events) != 0 {
		t.Fatalf("%+v", again)
	}
}

func TestAWaitingClientIsWokenByAnotherMembersChange(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "Race", domain.TicketAvailable)
	start, _ := tm.svc.WaitForWorkspaceChange(bg, tm.cy, 0, -1, 0)
	done := make(chan WorkspaceSync, 1)
	go func() {
		res, _ := tm.svc.WaitForWorkspaceChange(bg, tm.cy, start.Revision, start.Cursor, 5*time.Second)
		done <- res
	}()
	time.Sleep(50 * time.Millisecond)
	began := time.Now()
	if _, err := tm.svc.ClaimTicket(bg, tm.bo, tm.pid(), k.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case res := <-done:
		if !res.Changed || len(res.Events) != 1 || res.Events[0].Kind != domain.ActTicketClaimed || res.Events[0].ActorName != "Bo" || res.Events[0].TicketKey != k.Key {
			t.Fatalf("%+v", res)
		}
		if d := time.Since(began); d > time.Second {
			t.Fatalf("took %s to reach a waiting client", d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a claim did not reach a waiting member")
	}
	// A client that is ahead of the server (it was restored from an older backup) is told to start again.
	go func() {
		res, _ := tm.svc.WaitForWorkspaceChange(bg, tm.cy, start.Revision+100, -1, 5*time.Second) // a stale, higher revision: the server was restored from a backup
		done <- res
	}()
	select {
	case res := <-done:
		if !res.Changed || !res.Reset {
			t.Fatalf("a client ahead of the server must be told to reset: %+v", res)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no answer")
	}
}

// A client that was offline catches up from the revision and cursor it last had.
func TestAReconnectingClientCatchesUpAndASevereGapSaysReload(t *testing.T) {
	tm := newTeam(t)
	start, _ := tm.svc.WaitForWorkspaceChange(bg, tm.bo, 0, -1, 0)
	var keys []string
	for i := 0; i < MaxSyncEvents+20; i++ {
		keys = append(keys, tm.ticket(tm.cy, fmt.Sprintf("T%d", i), domain.TicketBacklog).Key)
	}
	res, err := tm.svc.WaitForWorkspaceChange(bg, tm.bo, start.Revision, start.Cursor, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || len(res.Events) != MaxSyncEvents || !res.Changed {
		t.Fatalf("truncated=%v events=%d", res.Truncated, len(res.Events))
	}
	// The cursor moved past everything, so the next call does not replay the gap...
	next, _ := tm.svc.WaitForWorkspaceChange(bg, tm.bo, res.Revision, res.Cursor, 0)
	if next.Changed || len(next.Events) != 0 {
		t.Fatalf("%+v", next)
	}
	// ...and the authoritative state is complete, whatever the events said.
	board, _ := tm.svc.Board(bg, tm.bo, tm.pid())
	if len(board.Tickets) != len(keys) {
		t.Fatalf("%d tickets on the board", len(board.Tickets))
	}
}

func TestEventsAreOnlyThoseOfProjectsTheClientCanSee(t *testing.T) {
	tm := newTeam(t)
	secret := tm.project2Named("Secret")
	start, _ := tm.svc.WaitForWorkspaceChange(bg, tm.bo, 0, -1, 0)
	if _, err := tm.svc.CreateTicket(bg, tm.owner, secret.ID, TicketInput{Title: "Hidden thing", Status: domain.TicketAvailable}); err != nil {
		t.Fatal(err)
	}
	visible := tm.ticket(tm.owner, "Shared thing", domain.TicketAvailable)
	res, _ := tm.svc.WaitForWorkspaceChange(bg, tm.bo, start.Revision, start.Cursor, 0)
	if len(res.Events) != 1 || res.Events[0].TicketKey != visible.Key {
		t.Fatalf("%+v", res.Events)
	}
	for _, p := range res.Projects {
		if p.ID == secret.ID {
			t.Fatalf("a project revision leaked: %+v", res.Projects)
		}
	}
	// The owner sees both.
	ostart, _ := tm.svc.WaitForWorkspaceChange(bg, tm.owner, 0, -1, 0)
	tm.ticket(tm.owner, "Third", domain.TicketBacklog)
	if _, err := tm.svc.CreateTicket(bg, tm.owner, secret.ID, TicketInput{Title: "Fourth"}); err != nil {
		t.Fatal(err)
	}
	if res, _ = tm.svc.WaitForWorkspaceChange(bg, tm.owner, ostart.Revision, ostart.Cursor, 0); len(res.Events) != 2 {
		t.Fatalf("%+v", res.Events)
	}
}

func TestChangesOtherThanTicketsMoveTheWorkspaceRevision(t *testing.T) {
	tm := newTeam(t)
	rev := func() int64 {
		res, err := tm.svc.WaitForWorkspaceChange(bg, tm.owner, 0, -1, 0)
		if err != nil {
			t.Fatal(err)
		}
		return res.Revision
	}
	last := rev()
	step := func(what string, fn func() error) {
		t.Helper()
		if err := fn(); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if now := rev(); now <= last {
			t.Fatalf("%s did not move the revision", what)
		} else {
			last = now
		}
	}
	step("renaming the workspace", func() error { _, err := tm.svc.RenameWorkspace(bg, tm.owner, "Acme Inc"); return err })
	var eve MemberWithToken
	step("adding a member", func() (err error) { eve, err = tm.svc.AddMember(bg, tm.owner, "Eve", "", domain.RoleMember); return })
	p := tm.project2Named("Docs")
	last = rev()
	step("putting someone on a project", func() error { return tm.svc.AddProjectMember(bg, tm.owner, p.ID, eve.Member.ID, "") })
	step("changing a project role", func() error {
		return tm.svc.AddProjectMember(bg, tm.owner, p.ID, eve.Member.ID, domain.ProjectReviewer)
	})
	step("archiving a project", func() error {
		yes := true
		_, err := tm.svc.UpdateProject(bg, tm.owner, p.ID, ProjectPatch{Archived: &yes})
		return err
	})
	step("removing a member", func() error { return tm.svc.RemoveMember(bg, tm.owner, eve.Member.ID) })
	// Another workspace's changes do not.
	w2, _ := tm.world.workspace("Rival", "Rex")
	before := rev()
	tm.world.project(w2, "Elsewhere")
	if rev() != before {
		t.Fatal("another workspace's change moved this revision")
	}
}

func TestOnlyAFewWaitingRequestsMayBeOpenPerMember(t *testing.T) {
	tm := newTeam(t)
	start, _ := tm.svc.WaitForWorkspaceChange(bg, tm.bo, 0, -1, 0)
	var wg sync.WaitGroup
	var busy, ok atomic.Int32
	for i := 0; i < MaxWaitsPerMember+4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := tm.svc.WaitForWorkspaceChange(bg, tm.bo, start.Revision, start.Cursor, 400*time.Millisecond)
			switch {
			case errors.Is(err, domain.ErrBusy):
				busy.Add(1)
			case err == nil:
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if int(busy.Load()) < 4 || int(ok.Load()) > MaxWaitsPerMember {
		t.Fatalf("busy %d, ok %d", busy.Load(), ok.Load())
	}
	// Other members are not affected, and the slots come back.
	if _, err := tm.svc.WaitForWorkspaceChange(bg, tm.cy, start.Revision, start.Cursor, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.WaitForWorkspaceChange(bg, tm.bo, start.Revision, start.Cursor, 0); err != nil {
		t.Fatalf("slots were not given back: %v", err)
	}
}

// ---- concurrency ----

// Two people finish the same step at once: exactly one wins and the board never
// ends up between states.
func TestRacingTransitionsHaveExactlyOneWinner(t *testing.T) {
	type op struct {
		name string
		fn   func(tm *team, k domain.Ticket) error
	}
	cases := []struct {
		name  string
		setup func(tm *team) domain.Ticket
		ops   []op
		final func(domain.Ticket) bool
	}{
		{
			name:  "the holder submits while the owner takes it back",
			setup: func(tm *team) domain.Ticket { return tm.claimed(tm.bo, "A") },
			ops: []op{
				{"submit", func(tm *team, k domain.Ticket) error {
					_, err := tm.svc.SubmitTicket(bg, tm.bo, tm.pid(), k.ID, SubmitInput{})
					return err
				}},
				{"release", func(tm *team, k domain.Ticket) error {
					_, err := tm.svc.ReleaseTicket(bg, tm.owner, tm.pid(), k.ID)
					return err
				}},
			},
			final: func(k domain.Ticket) bool {
				return (k.Status == domain.TicketReview && k.AssigneeID != "") || (k.Status == domain.TicketAvailable && k.AssigneeID == "")
			},
		},
		{
			name: "a reviewer completes while another sends it back",
			setup: func(tm *team) domain.Ticket {
				k := tm.claimed(tm.bo, "B")
				if _, err := tm.svc.SubmitTicket(bg, tm.bo, tm.pid(), k.ID, SubmitInput{}); err != nil {
					tm.t.Fatal(err)
				}
				return k
			},
			ops: []op{
				{"complete", func(tm *team, k domain.Ticket) error {
					_, err := tm.svc.CompleteTicket(bg, tm.di, tm.pid(), k.ID)
					return err
				}},
				{"request changes", func(tm *team, k domain.Ticket) error {
					_, err := tm.svc.RequestChanges(bg, tm.owner, tm.pid(), k.ID, "no")
					return err
				}},
			},
			final: func(k domain.Ticket) bool {
				return (k.Status == domain.TicketDone && k.AssigneeID != "") || (k.Status == domain.TicketInProgress && k.AssigneeID != "")
			},
		},
		{
			name:  "two owners reassign it to different people",
			setup: func(tm *team) domain.Ticket { return tm.claimed(tm.bo, "C") },
			ops: []op{
				{"to Cy", func(tm *team, k domain.Ticket) error {
					_, err := tm.svc.AssignTicket(bg, tm.owner, tm.pid(), k.ID, tm.cy.Member.ID)
					return err
				}},
				{"to Di", func(tm *team, k domain.Ticket) error {
					_, err := tm.svc.AssignTicket(bg, tm.owner, tm.pid(), k.ID, tm.di.Member.ID)
					return err
				}},
			},
			// Both are legal one after the other (Bo → Cy → Di), so both may succeed; the end state is one holder.
			final: func(k domain.Ticket) bool {
				return k.Status == domain.TicketInProgress && (k.AssigneeID != "")
			},
		},
		{
			name:  "the holder releases while someone else claims the moment it is free",
			setup: func(tm *team) domain.Ticket { return tm.claimed(tm.bo, "D") },
			ops: []op{
				{"release", func(tm *team, k domain.Ticket) error {
					_, err := tm.svc.ReleaseTicket(bg, tm.bo, tm.pid(), k.ID)
					return err
				}},
				{"claim", func(tm *team, k domain.Ticket) error {
					_, err := tm.svc.ClaimTicket(bg, tm.cy, tm.pid(), k.ID)
					return err
				}},
			},
			final: func(k domain.Ticket) bool {
				return (k.Status == domain.TicketAvailable && k.AssigneeID == "") || (k.Status == domain.TicketInProgress && k.AssigneeID != "")
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for round := 0; round < 25; round++ {
				tm := newTeam(t)
				k := c.setup(tm)
				before, _ := tm.svc.GetTicket(bg, tm.owner, tm.pid(), k.ID)
				var wg sync.WaitGroup
				start := make(chan struct{})
				errs := make([]error, len(c.ops))
				for i, o := range c.ops {
					wg.Add(1)
					go func() {
						defer wg.Done()
						<-start
						errs[i] = o.fn(tm, k)
					}()
				}
				close(start)
				wg.Wait()
				wins := 0
				for i, err := range errs {
					switch {
					case err == nil:
						wins++
					case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrForbidden):
					default:
						t.Fatalf("%s: unexpected error %v", c.ops[i].name, err)
					}
				}
				final, _ := tm.svc.GetTicket(bg, tm.owner, tm.pid(), k.ID)
				if wins == 0 {
					t.Fatalf("round %d: nobody won: %v", round, errs)
				}
				if !c.final(final) {
					t.Fatalf("round %d: the ticket ended up in %s held by %q", round, final.Status, final.AssigneeID)
				}
				// Every winning write is one version, and one entry in the history.
				if final.Version != before.Version+int64(wins) {
					t.Fatalf("round %d: version %d after %d wins from %d", round, final.Version, wins, before.Version)
				}
			}
		})
	}
}

// Taking someone off a project while they claim a ticket there: either the claim
// fails, or it succeeds and the removal puts the ticket back. Never a ticket held
// by someone who is not on the project.
func TestRemovingAMemberWhileTheyClaimNeverLeavesAGhostHolder(t *testing.T) {
	for round := 0; round < 30; round++ {
		tm := newTeam(t)
		k := tm.ticket(tm.owner, "Contested", domain.TicketAvailable)
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() { defer wg.Done(); <-start; _, _ = tm.svc.ClaimTicket(bg, tm.bo, tm.pid(), k.ID) }()
		go func() {
			defer wg.Done()
			<-start
			_ = tm.svc.RemoveProjectMember(bg, tm.owner, tm.pid(), tm.bo.Member.ID)
		}()
		close(start)
		wg.Wait()
		final, _ := tm.svc.GetTicket(bg, tm.owner, tm.pid(), k.ID)
		people, _ := tm.svc.ListProjectPeople(bg, tm.owner, tm.pid())
		onIt := false
		for _, p := range people {
			onIt = onIt || p.ID == tm.bo.Member.ID
		}
		if final.AssigneeID == tm.bo.Member.ID && !onIt {
			t.Fatalf("round %d: %s is held by Bo, who is no longer on the project", round, final.Key)
		}
		if final.Status == domain.TicketInProgress && final.AssigneeID == "" {
			t.Fatalf("round %d: in progress with nobody on it", round)
		}
	}
}

// A storm of every kind of operation at once: whatever order they ran in, the
// board is consistent and the history matches the writes that happened.
func TestAStormOfOperationsLeavesAConsistentBoard(t *testing.T) {
	tm := newTeam(t)
	const tickets = 12
	var ks []domain.Ticket
	for i := 0; i < tickets; i++ {
		ks = append(ks, tm.ticket(tm.owner, fmt.Sprintf("Storm %d", i), domain.TicketAvailable))
	}
	actors := []Actor{tm.bo, tm.cy, tm.di, tm.owner}
	var wg sync.WaitGroup
	var ok atomic.Int64
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 60; i++ {
				k := ks[(w*7+i*3)%tickets]
				a := actors[(w+i)%len(actors)]
				var err error
				switch (w + i) % 7 {
				case 0, 1:
					_, err = tm.svc.ClaimTicket(bg, a, tm.pid(), k.ID)
				case 2:
					_, err = tm.svc.ReleaseTicket(bg, a, tm.pid(), k.ID)
				case 3:
					_, err = tm.svc.SubmitTicket(bg, a, tm.pid(), k.ID, SubmitInput{})
				case 4:
					_, err = tm.svc.AssignTicket(bg, tm.owner, tm.pid(), k.ID, actors[(w+i+1)%3].Member.ID)
				case 5:
					_, err = tm.svc.RequestChanges(bg, tm.di, tm.pid(), k.ID, "again")
				case 6:
					_, err = tm.svc.CompleteTicket(bg, tm.di, tm.pid(), k.ID)
				}
				switch {
				case err == nil:
					ok.Add(1)
				case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrForbidden):
				default:
					t.Errorf("unexpected error: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	board, err := tm.svc.Board(bg, tm.owner, tm.pid())
	if err != nil {
		t.Fatal(err)
	}
	versionSum := int64(0)
	for _, k := range board.Tickets {
		held := k.Status == domain.TicketInProgress || k.Status == domain.TicketReview
		if held != (k.AssigneeID != "") && k.Status != domain.TicketDone {
			t.Fatalf("%s is %s held by %q", k.Key, k.Status, k.AssigneeID)
		}
		if k.Status == domain.TicketDone && k.CompletedAt == nil {
			t.Fatalf("%s is done without a completion time", k.Key)
		}
		if k.Status == domain.TicketReview && k.SubmittedAt == nil {
			t.Fatalf("%s is in review without a submission time", k.Key)
		}
		versionSum += k.Version - 1
	}
	if versionSum != ok.Load() {
		t.Fatalf("%d successful writes but the versions add up to %d", ok.Load(), versionSum)
	}
	// Each successful write left a history entry (a submit leaves one to three).
	acts, _ := tm.svc.ListActivity(bg, tm.owner, tm.pid(), 0, MaxActivityPage)
	if int64(len(acts)) < ok.Load() {
		t.Fatalf("%d history entries for %d writes", len(acts), ok.Load())
	}
}

// A merged pull request stays merged: a delayed report from a slow Werkbord cannot reopen it.
func TestAMergedPullRequestCannotBeTurnedBackIntoAnOpenOne(t *testing.T) {
	tm := newTeam(t)
	k := tm.claimed(tm.bo, "Merge me")
	tm.report(tm.bo, k, openPR())
	if _, err := tm.svc.SubmitTicket(bg, tm.bo, tm.pid(), k.ID, SubmitInput{}); err != nil {
		t.Fatal(err)
	}
	merged := openPR()
	merged.State = domain.PRMerged
	if _, err := tm.svc.ReportGit(bg, tm.di, tm.pid(), k.ID, GitReport{PullRequest: &merged}); err != nil {
		t.Fatal(err)
	}
	// Bo's runner, a little late, reports the same pull request as open.
	_, err := tm.svc.ReportGit(bg, tm.bo, tm.pid(), k.ID, GitReport{PullRequest: func() *domain.PullRequest { p := openPR(); return &p }()})
	wantErr(t, err, domain.ErrConflict)
	got, _ := tm.svc.GetTicket(bg, tm.owner, tm.pid(), k.ID)
	if got.PullRequest.State != domain.PRMerged {
		t.Fatalf("the merge was lost: %+v", got.PullRequest)
	}
	// Repeating the merge is harmless, and a different pull request may still be reported.
	if _, err := tm.svc.ReportGit(bg, tm.di, tm.pid(), k.ID, GitReport{PullRequest: &merged}); err != nil {
		t.Fatal(err)
	}
	next := openPR2()
	if _, err := tm.svc.ReportGit(bg, tm.bo, tm.pid(), k.ID, GitReport{PullRequest: &next}); err != nil {
		t.Fatalf("a follow-up pull request must be reportable: %v", err)
	}
}
