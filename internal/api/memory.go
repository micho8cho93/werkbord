package api

import (
	"net/http"
	"strconv"

	"werkbord/internal/domain"
	"werkbord/internal/service"
)

func (s *Server) memoryReady(w http.ResponseWriter) bool {
	if s.opt.Memory == nil {
		writeError(w, 503, "unavailable", "memory is available on the local controller only")
		return false
	}
	return true
}
func memoryScope(r *http.Request) domain.MemoryScope {
	return domain.MemoryScope{ProjectID: r.PathValue("pid"), Repository: r.URL.Query().Get("repository")}
}
func memoryQuery(r *http.Request) (domain.MemoryQuery, error) {
	q := r.URL.Query()
	limit := 50
	if q.Has("limit") {
		var e error
		limit, e = strconv.Atoi(q.Get("limit"))
		if e != nil {
			return domain.MemoryQuery{}, domain.ErrInvalid
		}
	}
	out := domain.MemoryQuery{Text: q.Get("q"), Type: q.Get("type"), State: domain.MemoryState(q.Get("status")), Authority: domain.MemoryAuthority(q.Get("authority")), Limit: limit}
	return out, domain.ValidateMemoryQuery(out)
}
func (s *Server) handleMemoryList(w http.ResponseWriter, r *http.Request) {
	if !s.memoryReady(w) {
		return
	}
	q, e := memoryQuery(r)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	out, e := s.opt.Memory.List(r.Context(), memoryScope(r), q)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	writeJSON(w, 200, map[string]any{"records": out, "bounded": len(out) == q.Limit})
}
func (s *Server) handleMemorySave(w http.ResponseWriter, r *http.Request) {
	if !s.memoryReady(w) {
		return
	}
	var in service.ManualMemory
	if e := decode(w, r, &in); e != nil {
		s.fail(w, r, e)
		return
	}
	out, e := s.opt.Memory.SaveManual(r.Context(), r.PathValue("pid"), in)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	writeJSON(w, 201, out)
}
func (s *Server) handleMemoryChange(w http.ResponseWriter, r *http.Request) {
	if !s.memoryReady(w) {
		return
	}
	var in struct {
		Version int64 `json:"version"`
		Pinned  *bool `json:"pinned,omitempty"`
		Archive bool  `json:"archive"`
	}
	if e := decode(w, r, &in); e != nil {
		s.fail(w, r, e)
		return
	}
	out, e := s.opt.Memory.Change(r.Context(), memoryScope(r), r.PathValue("id"), in.Version, in.Pinned, in.Archive)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) handleMemoryDelete(w http.ResponseWriter, r *http.Request) {
	if !s.memoryReady(w) {
		return
	}
	var in struct {
		Version int64 `json:"version"`
	}
	if e := decode(w, r, &in); e != nil {
		s.fail(w, r, e)
		return
	}
	if e := s.opt.Memory.Memory.Delete(r.Context(), memoryScope(r), r.PathValue("id"), in.Version); e != nil {
		s.fail(w, r, e)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) handleMemorySources(w http.ResponseWriter, r *http.Request) {
	if !s.memoryReady(w) {
		return
	}
	out, e := s.opt.Memory.Sources(r.Context(), memoryScope(r), r.PathValue("id"))
	if e != nil {
		s.fail(w, r, e)
		return
	}
	writeJSON(w, 200, map[string]any{"sources": out})
}
func (s *Server) handleMemoryContext(w http.ResponseWriter, r *http.Request) {
	if !s.memoryReady(w) {
		return
	}
	q, e := memoryQuery(r)
	if e != nil {
		s.fail(w, r, e)
		return
	}
	budget := 2048
	if r.URL.Query().Has("tokens") {
		budget, e = strconv.Atoi(r.URL.Query().Get("tokens"))
		if e != nil {
			s.fail(w, r, domain.ErrInvalid)
			return
		}
	}
	out, e := s.opt.Memory.Retrieve(r.Context(), service.MemoryContextQuery{Scope: memoryScope(r), Query: q, TokenBudget: budget, IncludeUnverified: r.URL.Query().Get("includeUnverified") == "true"})
	if e != nil {
		s.fail(w, r, e)
		return
	}
	writeJSON(w, 200, out)
}
