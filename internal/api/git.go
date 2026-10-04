package api

import (
	"net/http"

	"devboard/internal/domain"
	"devboard/internal/service"
)

// The Git Control Center's endpoints. They are thin: they decode, call
// service.GitControl and encode. Every action answers 200 with a GitActionResult,
// whose "outcome" says what happened (done, refused, conflict, rejected, ...): a
// refusal or a rejected push is an expected answer, not an HTTP error, and "ok"
// is true only when the action really happened.

func (s *Server) git(w http.ResponseWriter) bool {
	if s.opt.Git == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "the Git Control Center is not enabled")
		return false
	}
	return true
}

func (s *Server) handleGitOverview(w http.ResponseWriter, r *http.Request) {
	if !s.git(w) {
		return
	}
	o, err := s.opt.Git.Overview(r.Context(), r.PathValue("pid"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *Server) handleGitPullRequests(w http.ResponseWriter, r *http.Request) {
	if !s.git(w) {
		return
	}
	st, err := s.opt.Git.PullRequests(r.Context(), r.PathValue("pid"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleGitCommits(w http.ResponseWriter, r *http.Request) {
	if !s.git(w) {
		return
	}
	skip, err := intParam(r, "skip", 0, 0, 1<<30)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	limit, err := intParam(r, "limit", 30, 1, 100)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	q := r.URL.Query()
	page, err := s.opt.Git.Commits(r.Context(), r.PathValue("pid"), q.Get("scope"), q.Get("branch"), skip, limit)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleGitCompare(w http.ResponseWriter, r *http.Request) {
	if !s.git(w) {
		return
	}
	offset, err := intParam(r, "offset", 0, 0, 1<<30)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	limit, err := intParam(r, "limit", service.DefaultFilePage, 1, service.MaxFilePage)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	q := r.URL.Query()
	c, err := s.opt.Git.Compare(r.Context(), r.PathValue("pid"), q.Get("scope"), q.Get("branch"), q.Get("target"), offset, limit)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// paths reads the file a diff is wanted for: path, and for a renamed file oldPath too.
func paths(r *http.Request) []string {
	q := r.URL.Query()
	var out []string
	if old := q.Get("oldPath"); old != "" {
		out = append(out, old)
	}
	if p := q.Get("path"); p != "" {
		out = append(out, p)
	}
	return out
}

func (s *Server) diffWindow(w http.ResponseWriter, r *http.Request) (offset, lines int, ok bool) {
	offset, err := intParam(r, "offset", 0, 0, 1<<30)
	if err != nil {
		s.fail(w, r, err)
		return 0, 0, false
	}
	lines, err = intParam(r, "lines", 0, 0, 1<<20)
	if err != nil {
		s.fail(w, r, err)
		return 0, 0, false
	}
	return offset, lines, true
}

func (s *Server) handleGitDiff(w http.ResponseWriter, r *http.Request) {
	if !s.git(w) {
		return
	}
	offset, lines, ok := s.diffWindow(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	d, err := s.opt.Git.FileDiff(r.Context(), r.PathValue("pid"), q.Get("from"), q.Get("to"), paths(r), offset, lines)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleGitChanges(w http.ResponseWriter, r *http.Request) {
	if !s.git(w) {
		return
	}
	c, err := s.opt.Git.WorkingTree(r.Context(), r.PathValue("pid"), r.URL.Query().Get("worktree"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleGitChangeDiff(w http.ResponseWriter, r *http.Request) {
	if !s.git(w) {
		return
	}
	offset, lines, ok := s.diffWindow(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	d, err := s.opt.Git.WorkingDiff(r.Context(), r.PathValue("pid"), q.Get("worktree"), paths(r), q.Get("kind"), offset, lines)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleGitFetch(w http.ResponseWriter, r *http.Request) {
	if !s.git(w) {
		return
	}
	res, err := s.opt.Git.Fetch(r.Context(), r.PathValue("pid"))
	s.respondAction(w, r, res, err)
}

func (s *Server) respondAction(w http.ResponseWriter, r *http.Request, res *domain.GitActionResult, err error) {
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleGitPush(w http.ResponseWriter, r *http.Request) {
	if !s.git(w) {
		return
	}
	var req struct {
		Branch      string `json:"branch"`
		ExpectedSha string `json:"expectedSha"`
		Remote      string `json:"remote"`
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.opt.Git.Push(r.Context(), r.PathValue("pid"), service.PushInput{Branch: req.Branch, ExpectedSha: req.ExpectedSha, Remote: req.Remote})
	s.respondAction(w, r, res, err)
}

type mergeRequest struct {
	Branch    string `json:"branch"`
	BranchSha string `json:"branchSha"`
	Target    string `json:"target"`
	TargetSha string `json:"targetSha"`
	Strategy  string `json:"strategy"`
}

func (m mergeRequest) input() service.MergeInput {
	return service.MergeInput{Branch: m.Branch, BranchSha: m.BranchSha, Target: m.Target, TargetSha: m.TargetSha, Strategy: m.Strategy}
}

func (s *Server) handleGitMergePlan(w http.ResponseWriter, r *http.Request) {
	if !s.git(w) {
		return
	}
	var req mergeRequest
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	plan, err := s.opt.Git.MergePlan(r.Context(), r.PathValue("pid"), req.input())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) handleGitMerge(w http.ResponseWriter, r *http.Request) {
	if !s.git(w) {
		return
	}
	var req mergeRequest
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.opt.Git.Merge(r.Context(), r.PathValue("pid"), req.input())
	s.respondAction(w, r, res, err)
}

type deleteRequest struct {
	Branch       string `json:"branch"`
	BranchSha    string `json:"branchSha"`
	DeleteRemote bool   `json:"deleteRemote"`
}

func (d deleteRequest) input() service.DeleteInput {
	return service.DeleteInput{Branch: d.Branch, BranchSha: d.BranchSha, DeleteRemote: d.DeleteRemote}
}

func (s *Server) handleGitDeletePlan(w http.ResponseWriter, r *http.Request) {
	if !s.git(w) {
		return
	}
	var req deleteRequest
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	plan, err := s.opt.Git.DeletePlan(r.Context(), r.PathValue("pid"), req.input())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) handleGitDelete(w http.ResponseWriter, r *http.Request) {
	if !s.git(w) {
		return
	}
	var req deleteRequest
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.opt.Git.DeleteBranch(r.Context(), r.PathValue("pid"), req.input())
	s.respondAction(w, r, res, err)
}

type cleanRequest struct {
	WorktreeID string `json:"worktreeId"`
	HeadSha    string `json:"headSha"`
}

func (s *Server) handleGitCleanPlan(w http.ResponseWriter, r *http.Request) {
	if !s.git(w) {
		return
	}
	var req cleanRequest
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	plan, err := s.opt.Git.CleanPlan(r.Context(), r.PathValue("pid"), service.CleanInput{WorktreeID: req.WorktreeID, HeadSha: req.HeadSha})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) handleGitClean(w http.ResponseWriter, r *http.Request) {
	if !s.git(w) {
		return
	}
	var req cleanRequest
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.opt.Git.CleanWorktree(r.Context(), r.PathValue("pid"), service.CleanInput{WorktreeID: req.WorktreeID, HeadSha: req.HeadSha})
	s.respondAction(w, r, res, err)
}

func (s *Server) handleGitCreatePR(w http.ResponseWriter, r *http.Request) {
	if !s.git(w) {
		return
	}
	var req struct {
		Branch      string `json:"branch"`
		ExpectedSha string `json:"expectedSha"`
		Title       string `json:"title"`
		Body        string `json:"body"`
		Draft       bool   `json:"draft"`
	}
	if err := decode(w, r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.opt.Git.CreatePullRequest(r.Context(), r.PathValue("pid"), service.PRInput{
		Branch: req.Branch, ExpectedSha: req.ExpectedSha, Title: req.Title, Body: req.Body, Draft: req.Draft,
	})
	s.respondAction(w, r, res, err)
}
