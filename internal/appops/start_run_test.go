package appops

import (
	"strings"
	"testing"

	"werkbord/internal/domain"
	"werkbord/internal/service"
	"werkbord/internal/store"
)

// repoProject is a project that agents may work on. The fixture has no agent process, so the repository only has to be
// the kind of project the rules look at.
func (f *fx) repoProject(name string) *domain.Project {
	f.t.Helper()
	p := &domain.Project{ID: domain.NewID(domain.PrefixProject), Name: name, Kind: domain.ProjectRepository, RepoPath: f.t.TempDir(), CreatedAt: f.now(), UpdatedAt: f.now()}
	if err := f.db.Update(bg, func(tx store.Tx) error { return tx.Projects().Create(bg, p) }); err != nil {
		f.t.Fatal(err)
	}
	return p
}

func TestStartingAnAgentIsTheOwnersAssistantsAlone(t *testing.T) {
	f := newFx(t)
	owner := f.session("ast_owner")
	mcp := Principal{ID: "mcp:grant", Actor: "mcp:client", Via: "mcp", SessionID: "grant", Grants: AllPermissions}
	has := func(p Principal) bool {
		for _, spec := range f.svc.Catalog(p) {
			if spec.Name == "start_agent_run" {
				return true
			}
		}
		return false
	}
	if !has(owner) {
		t.Fatal("the owner's assistant is not offered start_agent_run")
	}
	if has(mcp) {
		t.Fatal("an MCP client is offered start_agent_run: only the person's own assistant may propose starting an agent")
	}
	if _, err := f.call(mcp, "start_agent_run", map[string]any{"projectId": "x", "ticketId": "y"}); CodeOf(err) != CodeUnknownOperation {
		t.Fatalf("an MCP client calling it got %v", err)
	}
	readOnly := f.session("ast_ro", PermRunsRead, PermTicketsRead)
	if has(readOnly) {
		t.Fatal("a read-only assistant is offered start_agent_run")
	}
}

func TestAnAgentIsStartedOnlyAfterConfirmationAndIsLinkedToItsConversation(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_launch")
	proj := f.repoProject("App")
	task := f.ticket(proj.ID, "Fix the login")

	res := f.mustCall(p, "start_agent_run", map[string]any{"projectId": proj.ID, "ticketId": task.ID, "agentId": "codex"})
	if res.Pending == nil || !strings.Contains(res.Pending.Summary, "Fix the login") || !strings.Contains(res.Pending.Summary, "codex") {
		t.Fatalf("proposal = %+v", res.Pending)
	}
	if runs, _ := f.runs.ListByTask(bg, task.ID); len(runs) != 0 || len(f.started) != 0 {
		t.Fatal("an agent was started before the person confirmed")
	}

	out, err := f.confirm(p, res.Pending.ActionID, true)
	if err != nil {
		t.Fatal(err)
	}
	ch := out.Data.(*Changed)
	runs, _ := f.runs.ListByTask(bg, task.ID)
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want exactly one", len(runs))
	}
	run := runs[0]
	if ch.Session == nil || ch.Session.Key != "run:"+run.ID || ch.Session.TaskID != task.ID || ch.Session.AgentID != "codex" {
		t.Fatalf("session = %+v for run %s", ch.Session, run.ID)
	}
	if ref, err := domain.ParseSessionRef(ch.Session.Key); err != nil || ref != domain.RunRef(run.ID) {
		t.Fatalf("the conversation's reference does not resolve to the run: %v %v", ref, err)
	}
	if run.Origin != domain.SalieOrigin("ast_launch") || domain.ConversationOf(run.Origin) != "ast_launch" {
		t.Fatalf("origin = %q: the run does not point back to the conversation that launched it", run.Origin)
	}

	// Confirming the same change again cannot start a second agent.
	if _, err := f.confirm(p, res.Pending.ActionID, true); CodeOf(err) != CodeNotPending {
		t.Fatalf("a second confirmation: %v", err)
	}
	// Nor can asking again: the task has a live session, and the refusal names it instead of starting another.
	_, err = f.call(p, "start_agent_run", map[string]any{"projectId": proj.ID, "ticketId": task.ID})
	if err == nil || !strings.Contains(err.Error(), "run:"+run.ID) {
		t.Fatalf("a second proposal for a task with a live session: %v", err)
	}
	if runs, _ = f.runs.ListByTask(bg, task.ID); len(runs) != 1 {
		t.Fatalf("runs = %d after the repeats, want one", len(runs))
	}
}

func TestAnAgentCannotBeProposedWhereTheRulesForStartingByHandRefuseIt(t *testing.T) {
	f := newFx(t)
	p := f.session("ast_refuse")
	work := f.project("Plain board")
	onWork := f.ticket(work.ID, "No repository here")
	repo := f.repoProject("App")
	done := f.ticket(repo.ID, "Finished")
	current, err := f.tasks.Get(bg, done.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.tasks.Update(bg, done.ID, service.TaskPatch{Version: current.Version, State: ptr(domain.TaskDone)}); err != nil {
		t.Fatal(err)
	}
	for name, a := range map[string]map[string]any{
		"a work project":  {"projectId": work.ID, "ticketId": onWork.ID},
		"a finished task": {"projectId": repo.ID, "ticketId": done.ID},
		"another project": {"projectId": work.ID, "ticketId": done.ID},
	} {
		if _, err := f.call(p, "start_agent_run", a); err == nil {
			t.Errorf("%s: proposed", name)
		}
	}
	if len(f.started) != 0 {
		t.Fatal("something started")
	}
}
