//go:build darwin || linux

package controller

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"werkbord/internal/config"
	"werkbord/internal/relay"
	"werkbord/internal/relay/relaytest"
	"werkbord/internal/terminal"
)

// Uses the real controller, PTY and encrypted HTTP tunnel, with no terminal WebSocket upgrade.
func TestPhoneTerminalOverEncryptedRelayAndPrivateNetwork(t *testing.T) {
	rl := relaytest.New()
	defer rl.Close()
	cfg := defaultConfig(t)
	cli := filepath.Join(t.TempDir(), "native-cli")
	script := "#!/bin/sh\ncase \"$1\" in --version) echo '9.9.9 (Claude Code)'; exit 0;; auth) echo '{\"loggedIn\":true}'; exit 0;; esac\n[ -t 0 ] || exit 17\necho READY\nwhile IFS= read -r line; do echo REMOTE:$line; done\n"
	if err := os.WriteFile(cli, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cfg.Agents = map[string]config.AgentConfig{"claude-code": {Command: cli}}
	cfg.Relay.URL, cfg.Relay.ConnectorURL = rl.URL, "https://connect.example.pages.dev"
	on := true
	cfg.Network.Enabled = &on
	node := newFakeNode()
	c := startWith(t, cfg, node)
	base := "http://" + c.Addr()
	owner, _ := c.cfg.ResolveToken(false)
	ask := func(method, path, token, body string) (int, string) {
		req, _ := http.NewRequest(method, base+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	waitFor(t, "relay", func() bool { _, b := get(t, base+"/api/relay", owner); return strings.Contains(b, `"connected":true`) })
	waitFor(t, "private listener", func() bool { return node.addr() != "" })
	repo := filepath.Join(t.TempDir(), "repo")
	if b, e := exec.Command("git", "init", "-q", repo).CombinedOutput(); e != nil {
		t.Fatalf("git: %v %s", e, b)
	}
	status, b := ask("POST", "/api/projects", owner, `{"path":"`+repo+`","name":"Mini"}`)
	if status != 201 {
		t.Fatalf("project: %d %s", status, b)
	}
	var project struct{ ID string }
	json.Unmarshal([]byte(b), &project)
	if status, b = ask("PUT", "/api/settings/selection", owner, `{"agents":{"claude-code":{"directCLI":true,"billing":"auto"}}}`); status != 200 {
		t.Fatalf("selection: %d %s", status, b)
	}
	status, b = ask("POST", "/api/projects/"+project.ID+"/terminals", owner, `{"agentId":"claude-code","device":"`+c.terminals.Device()+`","cols":80,"rows":24}`)
	if status != 201 {
		t.Fatalf("create: %d %s", status, b)
	}
	var session terminal.Metadata
	json.Unmarshal([]byte(b), &session)
	path := "/api/projects/" + project.ID + "/terminals/" + session.ID
	_, b = post(t, base+"/api/phones/codes", owner)
	var code struct{ Code, Link string }
	json.Unmarshal([]byte(b), &code)
	pairing, err := relay.DecodePairing(strings.TrimPrefix(code.Link, "https://connect.example.pages.dev/connect/#"))
	if err != nil {
		t.Fatal(err)
	}
	line, _ := dialPhone(t, pairing)
	status, b = line.do("POST", "/api/phones/enroll", "", `{"code":"`+code.Code+`","name":"Mini phone"}`)
	if status != 201 {
		t.Fatalf("enroll: %d %s", status, b)
	}
	var phone struct{ ID, Token string }
	json.Unmarshal([]byte(b), &phone)
	if status, b = line.do("GET", "/api/terminals", phone.Token, ""); status != 200 || strings.Contains(b, session.ID) {
		t.Fatalf("ungranted discovery: %d %s", status, b)
	}
	if status, b = ask("PUT", path+"/phone", owner, `{"phoneId":"`+phone.ID+`","enabled":true}`); status != 200 {
		t.Fatalf("grant: %d %s", status, b)
	}
	if status, b = line.do("GET", "/api/terminals", phone.Token, ""); status != 200 || !strings.Contains(b, session.ID) {
		t.Fatalf("discovery: %d %s", status, b)
	}
	if status, b = line.do("GET", "/api/terminals", owner, ""); status != 403 {
		t.Fatalf("relay owner token bypass: %d %s", status, b)
	}
	input := `{"data":"from phone\n","client":"relay-phone-0001","sequence":1}`
	for i := 0; i < 2; i++ {
		if status, b = line.do("POST", path+"/input", phone.Token, input); status != 200 {
			t.Fatalf("input: %d %s", status, b)
		}
	}
	_, offset := func() (string, uint64) {
		out, e := c.terminals.Output(context.Background(), project.ID, session.ID, 0)
		if e != nil {
			t.Fatal(e)
		}
		return string(out.Data), out.Next
	}()
	// Drop the entire encrypted link; a fresh handshake and cursor replay attach to the same process.
	line.c.CloseNow()
	line, _ = dialPhone(t, pairing)
	if status, b = line.do("GET", path+"/output?after=0", phone.Token, ""); status != 200 {
		t.Fatalf("reconnect: %d %s", status, b)
	}
	var output terminal.Output
	json.Unmarshal([]byte(b), &output)
	if output.Metadata.ID != session.ID || output.Metadata.State != "running" || output.Next < offset {
		t.Fatalf("process changed: %+v", output)
	}
	if status, b = line.do("POST", "/api/projects/other/terminals/"+session.ID+"/stop", phone.Token, `{}`); status != 404 {
		t.Fatalf("cross repository: %d %s", status, b)
	}
	// The Tailscale listener uses exactly the same grant. An owner token on that listener still cannot attach.
	priv := "http://" + node.addr()
	if st, _ := get(t, priv+"/api/terminals", phone.Token); st != 200 {
		t.Fatalf("private phone: %d", st)
	}
	if st, _ := get(t, priv+"/api/terminals", owner); st != 403 {
		t.Fatalf("private owner: %d", st)
	}
	if status, b = ask("DELETE", "/api/phones/"+phone.ID, owner, ""); status != 204 {
		t.Fatalf("revoke: %d %s", status, b)
	}
	if st, _ := get(t, priv+path+"/output?after=0", phone.Token); st != 401 {
		t.Fatalf("revoked private access: %d", st)
	}
	if l, closed := dialPhone(t, pairing); l != nil || closed != relay.CloseKicked {
		t.Fatalf("revoked relay access: %v %d", l, closed)
	}
	if err := c.terminals.Input(project.ID, session.ID, []byte("same process\n")); err != nil {
		t.Fatal(err)
	}
}
