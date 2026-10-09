package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"devboard/internal/team/domain"
	"devboard/internal/team/service"
	"devboard/internal/workspace"
)

func ticketItem(id, pid, key, title string, st domain.TicketStatus) service.WorkItem {
	return service.WorkItem{Ticket: domain.Ticket{ID: id, ProjectID: pid, Key: key, Title: title, Status: st, UpdatedAt: time.Unix(1_800_000_000, 0)}, Project: service.ProjectRef{ID: pid, Name: "Platform"}}
}

func TestATeamWorkspaceIsTranslatedIntoTheShellsWordsAndPassesItsStrictReader(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	in := summaryInputs{
		Slot: "main", Name: "Acme", Role: "admin", State: workspace.StateReady, DeviceRoles: []string{"runner", "workspace_host"}, MemberID: "tmb_me", Now: now,
		MyWork: &service.MyWork{
			InProgress:  []service.WorkItem{ticketItem("ttk_1", "tpj_1", "WB-1", "Fix login", domain.TicketInProgress)},
			Submitted:   []service.WorkItem{ticketItem("ttk_2", "tpj_1", "WB-2", "Docs", domain.TicketReview)},
			NeedsAction: []service.Action{{Kind: "changes_requested", Level: "problem", TicketID: "ttk_2", TicketKey: "WB-2", ProjectID: "tpj_1", Message: "Changes were requested."}},
		},
		Reviews: &service.ReviewQueue{Items: []service.ReviewItem{
			{WorkItem: ticketItem("ttk_9", "tpj_1", "WB-9", "Someone else's", domain.TicketReview), CanReview: true, RequestedOfMe: true},
			{WorkItem: ticketItem("ttk_2", "tpj_1", "WB-2", "Docs", domain.TicketReview), Mine: true, CanReview: true},
			{WorkItem: ticketItem("ttk_8", "tpj_1", "WB-8", "Not mine to review", domain.TicketReview)},
		}},
		Overview: &service.Overview{Projects: []service.ProjectSummary{{Project: domain.Project{ID: "tpj_1", Name: "Platform"}}, {Project: domain.Project{ID: "tpj_old", Name: "Old", Archived: true}}}},
		Schedules: map[string][]domain.Schedule{"tpj_1": {
			{ID: "sch_1", ProjectID: "tpj_1", TicketID: "ttk_1", MemberID: "tmb_me", At: now.Add(time.Hour), Timezone: "UTC", State: "scheduled"},
			{ID: "sch_2", ProjectID: "tpj_1", TicketID: "ttk_5", MemberID: "tmb_other", At: now.Add(time.Hour), State: "scheduled"},
			{ID: "sch_3", ProjectID: "tpj_1", TicketID: "ttk_1", MemberID: "tmb_me", At: now.Add(-time.Hour), State: "completed"},
		}},
		Resilience: &domain.Resilience{Writable: true, WorkspaceHosts: domain.HostCounts{Configured: 3, Online: 2},
			Hosts: domain.Check{Label: "Hosts", Value: "2 of 3 online", State: domain.CheckWarn},
			Advice: []domain.Advice{
				{Code: "hosts_offline", Severity: domain.SeverityWarning, Title: "A Workspace Host is offline", Action: &domain.AdviceAction{Kind: "bring_hosts_back", Label: "Review"}},
				{Code: "no_backups", Severity: domain.SeverityInfo, Title: "Set up backups"},
			}},
	}
	raw, err := json.Marshal(buildSummary(in))
	if err != nil {
		t.Fatal(err)
	}
	got, dropped, err := workspace.Decode(strings.NewReader(string(raw)), workspace.TeamID("main"))
	if err != nil || dropped != 0 {
		t.Fatalf("%v dropped %d\n%s", err, dropped, raw)
	}
	if len(got.Projects) != 1 || got.Projects[0].Name != "Platform" {
		t.Fatalf("an archived project is listed: %+v", got.Projects)
	}
	if len(got.Work) != 2 || got.Work[0].Status != workspace.StatusDoing || got.Work[1].Status != workspace.StatusReview || got.Work[0].Href != "?tab=board&project=tpj_1&ticket=ttk_1" {
		t.Fatalf("work = %+v", got.Work)
	}
	// Only the schedules the person owns and that are not over.
	if len(got.Schedule) != 1 || got.Schedule[0].ID != "sch_1" || got.Schedule[0].Title != "WB-1 Fix login" {
		t.Fatalf("schedule = %+v", got.Schedule)
	}
	// Reviews they may do, not their own and not ones they cannot review.
	reviews := 0
	var kinds []string
	for _, a := range got.Attention {
		kinds = append(kinds, a.Kind)
		if a.Kind == workspace.AttentionReview {
			reviews++
			if !strings.Contains(a.Title, "WB-9") {
				t.Errorf("review = %+v", a)
			}
		}
	}
	if reviews != 1 {
		t.Fatalf("reviews = %d in %v", reviews, kinds)
	}
	if got.Attention[0].Severity != workspace.SeverityWarning {
		t.Fatalf("attention is not worst first: %+v", got.Attention)
	}
	if got.Infra == nil || got.Infra.HostsConfigured != 3 || got.Infra.HostsOnline != 2 || len(got.Infra.Warnings) != 2 {
		t.Fatalf("infra = %+v", got.Infra)
	}
	hostAdvice := false
	for _, a := range got.Attention {
		if a.Kind == workspace.AttentionHost {
			hostAdvice = a.Href == "?tab=hosts"
		}
	}
	if !hostAdvice {
		t.Fatalf("no host attention linking to the hosts page: %+v", got.Attention)
	}
}

func TestAMemberWhoCannotSeeTheInfrastructureStillLearnsTheWorkspaceIsReadOnly(t *testing.T) {
	sum := buildSummary(summaryInputs{Slot: "main", Name: "Acme", Role: "member", State: workspace.StateReady, ReadOnly: true, Now: time.Now()})
	if sum.Infra == nil || sum.Infra.Writable || len(sum.Attention) != 1 || sum.Attention[0].Severity != workspace.SeverityCritical {
		t.Fatalf("%+v", sum)
	}
	raw, _ := json.Marshal(sum)
	if _, _, err := workspace.Decode(strings.NewReader(string(raw)), workspace.TeamID("main")); err != nil {
		t.Fatal(err)
	}
}

func TestAWorkspaceThatIsNotUsableYetSaysSoAndShowsNoWork(t *testing.T) {
	h := testHub(t)
	w := hubCall(h, "GET", "/w/main/api/device/v1/summary", "")
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	got, _, err := workspace.Decode(strings.NewReader(w.Body.String()), workspace.TeamID("main"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Workspace.State != workspace.StateSetup || len(got.Work) != 0 || got.Workspace.Name == "" {
		t.Fatalf("%+v", got.Workspace)
	}
	// An enrolled workspace whose host cannot be reached is offline, not empty.
	h2 := testHub(t)
	main := h2.slots[MainSlot]
	v, _ := main.d.setupVault(main.d.workspaceConfig())
	occupyAsHost(t, v, "tws_x", "10.233.0.0/16")
	_ = main.d.loadDevice()
	main.d.mu.Lock()
	main.d.lastError = "your workspace is offline; keep a Workspace Host online to reconnect"
	main.d.mu.Unlock()
	w = hubCall(h2, "GET", "/w/main/api/device/v1/summary", "")
	got, _, err = workspace.Decode(strings.NewReader(w.Body.String()), workspace.TeamID("main"))
	if err != nil || got.Workspace.State != workspace.StateOffline || !strings.Contains(got.Workspace.Detail, "offline") {
		t.Fatalf("%v %+v", err, got.Workspace)
	}
}
