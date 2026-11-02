package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRepositoryIdentityValidatesBeforeNormalizing(t *testing.T) {
	for _, s := range []string{"https://GitHub.com/Acme/Shop.git", "git@github.com:Acme/Shop", "ssh://git@github.com:22/Acme/Shop", "git://github.com/Acme/Shop/"} {
		got, err := RepositoryIdentity(s)
		if err != nil || got != "github.com/Acme/Shop" {
			t.Fatalf("%q: %q %v", s, got, err)
		}
	}
	for _, s := range []string{"https://token@github.com/a/b", "https://u:password@github.com/a/b", "ssh://token@github.com/a/b", "git@host:../repo", "https://host/a/../b", "https://host/a%2fb", "https://host/a?token=secret", "https://host/a#secret", "file:///tmp/repo", "/tmp/repo", "ext::sh", "git@host:a//b"} {
		if got, err := RepositoryIdentity(s); err == nil {
			t.Fatalf("unsafe %q accepted as %q", s, got)
		}
	}
	a, _ := RepositoryIdentity("ssh://git@host:2222/a/b")
	b, _ := RepositoryIdentity("git@host:a/b")
	if a == b {
		t.Fatal("nondefault port lost")
	}
	a, _ = RepositoryIdentity("https://host/A/B")
	b, _ = RepositoryIdentity("https://host/a/b")
	if a == b {
		t.Fatal("path case lost")
	}
}

func TestAgentIDIsAShortLowercaseSlugAndStartingIsAState(t *testing.T) {
	for _, ok := range []string{"codex", "claude-code", "github-copilot-cli", "amp-acp", "a1", strings.Repeat("a", 40)} {
		if !ValidAgentID(ok) || !(Execution{State: "running", AgentID: ok}).Valid() {
			t.Fatalf("%q should be accepted", ok)
		}
	}
	for _, bad := range []string{"Codex", "-codex", "codex-", "co--dex", "co dex", "codex/..", "codex\n", "é", strings.Repeat("a", 41)} {
		if ValidAgentID(bad) || (Execution{State: "running", AgentID: bad}).Valid() {
			t.Fatalf("%q should be refused", bad)
		}
	}
	if ValidAgentID("") || !(Execution{State: "running", AgentID: ""}).Valid() {
		t.Fatal("no agent is valid for an execution but is not an agent id")
	}
	if !(Execution{State: "starting"}).Valid() || (Execution{State: "starting", Outcome: "starting"}).Valid() {
		t.Fatal("starting is a live state with no outcome")
	}
	// An execution with no agent encodes exactly as it did before, so an older reader sees nothing new.
	raw, _ := json.Marshal(Execution{State: "running"})
	if strings.Contains(string(raw), "agentId") {
		t.Fatal(string(raw))
	}
	raw, _ = json.Marshal(Execution{State: "running", AgentID: "codex"})
	if !strings.Contains(string(raw), `"agentId":"codex"`) {
		t.Fatal(string(raw))
	}
}
