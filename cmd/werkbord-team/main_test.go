package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"net"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"devboard/internal/logging"
	"devboard/internal/team/config"
	"devboard/internal/team/server"
)

func runCLI(t *testing.T, env map[string]string, args ...string) (string, string, error) {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
	var out, errOut bytes.Buffer
	err := run(context.Background(), args, &out, &errOut)
	return out.String(), errOut.String(), err
}

func TestVersionAndHelp(t *testing.T) {
	old := version
	version = "v1.2.3"
	defer func() { version = old }()
	if out, _, err := runCLI(t, nil, "version"); err != nil || out != "v1.2.3\n" {
		t.Fatalf("%q %v", out, err)
	}
	if _, errOut, err := runCLI(t, nil); !errors.Is(err, flag.ErrHelp) || !strings.Contains(errOut, "workspace create") {
		t.Fatalf("no arguments should print the usage: %q %v", errOut, err)
	}
	if _, _, err := runCLI(t, nil, "bogus"); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("%v", err)
	}
}

var tokenRE = regexp.MustCompile(`wbt_[0-9a-f]{64}`)

func TestWorkspaceCreateMakesAnOwnerTokenThatSignsIn(t *testing.T) {
	dir := t.TempDir()
	env := map[string]string{"WERKBORD_TEAM_DATA_DIR": dir}
	out, _, err := runCLI(t, env, "workspace", "create", "--name", "Acme", "--owner", "Ada", "--email", "ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	token := tokenRE.FindString(out)
	if token == "" || !strings.Contains(out, "http://127.0.0.1:7430/#token="+token) || !strings.Contains(out, `"Acme"`) {
		t.Fatalf("output:\n%s", out)
	}

	cfg := config.Load()
	db, svc, err := server.Open(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, err := svc.Authenticate(context.Background(), token)
	if err != nil || a.Member.Name != "Ada" || a.Workspace.Name != "Acme" || a.Member.Email != "ada@example.com" {
		t.Fatalf("%+v %v", a, err)
	}

	for _, args := range [][]string{
		{"workspace"}, {"workspace", "destroy"},
		{"workspace", "create", "--owner", "Ada"}, {"workspace", "create", "--name", "Acme"},
	} {
		if _, _, err := runCLI(t, env, args...); err == nil {
			t.Errorf("%v should fail", args)
		}
	}
}

func TestMigrateReportsTheSchemaVersion(t *testing.T) {
	out, _, err := runCLI(t, map[string]string{"WERKBORD_TEAM_DATA_DIR": t.TempDir()}, "migrate")
	if err != nil || !strings.Contains(out, "schema version 1") {
		t.Fatalf("%q %v", out, err)
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func TestServeAnswersAndStopsWhenAsked(t *testing.T) {
	addr := freeAddr(t)
	cfg := config.Default()
	cfg.DataDir, cfg.Addr = t.TempDir(), addr
	log, _ := logging.New(&bytes.Buffer{}, "info", "text")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx, cfg, log, "v0.0.1") }()

	var health map[string]any
	for i := 0; i < 100; i++ {
		res, err := http.Get("http://" + addr + "/api/team/v1/health")
		if err == nil {
			_ = json.NewDecoder(res.Body).Decode(&health)
			res.Body.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if health["status"] != "ok" || health["version"] != "v0.0.1" {
		t.Fatalf("health: %v", health)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not stop")
	}
	// A second server on a taken address says so.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	cfg.Addr = ln.Addr().String()
	if err := server.Run(context.Background(), cfg, log, "v"); err == nil || !strings.Contains(err.Error(), "listen") {
		t.Fatalf("%v", err)
	}
}
