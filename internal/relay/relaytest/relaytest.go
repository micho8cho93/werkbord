// Package relaytest runs the production relay in-process for conformance tests.
package relaytest

import (
	"net/http/httptest"
	"werkbord/internal/relay/server"
)

type Server struct {
	*httptest.Server
	relay *server.Server
}

func New() *Server       { h := server.New(); return &Server{httptest.NewServer(h), h} }
func (s *Server) Close() { s.relay.Close(); s.Server.Close() }
