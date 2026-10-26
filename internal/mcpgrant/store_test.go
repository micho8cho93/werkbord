package mcpgrant

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"werkbord/internal/mcpcontract"
)

func TestCreatePersistsOnlyHashAndRevocationTakesEffect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp-clients.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.Create(mcpcontract.Principal{Name: "calendar agent", Projects: []string{"project-a"}, Operations: []string{"list_projects", "create_ticket"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.Token, TokenPrefix) || created.Grant.ID == "" {
		t.Fatalf("created grant = %+v", created)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), created.Token) || !strings.Contains(string(b), hashToken(created.Token)) {
		t.Fatal("credential must not be stored in clear text and its digest must be stored")
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("grant file permissions = %v", info.Mode().Perm())
	}

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+created.Token)
	if _, ok := s.Authenticate(req); !ok {
		t.Fatal("persisted credential did not authenticate")
	}
	if !s.Revoke(created.Grant.ID) {
		t.Fatal("grant was not revoked")
	}
	if _, ok := s.Authenticate(req); ok {
		t.Fatal("revoked credential still authenticated")
	}
	if len(s.List()) != 1 || !s.List()[0].Revoked {
		t.Fatalf("revoked grant metadata should remain for its pending proposals: %+v", s.List())
	}
}

func TestRejectsBroadOrMalformedScopes(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "mcp-clients.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []mcpcontract.Principal{
		{Name: "", Projects: []string{"p"}, Operations: []string{"list_projects"}},
		{Name: "agent", Projects: nil, Operations: []string{"list_projects"}},
		{Name: "agent", Projects: []string{"p"}, Operations: []string{"list-projects"}},
		{Name: "agent", Projects: []string{"p", "p"}, Operations: []string{"list_projects"}},
	} {
		if _, err := s.Create(p); err == nil {
			t.Errorf("accepted invalid scope %+v", p)
		}
	}
}
