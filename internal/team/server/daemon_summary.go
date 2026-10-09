package server

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"time"

	"devboard/internal/httpkit"
	"devboard/internal/team/domain"
	"devboard/internal/team/service"
	"devboard/internal/workspace"
)

// The desktop shell shows what a person has to do across all of their workspaces. For a Team workspace this device
// answers in the shell's words (internal/workspace) from what the person's own Workspace Host says to the person's own
// device credential: their tickets, the reviews they may do, the schedules they own, and how the machines that keep the
// workspace running are doing. It is a translation of reads the console makes anyway; it adds no authority and reveals
// nothing the member cannot already see. Personal data does not come in here and nothing here goes to the Personal side.

// summaryInputs is everything the summary is made of, gathered by the caller, so the translation is a pure function.
type summaryInputs struct {
	Slot, Name, Role string
	DeviceRoles      []string
	State            workspace.State
	Detail           string
	MemberID         string
	Now              time.Time

	MyWork     *service.MyWork
	Reviews    *service.ReviewQueue
	Overview   *service.Overview
	Resilience *domain.Resilience
	ReadOnly   bool
	// Schedules are the shared schedule requests, by project.
	Schedules map[string][]domain.Schedule
}

func ticketHref(projectID, ticketID string) string {
	return "?tab=board&project=" + projectID + "&ticket=" + ticketID
}

func statusOf(s domain.TicketStatus) workspace.Status {
	switch s {
	case domain.TicketInProgress:
		return workspace.StatusDoing
	case domain.TicketReview:
		return workspace.StatusReview
	case domain.TicketDone:
		return workspace.StatusDone
	}
	return workspace.StatusTodo
}

func scheduleExecution(state string) workspace.Execution {
	switch state {
	case "started", "running":
		return workspace.ExecRunning
	case "claimed", "approved", "waiting", "scheduled", "pending":
		return workspace.ExecQueued
	case "failed", "refused", "missed":
		return workspace.ExecFailed
	case "canceled":
		return workspace.ExecCanceled
	case "completed":
		return workspace.ExecCompleted
	}
	return workspace.ExecNone
}

func severityOf(s domain.Severity) workspace.Severity {
	switch s {
	case domain.SeverityCritical:
		return workspace.SeverityCritical
	case domain.SeverityWarning:
		return workspace.SeverityWarning
	}
	return workspace.SeverityInfo
}

// buildSummary translates one workspace. Anything missing (a member cannot see the infrastructure report; a
// disconnected workspace has nothing) leaves its part empty rather than inventing it.
func buildSummary(in summaryInputs) workspace.Summary {
	out := workspace.Summary{
		Schema: workspace.Schema,
		Workspace: workspace.Entry{ID: workspace.TeamID(in.Slot), Kind: workspace.KindTeam, Name: in.Name, Role: in.Role,
			State: in.State, Detail: in.Detail, DeviceRoles: in.DeviceRoles},
		Projects: []workspace.Project{}, Work: []workspace.Item{}, Attention: []workspace.Attention{}, Schedule: []workspace.Scheduled{},
		At: in.Now.UTC(),
	}
	if out.Workspace.Name == "" {
		out.Workspace.Name = "Team"
	}
	titles := map[string]string{}
	projectNames := map[string]string{}
	if in.Overview != nil {
		for _, p := range in.Overview.Projects {
			if p.Project.Archived {
				continue
			}
			projectNames[p.Project.ID] = p.Project.Name
			out.Projects = append(out.Projects, workspace.Project{ID: p.Project.ID, Name: p.Project.Name, Href: "?tab=board&project=" + p.Project.ID})
		}
	}
	if in.MyWork != nil {
		seen := map[string]bool{}
		add := func(items []service.WorkItem) {
			for _, w := range items {
				k := w.Ticket
				if seen[k.ID] {
					continue
				}
				seen[k.ID] = true
				titles[k.ID] = k.Key + " " + k.Title
				out.Work = append(out.Work, workspace.Item{ID: k.ID, Title: k.Key + " " + k.Title, Project: w.Project.Name, Status: statusOf(k.Status),
					Href: ticketHref(k.ProjectID, k.ID), UpdatedAt: k.UpdatedAt})
			}
		}
		add(in.MyWork.InProgress)
		add(in.MyWork.Submitted)
		for _, a := range in.MyWork.NeedsAction {
			kind := workspace.AttentionOther
			switch a.Kind {
			case "changes_requested":
				kind = workspace.AttentionChangesRequested
			case "conflict":
				kind = workspace.AttentionConflict
			case "review_requested":
				kind = workspace.AttentionReview
			}
			sev := workspace.SeverityInfo
			switch a.Level {
			case "problem":
				sev = workspace.SeverityWarning
			case "warning":
				sev = workspace.SeverityInfo
			}
			out.Attention = append(out.Attention, workspace.Attention{ID: "act-" + a.TicketID + "-" + a.Kind, Kind: kind, Severity: sev, Title: a.TicketKey + ": " + a.Message,
				Project: projectNames[a.ProjectID], Href: ticketHref(a.ProjectID, a.TicketID)})
		}
	}
	if in.Reviews != nil {
		for _, r := range in.Reviews.Items {
			if r.Mine || (!r.CanReview && !r.RequestedOfMe) {
				continue
			}
			sev := workspace.SeverityInfo
			if r.RequestedOfMe {
				sev = workspace.SeverityWarning
			}
			k := r.Ticket
			out.Attention = append(out.Attention, workspace.Attention{ID: "review-" + k.ID, Kind: workspace.AttentionReview, Severity: sev,
				Title: k.Key + " " + k.Title + " is waiting for a review", Project: r.Project.Name, Href: ticketHref(k.ProjectID, k.ID), At: k.UpdatedAt})
		}
	}
	for pid, list := range in.Schedules {
		for _, s := range list {
			if s.MemberID != in.MemberID || s.Terminal() {
				continue
			}
			title := titles[s.TicketID]
			if title == "" {
				title = "A ticket you hold"
			}
			out.Schedule = append(out.Schedule, workspace.Scheduled{ID: s.ID, Title: title, Project: projectNames[pid], At: s.At.UTC(), Zone: s.Timezone,
				State: s.State, Href: ticketHref(pid, s.TicketID)})
			if e := scheduleExecution(s.State); e == workspace.ExecFailed {
				out.Attention = append(out.Attention, workspace.Attention{ID: "sched-" + s.ID, Kind: workspace.AttentionBlocked, Severity: workspace.SeverityWarning,
					Title: title + " could not start on schedule", Detail: s.Reason, Project: projectNames[pid], Href: ticketHref(pid, s.TicketID)})
			}
		}
	}
	sort.SliceStable(out.Schedule, func(i, j int) bool { return out.Schedule[i].At.Before(out.Schedule[j].At) })

	if r := in.Resilience; r != nil {
		infra := &workspace.Infra{Writable: r.Writable, HostsConfigured: r.WorkspaceHosts.Configured, HostsOnline: r.WorkspaceHosts.Online}
		for _, c := range []domain.Check{r.Hosts, r.Quorum, r.Database, r.Connectivity, r.RemoteAccess, r.Backups, r.Runners} {
			infra.Checks = append(infra.Checks, workspace.Check{Label: c.Label, Value: c.Value, State: string(c.State)})
		}
		for _, a := range r.Advice {
			text := a.Title
			if a.Detail != "" {
				text += " " + a.Detail
			}
			infra.Warnings = append(infra.Warnings, text)
			if a.Severity == domain.SeverityInfo {
				continue
			}
			out.Attention = append(out.Attention, workspace.Attention{ID: "host-" + a.Code, Kind: workspace.AttentionHost, Severity: severityOf(a.Severity),
				Title: a.Title, Detail: a.Detail, Href: adviceHref(a)})
		}
		out.Infra = infra
	} else if in.ReadOnly {
		out.Attention = append(out.Attention, workspace.Attention{ID: "host-read-only", Kind: workspace.AttentionHost, Severity: workspace.SeverityCritical,
			Title: "This workspace is read-only for now", Detail: "You can keep reading. Changes can be saved again when enough Workspace Hosts are back online."})
		out.Infra = &workspace.Infra{Writable: false}
	}
	sort.SliceStable(out.Attention, func(i, j int) bool {
		return attentionRank(out.Attention[i].Severity) < attentionRank(out.Attention[j].Severity)
	})
	return out
}

func attentionRank(s workspace.Severity) int {
	switch s {
	case workspace.SeverityCritical:
		return 0
	case workspace.SeverityWarning:
		return 1
	}
	return 2
}

func adviceHref(a domain.Advice) string {
	if a.Action == nil {
		return "?tab=hosts"
	}
	switch a.Action.Kind {
	case "set_up_backups", "back_up_now":
		return "?tab=backups"
	case "add_connectivity_host":
		return "?tab=connectivity"
	}
	return "?tab=hosts"
}

// workspaceSummary answers GET …/api/device/v1/summary for this workspace.
func (d *Daemon) workspaceSummary(w http.ResponseWriter, r *http.Request) {
	httpkit.WriteJSON(w, http.StatusOK, d.gatherSummary(r.Context()))
}

func (d *Daemon) gatherSummary(ctx context.Context) workspace.Summary {
	slot := d.o.Slot
	if slot == "" {
		slot = MainSlot
	}
	view := d.stateView()
	in := summaryInputs{Slot: slot, Now: time.Now()}
	if ws, ok := view["workspace"].(map[string]any); ok {
		in.Name, _ = ws["name"].(string)
	}
	in.Role, _ = view["role"].(string)
	if rn, _ := view["runner"].(map[string]any); rn != nil {
		if on, _ := rn["connected"].(bool); on {
			in.DeviceRoles = append(in.DeviceRoles, "runner")
		}
	}
	if host, _ := view["workspaceHost"].(bool); host {
		in.DeviceRoles = append(in.DeviceRoles, "workspace_host")
	}
	occ := d.occupancy()
	op, _ := view["operation"].(string)
	problem, _ := view["error"].(string)
	connected, _ := view["connected"].(bool)
	switch {
	case occ.Leaving:
		in.State, in.Detail = workspace.StateLeaving, "Leaving this workspace."
	case !occ.Enrolled && (occ.Pending || op != ""):
		in.State = workspace.StateSetup
		in.Detail = "Setting up this workspace."
		if occ.Pending {
			in.Detail = "Waiting for an administrator to approve this computer."
		}
		if in.Name == "" {
			in.Name = "New Team workspace"
		}
	case !occ.Enrolled:
		in.State, in.Detail = workspace.StateSetup, "Create a Team or join one with an invitation."
		if in.Name == "" {
			in.Name = "New Team workspace"
		}
	case !connected:
		in.State, in.Detail = workspace.StateConnecting, "Connecting to a Workspace Host."
		if problem != "" {
			in.State, in.Detail = workspace.StateOffline, problem
		}
	default:
		in.State = workspace.StateReady
	}
	if in.State != workspace.StateReady {
		return buildSummary(in)
	}

	d.mu.RLock()
	host := d.host
	member := d.memberID
	d.mu.RUnlock()
	in.MemberID = member
	if host == nil {
		in.State, in.Detail = workspace.StateConnecting, "Connecting to a Workspace Host."
		return buildSummary(in)
	}
	var mw service.MyWork
	if err := host.Do(ctx, http.MethodGet, "/my-work", nil, &mw); err != nil {
		// The workspace answered the heartbeat a moment ago and not this: say it is unreachable, not that it is empty.
		in.State, in.Detail = workspace.StateOffline, "The workspace did not answer. Keep a Workspace Host online."
		return buildSummary(in)
	}
	in.MyWork = &mw
	var ov service.Overview
	if err := host.Do(ctx, http.MethodGet, "/overview", nil, &ov); err == nil {
		in.Overview = &ov
	}
	var rq service.ReviewQueue
	if err := host.Do(ctx, http.MethodGet, "/reviews", nil, &rq); err == nil {
		in.Reviews = &rq
	}
	if ds, err := host.Devices(ctx); err == nil {
		for _, dv := range ds {
			if dv.ID == d.deviceIDOrEmpty() {
				for _, c := range dv.Capabilities {
					if (c == domain.CapabilityConnectivityHost || c == domain.CapabilityWorkspaceHost) && !slices.Contains(in.DeviceRoles, string(c)) {
						in.DeviceRoles = append(in.DeviceRoles, string(c))
					}
				}
			}
		}
	}
	var res domain.Resilience
	if err := host.Do(ctx, http.MethodGet, "/resilience", nil, &res); err == nil {
		in.Resilience = &res
	} else {
		var health struct {
			Storage struct {
				ReadOnly bool `json:"readOnly"`
			} `json:"storage"`
		}
		if err := host.Do(ctx, http.MethodGet, "/health", nil, &health); err == nil {
			in.ReadOnly = health.Storage.ReadOnly
		}
	}
	// Only the projects the person has something scheduled in are asked about, and at most a few of them.
	in.Schedules = map[string][]domain.Schedule{}
	asked := 0
	for _, w := range append(append([]service.WorkItem{}, mw.InProgress...), mw.Submitted...) {
		pid := w.Project.ID
		if _, done := in.Schedules[pid]; done || asked >= 8 {
			continue
		}
		asked++
		var list []domain.Schedule
		if err := host.Do(ctx, http.MethodGet, fmt.Sprintf("/projects/%s/schedules", pid), nil, &list); err == nil {
			in.Schedules[pid] = list
		}
	}
	return buildSummary(in)
}

func (d *Daemon) deviceIDOrEmpty() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.mat == nil {
		return ""
	}
	return d.mat.Meta.HostDeviceID
}
