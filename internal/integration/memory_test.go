package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

func validMemoryProposal() MemoryProposal {
	return MemoryProposal{Schema: MemorySchema, Scope: SharedMemoryScope{WorkspaceID: "ws_one", Kind: "repository", ProjectID: "p_one", Repository: "github.com/acme/repo"}, Content: SharedMemoryContent{Kind: "convention", Summary: "Use explicit transactions", Classification: "team", Verification: "verified", Source: MemorySource{Kind: "human", ID: "review_one"}}, ClientID: "client_one"}
}
func TestMemoryProtocolIsClosedAndIndependent(t *testing.T) {
	good := validMemoryProposal()
	raw, _ := json.Marshal(good)
	var out MemoryProposal
	if err := DecodeMemory(raw, &out); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"path", "directory", "files", "diff", "handoff", "transcript", "conversation", "terminal", "credentials", "token", "agentId", "model", "shell", "command", "approvedBy", "authorId", "details", "execution"} {
		for _, where := range []string{"root", "content", "source", "scope"} {
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			target := m
			switch where {
			case "content":
				target = m["content"].(map[string]any)
			case "source":
				target = m["content"].(map[string]any)["source"].(map[string]any)
			case "scope":
				target = m["scope"].(map[string]any)
			}
			target[field] = "secret"
			bad, _ := json.Marshal(m)
			if DecodeMemory(bad, &out) == nil {
				t.Fatalf("accepted prohibited %s.%s", where, field)
			}
		}
	}
	for _, schema := range []string{Schema, "werkbord.memory/v2", ""} {
		bad := good
		bad.Schema = schema
		if bad.Validate() == nil {
			t.Fatalf("accepted %q", schema)
		}
	}
	for _, tail := range []string{`{}`, ` null`} {
		if DecodeMemory(append(append([]byte(nil), raw...), tail...), &out) == nil {
			t.Fatal("accepted trailing JSON")
		}
	}
	for _, text := range []string{"diff --git a/a b/b", "/Users/person/private", "Bearer secret", "ghp_secret", "Use\nraw output", strings.Repeat("a", 2049), "text\u202e"} {
		bad := good
		bad.Content.Summary = text
		if bad.Validate() == nil {
			t.Fatalf("accepted %q", text)
		}
	}
	for _, repo := range []string{"/tmp/repo", "file:///repo", "https://token@github.com/acme/repo", "https://github.com/acme/../repo", "https://github.com/acme/repo?secret=x"} {
		bad := good
		bad.Scope.Repository = repo
		if bad.Validate() == nil {
			t.Fatalf("accepted repository %q", repo)
		}
	}
}
func TestCrossRepositoryScopesAreExplicitAndBounded(t *testing.T) {
	s := SharedMemoryScope{WorkspaceID: "w", Kind: "cross_repository", Targets: []MemoryTarget{{ProjectID: "a", Repository: "github.com/acme/a"}, {ProjectID: "b", Repository: "github.com/acme/b"}}}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	s.Targets[1].ProjectID = "a"
	if s.Validate() == nil {
		t.Fatal("duplicate project accepted")
	}
	s.Targets[1] = MemoryTarget{ProjectID: "b", Repository: "github.com/acme/a"}
	if s.Validate() == nil {
		t.Fatal("duplicate repository accepted")
	}
	s.Targets = s.Targets[:1]
	if s.Validate() == nil {
		t.Fatal("unbounded/wildcard cross scope")
	}
}
