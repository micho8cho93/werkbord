package controller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/appops"
	"devboard/internal/config"
)

func TestTheAssistantIsServedToThePersonOnThisComputerAndNotOnThePrivateNetwork(t *testing.T) {
	node := newFakeNode()
	cfg := defaultConfig(t)
	on := true
	cfg.Network.Enabled = &on
	c := startWith(t, cfg, node)
	waitFor(t, "the node to be served", func() bool { return node.addr() != "" })
	tok, _ := c.cfg.ResolveToken(false)
	base := "http://" + c.Addr()

	if code, _ := get(t, base+"/api/assistant/providers", ""); code != 401 {
		t.Fatalf("without the token: %d", code)
	}
	code, body := get(t, base+"/api/assistant/providers", tok)
	if code != 200 || !strings.Contains(body, `"claude-code"`) || !strings.Contains(body, `"codex"`) {
		t.Fatalf("on this computer: %d %.300s", code, body)
	}
	// A phone on the private network has the token too, and is still not given the assistant: it would be a way to drive
	// the computer's coding agents from anywhere the network reaches.
	priv := "http://" + node.addr()
	for _, path := range []string{"/api/assistant/providers", "/api/assistant/sessions", "/api/assistant/audit"} {
		if code, body := get(t, priv+path, tok); code != 503 {
			t.Errorf("%s over the private network = %d %s", path, code, body)
		}
	}
	if code, body := post(t, priv+"/api/assistant/sessions", tok); code != 503 {
		t.Errorf("creating a conversation over the private network = %d %s", code, body)
	}
	// Nothing is started, or made on disk, until someone uses it.
	if _, err := os.Stat(filepath.Join(c.cfg.DataDir, "assistant")); err == nil {
		t.Error("the assistant made its directory before it was used")
	}
}

func TestTheAssistantCanBeTurnedOff(t *testing.T) {
	cfg := defaultConfig(t)
	cfg.Assistant.Disabled = true
	c := startWith(t, cfg, newFakeNode())
	tok, _ := c.cfg.ResolveToken(false)
	if code, body := get(t, "http://"+c.Addr()+"/api/assistant/providers", tok); code != 503 || !strings.Contains(body, "unavailable") {
		t.Fatalf("disabled: %d %s", code, body)
	}
}

func TestAReadOnlyAssistantIsNotGrantedAnythingThatChangesThings(t *testing.T) {
	all := assistantGrants(config.AssistantConfig{})
	if len(all) != len(appops.AllPermissions) {
		t.Fatalf("by default it has every permission there is: %v", all)
	}
	ro := assistantGrants(config.AssistantConfig{ReadOnly: true})
	for _, g := range ro {
		switch g {
		case appops.PermTicketsCreate, appops.PermTicketsUpdate, appops.PermQuestionsAnswer:
			t.Errorf("a read-only assistant holds %s", g)
		}
	}
	if len(ro) != 5 {
		t.Errorf("read-only grants = %v", ro)
	}
	if p := assistantProjects(config.AssistantConfig{}); p != nil {
		t.Errorf("no projects configured means all of them (nil), got %#v", p)
	}
	if p := assistantProjects(config.AssistantConfig{Projects: []string{"prj_a"}}); len(p) != 1 || p[0] != "prj_a" {
		t.Errorf("projects = %v", p)
	}
}

func TestAuditRetentionSettings(t *testing.T) {
	for days, want := range map[int]time.Duration{0: 0, -1: -1, 90: 90 * 24 * time.Hour} {
		if got := auditRetention(config.AssistantConfig{AuditRetentionDays: days}); got != want {
			t.Errorf("%d days -> %v, want %v", days, got, want)
		}
	}
}
