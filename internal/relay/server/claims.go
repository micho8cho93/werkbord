package server

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/coder/websocket"
	"werkbord/internal/relay"
)

// Open retains only join-key hashes across restarts. Rooms are bounded to 1024.
func Open(dir string) (*Server, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	s := New()
	s.claimDir = dir
	for _, e := range entries {
		if !roomRE.MatchString("/v1/room/" + e.Name()) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		if len(b) != 64 {
			return nil, errors.New("invalid relay claim")
		}
		if len(s.rooms) >= 1024 {
			return nil, errors.New("relay room limit")
		}
		s.rooms[e.Name()] = &room{auth: string(b), phones: map[relay.PeerID]*websocket.Conn{}}
	}
	return s, nil
}
func (s *Server) saveClaim(id, hash string) error {
	if s.claimDir == "" {
		return nil
	}
	return os.WriteFile(filepath.Join(s.claimDir, id), []byte(hash), 0600)
}
