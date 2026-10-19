package appops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"devboard/internal/domain"
	"devboard/internal/integration"
	"devboard/internal/service"
	"devboard/internal/store"
)

// recFx is the operations' fixture with a Recommender behind them. The projects here have no repository on disk, so what is
// tested is how the operation scopes, permits, bridges and bounds the answer; the rules and the Git reading have their own
// tests in internal/domain and internal/service.
func recFx(t *testing.T) (*fx, *service.Knowledge) {
	t.Helper()
	f := newFx(t)
	memory, err := service.NewMemory(service.Deps{Store: f.db}, "individual", domain.MemoryLimits{})
	if err != nil {
		t.Fatal(err)
	}
	k := &service.Knowledge{Memory: memory}
	rec := &service.Recommender{Deps: service.Deps{Store: f.db, Now: f.now}, Knowledge: k}
	f.svc.b = NewBackend(Services{Projects: f.projects, Tasks: f.tasks, Labels: f.labels, Runs: f.runs, Control: f.control, Knowledge: k, Recommender: rec})
	return f, k
}

func suggestions(t *testing.T, f *fx, who Principal, args map[string]any) domain.RecommendationSet {
	t.Helper()
	r := f.mustCall(who, "recommend_next_actions", args)
	set, ok := r.Data.(Recommendations)
	if !ok {
		t.Fatalf("the result is %T, not the rules' own set", r.Data)
	}
	return set
}

func TestSuggestionsAreAReadThatProposesAndChangesNothing(t *testing.T) {
	f, _ := recFx(t)
	p := f.project("Shop")
	f.ticket(p.ID, "Write docs")
	who := f.session("ast_rec", PermMemoryRead, PermTicketsRead)
	for _, s := range f.svc.Catalog(who) {
		if s.Name == "recommend_next_actions" && (s.Kind != KindRead || s.RequiresConfirmation || s.Permission != PermMemoryRead) {
			t.Fatalf("a suggestion is a read: %+v", s)
		}
	}
	before := len(f.tickets(p.ID))
	res := f.mustCall(who, "recommend_next_actions", map[string]any{"projectId": p.ID})
	if res.Pending != nil {
		t.Fatal("a suggestion must not hold a change for confirmation: it changes nothing")
	}
	if pending, _ := f.svc.Pending(bg, "ast_rec", who); len(pending) != 0 {
		t.Fatalf("%+v", pending)
	}
	if len(f.tickets(p.ID)) != before {
		t.Fatal("a ticket was created")
	}
	if last := f.lastAudit(); last.Operation != "recommend_next_actions" || last.Kind != domain.AuditRead || last.Outcome != "ok" {
		t.Fatalf("it is audited like any read: %+v", last)
	}
	set := suggestions(t, f, who, map[string]any{"projectId": p.ID})
	for _, it := range set.Items {
		if it.Action.StartsExecution {
			t.Fatalf("%+v", it)
		}
	}
	if set.Boundary != domain.RecommendationBoundary {
		t.Fatal("the boundary travels with the answer")
	}
}

func TestSuggestionsDoNotWidenWhatThePrincipalMaySee(t *testing.T) {
	f, _ := recFx(t)
	visible, hidden := f.project("Visible"), f.project("Hidden")
	f.ticket(visible.ID, "Visible task")
	f.ticket(hidden.ID, "Hidden task")
	who := f.session("ast_scope", PermMemoryRead, PermTicketsRead)
	who.Projects = []string{visible.ID}
	set := suggestions(t, f, who, map[string]any{"projectId": visible.ID})
	if raw, _ := json.Marshal(set); strings.Contains(string(raw), "Hidden") || set.ProjectID != visible.ID {
		t.Fatalf("%s", raw)
	}
	args, _ := json.Marshal(map[string]any{"projectId": hidden.ID})
	if _, err := f.svc.Call(bg, who, "recommend_next_actions", args); CodeOf(err) != CodeNotFound {
		t.Fatalf("a project the principal cannot see is not found: %v", err)
	}
	who.Grants = []Permission{PermTicketsRead}
	if _, err := f.svc.Call(bg, who, "recommend_next_actions", mustJSON(map[string]any{"projectId": visible.ID})); CodeOf(err) != CodePermissionDenied {
		t.Fatalf("without memory:read it is refused: %v", err)
	}
	who.Grants, who.Via = []Permission{PermMemoryRead}, "mcp"
	if _, err := f.svc.Call(bg, who, "recommend_next_actions", mustJSON(map[string]any{"projectId": visible.ID})); CodeOf(err) != CodeNotFound {
		t.Fatalf("it belongs to the person's own assistant, never to an outside client: %v", err)
	}
}

func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func TestWhatThePermissionsDoNotCoverIsLeftOutAndSaid(t *testing.T) {
	f, _ := recFx(t)
	p := f.project("Shop")
	f.ticket(p.ID, "Write docs")
	only := f.session("ast_only_memory", PermMemoryRead)
	set := suggestions(t, f, only, map[string]any{"projectId": p.ID})
	if set.Sources.Tasks != domain.SourceStatusDenied {
		t.Fatalf("%+v", set.Sources)
	}
	if raw, _ := json.Marshal(set); strings.Contains(string(raw), "Write docs") {
		t.Fatalf("a task the principal may not read reached the answer: %s", raw)
	}
	more := f.session("ast_tasks", PermMemoryRead, PermTicketsRead)
	if set = suggestions(t, f, more, map[string]any{"projectId": p.ID}); set.Sources.Tasks != domain.SourceStatusRead {
		t.Fatalf("%+v", set.Sources)
	}
}

func TestTheModelCannotChooseWhoItIsOrWhatItMayRead(t *testing.T) {
	f, _ := recFx(t)
	p := f.project("Shop")
	who := f.session("ast_args", PermMemoryRead)
	for _, bad := range []map[string]any{
		{"projectId": p.ID, "viewer": "owner"},
		{"projectId": p.ID, "personalMemory": true},
		{"projectId": p.ID, "redact": false},
		{"projectId": p.ID, "teamMemory": map[string]any{"items": []any{}}},
		{"projectId": p.ID, "focus": "deploy_to_production"},
	} {
		if _, err := f.svc.Call(bg, who, "recommend_next_actions", mustJSON(bad)); CodeOf(err) != CodeInvalidArguments {
			t.Errorf("%v was accepted: %v", bad, err)
		}
	}
	if _, err := f.svc.Call(bg, who, "recommend_next_actions", mustJSON(map[string]any{"projectId": p.ID, "focus": "next_tasks"})); err != nil {
		t.Fatal(err)
	}
}

func TestPersonalMemoryIsOnlyForSessionsNotLimitedToProjects(t *testing.T) {
	f, k := recFx(t)
	p := f.project("Shop")
	if _, err := k.SaveManual(bg, "", service.ManualMemory{Kind: "preference", Summary: "I like refactoring and cleanup of duplicate code"}); err != nil {
		t.Fatal(err)
	}
	who := f.session("ast_personal", PermMemoryRead)
	who.Projects = []string{p.ID}
	if raw, _ := json.Marshal(suggestions(t, f, who, map[string]any{"projectId": p.ID})); strings.Contains(string(raw), "refactoring") {
		t.Fatalf("personal memory reached a session limited to some projects: %s", raw)
	}
}

// ---- Team ----

func linkToTeam(t *testing.T, f *fx, projectID, workspace, teamProject string) {
	t.Helper()
	// The import path needs a repository on disk; what matters here is the stored provenance, so the task is written directly.
	at := f.now()
	task := &domain.Task{ID: domain.NewID(domain.PrefixTask), ProjectID: projectID, Title: "From Team", State: domain.TaskBacklog, Version: 1, CreatedAt: at, UpdatedAt: at,
		SourceRef: integration.SourcePrefix + workspace + ":" + teamProject + ":ttk_1:mem_1"}
	if err := f.db.Update(bg, func(tx store.Tx) error { return tx.Tasks().Create(bg, task) }); err != nil {
		t.Fatal(err)
	}
}

func bindTeam(f *fx, who Principal, items []service.MemoryContextItem, fail bool) {
	f.svc.BindWorkspaces(who.SessionID, []Workspace{{ID: "tw_1", Name: "Team"}}, func(_ context.Context, _ Workspace, method string, _ any) (json.RawMessage, error) {
		if fail {
			return nil, context.DeadlineExceeded
		}
		return json.Marshal(service.MemoryContext{Status: "available", Items: items})
	})
}

func policy(id, text string) service.MemoryContextItem {
	return service.MemoryContextItem{ID: id, Kind: "policy", Type: "policy", Summary: text, Verification: "verified", Freshness: "source_matches", Scope: domain.MemoryScope{ProjectID: "tpj_1"}}
}

func standingCodes(set domain.RecommendationSet) map[string]domain.RecConstraint {
	out := map[string]domain.RecConstraint{}
	for _, c := range set.Standing {
		out[c.Code] = c
	}
	return out
}

func TestTeamPoliciesOfALinkedProjectAreReadThroughTheDesktopAndCited(t *testing.T) {
	f, _ := recFx(t)
	p := f.project("Shop")
	linkToTeam(t, f, p.ID, "tw_1", "tpj_1")
	who := f.session("ast_team", PermMemoryRead, PermTicketsRead)
	bindTeam(f, who, []service.MemoryContextItem{policy("tmem_9", "Main is protected: pull requests, passing tests and review are required.")}, false)
	set := suggestions(t, f, who, map[string]any{"projectId": p.ID})
	if set.Sources.Team != domain.SourceStatusRead {
		t.Fatalf("%+v", set.Sources)
	}
	codes := standingCodes(set)
	if codes["team_pull_request"].Tier != domain.TierPolicy || codes["policy_integration"].Ref != "tmem_9" || codes["policy_integration"].Source != domain.RecSrcTeam {
		t.Fatalf("the Team policy must be cited by record: %+v", set.Standing)
	}
}

func TestTeamMemoryThatCannotBeReadMeansTheCarefulRouteNotConsent(t *testing.T) {
	f, _ := recFx(t)
	p := f.project("Shop")
	linkToTeam(t, f, p.ID, "tw_1", "tpj_1")
	who := f.session("ast_team_down", PermMemoryRead)
	bindTeam(f, who, nil, true)
	set := suggestions(t, f, who, map[string]any{"projectId": p.ID})
	if set.Sources.Team != domain.SourceStatusUnavailable {
		t.Fatalf("%+v", set.Sources)
	}
	if c := standingCodes(set); c["team_pull_request"].Code == "" || c["team_policy_unread"].Code == "" {
		t.Fatalf("an unreachable workspace is a gap that keeps the careful route: %+v", set.Standing)
	}
	// No desktop bridge at all (the browser): the Team link alone still decides the route.
	f.svc.BindWorkspaces(who.SessionID, nil, nil)
	set = suggestions(t, f, who, map[string]any{"projectId": p.ID})
	if set.Sources.Team != domain.SourceStatusNotChecked || standingCodes(set)["team_pull_request"].Code == "" {
		t.Fatalf("%+v %+v", set.Sources, set.Standing)
	}
}

func TestATeamPolicyAboutProjectsTheSessionCannotSeeIsNotUsedOrQuoted(t *testing.T) {
	f, _ := recFx(t)
	p := f.project("Shop")
	linkToTeam(t, f, p.ID, "tw_1", "tpj_1")
	who := f.session("ast_team_scope", PermMemoryRead)
	who.Projects = []string{p.ID}
	secret := policy("tmem_secret", "SECRET cross-project policy: merges need review")
	secret.RelatedScopes = []domain.MemoryScope{{ProjectID: "tpj_1"}, {ProjectID: "tpj_other"}}
	bindTeam(f, who, []service.MemoryContextItem{secret, policy("tmem_ok", "Reviews are required before merging.")}, false)
	set := suggestions(t, f, who, map[string]any{"projectId": p.ID})
	raw, _ := json.Marshal(set)
	if strings.Contains(string(raw), "SECRET") || !strings.Contains(string(raw), "tmem_ok") {
		t.Fatalf("%s", raw)
	}
}

func TestAConnectedWorkspaceProjectGetsTaskSuggestionsAndItsPolicies(t *testing.T) {
	f, _ := recFx(t)
	who := f.session("ast_connected", PermMemoryRead, PermTicketsRead, PermProjectsRead)
	var asked []string
	f.svc.BindWorkspaces(who.SessionID, []Workspace{{ID: "tw_1", Name: "Team"}}, func(_ context.Context, _ Workspace, method string, _ any) (json.RawMessage, error) {
		asked = append(asked, method)
		switch method {
		case "get_project":
			return json.Marshal(ProjectView{ID: "tpj_1", Name: "Shop"})
		case "list_tickets":
			return json.Marshal([]TicketView{{ID: "ttk_1", ProjectID: "tpj_1", Title: "Add the cart", State: "backlog"}, {ID: "ttk_2", ProjectID: "tpj_1", Title: "Done already", State: "done"}})
		case "retrieve_memory":
			return json.Marshal(service.MemoryContext{Status: "available", Items: []service.MemoryContextItem{policy("tmem_1", "No refactoring during the release freeze.")}})
		}
		return nil, context.Canceled
	})
	pid := workspaceProjectID("tw_1", "tpj_1")
	set := suggestions(t, f, who, map[string]any{"projectId": pid})
	if len(set.Items) != 1 || set.Items[0].Intent != domain.IntentNextTasks || set.Items[0].Action.TaskID != workspaceProjectID("tw_1", "ttk_1") {
		t.Fatalf("%+v", set.Items)
	}
	if set.Sources.Team != domain.SourceStatusRead || set.Sources.Git != domain.SourceStatusNotChecked {
		t.Fatalf("no repository is read for a workspace project: %+v", set.Sources)
	}
	for _, m := range asked {
		if m != "get_project" && m != "list_tickets" && m != "retrieve_memory" {
			t.Fatalf("a connected workspace is only read: %v", asked)
		}
	}
	// The workspace being down is a gap, not an empty board.
	f.svc.BindWorkspaces(who.SessionID, []Workspace{{ID: "tw_1", Name: "Team"}}, func(_ context.Context, _ Workspace, method string, _ any) (json.RawMessage, error) {
		if method == "retrieve_memory" {
			return nil, context.DeadlineExceeded
		}
		if method == "get_project" {
			return json.Marshal(ProjectView{ID: "tpj_1", Name: "Shop"})
		}
		return json.Marshal([]TicketView{})
	})
	if set = suggestions(t, f, who, map[string]any{"projectId": pid}); set.Sources.Team != domain.SourceStatusUnavailable {
		t.Fatalf("%+v", set.Sources)
	}
}

func TestTeamMemoryIsNotReadForAViewerWhoMayNotReadMemory(t *testing.T) {
	f, _ := recFx(t)
	p := f.project("Shop")
	linkToTeam(t, f, p.ID, "tw_1", "tpj_1")
	reads := 0
	call := func(_ context.Context, _ Workspace, method string, _ any) (json.RawMessage, error) {
		reads++
		if method == "get_project" {
			return json.Marshal(ProjectView{ID: "tpj_1", Name: "Shop"})
		}
		if method == "list_tickets" {
			return json.Marshal([]TicketView{})
		}
		return json.Marshal(service.MemoryContext{Status: "available", Items: []service.MemoryContextItem{policy("tmem_secret", "SECRET-POLICY text")}})
	}
	b := &workspaceBackend{Backend: f.svc.b, binding: workspaceBinding{workspaces: []Workspace{{ID: "tw_1", Name: "Team"}}, call: call}}
	noMemory := domain.RecViewer{Kind: "assistant", Tasks: true}
	set, err := b.Recommend(bg, RecommendRequest{ProjectID: p.ID, Now: f.now(), Viewer: noMemory})
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := json.Marshal(set); strings.Contains(string(raw), "SECRET-POLICY") || reads != 0 || set.Sources.Team != domain.SourceStatusDenied {
		t.Fatalf("Team memory was read for a viewer who may not read memory (reads %d): %s", reads, raw)
	}
	// The same for a project that lives in the workspace.
	reads = 0
	set, err = b.Recommend(bg, RecommendRequest{ProjectID: workspaceProjectID("tw_1", "tpj_1"), Now: f.now(), Viewer: noMemory})
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := json.Marshal(set); strings.Contains(string(raw), "SECRET-POLICY") || set.Sources.Team != domain.SourceStatusDenied {
		t.Fatalf("%s", raw)
	}
}

func TestMoreLinkedTeamProjectsThanCanBeReadIsAGapNotSilence(t *testing.T) {
	f, _ := recFx(t)
	p := f.project("Shop")
	for i := 0; i < service.MaxTeamReads+2; i++ {
		linkToTeam(t, f, p.ID, "tw_1", fmt.Sprintf("tpj_%02d", i))
	}
	who := f.session("ast_many", PermMemoryRead)
	bindTeam(f, who, []service.MemoryContextItem{policy("tmem_1", "Reviews are required before merging.")}, false)
	set := suggestions(t, f, who, map[string]any{"projectId": p.ID})
	if set.Sources.Team != domain.SourceStatusUnavailable {
		t.Fatalf("some linked projects were not read, and that must not read as complete: %+v", set.Sources)
	}
	if c := standingCodes(set); c["team_pull_request"].Code == "" || c["team_policy_unread"].Code == "" {
		t.Fatalf("%+v", set.Standing)
	}
}
