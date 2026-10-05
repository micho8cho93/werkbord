package api

import (
	"net/http"
	"strconv"
	"time"

	"devboard/internal/httpkit"
	"devboard/internal/team/domain"
	"devboard/internal/team/service"
)

// The collaborative workflow: the project board, tickets and their lifecycle,
// reported Git metadata, repository state, activity, invites and change sync.
// Every handler decodes, calls one service method and encodes; the rules are in
// the service and the domain.

func (s *Server) respond(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, status, v)
}

func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request) {
	b, err := s.opt.Service.Board(r.Context(), actorOf(r), r.PathValue("id"))
	s.respond(w, r, http.StatusOK, b, err)
}

func (s *Server) handleProjectPeople(w http.ResponseWriter, r *http.Request) {
	p, err := s.opt.Service.ListProjectPeople(r.Context(), actorOf(r), r.PathValue("id"))
	s.respond(w, r, http.StatusOK, p, err)
}

func (s *Server) handleCreateTicket(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title        string `json:"title"`
		Description  string `json:"description"`
		Requirements string `json:"requirements"`
		Status       string `json:"status"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	k, err := s.opt.Service.CreateTicket(r.Context(), actorOf(r), r.PathValue("id"),
		service.TicketInput{Title: in.Title, Description: in.Description, Requirements: in.Requirements, Status: domain.TicketStatus(in.Status)})
	s.respond(w, r, http.StatusCreated, k, err)
}

func (s *Server) handleGetTicket(w http.ResponseWriter, r *http.Request) {
	k, err := s.opt.Service.GetTicket(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("tid"))
	s.respond(w, r, http.StatusOK, k, err)
}

func (s *Server) handleUpdateTicket(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title        *string `json:"title"`
		Description  *string `json:"description"`
		Requirements *string `json:"requirements"`
		Version      *int64  `json:"version"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	k, err := s.opt.Service.UpdateTicket(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("tid"),
		service.TicketPatch{Title: in.Title, Description: in.Description, Requirements: in.Requirements, Version: in.Version})
	s.respond(w, r, http.StatusOK, k, err)
}

func (s *Server) handleMoveTicket(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status string `json:"status"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	to, err := domain.ParseTicketStatus(in.Status)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	k, err := s.opt.Service.MoveTicket(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("tid"), to)
	s.respond(w, r, http.StatusOK, k, err)
}

func (s *Server) handleClaimTicket(w http.ResponseWriter, r *http.Request) {
	k, err := s.opt.Service.ClaimTicket(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("tid"))
	s.respond(w, r, http.StatusOK, k, err)
}

func (s *Server) handleReleaseTicket(w http.ResponseWriter, r *http.Request) {
	k, err := s.opt.Service.ReleaseTicket(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("tid"))
	s.respond(w, r, http.StatusOK, k, err)
}

func (s *Server) handleAssignTicket(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MemberID string `json:"memberId"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	k, err := s.opt.Service.AssignTicket(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("tid"), in.MemberID)
	s.respond(w, r, http.StatusOK, k, err)
}

// pullRequestIn is a pull request as a client reports it. Behind is a pointer so
// that leaving it out means "not known" and not "up to date".
type pullRequestIn struct {
	Number     int    `json:"number"`
	URL        string `json:"url"`
	State      string `json:"state"`
	Draft      bool   `json:"draft"`
	Mergeable  string `json:"mergeable"`
	BaseBranch string `json:"baseBranch"`
	Behind     *int   `json:"behind"`
	Ahead      int    `json:"ahead"`
}

func (p *pullRequestIn) domain() *domain.PullRequest {
	if p == nil {
		return nil
	}
	behind := -1
	if p.Behind != nil {
		behind = *p.Behind
	}
	return &domain.PullRequest{Number: p.Number, URL: p.URL, State: domain.PRState(p.State), Draft: p.Draft, Mergeable: domain.Mergeable(p.Mergeable),
		BaseBranch: p.BaseBranch, Behind: behind, Ahead: p.Ahead}
}

func (s *Server) handleSubmitTicket(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ReviewerID  string         `json:"reviewerId"`
		PullRequest *pullRequestIn `json:"pullRequest"`
	}
	if r.ContentLength != 0 && !s.decode(w, r, &in) {
		return
	}
	k, err := s.opt.Service.SubmitTicket(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("tid"),
		service.SubmitInput{ReviewerID: in.ReviewerID, PullRequest: in.PullRequest.domain()})
	s.respond(w, r, http.StatusOK, k, err)
}

func (s *Server) handleRequestChanges(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Note string `json:"note"`
	}
	if r.ContentLength != 0 && !s.decode(w, r, &in) {
		return
	}
	k, err := s.opt.Service.RequestChanges(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("tid"), in.Note)
	s.respond(w, r, http.StatusOK, k, err)
}

func (s *Server) handleCompleteTicket(w http.ResponseWriter, r *http.Request) {
	k, err := s.opt.Service.CompleteTicket(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("tid"))
	s.respond(w, r, http.StatusOK, k, err)
}

type commitIn struct {
	SHA         string    `json:"sha"`
	Subject     string    `json:"subject"`
	Author      string    `json:"author"`
	CommittedAt time.Time `json:"committedAt"`
}

type branchStateIn struct {
	HeadSHA      string    `json:"headSha"`
	BaseBranch   string    `json:"baseBranch"`
	Ahead        *int      `json:"ahead"`
	Behind       *int      `json:"behind"`
	LastCommitAt time.Time `json:"lastCommitAt"`
	Files        []string  `json:"files"`
}

func (b *branchStateIn) state() *service.BranchState {
	if b == nil {
		return nil
	}
	return &service.BranchState{HeadSHA: b.HeadSHA, BaseBranch: b.BaseBranch, Ahead: b.Ahead, Behind: b.Behind, LastCommitAt: b.LastCommitAt, Files: b.Files}
}

func (s *Server) handleReportGit(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Branch      string         `json:"branch"`
		Commits     *[]commitIn    `json:"commits"`
		PullRequest *pullRequestIn `json:"pullRequest"`
		State       *branchStateIn `json:"state"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	rep := service.GitReport{Branch: in.Branch, PullRequest: in.PullRequest.domain(), State: in.State.state()}
	if in.Commits != nil {
		cs := make([]domain.Commit, 0, len(*in.Commits))
		for _, c := range *in.Commits {
			cs = append(cs, domain.Commit{SHA: c.SHA, Subject: c.Subject, Author: c.Author, CommittedAt: c.CommittedAt})
		}
		rep.Commits = &cs
	}
	k, err := s.opt.Service.ReportGit(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("tid"), rep)
	s.respond(w, r, http.StatusOK, k, err)
}

func (s *Server) handleHandoff(w http.ResponseWriter, r *http.Request) {
	h, err := s.opt.Service.HandoffTicketToRunner(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("tid"))
	s.respond(w, r, http.StatusOK, h, err)
}

func (s *Server) handleRepositoryState(w http.ResponseWriter, r *http.Request) {
	st, err := s.opt.Service.RepositoryState(r.Context(), actorOf(r), r.PathValue("id"))
	s.respond(w, r, http.StatusOK, st, err)
}

func (s *Server) handleReportBranches(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Branches []struct {
			Name string `json:"name"`
			branchStateIn
		} `json:"branches"`
		Gone []string `json:"gone"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	bs := make([]service.BranchInput, 0, len(in.Branches))
	for _, b := range in.Branches {
		st := b.branchStateIn
		bs = append(bs, service.BranchInput{Name: b.Name, BranchState: *st.state()})
	}
	if err := s.opt.Service.ReportBranches(r.Context(), actorOf(r), r.PathValue("id"), bs, in.Gone); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	acts, err := s.opt.Service.ListActivity(r.Context(), actorOf(r), r.PathValue("id"), before, limit)
	s.respond(w, r, http.StatusOK, acts, err)
}

// handleSync holds the request open until the project changes (or ~20 seconds
// pass), so every member's board follows the others' changes within moments
// without polling.
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	wait := time.Duration(0)
	if v, err := strconv.Atoi(r.URL.Query().Get("wait")); err == nil && v > 0 {
		wait = time.Duration(v) * time.Second
	}
	res, err := s.opt.Service.WaitForChange(r.Context(), actorOf(r), r.PathValue("id"), since, wait)
	s.respond(w, r, http.StatusOK, res, err)
}

func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Role         string `json:"role"`
		ExpiresInHrs int    `json:"expiresInHours"`
		MaxUses      int    `json:"maxUses"`
	}
	if r.ContentLength != 0 && !s.decode(w, r, &in) {
		return
	}
	role, err := domain.ParseProjectRole(in.Role)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	inv, err := s.opt.Service.CreateInvite(r.Context(), actorOf(r), r.PathValue("id"),
		service.InviteInput{Role: role, TTL: time.Duration(in.ExpiresInHrs) * time.Hour, MaxUses: in.MaxUses})
	s.respond(w, r, http.StatusCreated, inv, err)
}

func (s *Server) handleListInvites(w http.ResponseWriter, r *http.Request) {
	inv, err := s.opt.Service.ListInvites(r.Context(), actorOf(r), r.PathValue("id"))
	s.respond(w, r, http.StatusOK, inv, err)
}

func (s *Server) handleRevokeInvite(w http.ResponseWriter, r *http.Request) {
	if err := s.opt.Service.RevokeInvite(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("inviteId")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRedeemInvite is public on purpose: the person has no account yet, and the
// invite code is their credential.
func (s *Server) handleRedeemInvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code  string `json:"code"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	j, err := s.opt.Service.RedeemInvite(r.Context(), in.Code, in.Name, in.Email)
	s.respond(w, r, http.StatusCreated, j, err)
}

func (s *Server) handleJoinInvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	j, err := s.opt.Service.JoinWithInvite(r.Context(), actorOf(r), in.Code)
	s.respond(w, r, http.StatusOK, j, err)
}
