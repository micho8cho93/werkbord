package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"devboard/internal/httpkit"
	"devboard/internal/integration"
	"devboard/internal/team/connector"
	"devboard/internal/team/devicestate"
	"devboard/internal/team/domain"
	"devboard/internal/team/hostclient"
	"devboard/internal/team/localwerkbord"
	"devboard/internal/team/service"
)

type localExecutionInput struct {
	ProjectID   string `json:"projectId"`
	TicketID    string `json:"ticketId"`
	ExecutionID string `json:"executionId"`
	RunnerID    string `json:"runnerId"`
	AgentID     string `json:"agentId"`
	Model       string `json:"model"`
	Reasoning   string `json:"reasoning"`
	Interaction string `json:"interaction"`
	Digest      string `json:"digest"`
	Preapprove  bool   `json:"preapprove"`
}

// localExecutionContext always refreshes membership and holder. The local device
// credential is authenticated by Daemon.Handler, independently of workspace roles.
func (d *Daemon) localExecutionContext(ctx context.Context, in localExecutionInput) (*localwerkbord.Client, *hostclient.Client, domain.Project, domain.Ticket, string, error) {
	var p domain.Project
	var k domain.Ticket
	if !integration.Identifier(in.ProjectID) || !integration.Identifier(in.TicketID) {
		return nil, nil, p, k, "", errors.New("invalid ticket identity")
	}
	d.mu.RLock()
	bridge, host, mat, member := d.bridge, d.host, d.mat, d.memberID
	d.mu.RUnlock()
	if bridge == nil || host == nil || mat == nil || member == "" {
		return nil, nil, p, k, "", errors.New("connect your workspace and local runner first")
	}
	me, err := host.Me(ctx)
	if err != nil {
		return nil, nil, p, k, "", err
	}
	if me.Member.ID != member || me.Workspace.ID != mat.Meta.WorkspaceID {
		return nil, nil, p, k, "", errors.New("workspace identity changed")
	}
	if err := host.Do(ctx, "GET", "/projects/"+in.ProjectID, nil, &p); err != nil {
		return nil, nil, p, k, "", err
	}
	if err := host.Do(ctx, "GET", "/projects/"+in.ProjectID+"/tickets/"+in.TicketID, nil, &k); err != nil {
		return nil, nil, p, k, "", err
	}
	if k.AssigneeID != member || !k.Status.Held() || k.ArchivedAt != nil || p.Archived {
		return nil, nil, p, k, "", errors.New("only the current holder can control their ticket")
	}
	return bridge, host, p, k, service.ScheduleContext(me.Workspace.ID, p, k), nil
}
func (d *Daemon) importExecutionTask(ctx context.Context, bridge *localwerkbord.Client, host *hostclient.Client, p domain.Project, k domain.Ticket) (string, string, error) {
	hf, err := host.Handoff(ctx, p.ID, k.ID)
	if err != nil {
		return "", "", err
	}
	d.mu.RLock()
	w, member := d.mat.Meta.WorkspaceID, d.memberID
	d.mu.RUnlock()
	if hf.For.ID != member || hf.Project.ID != p.ID || hf.Ticket.ID != k.ID {
		return "", "", errors.New("handoff identity changed")
	}
	projects, err := bridge.IntegrationProjects(ctx)
	if err != nil {
		return "", "", err
	}
	want, err := integration.RepositoryIdentity(p.Repository)
	if err != nil {
		return "", "", err
	}
	pid := ""
	for _, proj := range projects {
		for _, remote := range proj.Remotes {
			got, err := integration.RepositoryIdentity(remote)
			if err == nil && got == want {
				if pid != "" && pid != proj.ID {
					return "", "", errors.New("multiple local repositories match; configure the Phase 1 connector selection")
				}
				pid = proj.ID
				break
			}
		}
	}
	if pid == "" {
		return "", "", errors.New("register this repository in your Individual controller first")
	}
	title := []rune(hf.Ticket.Key + ": " + hf.Ticket.Title)
	if len(title) > 200 {
		title = title[:200]
	}
	branch := hf.Git.Branch
	prefix := "wb/" + w + "/"
	if !strings.HasPrefix(branch, prefix) {
		branch = prefix + branch
	}
	description := "Local working branch: " + branch + ". Use this task's working branch in place of the Team branch suggestion below.\n\n" + hf.Prompt
	in := integration.Import{Schema: integration.Schema, ProjectID: pid, SourceRef: connector.Source(w, p.ID, k.ID, member), Repository: p.Repository, Title: string(title), Description: description, WorkBranch: branch, BaseBranch: hf.Git.BaseBranch}
	for _, base := range host.Bases() {
		in.SourceAliases = append(in.SourceAliases, base+"/?tab=board&project="+url.QueryEscape(p.ID)+"&ticket="+url.QueryEscape(k.ID))
	}
	out, err := bridge.Import(ctx, in)
	if err != nil {
		return "", "", err
	}
	// An already-run/locally edited task may intentionally differ. The exact local
	// context is displayed and hashed by Individual before authorizing execution.
	return pid, out.TaskID, nil
}
func (d *Daemon) executionRequest(ctx context.Context, in localExecutionInput) (*localwerkbord.Client, integration.ExecutionRequest, error) {
	bridge, host, p, k, fence, err := d.localExecutionContext(ctx, in)
	if err != nil {
		return nil, integration.ExecutionRequest{}, err
	}
	pid, tid, err := d.importExecutionTask(ctx, bridge, host, p, k)
	if err != nil {
		return nil, integration.ExecutionRequest{}, err
	}
	req := integration.ExecutionRequest{ExecutionID: in.ExecutionID, ProjectID: pid, TaskID: tid, Fence: fence, RunnerID: in.RunnerID, AgentID: in.AgentID, Model: in.Model, Reasoning: in.Reasoning, Interaction: in.Interaction}
	var schedules []domain.Schedule
	if err := host.Do(ctx, "GET", "/projects/"+p.ID+"/schedules", nil, &schedules); err != nil {
		return nil, req, err
	}
	for _, v := range schedules {
		if v.TicketID == k.ID && v.ExecutionID == in.ExecutionID && !v.Terminal() {
			if v.Fence != fence {
				return nil, req, errors.New("schedule assignment or ticket text changed")
			}
			req.Fence = service.ScheduleFence(v)
			req.NotBefore = v.At
			if v.MissedPolicy == "skip" {
				req.Deadline = v.At.Add(time.Duration(v.GraceSeconds) * time.Second)
			}
		}
	}
	return bridge, req, nil
}
func (d *Daemon) previewExecution(w http.ResponseWriter, r *http.Request) {
	var in localExecutionInput
	if !daemonDecode(w, r, &in) {
		return
	}
	bridge, req, err := d.executionRequest(r.Context(), in)
	if err != nil {
		daemonFail(w, err)
		return
	}
	out, err := bridge.ExecutionPreview(r.Context(), req)
	if err != nil {
		daemonFail(w, err)
		return
	}
	httpkit.WriteJSON(w, 200, out)
}
func (d *Daemon) approveExecution(w http.ResponseWriter, r *http.Request) {
	var in localExecutionInput
	if !daemonDecode(w, r, &in) {
		return
	}
	bridge, req, err := d.executionRequest(r.Context(), in)
	if err != nil {
		daemonFail(w, err)
		return
	}
	preview, err := bridge.ExecutionPreview(r.Context(), req)
	if err != nil {
		daemonFail(w, err)
		return
	}
	if preview.Digest != in.Digest {
		daemonFail(w, errors.New("context or policy changed; review the effective policy again"))
		return
	}
	expires := time.Now().Add(time.Hour)
	if in.Preapprove {
		expires = time.Now().Add(30 * 24 * time.Hour)
	}
	if !req.Deadline.IsZero() && req.Deadline.Before(expires) {
		expires = req.Deadline
	}
	out, err := bridge.ApproveExecution(r.Context(), preview, expires)
	if err != nil {
		daemonFail(w, err)
		return
	}
	_, _, _, _, context, err := d.localExecutionContext(r.Context(), in)
	if err == nil {
		_, fresh, e := d.executionRequest(r.Context(), in)
		if e != nil {
			err = e
		} else if fresh != req {
			err = errors.New("ticket context changed during approval")
		}
	}
	if err != nil {
		_ = bridge.RevokeExecution(r.Context(), req.ExecutionID)
		daemonFail(w, err)
		return
	}
	if err := d.state.NoteExecution(devicestate.ExecutionRef{ExecutionID: req.ExecutionID, TeamProjectID: in.ProjectID, TicketID: in.TicketID, Fence: req.Fence, Context: context, Title: preview.Title, ExpiresAt: expires}); err != nil {
		daemonFail(w, err)
		return
	}
	httpkit.WriteJSON(w, 200, out)
}
func (d *Daemon) startExecution(w http.ResponseWriter, r *http.Request) {
	var in localExecutionInput
	if !daemonDecode(w, r, &in) {
		return
	}
	bridge, req, err := d.executionRequest(r.Context(), in)
	if err != nil {
		daemonFail(w, err)
		return
	}
	a, err := bridge.ExecutionApproval(r.Context(), integration.ExecutionDispatch{ExecutionID: req.ExecutionID, Fence: req.Fence})
	if err != nil {
		daemonFail(w, err)
		return
	}
	if a.Preview.Request != req {
		daemonFail(w, errors.New("selection differs from the approved policy"))
		return
	}
	if !req.NotBefore.IsZero() {
		d.mu.RLock()
		host, device := d.host, d.mat.Host.DeviceID()
		d.mu.RUnlock()
		var schedules []domain.Schedule
		if err := host.Do(r.Context(), "GET", "/projects/"+in.ProjectID+"/schedules", nil, &schedules); err != nil {
			daemonFail(w, err)
			return
		}
		found := false
		for _, v := range schedules {
			if v.ExecutionID == req.ExecutionID && service.ScheduleFence(v) == req.Fence && !v.Terminal() {
				if err := connector.CoordinateSchedule(r.Context(), host, bridge, device, v, func(request integration.ExecutionRequest) bool { return request == req }); err != nil {
					daemonFail(w, err)
					return
				}
				found = true
			}
		}
		if !found {
			daemonFail(w, errors.New("schedule changed or canceled"))
			return
		}
		recovered, err := bridge.ExecutionApproval(r.Context(), integration.ExecutionDispatch{ExecutionID: req.ExecutionID, Fence: req.Fence})
		if err != nil {
			daemonFail(w, err)
			return
		}
		httpkit.WriteJSON(w, 200, map[string]string{"runId": recovered.RunID})
		return
	}
	run, err := bridge.DispatchExecution(r.Context(), integration.ExecutionDispatch{ExecutionID: req.ExecutionID, Fence: req.Fence})
	if err != nil {
		daemonFail(w, err)
		return
	}
	httpkit.WriteJSON(w, 200, map[string]string{"runId": run})
}
func (d *Daemon) executionDetail(w http.ResponseWriter, r *http.Request) {
	in := localExecutionInput{ProjectID: r.URL.Query().Get("projectId"), TicketID: r.URL.Query().Get("ticketId")}
	bridge, host, p, k, _, err := d.localExecutionContext(r.Context(), in)
	if err != nil {
		daemonFail(w, err)
		return
	}
	pid, tid, err := d.importExecutionTask(r.Context(), bridge, host, p, k)
	if err != nil {
		daemonFail(w, err)
		return
	}
	out, err := bridge.TaskDetail(r.Context(), pid, tid)
	if err != nil {
		daemonFail(w, err)
		return
	}
	httpkit.WriteJSON(w, 200, out)
}
func (d *Daemon) executionControl(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ProjectID  string `json:"projectId"`
		TicketID   string `json:"ticketId"`
		RunID      string `json:"runId"`
		QuestionID string `json:"questionId"`
		Option     string `json:"option"`
		Reply      string `json:"reply"`
	}
	if !daemonDecode(w, r, &in) {
		return
	}
	bridge, host, p, k, _, err := d.localExecutionContext(r.Context(), localExecutionInput{ProjectID: in.ProjectID, TicketID: in.TicketID})
	if err != nil {
		daemonFail(w, err)
		return
	}
	pid, tid, err := d.importExecutionTask(r.Context(), bridge, host, p, k)
	if err != nil {
		daemonFail(w, err)
		return
	}
	detail, err := bridge.TaskDetail(r.Context(), pid, tid)
	if err != nil {
		daemonFail(w, err)
		return
	}
	owned := false
	for _, run := range detail["runs"].([]map[string]any) {
		if run["id"] == in.RunID {
			owned = true
		}
	}
	if !owned {
		daemonFail(w, errors.New("run does not belong to this ticket"))
		return
	}
	if r.PathValue("action") == "stop" {
		err = bridge.StopRun(r.Context(), in.RunID)
	} else if r.PathValue("action") == "answer" {
		err = bridge.Answer(r.Context(), in.RunID, in.QuestionID, in.Option, in.Reply)
	} else {
		err = errors.New("unsupported action; this runtime supports stop/restart")
	}
	if err != nil {
		daemonFail(w, err)
		return
	}
	w.WriteHeader(204)
}

func (d *Daemon) coordinateSchedules(ctx context.Context, host *hostclient.Client, local *localwerkbord.Client, device string) error {
	refs := d.state.Executions()
	projects := map[string]bool{}
	for _, ref := range refs {
		projects[ref.TeamProjectID] = true
	}
	for pid := range projects {
		var schedules []domain.Schedule
		if err := host.Do(ctx, "GET", "/projects/"+pid+"/schedules", nil, &schedules); err != nil {
			return err
		}
		for _, v := range schedules {
			for _, ref := range refs {
				if ref.ExecutionID == v.ExecutionID && ref.Fence == service.ScheduleFence(v) {
					if err := connector.CoordinateSchedule(ctx, host, local, device, v, func(req integration.ExecutionRequest) bool {
						return req.ExecutionID == ref.ExecutionID && req.Fence == ref.Fence
					}); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
