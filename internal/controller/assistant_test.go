package controller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"werkbord/internal/appops"
	"werkbord/internal/config"
)

func TestTheAssistantIsServedToThePersonOnThisComputerAndToEnrolledPhonesThroughTheRelay(t *testing.T) {
	cfg := defaultConfig(t)
	withRelay(t, &cfg)
	c := startWith(t, cfg, newFakeNode())
	tok, _ := c.cfg.ResolveToken(false)
	base := "http://" + c.Addr()

	if code, _ := get(t, base+"/api/assistant/providers", ""); code != 401 {
		t.Fatalf("without the token: %d", code)
	}
	code, body := get(t, base+"/api/assistant/providers", tok)
	if code != 200 || !strings.Contains(body, `"claude-code"`) || !strings.Contains(body, `"codex"`) {
		t.Fatalf("on this computer: %d %.300s", code, body)
	}
	// The controller's own token is still not given the assistant through the relay: only a phone the person has
	// enrolled is (below), so the assistant is never reached by whoever merely holds a token that was shared or leaked.
	priv := phoneDoor(t, c)
	for _, path := range []string{"/api/assistant/providers", "/api/assistant/sessions", "/api/assistant/audit"} {
		if code, body := get(t, priv+path, tok); code != 403 {
			t.Errorf("%s through the relay with the controller's token = %d %s", path, code, body)
		}
	}
	if code, body := post(t, priv+"/api/assistant/sessions", tok); code != 403 {
		t.Errorf("creating a conversation through the relay with the controller's token = %d %s", code, body)
	}
	// An enrolled phone has all of it.
	enrolled := struct{ Token string }{enrollPhone(t, base, tok, priv, "Phone")}
	if code, body := get(t, priv+"/api/assistant/providers", enrolled.Token); code != 200 || !strings.Contains(body, `"claude-code"`) {
		t.Errorf("an enrolled phone's assistant = %d %.200s", code, body)
	}
	// All of it, as on the desktop: the audit log too.
	if code, _ := get(t, priv+"/api/assistant/audit", enrolled.Token); code == 403 || code == 401 {
		t.Errorf("an enrolled phone's assistant audit = %d", code)
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
	if len(all) != len(appops.AllPermissions)-1 {
		t.Fatalf("by default it has every permission there is: %v", all)
	}
	for _, permission := range all {
		if permission == appops.PermTaskPlan {
			t.Fatal("the conversational assistant must not receive the inbound MCP-only plan permission")
		}
	}
	ro := assistantGrants(config.AssistantConfig{ReadOnly: true})
	for _, g := range ro {
		switch g {
		case appops.PermTicketsCreate, appops.PermTicketsUpdate, appops.PermQuestionsAnswer, appops.PermPersonalWrite:
			t.Errorf("a read-only assistant holds %s", g)
		}
	}
	if len(ro) != 8 {
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
