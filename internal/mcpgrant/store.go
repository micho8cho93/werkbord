// Package mcpgrant stores Individual's revocable MCP client grants. Only a
// one-way credential hash is persisted; the bearer is returned at creation.
package mcpgrant

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"devboard/internal/mcpcontract"
)

const TokenPrefix = "wbm_"

type Grant struct {
	mcpcontract.Principal
	CreatedAt time.Time `json:"createdAt"`
	Revoked   bool      `json:"revoked"`
	tokenHash string
}

type diskGrant struct {
	Principal mcpcontract.Principal `json:"principal"`
	CreatedAt time.Time             `json:"createdAt"`
	Revoked   bool                  `json:"revoked"`
	TokenHash string                `json:"tokenHash"`
}

type diskState struct {
	Grants []diskGrant `json:"grants"`
}

type Store struct {
	mu     sync.Mutex
	path   string
	grants []Grant
}

type Created struct {
	Grant Grant  `json:"grant"`
	Token string `json:"token"`
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("MCP grant path is required")
	}
	s := &Store{path: path, grants: []Grant{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		if err := s.persistLocked(); err != nil {
			return nil, err
		}
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) > 1<<20 {
		return nil, errors.New("MCP grant file exceeds 1 MB")
	}
	var stored diskState
	if err := json.Unmarshal(b, &stored); err != nil {
		return nil, fmt.Errorf("read MCP grants: %w", err)
	}
	for _, grant := range stored.Grants {
		if grant.Principal.ID == "" || grant.TokenHash == "" || len(grant.Principal.Projects) == 0 || len(grant.Principal.Operations) == 0 {
			return nil, errors.New("MCP grant file contains an invalid grant")
		}
		s.grants = append(s.grants, Grant{Principal: grant.Principal, CreatedAt: grant.CreatedAt, Revoked: grant.Revoked, tokenHash: grant.TokenHash})
	}
	return s, nil
}

func (s *Store) List() []Grant {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.grants)
}

func (s *Store) Create(principal mcpcontract.Principal) (Created, error) {
	if err := principal.Validate(); err != nil {
		return Created{}, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Created{}, err
	}
	token := TokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	key := hashToken(token)
	idBytes := make([]byte, 12)
	if _, err := rand.Read(idBytes); err != nil {
		return Created{}, err
	}
	grant := Grant{Principal: principal, CreatedAt: time.Now().UTC().Truncate(time.Millisecond), tokenHash: key}
	grant.ID = "mcp_" + hex.EncodeToString(idBytes)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.grants = append(s.grants, grant)
	if err := s.persistLocked(); err != nil {
		s.grants = s.grants[:len(s.grants)-1]
		return Created{}, err
	}
	return Created{Grant: public(grant), Token: token}, nil
}

func (s *Store) Revoke(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.grants {
		if s.grants[i].ID != id {
			continue
		}
		if s.grants[i].Revoked {
			return false
		}
		previous := s.grants[i]
		s.grants[i].Revoked = true
		if err := s.persistLocked(); err != nil {
			s.grants[i] = previous
			return false
		}
		return true
	}
	return false
}

func (s *Store) Authenticate(r *http.Request) (mcpcontract.Principal, bool) {
	if len(r.Header.Values("Authorization")) != 1 {
		return mcpcontract.Principal{}, false
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || !strings.HasPrefix(token, TokenPrefix) || len(token) > 128 {
		return mcpcontract.Principal{}, false
	}
	got := hashToken(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, grant := range s.grants {
		if !grant.Revoked && len(got) == len(grant.tokenHash) && subtle.ConstantTimeCompare([]byte(got), []byte(grant.tokenHash)) == 1 {
			return grant.Principal, true
		}
	}
	return mcpcontract.Principal{}, false
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func public(g Grant) Grant {
	g.tokenHash = ""
	g.Principal.Projects = append([]string(nil), g.Projects...)
	g.Principal.Operations = append([]string(nil), g.Operations...)
	return g
}

func clone(grants []Grant) []Grant {
	out := make([]Grant, 0, len(grants))
	for _, grant := range grants {
		out = append(out, public(grant))
	}
	return out
}

func (s *Store) persistLocked() error {
	state := diskState{Grants: make([]diskGrant, 0, len(s.grants))}
	for _, grant := range s.grants {
		state.Grants = append(state.Grants, diskGrant{Principal: grant.Principal, CreatedAt: grant.CreatedAt, Revoked: grant.Revoked, TokenHash: grant.tokenHash})
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".mcp-grants-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return err
	}
	if dir, err := os.Open(filepath.Dir(s.path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}
