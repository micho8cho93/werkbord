package api

import (
	"fmt"
	"net/http"
	"werkbord/internal/integration"
	"werkbord/internal/team/domain"
)

func (s *Server) handleMemoryPropose(w http.ResponseWriter, r *http.Request) {
	var in integration.MemoryProposal
	if !s.decode(w, r, &in) {
		return
	}
	if err := in.Validate(); err != nil {
		s.fail(w, r, fmt.Errorf("%w: %v", domain.ErrInvalid, err))
		return
	}
	out, err := s.opt.Service.ProposeMemory(r.Context(), actorOf(r), in)
	s.respond(w, r, 201, out, err)
}
func (s *Server) handleMemoryChange(w http.ResponseWriter, r *http.Request) {
	var in integration.MemoryCommand
	if !s.decode(w, r, &in) {
		return
	}
	if err := in.Validate(); err != nil {
		s.fail(w, r, fmt.Errorf("%w: %v", domain.ErrInvalid, err))
		return
	}
	out, err := s.opt.Service.ChangeMemory(r.Context(), actorOf(r), r.PathValue("mid"), in)
	s.respond(w, r, 200, out, err)
}
func (s *Server) handleMemoryRead(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Schema    string                        `json:"schema"`
		Scope     integration.SharedMemoryScope `json:"scope"`
		After     string                        `json:"after,omitempty"`
		Published bool                          `json:"published"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	if in.Schema != integration.MemorySchema {
		s.fail(w, r, domain.ErrInvalid)
		return
	}
	out, err := s.opt.Service.ReadMemories(r.Context(), actorOf(r), in.Scope, in.After, in.Published)
	s.respond(w, r, 200, out, err)
}

func (s *Server) handleMemoryDelivery(w http.ResponseWriter, r *http.Request) {
	var in integration.MemoryEvent
	if !s.decode(w, r, &in) {
		return
	}
	if in.Validate() != nil {
		s.fail(w, r, domain.ErrInvalid)
		return
	}
	out, err := s.opt.Service.DeliverMemory(r.Context(), actorOf(r), in)
	s.respond(w, r, 200, out, err)
}

func (s *Server) handleMemoryEvidence(w http.ResponseWriter, r *http.Request) {
	if _, err := s.opt.Service.GetProject(r.Context(), actorOf(r), r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	var in domain.MemoryEvidence
	if !s.decode(w, r, &in) {
		return
	}
	if in.Scope.ProjectID != r.PathValue("id") {
		s.fail(w, r, domain.ErrInvalid)
		return
	}
	err := s.opt.Service.ReportMemoryEvidence(r.Context(), actorOf(r), in)
	s.respond(w, r, 200, map[string]bool{"accepted": err == nil}, err)
}
func (s *Server) handleMemorySearch(w http.ResponseWriter, r *http.Request) {
	out, err := s.opt.Service.SearchMemories(r.Context(), actorOf(r), r.PathValue("id"), r.URL.Query().Get("q"), r.URL.Query().Get("after"), r.PathValue("mid"))
	if r.PathValue("mid") != "" && err == nil {
		s.respond(w, r, 200, out.Records[0], nil)
		return
	}
	s.respond(w, r, 200, out, err)
}
func (s *Server) handleMemoryHistory(w http.ResponseWriter, r *http.Request) {
	out, err := s.opt.Service.MemoryHistory(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("mid"))
	s.respond(w, r, 200, out, err)
}
func (s *Server) handleMemoryResolve(w http.ResponseWriter, r *http.Request) {
	if _, err := s.opt.Service.GetProject(r.Context(), actorOf(r), r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	var in domain.MemoryResolution
	if !s.decode(w, r, &in) {
		return
	}
	out, err := s.opt.Service.ResolveMemory(r.Context(), actorOf(r), r.PathValue("id"), r.PathValue("mid"), in)
	s.respond(w, r, 200, out, err)
}
