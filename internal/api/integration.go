package api

import (
	"net/http"
	"strconv"

	"devboard/internal/integration"
)

func (s *Server) handleIntegrationProjects(w http.ResponseWriter, r *http.Request) {
	out, err := s.opt.Tasks.IntegrationProjects(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleIntegrationImport(w http.ResponseWriter, r *http.Request) {
	var in integration.Import
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	out, err := s.opt.Tasks.Import(r.Context(), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}

// handleIntegrationWaiting replaces what one synchronizing source says waits for a repository on this computer.
func (s *Server) handleIntegrationWaiting(w http.ResponseWriter, r *http.Request) {
	var in integration.WaitingSet
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.opt.Tasks.SetWaiting(r.Context(), in); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListWaiting is the app's own view of the tickets waiting for a repository (the controller's credential only).
func (s *Server) handleListWaiting(w http.ResponseWriter, r *http.Request) {
	items, err := s.opt.Tasks.Waiting(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleLinkWaiting registers the folder the person chose for a waiting ticket, if it is a clone of the ticket's repository.
func (s *Server) handleLinkWaiting(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path       string `json:"path"`
		Name       string `json:"name"`
		Repository string `json:"repository"`
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	p, err := s.opt.Projects.RegisterFor(r.Context(), req.Path, req.Name, req.Repository)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleIntegrationEvents(w http.ResponseWriter, r *http.Request) {
	after, err := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if err != nil {
		writeError(w, 400, "invalid", "after must be a nonnegative cursor")
		return
	}
	out, err := s.opt.Tasks.IntegrationEvents(r.Context(), r.PathValue("pid"), r.PathValue("id"), after)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleIntegrationStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pid, tid := r.PathValue("pid"), r.PathValue("id")
	out, err := s.opt.Tasks.IntegrationSnapshot(ctx, pid, tid)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	t, err := s.opt.Tasks.GetIn(ctx, pid, tid)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out.GitUnavailable = true
	if s.opt.Git != nil && out.Execution.Branch != "" {
		scope := "local"
		if out.Execution.RunID != "" {
			if run, e := s.opt.Runs.GetIn(ctx, pid, out.Execution.RunID); e == nil && run.Remote {
				scope = "remote"
			}
		}
		cmp, e := s.opt.Git.CompareWithCommitLimit(ctx, pid, scope, out.Execution.Branch, t.BaseBranch, 0, 1, 200)
		if e == nil && !cmp.Unique.Truncated {
			g := &integration.Git{Branch: cmp.Branch, HeadSHA: cmp.BranchSha, BaseBranch: cmp.Target, Ahead: cmp.Ahead, Behind: cmp.Behind, Commits: []integration.Commit{}}
			for _, c := range cmp.Unique.Items {
				g.Commits = append(g.Commits, integration.Commit{SHA: c.SHA, CommittedAt: c.Date})
			}
			if prs, e := s.opt.Git.PullRequests(ctx, pid); e == nil && prs.Available {
				for _, pr := range prs.PullRequests {
					if pr.HeadBranch == g.Branch && !pr.CrossRepo {
						g.PullRequest = &integration.PullRequest{Number: pr.Number, URL: pr.URL, State: pr.State, Draft: pr.Draft, BaseBranch: pr.BaseBranch, Mergeable: pr.Mergeable}
						break
					}
				}
			}
			out.Git, out.GitUnavailable = g, false
		}
	}
	writeJSON(w, 200, out)
}
