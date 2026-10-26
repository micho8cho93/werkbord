// Package premium keeps the person's Premium unlock key for the desktop app. The key is theirs, not any workspace's: it is one
// string they paste into Settings once, kept in the app's per-user configuration directory (mode 0600), checked against the public
// key this build is stamped with, and given to each Team workspace they belong to, which checks it again. There is no account, no
// server and no expiry (docs/PLANS.md).
package premium

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"werkbord/internal/entitlement"
)

// Status is what the app tells the person about their plan.
type Status struct {
	// Active: a valid key is kept, so this person is on Premium.
	Active bool `json:"active"`
	// ID and Holder are the key's identifier and the label its issuer gave it, for the person to recognise their key by. Never the key.
	ID     string `json:"id,omitempty"`
	Holder string `json:"holder,omitempty"`
	// CanCheck: this build carries the public key that checks unlock keys. A build without it (a development build) says so, rather
	// than refusing every key as if it were wrong.
	CanCheck bool `json:"canCheck"`
}

// Store is the key on this computer.
type Store struct {
	path   string
	verify func(string) (entitlement.Claims, error)
	// canCheck says whether verify has anything to check against.
	canCheck func() bool
}

// New keeps the key in the file at path ("" keeps none: nothing can be unlocked).
func New(path string) *Store {
	return &Store{path: path, verify: entitlement.VerifyKey, canCheck: func() bool { return entitlement.IssuerKey() != nil }}
}

// Path is where the app keeps its configuration and so its key, in the per-user directory.
func Path() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "werkbord-desktop", "premium.key")
}

// Key is the unlock key kept here, or "" when there is none, it is not a private file, or it no longer checks.
func (s *Store) Key() string {
	if s == nil || s.path == "" {
		return ""
	}
	fi, err := os.Lstat(s.path)
	if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 != 0 {
		return ""
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return ""
	}
	key := strings.TrimSpace(string(b))
	if _, err := s.verify(key); err != nil {
		return ""
	}
	return key
}

// Status says whether the person is on Premium.
func (s *Store) Status() Status {
	st := Status{}
	if s == nil {
		return st
	}
	st.CanCheck = s.canCheck()
	if key := s.Key(); key != "" {
		if c, err := s.verify(key); err == nil {
			st.Active, st.ID, st.Holder = true, c.ID, c.Holder
		}
	}
	return st
}

// Unlock checks a pasted key and keeps it. A key that does not check is refused with a sentence for the person, and nothing is kept.
func (s *Store) Unlock(text string) (Status, error) {
	if s == nil || s.path == "" {
		return Status{}, errors.New("this app has nowhere to keep an unlock key")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return Status{}, errors.New("paste your unlock key first")
	}
	if !s.canCheck() {
		return Status{}, errors.New("this build of Werkbord cannot check unlock keys; install the release from werkbord's download page")
	}
	if _, err := s.verify(text); err != nil {
		return Status{}, err
	}
	if err := writePrivate(s.path, []byte(text+"\n")); err != nil {
		return Status{}, fmt.Errorf("could not keep the unlock key: %w", err)
	}
	return s.Status(), nil
}

func writePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".premium-*")
	if err != nil {
		return err
	}
	name := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	return nil
}
