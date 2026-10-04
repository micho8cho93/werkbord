package doctor

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devboard/internal/agent"
	"devboard/internal/agent/fake"
	"devboard/internal/config"
	"devboard/internal/domain"
	"devboard/internal/netprivate"
	"devboard/internal/store/sqlite"
)

var bg = context.Background()

func one(t *testing.T, cs []Check) Check {
	t.Helper()
	if len(cs) != 1 {
		t.Fatalf("checks = %+v", cs)
	}
	return cs[0]
}

func env(t *testing.T) Env {
	t.Helper()
	c := config.Default()
	c.DataDir = t.TempDir()
	if err := os.Chmod(c.DataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return Env{Config: c}
}

func TestReportWorstAndRendering(t *testing.T) {
	r := Report{Version: "v1", Checks: []Check{
		{ID: "a", Name: "alpha", Status: OK, Summary: "fine"},
		{ID: "b", Name: "b", Status: Warn, Summary: "meh", Fix: "do this\nand that"},
		{ID: "c", Name: "c", Status: Skip, Summary: "later"},
	}}
	if r.Worst() != Warn {
		t.Fatalf("worst = %s", r.Worst())
	}
	var plain bytes.Buffer
	r.Text(&plain, false)
	out := plain.String()
	for _, want := range []string{"✓ alpha", "! b", "- c", "do this", "and that", "Working, with 1 thing(s) to look at."} {
		if !strings.Contains(out, want) {
			t.Errorf("text lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("colour without being asked")
	}
	var coloured bytes.Buffer
	r.Text(&coloured, true)
	if !strings.Contains(coloured.String(), "\x1b[33m") {
		t.Error("no colour when asked")
	}
	r.Add(Check{ID: "d", Name: "d", Status: Fail, Summary: "broken"})
	if r.Worst() != Fail {
		t.Fatalf("worst = %s", r.Worst())
	}
	var js bytes.Buffer
	if err := r.JSON(&js); err != nil {
		t.Fatal(err)
	}
	var back Report
	if err := json.Unmarshal(js.Bytes(), &back); err != nil || len(back.Checks) != 4 || back.Version != "v1" {
		t.Fatalf("json = %s, %v", js.String(), err)
	}
	if c, ok := r.Find("b"); !ok || c.Fix == "" {
		t.Fatalf("Find = %+v", c)
	}
	if (Report{}).Worst() != OK {
		t.Fatal("an empty report is fine")
	}
}

func TestACrashingCheckDoesNotTakeTheReportDown(t *testing.T) {
	r := Run(bg, "v", func(context.Context) []Check { return []Check{{ID: "x", Name: "x", Status: OK, Summary: "ok"}} },
		func(context.Context) []Check { panic("boom") },
		func(context.Context) []Check { return []Check{{ID: "y", Name: "y", Status: OK, Summary: "ok"}} })
	if len(r.Checks) != 3 || r.Checks[1].Status != Fail || !strings.Contains(r.Checks[1].Summary, "boom") || r.Checks[2].ID != "y" {
		t.Fatalf("checks = %+v", r.Checks)
	}
}

func TestDatabaseCheck(t *testing.T) {
	e := env(t)
	if c := one(t, e.database(bg)); c.Status != Warn || !strings.Contains(c.Summary, "not created yet") {
		t.Fatalf("missing = %+v", c)
	}
	db, err := sqlite.Open(bg, e.Config.DBPath(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if c := one(t, e.database(bg)); c.Status != OK || !strings.Contains(c.Summary, "integrity ok") || !strings.Contains(c.Summary, "(current)") {
		t.Fatalf("current = %+v", c)
	}

	// Behind this build: it will be upgraded, after a backup, at the next start.
	raw, _ := sql.Open("sqlite", e.Config.DBPath())
	if _, err := raw.Exec(`DELETE FROM schema_migrations WHERE version = (SELECT MAX(version) FROM schema_migrations)`); err != nil {
		t.Fatal(err)
	}
	if c := one(t, e.database(bg)); c.Status != Warn || !strings.Contains(c.Summary, "behind") {
		t.Fatalf("behind = %+v", c)
	}
	// Newer than this build.
	_, _ = raw.Exec(`INSERT INTO schema_migrations (version, name, applied_at) VALUES (999, 'future', 1)`)
	_ = raw.Close()
	if c := one(t, e.database(bg)); c.Status != Fail || !strings.Contains(c.Fix, "devboard update") {
		t.Fatalf("newer = %+v", c)
	}
	// Not a database at all.
	if err := os.WriteFile(e.Config.DBPath(), []byte(strings.Repeat("junk ", 500)), 0o600); err != nil {
		t.Fatal(err)
	}
	if c := one(t, e.database(bg)); c.Status != Fail || !strings.Contains(c.Fix, "backups") {
		t.Fatalf("damaged = %+v", c)
	}
}

func TestConfigCheck(t *testing.T) {
	e := env(t)
	if c := one(t, e.configCheck(bg)); c.Status != OK {
		t.Fatalf("default = %+v", c)
	}

	// A token anyone on the computer can read.
	if err := os.WriteFile(e.Config.TokenPath(), []byte("secret-token-value"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := one(t, e.configCheck(bg))
	if c.Status != Warn || !strings.Contains(c.Summary, "can be read by other users") {
		t.Fatalf("world-readable token = %+v", c)
	}
	if strings.Contains(c.Summary+c.Fix, "secret-token-value") {
		t.Fatal("the check printed the token")
	}
	_ = os.Chmod(e.Config.TokenPath(), 0o600)

	e.Config.RequireToken = false
	if c := one(t, e.configCheck(bg)); c.Status != Warn || !strings.Contains(c.Summary, "any program here") {
		t.Fatalf("no token = %+v", c)
	}
	e.Config.RequireToken = true
	e.Config.Addr = "0.0.0.0:7420"
	if c := one(t, e.configCheck(bg)); c.Status != Warn || !strings.Contains(c.Summary, "not only on this computer") {
		t.Fatalf("non-loopback = %+v", c)
	}
	e.Config.Addr = "127.0.0.1:7420"
	e.Config.Agents = map[string]config.AgentConfig{config.AgentCodex: {Sandbox: "danger-full-access"}}
	if c := one(t, e.configCheck(bg)); c.Status != Warn || !strings.Contains(c.Summary, "danger-full-access") {
		t.Fatalf("risky agent setting = %+v", c)
	}
	e.Config.Agents = nil
	e.Config.LogFormat = "xml"
	if c := one(t, e.configCheck(bg)); c.Status != Fail {
		t.Fatalf("invalid config = %+v", c)
	}
}

func TestGitCheck(t *testing.T) {
	e := env(t)
	e.GitBinary = filepath.Join(t.TempDir(), "no-git")
	if c := one(t, e.git(bg)); c.Status != Fail || !strings.Contains(c.Fix, "Install Git") {
		t.Fatalf("missing git = %+v", c)
	}
	script := func(version string) string {
		p := filepath.Join(t.TempDir(), "git")
		_ = os.WriteFile(p, []byte("#!/bin/sh\necho 'git version "+version+"'\n"), 0o755)
		return p
	}
	for version, want := range map[string]Status{"2.47.1": OK, "2.38.0": OK, "2.30.2": Warn, "2.20.1": Fail, "1.9.5": Fail} {
		e.GitBinary = script(version)
		if c := one(t, e.git(bg)); c.Status != want {
			t.Errorf("git %s = %s (%s), want %s", version, c.Status, c.Summary, want)
		}
	}
}

func registry(t *testing.T, infos ...domain.Agent) *agent.Registry {
	t.Helper()
	r := agent.NewRegistry()
	for _, in := range infos {
		if err := r.Register(&fake.Adapter{Name: in.ID, Info: in}); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func TestAgentChecks(t *testing.T) {
	e := env(t)
	e.Agents = registry(t,
		domain.Agent{ID: "claude-code", Name: "Claude Code", Installed: true, SignIn: domain.SignedIn, Available: true, Version: "2.1.285"},
		domain.Agent{ID: "codex", Name: "Codex", Installed: true, SignIn: domain.SignedOut, Version: "0.155.1", Detail: "not signed in: run `codex login`", Guidance: "Run `codex login`."},
	)
	cs := e.agents(bg)
	by := map[string]Check{}
	for _, c := range cs {
		by[c.ID] = c
	}
	if c := by["claude-code"]; c.Status != OK || !strings.Contains(c.Summary, "signed in") {
		t.Fatalf("claude = %+v", c)
	}
	if c := by["codex"]; c.Status != Warn || !strings.Contains(c.Summary, "not signed in") || c.Fix != "Run `codex login`." {
		t.Fatalf("codex = %+v", c)
	}
	if _, bad := by["agent"]; bad {
		t.Fatal("one working agent is enough")
	}

	e.Agents = registry(t,
		domain.Agent{ID: "claude-code", Name: "Claude Code", Detail: `"claude" was not found on PATH`, Guidance: "Install Claude Code."},
		domain.Agent{ID: "codex", Name: "Codex", Detail: `"codex" was not found on PATH`, Guidance: "Install Codex."},
	)
	cs = e.agents(bg)
	last := cs[len(cs)-1]
	if last.ID != "agent" || last.Status != Fail || !strings.Contains(last.Fix, "never asks for an API key") {
		t.Fatalf("no agent = %+v", cs)
	}
	for _, c := range cs[:2] {
		if c.Status != Warn || !strings.Contains(c.Summary, "not installed") || c.Fix == "" {
			t.Fatalf("missing agent = %+v", c)
		}
	}
	if c := one(t, (Env{}).agents(bg)); c.Status != Skip {
		t.Fatalf("no registry = %+v", c)
	}
}

func TestNetworkChecksNeverShowTheSignInLink(t *testing.T) {
	e := env(t)
	cases := []struct {
		st   netprivate.Status
		want Status
		in   string
	}{
		{netprivate.Status{State: netprivate.StateOff}, Warn, "is off"},
		{netprivate.Status{State: netprivate.StateStarting}, Warn, "starting"},
		{netprivate.Status{State: netprivate.StateNeedsLogin, AuthURL: "https://login.tailscale.com/a/SECRET"}, Warn, "sign in"},
		{netprivate.Status{State: netprivate.StateNeedsApproval}, Warn, "approve"},
		{netprivate.Status{State: netprivate.StateError, Error: "no route"}, Fail, "no route"},
		{netprivate.Status{State: netprivate.StateConnected, URL: "https://devboard.ts.net/", HTTPS: true}, OK, "https://devboard.ts.net/"},
		{netprivate.Status{State: netprivate.StateConnected, URL: "http://100.1.1.1/", HTTPSHint: "Turn on HTTPS"}, Warn, "http"},
	}
	for _, tc := range cases {
		tc := tc
		e.Network = func() (netprivate.Status, bool) { return tc.st, true }
		c := one(t, e.network(bg))
		if c.Status != tc.want || !strings.Contains(c.Summary+c.Fix, tc.in) {
			t.Errorf("%s = %+v", tc.st.State, c)
		}
		if strings.Contains(c.Summary+c.Fix, "SECRET") {
			t.Errorf("%s: the sign-in link leaked into %+v", tc.st.State, c)
		}
	}
	e.Network = nil
	if c := one(t, e.network(bg)); c.Status != Skip {
		t.Fatalf("no controller = %+v", c)
	}
	e.Network = func() (netprivate.Status, bool) { return netprivate.Status{}, false }
	if c := one(t, e.network(bg)); c.Status != Skip {
		t.Fatalf("controller down = %+v", c)
	}
}

func TestSkipsWhatNeedsAControllerAndSaysSo(t *testing.T) {
	e := env(t)
	for _, f := range []Func{e.runner, e.projects, e.github} {
		if c := one(t, f(bg)); c.Status != Skip {
			t.Errorf("%+v: a check with nothing to look at must say it was skipped, not pass", c)
		}
	}
}
