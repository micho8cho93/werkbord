package api

import (
	"net/http"

	"devboard/internal/integration"
)

func (s *Server) handleExportTransfer(w http.ResponseWriter, r *http.Request) {
	out, err := s.opt.Projects.ExportTransfer(r.Context(), r.PathValue("pid"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *Server) handleImportTransfer(w http.ResponseWriter, r *http.Request) {
	var in integration.ProjectTransfer
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	id, err := s.opt.Projects.ImportTransfer(r.Context(), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}
func (s *Server) handleArchiveTransfer(w http.ResponseWriter, r *http.Request) {
	var in integration.ProjectTransfer
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.opt.Projects.ArchiveTransfer(r.Context(), r.PathValue("pid"), in); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"archived": true})
}
