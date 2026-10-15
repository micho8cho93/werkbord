package controller

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/appops"
	"devboard/internal/config"
)

func TestTheAssistantIsServedToThePersonOnThisComputerAndToEnrolledPhonesOnThePrivateNetwork(t *testing.T) {
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
	// The controller's own token is still not given the assistant over the private network: only a phone the person has
	// enrolled is (below), so the assistant is never reached by whoever merely holds a token that was shared or leaked.
	priv := "http://" + node.addr()
	for _, path := range []string{"/api/assistant/providers", "/api/assistant/sessions", "/api/assistant/audit"} {
		if code, body := get(t, priv+path, tok); code != 403 {
			t.Errorf("%s over the private network with the controller's token = %d %s", path, code, body)
		}
	}
	if code, body := post(t, priv+"/api/assistant/sessions", tok); code != 403 {
		t.Errorf("creating a conversation over the private network with the controller's token = %d %s", code, body)
	}
	// An enrolled phone has all of it.
	waitFor(t, "connected", func() bool {
		code, body := get(t, base+"/api/network", tok)
		return code == 200 && strings.Contains(body, `"connected"`)
	})
	_, enrollBody := post(t, base+"/api/phones/codes", tok)
	var link struct{ Link string }
	_ = json.Unmarshal([]byte(enrollBody), &link)
	secret := link.Link[strings.Index(link.Link, "#enroll=")+len("#enroll="):]
	resp, err := http.Post(priv+"/api/phones/enroll", "application/json", strings.NewReader(`{"code":"`+secret+`","name":"Phone"}`))
	if err != nil {
		t.Fatal(err)
	}
	var enrolled struct{ Token string }
	_ = json.NewDecoder(resp.Body).Decode(&enrolled)
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("enroll = %d", resp.StatusCode)
	}
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
		case appops.PermTicketsCreate, appops.PermTicketsUpdate, appops.PermQuestionsAnswer:
			t.Errorf("a read-only assistant holds %s", g)
		}
	}
	if len(ro) != 6 {
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
