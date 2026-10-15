// Package phoneaccess is how a phone is given its own way into the controller, instead of the controller's token.
//
// The controller's token does everything its owner can, is shared by whoever holds it, and cannot be taken away from one
// device. A phone is enrolled instead: the owner makes a short-lived, single-use *enrollment code* (shown as a QR code), the
// phone presents it once, and is given a *phone token* of its own. The owner sees every phone, when it was added and last
// seen, and revokes any of them at once. The API accepts a phone token only on a short list of routes
// (internal/api/phone.go) and only from the doors the controller trusts for it.
//
// This package knows nothing about how the phone reaches the controller (a private network today, something else later),
// about Team, or about HTTP. Only hashes are kept: tokens in one file in the data directory that only its owner can read,
// enrollment codes in memory only (they last minutes, and a restart voids them).
package phoneaccess

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
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
	// TokenPrefix marks a phone token, so it is never mistaken for the controller's own or a program's (wba_).
	TokenPrefix = "wbp_"
	// MaxEntries bounds how many phones may be enrolled.
	MaxEntries = 16
	// MaxNameLen bounds a phone's name.
	MaxNameLen = 60
	// CodeTTL is how long an enrollment code can be used.
	CodeTTL = 5 * time.Minute
	// MaxPendingCodes bounds the codes waiting to be used; the oldest is dropped for a new one.
	MaxPendingCodes = 4

	// touchEvery is how often a token's last use is written to disk.
	touchEvery = time.Minute
	// codeBytes is the entropy of an enrollment code: 32 bytes, 256 bits.
	codeBytes = 32
)

var (
	// ErrTooMany is returned when the limit is reached.
	ErrTooMany = fmt.Errorf("phoneaccess: at most %d phones may be enrolled; revoke one first", MaxEntries)
	// ErrInvalidCode is the one answer for a code that is wrong, expired or already used, so none of them can be told
	// from another.
	ErrInvalidCode = errors.New("phoneaccess: that code does not work; make a new one")
)

// Entry is what is known about a phone, and never its token.
type Entry struct {
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

type pendingCode struct {
	hash    string
	expires time.Time
}

// Store keeps the phones and the codes waiting to enroll one.
type Store struct {
	path string
	now  func() time.Time

	mu      sync.Mutex
	recs    []record
	codes   []pendingCode
	touched time.Time // when a last use was last written
	watch   map[string][]chan struct{}
}

type file struct {
	Version int      `json:"version"`
	Records []record `json:"records"`
}

// Open reads the store at path (it need not exist yet).
func Open(path string) (*Store, error) {
	s := &Store{path: path, now: time.Now, watch: map[string][]chan struct{}{}}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("phoneaccess: %s is not readable: %w", path, err)
	}
	s.recs = f.Records
	return s, nil
}

// CleanName checks a phone's name.
func CleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > MaxNameLen {
		return "", fmt.Errorf("phoneaccess: a phone's name is 1 to %d characters", MaxNameLen)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", errors.New("phoneaccess: a phone's name has no control characters")
		}
	}
	return name, nil
}

func hashOf(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func random(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	return b, err
}

// NewCode makes a single-use enrollment code, valid for CodeTTL, and returns it once. Only its hash is kept.
func (s *Store) NewCode() (code string, expires time.Time, err error) {
	raw, err := random(codeBytes)
	if err != nil {
		return "", time.Time{}, err
	}
	code = base64.RawURLEncoding.EncodeToString(raw)
	expires = s.now().Add(CodeTTL).UTC().Truncate(time.Second)

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.recs) >= MaxEntries {
		return "", time.Time{}, ErrTooMany
	}
	s.dropExpiredCodesLocked()
	if len(s.codes) >= MaxPendingCodes {
		s.codes = s.codes[len(s.codes)-MaxPendingCodes+1:]
	}
	s.codes = append(s.codes, pendingCode{hash: hashOf(code), expires: expires})
	return code, expires, nil
}

func (s *Store) dropExpiredCodesLocked() {
	now := s.now()
	kept := s.codes[:0]
	for _, c := range s.codes {
		if now.Before(c.expires) {
			kept = append(kept, c)
		}
	}
	s.codes = kept
}

// Enroll spends a code and enrolls a phone under name. It returns the phone's token, once. A code that is wrong,
// expired or already used is ErrInvalidCode, whichever it is. A full store leaves the code unspent.
func (s *Store) Enroll(code, name string) (Entry, string, error) {
	name, nameErr := CleanName(name)
	want := []byte(hashOf(code))

	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropExpiredCodesLocked()
	found := -1
	for i, c := range s.codes { // every code is compared, so the time taken does not say which matched
		if subtle.ConstantTimeCompare([]byte(c.hash), want) == 1 {
			found = i
		}
	}
	if found < 0 || len(code) > 200 {
		return Entry{}, "", ErrInvalidCode
	}
	if nameErr != nil {
		return Entry{}, "", nameErr
	}
	if len(s.recs) >= MaxEntries {
		return Entry{}, "", ErrTooMany
	}
	tok, err := random(32)
	if err != nil {
		return Entry{}, "", err
	}
	id, err := random(8)
	if err != nil {
		return Entry{}, "", err
	}
	token := TokenPrefix + hex.EncodeToString(tok)
	e := Entry{ID: "ph_" + hex.EncodeToString(id), Name: name, CreatedAt: s.now().UTC().Truncate(time.Second)}

	prev := s.recs
	s.recs = append(append([]record{}, s.recs...), record{Entry: e, Hash: hashOf(token)})
	if err := s.saveLocked(); err != nil {
		s.recs = prev
		return Entry{}, "", err
	}
	s.codes = append(s.codes[:found], s.codes[found+1:]...) // spent only once the phone is safely recorded
	return e, token, nil
}

// Authenticate finds the phone a token belongs to. It compares every stored hash, so how long it takes does not say
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

// List returns the enrolled phones, oldest first.
func (s *Store) List() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, 0, len(s.recs))
	for _, r := range s.recs {
		out = append(out, r.Entry)
	}
	return out
}

// Get returns one phone.
func (s *Store) Get(id string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.recs {
		if r.ID == id {
			return r.Entry, true
		}
	}
	return Entry{}, false
}

// Rename gives a phone another name. It reports whether there was one.
func (s *Store) Rename(id, name string) (Entry, bool, error) {
	name, err := CleanName(name)
	if err != nil {
		return Entry{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.recs {
		if s.recs[i].ID != id {
			continue
		}
		prev := s.recs[i].Name
		s.recs[i].Name = name
		if err := s.saveLocked(); err != nil {
			s.recs[i].Name = prev
			return Entry{}, false, err
		}
		return s.recs[i].Entry, true, nil
	}
	return Entry{}, false, nil
}

// Revoke takes a phone's access away at once, and ends whatever it has open (see Watch). It reports whether there was
// one.
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
	for _, ch := range s.watch[id] {
		close(ch)
	}
	delete(s.watch, id)
	return true, nil
}

// Watch returns a channel that is closed when the phone is revoked, and a function that stops watching. For a phone that
// is already gone the channel is closed at once.
func (s *Store) Watch(id string) (<-chan struct{}, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan struct{})
	exists := false
	for _, r := range s.recs {
		if r.ID == id {
			exists = true
		}
	}
	if !exists {
		close(ch)
		return ch, func() {}
	}
	s.watch[id] = append(s.watch[id], ch)
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		list := s.watch[id]
		for i, c := range list {
			if c == ch {
				s.watch[id] = append(list[:i:i], list[i+1:]...)
				break
			}
		}
		if len(s.watch[id]) == 0 {
			delete(s.watch, id)
		}
	}
}

func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(file{Version: 1, Records: s.recs}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".phones.*")
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
