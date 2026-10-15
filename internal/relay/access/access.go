// Package access is the runner's side of who may reach it through the relay: its long-term key, its room, and the
// pre-shared secrets of the phones. It implements e2e.PSKStore.
//
// A secret has two lives. Made for a QR code it is *pending*: good for the few minutes the enrollment code beside it is
// good for, and for nothing afterwards. When the phone behind it enrolls (a phone token is issued) it is *bound* to that
// phone and stays good until the phone is revoked, which is how the phone gets back in without a second scan. Whatever
// the secret lets through, the phone's own token is still checked on every request (internal/api/phone.go).
package access

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"devboard/internal/relay"
	"devboard/internal/relay/e2e"
)

// PendingTTL is how long a QR code's secret is good for, matching the enrollment code's life.
const PendingTTL = 5 * time.Minute

// maxPending bounds the secrets waiting to be used, so a loop making codes cannot grow memory.
const maxPending = 32

type boundRecord struct {
	ID     string    `json:"id"`     // hex of the secret's e2e.PSKID
	Secret string    `json:"secret"` // base64url
	Phone  string    `json:"phone"`  // phoneaccess entry ID
	Added  time.Time `json:"added"`
}

type file struct {
	Version int           `json:"version"`
	Seed    string        `json:"seed"` // base64url X25519 seed: the runner's identity
	Room    string        `json:"room"`
	JoinKey string        `json:"joinKey"`
	Bound   []boundRecord `json:"bound"`
}

type pending struct {
	secret  []byte
	expires time.Time
}

// Store is safe for concurrent use.
type Store struct {
	path string
	now  func() time.Time

	mu      sync.Mutex
	id      *e2e.Identity
	room    relay.Room
	bound   map[e2e.PSKID]boundRecord
	pending map[e2e.PSKID]pending
}

// Open reads the store, making the identity and room the first time. The file holds secrets and is private to the user.
func Open(path string) (*Store, error) {
	s := &Store{path: path, now: time.Now, bound: map[e2e.PSKID]boundRecord{}, pending: map[e2e.PSKID]pending{}}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		id, err := e2e.NewIdentity()
		if err != nil {
			return nil, err
		}
		s.id, s.room = id, relay.NewRoom()
		return s, s.save()
	case err != nil:
		return nil, err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil || f.Version != 1 {
		return nil, errors.New("relay access: " + path + " is not readable; move it aside to start over")
	}
	seed, err := base64.RawURLEncoding.DecodeString(f.Seed)
	if err != nil {
		return nil, errors.New("relay access: bad identity in " + path)
	}
	if s.id, err = e2e.IdentityFromSeed(seed); err != nil {
		return nil, errors.New("relay access: bad identity in " + path)
	}
	s.room = relay.Room{ID: f.Room, JoinKey: f.JoinKey}
	for _, r := range f.Bound {
		raw, err := base64.RawURLEncoding.DecodeString(r.Secret)
		if err != nil {
			continue
		}
		s.bound[e2e.IDOf(raw)] = r
	}
	return s, nil
}

func (s *Store) Identity() *e2e.Identity { return s.id }
func (s *Store) Room() relay.Room        { return s.room }

// NewPairing makes a pending secret and the pairing that carries it, for a QR code. code is the enrollment code the phone
// will spend once it is in.
func (s *Store) NewPairing(relayURL, code string) (relay.Pairing, error) {
	secret := e2e.NewSecret()
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for id, p := range s.pending {
		if !now.Before(p.expires) {
			delete(s.pending, id)
		}
	}
	if len(s.pending) >= maxPending {
		return relay.Pairing{}, errors.New("relay access: too many codes waiting: use or wait out the ones you made")
	}
	s.pending[e2e.IDOf(secret)] = pending{secret: secret, expires: now.Add(PendingTTL)}
	return relay.Pairing{
		V: 1, Relay: relayURL, Room: s.room.ID, JoinKey: s.room.JoinKey,
		RunnerKey: base64.RawURLEncoding.EncodeToString(s.id.Public()),
		Secret:    base64.RawURLEncoding.EncodeToString(secret),
		Code:      code,
	}, nil
}

// Lookup implements e2e.PSKStore.
func (s *Store) Lookup(id e2e.PSKID) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.bound[id]; ok {
		raw, err := base64.RawURLEncoding.DecodeString(r.Secret)
		return raw, err == nil
	}
	if p, ok := s.pending[id]; ok {
		if s.now().Before(p.expires) {
			return p.secret, true
		}
		delete(s.pending, id)
	}
	return nil, false
}

// Confirm implements e2e.PSKStore. A pending secret is not spent here: a phone that reloads in its first minutes must
// get back in. What ends it is Bind, or the clock.
func (s *Store) Confirm(e2e.PSKID) {}

// Bind ties a pending secret to the phone that has enrolled with it.
func (s *Store) Bind(id e2e.PSKID, phone string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pending[id]
	if !ok {
		if _, already := s.bound[id]; already {
			return nil
		}
		return errors.New("relay access: that secret is not waiting")
	}
	delete(s.pending, id)
	s.bound[id] = boundRecord{ID: hex.EncodeToString(id[:]), Secret: base64.RawURLEncoding.EncodeToString(p.secret), Phone: phone, Added: s.now()}
	if err := s.save(); err != nil {
		delete(s.bound, id)
		s.pending[id] = p
		return err
	}
	return nil
}

// Unbind takes away the secrets of a phone (it was revoked) and returns them, so the connections made with them can be
// ended.
func (s *Store) Unbind(phone string) []e2e.PSKID {
	s.mu.Lock()
	defer s.mu.Unlock()
	var gone []e2e.PSKID
	for id, r := range s.bound {
		if r.Phone == phone {
			gone = append(gone, id)
		}
	}
	if len(gone) == 0 {
		return nil
	}
	for _, id := range gone {
		delete(s.bound, id)
	}
	// If the save fails the secrets are still gone from memory, which is what matters now; Prune removes them on the
	// next start, since their phone is revoked.
	_ = s.save()
	return gone
}

// Prune forgets the secrets of phones that no longer exist, for a start after a restore or a failed save.
func (s *Store) Prune(exists func(phone string) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for id, r := range s.bound {
		if !exists(r.Phone) {
			delete(s.bound, id)
			changed = true
		}
	}
	if changed {
		_ = s.save()
	}
}

// Phones reports how many phones are bound.
func (s *Store) Phones() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bound)
}

func (s *Store) save() error {
	f := file{Version: 1, Seed: base64.RawURLEncoding.EncodeToString(s.id.Seed()), Room: s.room.ID, JoinKey: s.room.JoinKey}
	for _, r := range s.bound {
		f.Bound = append(f.Bound, r)
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".relay.*")
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
