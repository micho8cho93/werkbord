// Package clientaccess holds independently revocable desktop-client grants.
// It never accepts owner, phone, runner or local-program credentials.
package clientaccess

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
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"
)

const TokenPrefix = "wbc_"
const CodeTTL = 5 * time.Minute
const MaxEntries = 32

var ErrCode = errors.New("invalid, expired or spent desktop pairing code")

type Entry struct {
	ID         string     `json:"id"`
	DeviceID   string     `json:"deviceId"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}
type record struct {
	Entry
	Hash string `json:"hash"`
}
type pending struct {
	hash    string
	expires time.Time
}
type Store struct {
	mu       sync.Mutex
	path     string
	now      func() time.Time
	entries  []record
	codes    []pending
	watchers map[string]map[chan struct{}]bool
	touched  time.Time
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, now: time.Now, watchers: map[string]map[chan struct{}]bool{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var data struct {
		Version int      `json:"version"`
		Entries []record `json:"entries"`
	}
	if err = json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	if data.Version != 1 || len(data.Entries) > MaxEntries {
		return nil, errors.New("invalid desktop grant store")
	}
	for _, e := range data.Entries {
		if e.ID == "" || e.DeviceID == "" || len(e.Hash) != 64 {
			return nil, errors.New("invalid desktop grant")
		}
	}
	s.entries = data.Entries
	return s, nil
}
func secret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func hash(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }
func (s *Store) NewCode() (string, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) >= MaxEntries {
		return "", time.Time{}, errors.New("revoke a desktop grant before pairing another device")
	}
	code := secret()
	expires := s.now().Add(CodeTTL)
	kept := []pending{}
	for _, c := range s.codes {
		if s.now().Before(c.expires) {
			kept = append(kept, c)
		}
	}
	if len(kept) >= 4 {
		kept = kept[len(kept)-3:]
	}
	s.codes = append(kept, pending{hash(code), expires})
	return code, expires, nil
}
func (s *Store) Enroll(code, name, deviceID string) (Entry, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i, c := range s.codes {
		if subtle.ConstantTimeCompare([]byte(c.hash), []byte(hash(code))) == 1 && s.now().Before(c.expires) {
			idx = i
		}
	}
	if idx < 0 || len(code) > 200 {
		return Entry{}, "", ErrCode
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 60 || len(deviceID) < 16 || len(deviceID) > 100 {
		return Entry{}, "", errors.New("a desktop needs a name and device identifier")
	}
	for _, r := range name + deviceID {
		if unicode.IsControl(r) {
			return Entry{}, "", errors.New("invalid desktop name or identifier")
		}
	}
	if len(s.entries) >= MaxEntries {
		return Entry{}, "", errors.New("desktop grant limit reached")
	}
	token := TokenPrefix + secret()
	e := Entry{ID: "cl_" + secret()[:16], DeviceID: deviceID, Name: name, CreatedAt: s.now().UTC()}
	before := s.entries
	s.entries = append(append([]record{}, before...), record{e, hash(token)})
	if err := s.save(); err != nil {
		s.entries = before
		return Entry{}, "", err
	}
	s.codes = append(s.codes[:idx], s.codes[idx+1:]...)
	return e, token, nil
}
func (s *Store) Authenticate(token string) (Entry, bool) {
	if !strings.HasPrefix(token, TokenPrefix) || len(token) != len(TokenPrefix)+64 {
		return Entry{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i, r := range s.entries {
		if subtle.ConstantTimeCompare([]byte(r.Hash), []byte(hash(token))) == 1 {
			idx = i
		}
	}
	if idx < 0 {
		return Entry{}, false
	}
	now := s.now().UTC()
	s.entries[idx].LastUsedAt = &now
	if now.Sub(s.touched) >= time.Minute {
		if s.save() == nil {
			s.touched = now
		}
	}
	return s.entries[idx].Entry, true
}
func (s *Store) List() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Entry{}
	for _, e := range s.entries {
		out = append(out, e.Entry)
	}
	return out
}
func (s *Store) Revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := []record{}
	for _, e := range s.entries {
		if e.ID != id {
			kept = append(kept, e)
		}
	}
	before := s.entries
	s.entries = kept
	if err := s.save(); err != nil {
		s.entries = before
		return err
	}
	for ch := range s.watchers[id] {
		close(ch)
	}
	delete(s.watchers, id)
	return nil
}
func (s *Store) Watch(id string) (<-chan struct{}, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan struct{})
	exists := false
	for _, e := range s.entries {
		if e.ID == id {
			exists = true
		}
	}
	if !exists {
		close(ch)
		return ch, func() {}
	}
	if s.watchers[id] == nil {
		s.watchers[id] = map[chan struct{}]bool{}
	}
	s.watchers[id][ch] = true
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.watchers[id], ch)
		if len(s.watchers[id]) == 0 {
			delete(s.watchers, id)
		}
	}
}
func (s *Store) save() error {
	return WriteJSON(s.path, struct {
		Version int      `json:"version"`
		Entries []record `json:"entries"`
	}{1, s.entries})
}

// WriteJSON durably replaces a private file, including its directory entry.
func WriteJSON(path string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".device-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("save private device state: %w", err)
	}
	// Windows has no directory fsync through os.File. The staged contents were
	// synced before rename; Unix also persists the containing directory entry.
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
