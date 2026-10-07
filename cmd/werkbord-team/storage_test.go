package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/team/infra/rqlite/rqlitetest"
)

// The commands for a workspace whose data is in a cluster, through the real commands and over real HTTP, against the pinned
// database program. They skip when it has not been fetched (scripts/fetch-rqlite.sh); CI requires it.

func TestAReplicatedWorkspaceIsCreatedServedReportedAndBackedUpThroughTheCommands(t *testing.T) {
	program := rqlitetest.BinaryDir(t)
	data, backups := t.TempDir(), filepath.Join(t.TempDir(), "backups")
	addr := freeAddr(t)
	env := map[string]string{
		"WERKBORD_TEAM_DATA_DIR": data, "WERKBORD_TEAM_STORAGE": "replicated", "WERKBORD_TEAM_DATABASE_DIR": program,
		"WERKBORD_TEAM_STORAGE_PORT": fmt.Sprint(rqlitetest.FreePort(t)), "WERKBORD_TEAM_STORAGE_RAFT_PORT": fmt.Sprint(rqlitetest.FreePort(t)),
		"WERKBORD_TEAM_BACKUP_DIR": backups, "WERKBORD_TEAM_ADDR": addr,
	}
	out, _, err := runCLI(t, env, "workspace", "create", "--name", "Acme", "--owner", "Ada")
	if err != nil {
		t.Fatal(err)
	}
	token := tokenRE.FindString(out)
	if token == "" || !strings.Contains(out, "cluster of Workspace Hosts") || !strings.Contains(out, "no high availability") {
		t.Fatalf("workspace create did not say where the data is kept:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(data, "storage.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(data, "team.db")); err == nil {
		t.Error("a replicated workspace has a team.db")
	}

	// Serve it.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		var o, e bytes.Buffer
		done <- run(ctx, []string{"serve"}, &o, &e)
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(60 * time.Second):
			t.Error("the server did not stop")
		}
	}()
	base := "http://" + addr
	var health map[string]any
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		res, err := http.Get(base + "/api/team/v1/health")
		if err == nil {
			health = map[string]any{}
			_ = json.NewDecoder(res.Body).Decode(&health)
			res.Body.Close()
			if res.StatusCode == 200 && health["status"] == "ok" {
				break // (a host that has just started answers read_only until its database node has a leader)
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	if health["status"] != "ok" {
		t.Fatalf("the server did not come up: %v", health)
	}
	if st, _ := health["storage"].(map[string]any); st["state"] != "replicated" || st["readOnly"] != false {
		t.Fatalf("health: %v", health)
	}

	// The report an administrator asks for, as the command prints it and as the API gives it.
	cliEnv := map[string]string{"WERKBORD_TEAM_SERVER": base, "WERKBORD_TEAM_TOKEN": token}
	report, _, err := runCLI(t, cliEnv, "storage", "status")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"replicated across 1 Workspace Host", "writable", "no high availability", "Add two more Workspace Hosts", "Backups: 0 in " + backups} {
		if !strings.Contains(report, want) {
			t.Errorf("the report does not say %q:\n%s", want, report)
		}
	}
	js, _, err := runCLI(t, cliEnv, "storage", "status", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var st struct {
		State    string
		Writable bool
		Topology struct{ Voters, Quorum, FaultTolerance int }
		Program  struct{ Version string }
	}
	if err := json.Unmarshal([]byte(js), &st); err != nil || st.State != "replicated" || !st.Writable || st.Topology.Voters != 1 || st.Topology.FaultTolerance != 0 || st.Program.Version == "" {
		t.Fatalf("%+v %v\n%s", st, err, js)
	}

	// Writes work, and a backup is taken, checked and found.
	me := person{t: t, base: base, token: token}
	me.ok("POST", "/projects", map[string]any{"name": "App"})
	bout, _, err := runCLI(t, cliEnv, "storage", "backup")
	if err != nil || !strings.Contains(bout, "read back and checked") {
		t.Fatalf("%q %v", bout, err)
	}
	lout, _, err := runCLI(t, nil, "storage", "backups", "--dir", backups)
	if err != nil || !strings.Contains(lout, "workspace-") {
		t.Fatalf("%q %v", lout, err)
	}
	name := strings.Fields(lout)[0]
	vout, _, err := runCLI(t, nil, "storage", "verify-backup", name, "--dir", backups)
	if err != nil || !strings.Contains(vout, "can be restored with this version") {
		t.Fatalf("%q %v", vout, err)
	}
	// Removing the only host is refused, in words.
	if _, _, err := runCLI(t, cliEnv, "host", "remove", "tdv_nothing"); err == nil {
		t.Error("removing a device that is not a host succeeded")
	}
}

func TestStorageCommandsRefuseWhatIsNotSafe(t *testing.T) {
	for _, args := range [][]string{
		{"storage"}, {"storage", "bogus"}, {"storage", "restore", "x"}, {"storage", "restore", "x", "--safety-dir", "/tmp/x"},
		{"storage", "rollback"}, {"storage", "verify-backup"}, {"storage", "migrate", "--dry-run", "--data-dir", t.TempDir()},
	} {
		if _, _, err := runCLI(t, map[string]string{"WERKBORD_TEAM_DATA_DIR": t.TempDir()}, args...); err == nil {
			t.Errorf("%v should fail", args)
		}
	}
	// A restore without --yes says what it would do and does nothing.
	_, _, err := runCLI(t, nil, "storage", "restore", "x", "--safety-dir", "/tmp/x", "--data-dir", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Errorf("%v", err)
	}
}
