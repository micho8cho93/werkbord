package api

import (
	"net/http"

	"werkbord/internal/httpkit"
	"werkbord/internal/integration"
)

func (s *Server) handleExportTransfer(w http.ResponseWriter, r *http.Request) {
	out, err := s.opt.Service.ExportTransfer(r.Context(), actorOf(r), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, out)
}
func (s *Server) handleImportTransfer(w http.ResponseWriter, r *http.Request) {
	var in integration.ProjectTransfer
	if !s.decode(w, r, &in) {
		return
	}
	id, err := s.opt.Service.ImportTransfer(r.Context(), actorOf(r), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, map[string]string{"id": id})
}
func (s *Server) handleArchiveTransfer(w http.ResponseWriter, r *http.Request) {
	var in integration.ProjectTransfer
	if !s.decode(w, r, &in) {
		return
	}
	if err := s.opt.Service.ArchiveTransfer(r.Context(), actorOf(r), r.PathValue("id"), in); err != nil {
		s.fail(w, r, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, map[string]bool{"archived": true})
}
