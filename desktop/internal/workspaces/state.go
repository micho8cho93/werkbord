package workspaces

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"devboard/internal/workspace"
)

// State is what the shell remembers between runs, and only that: which workspace was open, and where in each workspace the
// person was. It holds no credential, no title and nothing from any workspace's content; a place is a link inside a
// workspace (workspace.ValidHref) such as #/p/<id>/board.
type State struct {
	path string
	mu   sync.Mutex
	data stateFile
}

type stateFile struct {
	Version int               `json:"version"`
	Last    string            `json:"last,omitempty"`
	Places  map[string]string `json:"places,omitempty"`
}

// maxPlaces bounds the places kept, so a long-gone workspace's place does not stay forever.
const maxPlaces = 64

var sensitivePlace = regexp.MustCompile(`(?i)(?:[?&])(?:token|join|invite)=`)

// Enrollment and page authentication belong only to the live target, never persisted navigation.
func rememberablePlace(place string) bool {
	decoded, err := url.QueryUnescape(place)
	return err == nil && workspace.ValidHref(place) && !sensitivePlace.MatchString(decoded)
}

// OpenState reads the state at path. A missing or unreadable file is an empty state, never an error: the worst it costs is
// opening Individual.
func OpenState(path string) *State {
	s := &State{path: path, data: stateFile{Version: 1, Places: map[string]string{}}}
	b, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var f stateFile
	if json.Unmarshal(b, &f) != nil || f.Version != 1 {
		return s
	}
	if f.Last != "" && workspace.ValidID(f.Last) {
		s.data.Last = f.Last
	}
	for id, place := range f.Places {
		if workspace.ValidID(id) && rememberablePlace(place) && len(s.data.Places) < maxPlaces {
			s.data.Places[id] = place
		}
	}
	return s
}

// Last is the workspace that was open when the shell was last used; empty if none.
func (s *State) Last() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Last
}

// SetLast remembers which workspace is open.
func (s *State) SetLast(id string) error {
	if !workspace.ValidID(id) {
		return errors.New("not a workspace")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Last == id {
		return nil
	}
	s.data.Last = id
	return s.saveLocked()
}

// Place is where the person was in a workspace, or "".
func (s *State) Place(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Places[id]
}

// SetPlace remembers where the person is in a workspace. A place that is not a link inside a workspace is refused, so
// a workspace's page cannot make the shell remember anything else.
func (s *State) SetPlace(id, place string) error {
	if !workspace.ValidID(id) || !rememberablePlace(place) {
		return errors.New("not a place in a workspace")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Places[id] == place {
		return nil
	}
	if _, ok := s.data.Places[id]; !ok && len(s.data.Places) >= maxPlaces {
		return errors.New("too many places remembered")
	}
	s.data.Places[id] = place
	return s.saveLocked()
}

// Forget drops what is remembered about a workspace that is gone.
func (s *State) Forget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	if s.data.Last == id {
		s.data.Last, changed = "", true
	}
	if _, ok := s.data.Places[id]; ok {
		delete(s.data.Places, id)
		changed = true
	}
	if changed {
		_ = s.saveLocked()
	}
}

func (s *State) saveLocked() error {
	if s.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".shell-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0o600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), s.path)
}
