package service

import (
	"errors"
	"strings"
	"testing"

	"devboard/internal/planning"
	"devboard/internal/team/domain"
)

func (tm *team) label(a Actor, name, color string) domain.Label {
	tm.t.Helper()
	l, err := tm.svc.CreateLabel(bg, a, LabelInput{Name: name, Color: color})
	if err != nil {
		tm.t.Fatal(err)
	}
	return l
}

func (tm *team) planned(a Actor, title string, in TicketInput) domain.Ticket {
	tm.t.Helper()
	in.Title = title
	k, err := tm.svc.CreateTicket(bg, a, tm.pid(), in)
	if err != nil {
		tm.t.Fatal(err)
	}
	return k
}

func TestOnlyPeopleWhoManageTheWorkspaceDefineLabels(t *testing.T) {
	tm := newTeam(t)
	admin, _ := tm.member(tm.owner, "Ad")
	if _, err := tm.svc.SetMemberRole(bg, tm.owner, admin.Member.ID, domain.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	admin = tm.signInAgain(Actor{Member: func() domain.Member { m := admin.Member; m.Role = domain.RoleAdmin; return m }(), Workspace: admin.Workspace})

	// The owner and an admin may; a member, a project reviewer and even a project owner may not.
	design := tm.label(tm.owner, "Design", "#336699")
	tm.label(admin, "Marketing", "#aa3366")
	for name, a := range map[string]Actor{"member": tm.bo, "reviewer": tm.di} {
		if _, err := tm.svc.CreateLabel(bg, a, LabelInput{Name: "Mine", Color: "#000000"}); !errorIs(err, domain.ErrForbidden) {
			t.Errorf("a %s created a label: %v", name, err)
		}
		n := "renamed"
		if _, err := tm.svc.UpdateLabel(bg, a, design.ID, LabelPatch{Name: &n, Version: design.Version}); !errorIs(err, domain.ErrForbidden) {
			t.Errorf("a %s renamed a label: %v", name, err)
		}
		if err := tm.svc.DeleteLabel(bg, a, design.ID); !errorIs(err, domain.ErrForbidden) {
			t.Errorf("a %s deleted a label: %v", name, err)
		}
	}
	if err := tm.svc.AddProjectMember(bg, tm.owner, tm.pid(), tm.bo.Member.ID, domain.ProjectOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.CreateLabel(bg, tm.signInAgain(tm.bo), LabelInput{Name: "Mine", Color: "#000000"}); !errorIs(err, domain.ErrForbidden) {
		t.Errorf("owning a project is not managing the workspace: %v", err)
	}

	// Every member sees them, though.
	for _, a := range []Actor{tm.bo, tm.cy, tm.di} {
		ls, err := tm.svc.ListLabels(bg, a)
		if err != nil || len(ls) != 2 {
			t.Fatalf("a member listed %+v, %v", ls, err)
		}
	}
	if !domain.RoleAdmin.Can(domain.PermLabelsManage) || !domain.RoleOwner.Can(domain.PermLabelsManage) || domain.RoleMember.Can(domain.PermLabelsManage) {
		t.Fatal("labels.manage belongs to the owner and admins")
	}
}

func errorIs(err, target error) bool { return errors.Is(err, target) }

func TestLabelsAreWorkspaceWideAndUnique(t *testing.T) {
	tm := newTeam(t)
	if _, err := tm.svc.CreateLabel(bg, tm.owner, LabelInput{Name: "x", Color: "red"}); !errorIs(err, domain.ErrInvalid) {
		t.Errorf("bad colour: %v", err)
	}
	l := tm.label(tm.owner, "  Q4   launch ", "#ABC")
	if l.Name != "Q4 launch" || l.Color != "#aabbcc" || l.Version != 1 {
		t.Fatalf("label = %+v", l)
	}
	if _, err := tm.svc.CreateLabel(bg, tm.owner, LabelInput{Name: "q4 LAUNCH", Color: "#000000"}); !errorIs(err, domain.ErrConflict) {
		t.Errorf("same name: %v", err)
	}

	// Another workspace has its own: names are free there, and a label of one cannot be put on a ticket of the other.
	other, _ := tm.workspace("Rival", "Rex")
	rivalLabel, err := tm.svc.CreateLabel(bg, other, LabelInput{Name: "Q4 launch", Color: "#000000"})
	if err != nil {
		t.Fatalf("another workspace may use the name: %v", err)
	}
	ids := []string{rivalLabel.ID}
	if _, err := tm.svc.CreateTicket(bg, tm.owner, tm.pid(), TicketInput{Title: "t", LabelIDs: ids}); !errorIs(err, domain.ErrInvalid) {
		t.Errorf("another workspace's label on a ticket: %v", err)
	}
	if ls, _ := tm.svc.ListLabels(bg, other); len(ls) != 1 || ls[0].ID != rivalLabel.ID {
		t.Errorf("a workspace sees only its own labels: %+v", ls)
	}
	if err := tm.svc.DeleteLabel(bg, tm.owner, rivalLabel.ID); !errorIs(err, domain.ErrNotFound) {
		t.Errorf("deleting another workspace's label: %v", err)
	}
}

func TestLabelsOnTicketsAreTicketEditsWithTicketRules(t *testing.T) {
	tm := newTeam(t)
	a, b := tm.label(tm.owner, "A", "#111111"), tm.label(tm.owner, "B", "#222222")
	k := tm.planned(tm.bo, "Bo's ticket", TicketInput{LabelIDs: []string{b.ID, a.ID}})
	if strings.Join(k.LabelIDs, ",") != b.ID+","+a.ID {
		t.Fatalf("labels = %v: they keep the order they were put on", k.LabelIDs)
	}
	got, _ := tm.svc.GetTicket(bg, tm.bo, tm.pid(), k.ID)
	if len(got.LabelIDs) != 2 {
		t.Fatalf("stored labels = %v", got.LabelIDs)
	}

	// The creator may change them; so may anyone with tickets.edit (the project owner); another contributor may not.
	only := []string{a.ID}
	k2, err := tm.svc.UpdateTicket(bg, tm.bo, tm.pid(), k.ID, TicketPatch{LabelIDs: &only})
	if err != nil || len(k2.LabelIDs) != 1 {
		t.Fatalf("creator: %+v, %v", k2, err)
	}
	if _, err := tm.svc.UpdateTicket(bg, tm.cy, tm.pid(), k.ID, TicketPatch{LabelIDs: &[]string{b.ID}}); !errorIs(err, domain.ErrForbidden) {
		t.Errorf("another contributor relabelled it: %v", err)
	}
	if _, err := tm.svc.UpdateTicket(bg, tm.owner, tm.pid(), k.ID, TicketPatch{LabelIDs: &[]string{b.ID}}); err != nil {
		t.Errorf("the project owner could not: %v", err)
	}
	if _, err := tm.svc.UpdateTicket(bg, tm.owner, tm.pid(), k.ID, TicketPatch{LabelIDs: &[]string{"tlb_nope"}}); !errorIs(err, domain.ErrInvalid) {
		t.Errorf("unknown label: %v", err)
	}
	if _, err := tm.svc.UpdateTicket(bg, tm.owner, tm.pid(), k.ID, TicketPatch{LabelIDs: &[]string{a.ID, a.ID}}); err != nil {
		t.Errorf("a repeated label is only kept once: %v", err)
	}

	// A claim, a submit and a review do not lose them.
	av := tm.planned(tm.owner, "claimed", TicketInput{Status: domain.TicketAvailable, LabelIDs: []string{a.ID}})
	if _, err := tm.svc.ClaimTicket(bg, tm.bo, tm.pid(), av.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.SubmitTicket(bg, tm.bo, tm.pid(), av.ID, SubmitInput{}); err != nil {
		t.Fatal(err)
	}
	after, _ := tm.svc.GetTicket(bg, tm.owner, tm.pid(), av.ID)
	if len(after.LabelIDs) != 1 || after.Status != domain.TicketReview {
		t.Fatalf("after claim and submit: %+v", after)
	}
}

func TestRenamingAndDeletingALabelTouchesTicketsOnlyAsDescription(t *testing.T) {
	tm := newTeam(t)
	keep, drop := tm.label(tm.owner, "Keep", "#111111"), tm.label(tm.owner, "Drop", "#222222")
	k := tm.planned(tm.owner, "t", TicketInput{Status: domain.TicketAvailable, LabelIDs: []string{drop.ID, keep.ID}})
	other := tm.project2()
	k2, err := tm.svc.CreateTicket(bg, tm.owner, other.ID, TicketInput{Title: "elsewhere", LabelIDs: []string{drop.ID}})
	if err != nil {
		t.Fatal(err)
	}

	ls, _ := tm.svc.ListLabels(bg, tm.owner)
	use := map[string]int{}
	for _, l := range ls {
		use[l.Name] = l.Tickets
	}
	if use["Drop"] != 2 || use["Keep"] != 1 {
		t.Fatalf("usage = %v", use)
	}
	// A member who is not on the second project is not told how many tickets are there.
	bo, _ := tm.svc.ListLabels(bg, tm.bo)
	for _, l := range bo {
		if l.Name == "Drop" && l.Tickets != 1 {
			t.Errorf("a member counted tickets of a project they cannot see: %d", l.Tickets)
		}
	}

	rev := func() int64 { p, _ := tm.svc.GetProject(bg, tm.owner, tm.pid()); return p.Revision }
	before := rev()
	name := "Dropped"
	renamed, err := tm.svc.UpdateLabel(bg, tm.owner, drop.ID, LabelPatch{Name: &name, Version: drop.Version})
	if err != nil || renamed.Name != "Dropped" || renamed.Version != 2 {
		t.Fatalf("rename = %+v, %v", renamed, err)
	}
	if rev() <= before {
		t.Error("the board was not told a label on it was renamed")
	}
	stale := "stale"
	if _, err := tm.svc.UpdateLabel(bg, tm.owner, drop.ID, LabelPatch{Name: &stale, Version: drop.Version}); !errorIs(err, domain.ErrConflict) {
		t.Errorf("stale version: %v", err)
	}

	before = rev()
	if err := tm.svc.DeleteLabel(bg, tm.owner, drop.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := tm.svc.GetTicket(bg, tm.owner, tm.pid(), k.ID)
	if len(got.LabelIDs) != 1 || got.LabelIDs[0] != keep.ID || got.Status != domain.TicketAvailable || got.Version != k.Version+1 {
		t.Fatalf("after deleting: %+v", got)
	}
	got2, _ := tm.svc.GetTicket(bg, tm.owner, other.ID, k2.ID)
	if len(got2.LabelIDs) != 0 {
		t.Fatalf("the other project's ticket kept it: %+v", got2.LabelIDs)
	}
	if rev() <= before {
		t.Error("the board was not told a label on it was deleted")
	}
	if err := tm.svc.DeleteLabel(bg, tm.owner, drop.ID); !errorIs(err, domain.ErrNotFound) {
		t.Errorf("deleting twice: %v", err)
	}
}

func TestWorkModeIsNotALabelAndOnlyHumanWorkRefusesAnAgent(t *testing.T) {
	tm := newTeam(t)
	human := tm.label(tm.owner, "human", "#123456") // a label may be called anything
	k := tm.planned(tm.bo, "t", TicketInput{Status: domain.TicketAvailable, LabelIDs: []string{human.ID}})
	if k.WorkMode != planning.ModeAgent {
		t.Fatalf("a label called human changed the mode: %q", k.WorkMode)
	}
	k, err := tm.svc.ClaimTicket(bg, tm.bo, tm.pid(), k.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Agent work (and hybrid) may be scheduled as before; human work may not.
	if _, err := tm.svc.SetSchedule(bg, tm.owner, tm.pid(), k.ID, scheduleInput()); err != nil {
		t.Fatalf("agent work: %v", err)
	}
	mode := planning.ModeHuman
	hk, err := tm.svc.UpdateTicket(bg, tm.owner, tm.pid(), k.ID, TicketPatch{WorkMode: &mode})
	if err != nil || hk.WorkMode != planning.ModeHuman || len(hk.LabelIDs) != 1 {
		t.Fatalf("human: %+v, %v", hk, err)
	}
	if _, err := tm.svc.SetSchedule(bg, tm.owner, tm.pid(), k.ID, ScheduleInput{Version: 1, At: scheduleInput().At, Timezone: "UTC", MissedPolicy: "run_late", Priority: 1}); !errorIs(err, domain.ErrConflict) || !strings.Contains(err.Error(), "human work") {
		t.Errorf("scheduling human work: %v", err)
	}
	hybrid := planning.ModeHybrid
	if _, err := tm.svc.UpdateTicket(bg, tm.owner, tm.pid(), k.ID, TicketPatch{WorkMode: &hybrid}); err != nil {
		t.Fatal(err)
	}
	bad := planning.ExecutionMode("robot")
	if _, err := tm.svc.UpdateTicket(bg, tm.owner, tm.pid(), k.ID, TicketPatch{WorkMode: &bad}); !errorIs(err, domain.ErrInvalid) {
		t.Errorf("bad mode: %v", err)
	}
}

func TestTicketsOfAProjectWithNoRepositoryNeedNoGit(t *testing.T) {
	tm := newTeam(t)
	if tm.project.Repository != "" || tm.project.HasRepository() {
		t.Fatal("the project has a repository")
	}
	// The whole flow of work, with no Git report of any kind.
	k := tm.planned(tm.owner, "Book the venue", TicketInput{Status: domain.TicketAvailable, WorkMode: planning.ModeHuman, Plan: planning.Range{Start: "2026-10-05", End: "2026-10-09"}})
	if k.WorkMode != planning.ModeHuman || k.Plan.Start != "2026-10-05" {
		t.Fatalf("ticket = %+v", k)
	}
	if _, err := tm.svc.ClaimTicket(bg, tm.bo, tm.pid(), k.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.SubmitTicket(bg, tm.bo, tm.pid(), k.ID, SubmitInput{}); err != nil {
		t.Fatalf("submitting without a branch or pull request: %v", err)
	}
	done, err := tm.svc.CompleteTicket(bg, tm.owner, tm.pid(), k.ID)
	if err != nil || done.Status != domain.TicketDone {
		t.Fatalf("complete: %+v, %v", done, err)
	}
	// Its board and handoff are ordinary.
	if _, err := tm.svc.Board(bg, tm.bo, tm.pid()); err != nil {
		t.Fatal(err)
	}
}

func TestPlansAreValidated(t *testing.T) {
	tm := newTeam(t)
	for _, plan := range []planning.Range{{Start: "2026-10-09", End: "2026-10-05"}, {Start: "soon"}, {Start: "2026-02-30"}, {Start: "2026-10-05", End: "2026-10-06", Milestone: true}} {
		if _, err := tm.svc.CreateTicket(bg, tm.owner, tm.pid(), TicketInput{Title: "x", Plan: plan}); !errorIs(err, domain.ErrInvalid) {
			t.Errorf("%+v: %v", plan, err)
		}
	}
	m := tm.planned(tm.owner, "Launch", TicketInput{Plan: planning.Range{End: "2026-10-20", Milestone: true}})
	if !m.Plan.Milestone || m.Plan.Start != "2026-10-20" || m.Plan.End != "" {
		t.Fatalf("milestone = %+v", m.Plan)
	}
	cleared := planning.Range{}
	got, err := tm.svc.UpdateTicket(bg, tm.owner, tm.pid(), m.ID, TicketPatch{Plan: &cleared})
	if err != nil || !got.Plan.IsZero() {
		t.Fatalf("cleared = %+v, %v", got.Plan, err)
	}
}

func TestDependenciesAreCheckedAndTheTimelineOnlyWarns(t *testing.T) {
	tm := newTeam(t)
	design := tm.planned(tm.owner, "Design", TicketInput{Plan: planning.Range{Start: "2026-10-05", End: "2026-10-09"}})
	build := tm.planned(tm.owner, "Build", TicketInput{Plan: planning.Range{Start: "2026-10-07", End: "2026-10-14"}, Dependencies: []string{design.ID}})
	ship := tm.planned(tm.owner, "Ship", TicketInput{Plan: planning.Range{Start: "2026-10-15"}, Dependencies: []string{build.ID, design.ID}})
	if len(ship.Dependencies) != 2 {
		t.Fatalf("ship = %+v", ship.Dependencies)
	}

	tl, err := tm.svc.Timeline(bg, tm.bo, tm.pid())
	if err != nil {
		t.Fatal(err)
	}
	if len(tl.Warnings) != 1 || tl.Warnings[0].Code != planning.CodeStartsBeforeDependent || tl.Warnings[0].ItemID != build.ID || tl.Warnings[0].OtherID != design.ID {
		t.Fatalf("warnings = %+v", tl.Warnings)
	}
	// Finding out changed nothing.
	for _, want := range []domain.Ticket{design, build, ship} {
		got, _ := tm.svc.GetTicket(bg, tm.owner, tm.pid(), want.ID)
		if got.Plan != want.Plan || got.Version != want.Version || got.Status != want.Status {
			t.Errorf("%s changed: %+v", want.Title, got)
		}
	}

	// What cannot be written is refused: itself, another project's ticket, an unknown one, a circle, a repeat.
	other := tm.project2()
	foreign, _ := tm.svc.CreateTicket(bg, tm.owner, other.ID, TicketInput{Title: "foreign"})
	for name, deps := range map[string][]string{
		"itself":  {design.ID},
		"foreign": {foreign.ID},
		"unknown": {"ttk_nope"},
		"circle":  {ship.ID},
		"repeat":  {build.ID, build.ID},
		"empty":   {""},
	} {
		d := deps
		if _, err := tm.svc.UpdateTicket(bg, tm.owner, tm.pid(), design.ID, TicketPatch{Dependencies: &d}); !errorIs(err, domain.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Only people who may edit a ticket may change what it waits for.
	if _, err := tm.svc.UpdateTicket(bg, tm.cy, tm.pid(), ship.ID, TicketPatch{Dependencies: &[]string{}}); !errorIs(err, domain.ErrForbidden) {
		t.Errorf("another contributor: %v", err)
	}
	// Clearing them is allowed, and the warning goes.
	none := []string{}
	if _, err := tm.svc.UpdateTicket(bg, tm.owner, tm.pid(), build.ID, TicketPatch{Dependencies: &none}); err != nil {
		t.Fatal(err)
	}
	tl, _ = tm.svc.Timeline(bg, tm.owner, tm.pid())
	if len(tl.Warnings) != 0 {
		t.Fatalf("warnings = %+v", tl.Warnings)
	}
	// A person who is not on the project cannot read its timeline.
	outsider, _ := tm.member(tm.owner, "Eve")
	if _, err := tm.svc.Timeline(bg, outsider, tm.pid()); !errorIs(err, domain.ErrNotFound) {
		t.Errorf("outsider: %v", err)
	}
}

func TestTimelineReportsWhatTheWritePathWouldRefuse(t *testing.T) {
	// A graph that arrived another way (a restore, an older build) is still told about.
	ws := TimelineWarnings([]domain.Ticket{
		{ID: "a", Key: "WB-1", Title: "A", Dependencies: []string{"b"}},
		{ID: "b", Key: "WB-2", Title: "B", Dependencies: []string{"a", "ghost"}},
	})
	got := map[planning.Code]bool{}
	for _, w := range ws {
		got[w.Code] = true
	}
	if !got[planning.CodeDependencyCycle] || !got[planning.CodeMissingDependency] {
		t.Fatalf("warnings = %+v", ws)
	}
	if len(TimelineWarnings(nil)) != 0 || TimelineWarnings(nil) == nil {
		t.Fatal("no tickets is an empty, non-nil list")
	}
}

func TestExistingTicketsKeepBehavingAsAgentWorkWithNoPlan(t *testing.T) {
	tm := newTeam(t)
	k := tm.ticket(tm.owner, "plain", domain.TicketAvailable)
	if k.Mode() != planning.ModeAgent || k.WorkMode != planning.ModeAgent || len(k.LabelIDs) != 0 || len(k.Dependencies) != 0 || !k.Plan.IsZero() {
		t.Fatalf("a ticket made the old way: %+v", k)
	}
	if k.LabelIDs == nil || k.Dependencies == nil {
		t.Fatal("empty lists, not null: they are sent as JSON")
	}
	k, _ = tm.svc.ClaimTicket(bg, tm.bo, tm.pid(), k.ID)
	if _, err := tm.svc.SetSchedule(bg, tm.owner, tm.pid(), k.ID, scheduleInput()); err != nil {
		t.Fatalf("scheduling an ordinary ticket still works: %v", err)
	}
}

func TestHumanWorkHasNoHandoffForAnAgent(t *testing.T) {
	tm := newTeam(t)
	k := tm.claimed(tm.bo, "to hand off")
	if _, err := tm.svc.HandoffTicketToRunner(bg, tm.bo, tm.pid(), k.ID); err != nil {
		t.Fatalf("agent work still opens in a runner: %v", err)
	}
	human := planning.ModeHuman
	if _, err := tm.svc.UpdateTicket(bg, tm.owner, tm.pid(), k.ID, TicketPatch{WorkMode: &human}); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.svc.HandoffTicketToRunner(bg, tm.bo, tm.pid(), k.ID); !errorIs(err, domain.ErrConflict) || !strings.Contains(err.Error(), "human work") {
		t.Errorf("handing human work to an agent: %v", err)
	}
}
