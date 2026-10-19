package api

import (
	"devboard/internal/integration"
	"devboard/internal/team/domain"
	"fmt"
	"net/http"
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
