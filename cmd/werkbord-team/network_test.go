package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"devboard/internal/enrollment"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// The command line, end to end, on one computer standing in for two: make a workspace with its private network, run the server, invite a second
// host, have it join over TLS, hand it the workspace's keys, and revoke it.
func TestTheCommandsMakeAWorkspaceInviteAHostHandItTheKeysAndRevokeIt(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	api := "127.0.0.1:" + strconv.Itoa(freePort(t))
	boot := "127.0.0.1:" + strconv.Itoa(freePort(t))
	env := map[string]string{"WERKBORD_TEAM_DATA_DIR": first, "WERKBORD_TEAM_ADDR": api, "WERKBORD_TEAM_BOOTSTRAP_ADDR": boot,
		"WERKBORD_TEAM_ENDPOINTS": "localhost", "WERKBORD_TEAM_NETWORK_NODE": "off", "WERKBORD_TEAM_SERVER": "http://" + api}
	for k, v := range env {
		t.Setenv(k, v)
	}
	out, errOut, err := runCLI(t, nil, "workspace", "create", "--name", "Acme", "--owner", "Ada", "--network")
	if err != nil {
		t.Fatalf("workspace create: %v\n%s", err, errOut)
	}
	token := tokenRE.FindString(out)
	fp := regexp.MustCompile(`(?m)^\s+([a-z2-7]{4}(-[a-z2-7]{4}){12})$`).FindStringSubmatch(out)
	if token == "" || fp == nil || !strings.Contains(out, "Private network 10.") || !strings.Contains(out, "first Workspace Host") || !strings.Contains(out, "high-trust") {
		t.Fatalf("output:\n%s", out)
	}
	if !strings.Contains(out, "also a Connectivity Host") {
		t.Errorf("a host reachable at a name was not made a Connectivity Host:\n%s", out)
	}
	t.Setenv("WERKBORD_TEAM_TOKEN", token)

	// Run the server as `serve` does.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		var o, e strings.Builder
		done <- run(ctx, []string{"serve"}, &o, &e)
	}()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("serve: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("serve did not stop")
		}
	}()
	up := false
	for i := 0; i < 150 && !up; i++ {
		if res, err := http.Get("http://" + api + "/api/team/v1/health"); err == nil {
			res.Body.Close()
			up = true
		} else {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !up {
		t.Fatal("the server did not start")
	}

	// status
	out, _, err = runCLI(t, nil, "network", "status")
	if err != nil || !strings.Contains(out, "Remote access: not guaranteed") || !strings.Contains(out, "What to fix") || !strings.Contains(out, fp[1]) {
		t.Fatalf("status: %v\n%s", err, out)
	}

	// An invitation for a new host of the owner's.
	me := struct{ Member struct{ ID string } }{}
	req, _ := http.NewRequest("GET", "http://"+api+"/api/team/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.NewDecoder(res.Body).Decode(&me)
	res.Body.Close()
	out, _, err = runCLI(t, nil, "network", "invite", "--for-member", me.Member.ID, "--capability", "workspace_host", "--label", "second", "--qr")
	if err != nil {
		t.Fatalf("invite: %v\n%s", err, out)
	}
	link := regexp.MustCompile(`werkbord://join/\S+`).FindString(out)
	if link == "" || !strings.Contains(out, fp[1]) || !strings.Contains(out, "only copy") || !strings.Contains(out, "█") && !strings.Contains(out, "▀") {
		t.Fatalf("invite output:\n%s", out)
	}
	// A link for a member's own device is not for this command.
	memberLink := func() string {
		o, _, err := runCLI(t, nil, "network", "invite")
		if err != nil {
			t.Fatal(err)
		}
		return regexp.MustCompile(`werkbord://join/\S+`).FindString(o)
	}()
	if _, _, err := runCLI(t, nil, "device", "join", memberLink, "--data-dir", t.TempDir()); err == nil || !strings.Contains(err.Error(), "member's own device") {
		t.Errorf("a member's invitation was joined as a host: %v", err)
	}
	// A wrong expected fingerprint stops it before anything is sent.
	if _, _, err := runCLI(t, nil, "device", "join", link, "--data-dir", second, "--expect-fingerprint", "zzzz-zzzz"); !errorsIs(err, enrollment.ErrWrongWorkspace) {
		t.Errorf("a wrong fingerprint: %v", err)
	}
	out, _, err = runCLI(t, nil, "device", "join", link, "--data-dir", second, "--name", "second-host", "--expect-fingerprint", fp[1])
	if err != nil {
		t.Fatalf("device join: %v\n%s", err, out)
	}
	if !strings.Contains(out, "joined") || !strings.Contains(out, "host promote") {
		t.Errorf("join output:\n%s", out)
	}
	if _, err := os.Stat(second + "/pki/host.keys.sealed"); err != nil {
		t.Error("the joined host has no sealed keys")
	}
	if b, _ := os.ReadFile(second + "/pki/workspace.key.sealed"); len(b) != 0 {
		t.Error("a host that has only joined holds the workspace's key")
	}
	devID := regexp.MustCompile(`device (dev_[a-z2-7]{16})`).FindStringSubmatch(out)
	if devID == nil {
		t.Fatalf("no device ID in:\n%s", out)
	}

	// Hand it the keys: sealed by an administrator, collected by that host.
	if _, _, err := runCLI(t, nil, "host", "collect", "--data-dir", second, "--server", "http://"+api); err == nil {
		t.Error("collected keys that were never sent")
	}
	if _, _, err := runCLI(t, nil, "host", "promote", devID[1]); err != nil {
		t.Fatal(err)
	}
	// The host authenticates with its own credential, kept sealed in its vault.
	t.Setenv("WERKBORD_TEAM_TOKEN", "unused-on-this-host")
	out, _, err = runCLI(t, nil, "host", "collect", "--data-dir", second, "--server", "http://"+api)
	if err != nil || !strings.Contains(out, "now holds the workspace's keys") {
		t.Fatalf("collect: %v\n%s", err, out)
	}
	if b, _ := os.ReadFile(second + "/pki/workspace.key.sealed"); len(b) == 0 {
		t.Error("the promoted host holds no workspace key")
	}
	// It speaks for the same workspace.
	if _, _, err := runCLI(t, nil, "host", "collect", "--data-dir", second, "--server", "http://"+api); err == nil || !strings.Contains(err.Error(), "already holds") {
		t.Errorf("collecting twice: %v", err)
	}

	// Revocation, from the owner's side.
	t.Setenv("WERKBORD_TEAM_TOKEN", token)
	out, _, err = runCLI(t, nil, "device", "list")
	if err != nil || !strings.Contains(out, "second-host") {
		t.Fatalf("device list: %v\n%s", err, out)
	}
	out, _, err = runCLI(t, nil, "device", "revoke", devID[1])
	if err != nil || !strings.Contains(out, "is revoked") {
		t.Fatalf("device revoke: %v\n%s", err, out)
	}
	out, _, _ = runCLI(t, nil, "device", "list")
	if !strings.Contains(out, "revoked") {
		t.Errorf("device list after revoking:\n%s", out)
	}
	t.Setenv("WERKBORD_TEAM_TOKEN", "unused-on-this-host")
	if _, _, err := runCLI(t, nil, "device", "list"); err == nil {
		t.Error("a bogus token listed devices")
	}
}

func errorsIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
