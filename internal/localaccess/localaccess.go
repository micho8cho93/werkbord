// Package localaccess is how another program on this computer is given a narrow, revocable way into
// the controller, without being given the controller's own access token.
//
// The controller's token can do everything its owner can: register repositories, change settings,
// push, merge, pair runners. A program that only needs to hand the controller a task, see what is
// running, answer an agent's question or stop a run does not need that. It is given a *local access
// token* instead: a separate credential, one per program, that the owner can see and revoke, that only
// works from this computer (never on the private network), and that the API accepts only on a short list
// of routes (internal/api/localaccess.go).
//
// Only hashes are kept, in one file in the data directory that only its owner can read.
package localaccess

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Limits and names.
const (
	// TokenPrefix marks a local access token, so it is never mistaken for the controller's own.
	TokenPrefix = "wba_"
	// MaxEntries bounds how many programs may hold one.
	MaxEntries = 16
	// MaxNameLen bounds a program's name.
	MaxNameLen = 60

	// touchEvery is how often a token's last use is written to disk.
	touchEvery = time.Minute
)

// ErrTooMany is returned when the limit is reached.
var ErrTooMany = fmt.Errorf("localaccess: at most %d programs may have access; revoke one first", MaxEntries)

// Entry is what is known about a program's access, and never its token.
type Entry struct {
	Scope      string     `json:"scope,omitempty"`
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

type record struct {
	Entry
	// Hash is the SHA-256 of the token, hex.
	Hash string `json:"hash"`
}

// Store keeps the tokens.
type Store struct {
	path string
	now  func() time.Time

	mu      sync.Mutex
	recs    []record
	touched time.Time // when a last use was last written
}

type file struct {
	Version int      `json:"version"`
	Records []record `json:"records"`
}

// Open reads the store at path (it need not exist yet).
func Open(path string) (*Store, error) {
	s := &Store{path: path, now: time.Now}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("localaccess: %s is not readable: %w", path, err)
	}
	s.recs = f.Records
	return s, nil
}

// CleanName checks a program's name.
func CleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > MaxNameLen {
		return "", fmt.Errorf("localaccess: a program's name is 1 to %d characters", MaxNameLen)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", errors.New("localaccess: a program's name has no control characters")
		}
	}
	return name, nil
}

func hashOf(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Create gives a program access and returns its token, once. A program that already has access under the
// same name is replaced: reconnecting never leaves two.
func (s *Store) Create(name string) (Entry, string, error) {
	return s.CreateScoped(name, "")
}

// CreateScoped can restrict a grant to metadata exchange with no run control.
func (s *Store) CreateScoped(name, scope string) (Entry, string, error) {
	if scope != "" && scope != "integration-v1" {
		return Entry{}, "", errors.New("unsupported local access scope")
	}
	name, err := CleanName(name)
	if err != nil {
		return Entry{}, "", err
	}
	var raw, id [16]byte
	var tok [32]byte
	for _, b := range [][]byte{raw[:], id[:], tok[:]} {
		if _, err := rand.Read(b); err != nil {
			return Entry{}, "", err
		}
	}
	token := TokenPrefix + hex.EncodeToString(tok[:])
	e := Entry{ID: "la_" + hex.EncodeToString(id[:8]), Name: name, Scope: scope, CreatedAt: s.now().UTC().Truncate(time.Second)}

	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make([]record, 0, len(s.recs)+1)
	for _, r := range s.recs {
		if !strings.EqualFold(r.Name, name) {
			kept = append(kept, r)
		}
	}
	if len(kept) >= MaxEntries {
		return Entry{}, "", ErrTooMany
	}
	prev := s.recs
	s.recs = append(kept, record{Entry: e, Hash: hashOf(token)})
	if err := s.saveLocked(); err != nil {
		s.recs = prev
		return Entry{}, "", err
	}
	return e, token, nil
}

// Authenticate finds the program a token belongs to. It compares every stored hash, so how long it takes does not say
// which one matched.
func (s *Store) Authenticate(token string) (Entry, bool) {
	if !strings.HasPrefix(token, TokenPrefix) || len(token) > 200 {
		return Entry{}, false
	}
	want := []byte(hashOf(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	found := -1
	for i, r := range s.recs {
		if subtle.ConstantTimeCompare([]byte(r.Hash), want) == 1 {
			found = i
		}
	}
	if found < 0 {
		return Entry{}, false
	}
	now := s.now().UTC()
	s.recs[found].LastUsedAt = &now
	if now.Sub(s.touched) >= touchEvery {
		if err := s.saveLocked(); err == nil {
			s.touched = now
		}
	}
	return s.recs[found].Entry, true
}

// List returns the programs that have access, oldest first.
func (s *Store) List() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, 0, len(s.recs))
	for _, r := range s.recs {
		out = append(out, r.Entry)
	}
	return out
}

// Revoke takes a program's access away at once. It reports whether there was one.
func (s *Store) Revoke(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make([]record, 0, len(s.recs))
	for _, r := range s.recs {
		if r.ID != id {
			kept = append(kept, r)
		}
	}
	if len(kept) == len(s.recs) {
		return false, nil
	}
	prev := s.recs
	s.recs = kept
	if err := s.saveLocked(); err != nil {
		s.recs = prev
		return false, err
	}
	return true, nil
}

func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(file{Version: 1, Records: s.recs}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".local-access.*")
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp.Name())
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return err
	}
	ok = true
	return nil
}
