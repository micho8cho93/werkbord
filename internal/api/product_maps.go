package api

import (
	"net/http"
	"werkbord/internal/productmap"
)

// These endpoints exist only on the owner's local listener. Narrow local
// access, phone, MCP and execution grants cannot reach them.
func (s *Server) productMapRoutes(mux *http.ServeMux) {
	if s.opt.ProductMaps == nil {
		return
	}
	mux.HandleFunc("GET /api/projects/{id}/product-map", s.handleProductMap)
	mux.HandleFunc("GET /api/projects/{id}/product-map/snapshot", s.handleProductMapSnapshot)
	mux.HandleFunc("POST /api/projects/{id}/product-map/jobs", s.handleProductMapStart)
	mux.HandleFunc("POST /api/projects/{id}/product-map/jobs/{job}/cancel", s.handleProductMapCancel)
}
func (s *Server) handleProductMap(w http.ResponseWriter, r *http.Request) {
	out, err := s.opt.ProductMaps.View(r.Context(), r.PathValue("id"), r.URL.Query().Get("branch"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *Server) handleProductMapStart(w http.ResponseWriter, r *http.Request) {
	var in productmap.Request
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	out, err := s.opt.ProductMaps.Start(r.Context(), r.PathValue("id"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, out)
}
func (s *Server) handleProductMapCancel(w http.ResponseWriter, r *http.Request) {
	if err := s.opt.ProductMaps.Cancel(r.Context(), r.PathValue("id"), r.PathValue("job")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleProductMapSnapshot(w http.ResponseWriter, r *http.Request) {
	out, err := s.opt.ProductMaps.View(r.Context(), r.PathValue("id"), "")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out.Snapshot)
}
