package api

import (
	"crypto/subtle"
	"net/http"
	"sort"
	"strings"

	"werkbord/internal/clientaccess"
	"werkbord/internal/devicewire"
	"werkbord/internal/domain"
	"werkbord/internal/netprivate"
	"werkbord/internal/phoneaccess"
)

// recentRunsPerProject bounds how many finished runs of one project the list adds to the live ones.
const recentRunsPerProject = 8

// agentSessionList is what GET /api/agent-sessions answers.
type agentSessionList struct {
	Sessions []domain.AgentSession `json:"sessions"`
	// Terminals is false when native terminals cannot be listed to this caller (disabled, or a network listener), so
	// that a screen can tell "no terminals" from "not available here".
	Terminals bool `json:"terminals"`
}

// handleAgentSessions lists every agent execution side by side, as Salie, Agents, Work and Calendar show them. It adds
// nothing to what the owning records say: a run comes from the runs table, a terminal from the terminal service, and the
// key of each is derived from the owner's own ID. A session therefore cannot appear twice, and opening it anywhere
// resolves to the same record. Query: project=<id> narrows to one project, task=<id> to one task's sessions, and
// live=1 leaves out finished runs and ended terminals.
func (s *Server) handleAgentSessions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	project, task, liveOnly := q.Get("project"), q.Get("task"), q.Get("live") == "1"
	ctx := r.Context()
	if s.opt.Projects == nil || s.opt.Runs == nil || s.opt.Tasks == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "sessions are unavailable")
		return
	}
	projects, err := s.opt.Projects.List(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	known := map[string]bool{}
	for _, p := range projects {
		if project == "" || p.ID == project {
			known[p.ID] = true
		}
	}
	var candidates []domain.Run
	active, err := s.opt.Runs.ListActive(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	candidates = append(candidates, active...)
	if !liveOnly {
		for id := range known {
			history, e := s.opt.Runs.History(ctx, id, recentRunsPerProject)
			if e != nil {
				s.fail(w, r, e)
				return
			}
			candidates = append(candidates, history...)
		}
	}
	terminals, available := s.sessionTerminals(r)

	// Task titles and plans are read only for the projects that have a session to show, not for every project there is.
	titles, plans := map[string]string{}, map[string][]string{}
	loaded := map[string]bool{}
	load := func(projectID string) error {
		if loaded[projectID] || !known[projectID] {
			return nil
		}
		loaded[projectID] = true
		tasks, err := s.opt.Tasks.List(ctx, projectID)
		if err != nil {
			return err
		}
		for _, t := range tasks {
			titles[t.ID] = t.Title
		}
		workPlans, err := s.opt.Tasks.WorkPlans(ctx, projectID)
		if err != nil {
			return err
		}
		for _, plan := range workPlans {
			for _, id := range plan.TaskIDs {
				plans[id] = append(plans[id], plan.ID)
			}
		}
		return nil
	}
	for _, run := range candidates {
		if err := load(run.ProjectID); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	for _, t := range terminals {
		if err := load(t.ProjectID); err != nil {
			s.fail(w, r, err)
			return
		}
	}

	out := agentSessionList{Sessions: []domain.AgentSession{}, Terminals: available}
	seen := map[string]bool{}
	add := func(a domain.AgentSession) {
		if seen[a.Key] || !known[a.ProjectID] || (task != "" && a.TaskID != task) || (liveOnly && !a.Live) {
			return
		}
		seen[a.Key] = true
		out.Sessions = append(out.Sessions, a)
	}
	for _, run := range candidates {
		add(domain.SessionOfRun(run, titles[run.TaskID], plans[run.TaskID]))
	}
	for _, t := range terminals {
		add(sessionOfTerminal(t, titles[t.TaskID], plans[t.TaskID]))
	}
	// Live and waiting sessions first, then the most recently active.
	sort.SliceStable(out.Sessions, func(i, j int) bool {
		a, b := out.Sessions[i], out.Sessions[j]
		if a.Live != b.Live {
			return a.Live
		}
		if a.Waiting != b.Waiting {
			return a.Waiting
		}
		if !a.UpdatedAt.Equal(b.UpdatedAt) {
			return a.UpdatedAt.After(b.UpdatedAt)
		}
		return a.Key < b.Key
	})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

// sessionOfTerminal is the read model of a native terminal. A terminal has no structured feed or questions of its own:
// it is the agent's own interactive command line, and everything provider-specific stays in that program.
func sessionOfTerminal(t devicewire.Terminal, taskTitle string, planIDs []string) domain.AgentSession {
	live := t.State == "running"
	title := t.Repository
	if taskTitle != "" {
		title = taskTitle
	}
	return domain.AgentSession{
		Key: domain.TerminalRef(t.ID).String(), Kind: domain.SessionTerminal, ID: t.ID, ProjectID: t.ProjectID, TaskID: t.TaskID, PlanIDs: planIDs,
		AgentID: t.AgentID, Title: title, State: t.State, Live: live, Device: t.Device, Repository: t.Repository,
		Capabilities: domain.SessionCapabilities{Terminal: true, Stop: live},
		CreatedAt:    t.CreatedAt, UpdatedAt: t.CreatedAt,
	}
}

// sessionTerminals is the terminals this caller may see in a list, and whether it may see any. Only the authenticated
// local owner (or an enrolled execution client) lists terminals here; a phone keeps its per-session grants and its own
// session picker, and a private-network listener sees none, exactly as for GET /api/terminals.
func (s *Server) sessionTerminals(r *http.Request) ([]devicewire.Terminal, bool) {
	if _, phone := r.Context().Value(phoneKey{}).(phoneaccess.Entry); phone {
		return nil, false
	}
	ctx := r.Context()
	if s.opt.DeviceOperations != nil {
		actor, ok := s.quietDeviceActor(r)
		if !ok {
			return nil, false
		}
		all, err := s.allDeviceTerminals(ctx)
		if err != nil {
			return nil, false
		}
		out := []devicewire.Terminal{}
		for _, t := range all {
			if s.opt.DeviceOperations.TerminalAllowed(ctx, actor, t.Device, t.ProjectID, t.ID) {
				if actor != "owner" {
					t.Phones = nil
				}
				out = append(out, t)
			}
		}
		return out, true
	}
	if !s.localOwnerTerminals(r) {
		return nil, false
	}
	return s.opt.Terminals.List(), true
}

func (s *Server) localOwnerTerminals(r *http.Request) bool {
	return s.opt.AuthRequired && s.opt.Terminals != nil && s.opt.Terminals.Enabled() && !netprivate.IsPrivateRequest(r.Context()) && fromThisComputer(r)
}

// quietDeviceActor is deviceActor without writing a refusal: a caller that is not allowed terminals still gets its runs.
func (s *Server) quietDeviceActor(r *http.Request) (string, bool) {
	if entry, ok := r.Context().Value(clientKey{}).(clientaccess.Entry); ok && s.opt.ClientListener {
		return entry.ID, true
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	want := s.currentToken()
	if s.opt.ClientListener || !s.opt.AuthRequired || !fromThisComputer(r) || netprivate.IsPrivateRequest(r.Context()) || want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		return "", false
	}
	return "owner", true
}
