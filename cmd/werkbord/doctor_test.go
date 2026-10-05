package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devboard/internal/doctor"
)

func (e *testEnv) doctorReport() doctor.Report {
	e.t.Helper()
	e.out.Reset()
	if err := e.app.cmdDoctor(bg, []string{"--json"}); err != nil {
		if _, silent := err.(silentError); !silent {
			e.t.Fatal(err)
		}
	}
	var r doctor.Report
	if err := json.Unmarshal(e.out.Bytes(), &r); err != nil {
		e.t.Fatalf("doctor --json: %v\n%s", err, e.out.String())
	}
	return r
}

func status(t *testing.T, r doctor.Report, id string) doctor.Check {
	t.Helper()
	c, ok := r.Find(id)
	if !ok {
		t.Fatalf("no %q check in %+v", id, r.Checks)
	}
	return c
}

func TestDoctorOnAHealthySetup(t *testing.T) {
	e := newTestEnv(t)
	e.fakeAgent("codex", fakeCodex)
	e.fakeAgent("claude-code", fakeClaude)
	e.signInWhenOpened()
	if err := e.app.cmdSetup(bg, nil); err != nil {
		t.Fatal(err)
	}
	repo := newGitRepo(t)
	c, _ := e.app.client()
	if _, err := c.registerProject(bg, repo, "demo"); err != nil {
		t.Fatal(err)
	}
	// The agents were reconfigured after the controller started: restart so it reads them.
	if err := e.app.cmdRestart(bg, nil); err != nil {
		t.Fatal(err)
	}

	r := e.doctorReport()
	want := map[string]doctor.Status{
		"controller": doctor.OK, "service": doctor.OK, "database": doctor.OK, "config": doctor.OK, "git": doctor.OK,
		"codex": doctor.OK, "claude-code": doctor.OK, "network": doctor.OK, "runner": doctor.OK, "projects": doctor.OK,
		"github": doctor.Warn, // gh is not installed in this test: optional, so a warning, never a failure
	}
	for id, st := range want {
		if got := status(t, r, id); got.Status != st {
			t.Errorf("%s = %s (%s), want %s", id, got.Status, got.Summary, st)
		}
	}
	if r.Worst() != doctor.Warn {
		t.Errorf("worst = %s", r.Worst())
	}
	if s := status(t, r, "network").Summary; !strings.Contains(s, "https://devboard-test.tail1234.ts.net/") {
		t.Errorf("network = %s", s)
	}
	if s := status(t, r, "projects").Summary; !strings.Contains(s, "1 configured") {
		t.Errorf("projects = %s", s)
	}
	if s := status(t, r, "codex").Summary; !strings.Contains(s, "7.7.7") {
		t.Errorf("codex = %s", s)
	}
	// Warnings alone do not fail the command; --strict makes them.
	e.out.Reset()
	if err := e.app.cmdDoctor(bg, nil); err != nil {
		t.Fatalf("doctor with warnings: %v", err)
	}
	if err := e.app.cmdDoctor(bg, []string{"--strict"}); err == nil {
		t.Fatal("--strict ignored a warning")
	}
}

func TestDoctorNeverPrintsASecret(t *testing.T) {
	e := newTestEnv(t)
	e.fakeAgent("codex", fakeCodex)
	// The private network is waiting for the user to sign in, so its one-time link exists.
	if err := e.app.cmdSetup(bg, []string{"--no-open", "--network-wait", "200ms"}); err != nil {
		t.Fatal(err)
	}
	tok := e.token()
	for _, args := range [][]string{nil, {"--json"}} {
		e.out.Reset()
		_ = e.app.cmdDoctor(bg, args)
		out := e.out.String()
		for what, secret := range map[string]string{"the access token": tok, "the network sign-in link": e.node.authURL, "its path": "one-time-link"} {
			if secret != "" && strings.Contains(out, secret) {
				t.Errorf("doctor %v printed %s:\n%s", args, what, out)
			}
		}
	}
	if c := status(t, e.doctorReport(), "network"); c.Status != doctor.Warn || !strings.Contains(c.Summary, "sign in") {
		t.Fatalf("network = %+v", c)
	}
}

func TestDoctorWithNoAgentAtAllFails(t *testing.T) {
	e := newTestEnv(t) // both agents point at commands that do not exist
	if err := e.app.cmdSetup(bg, []string{"--no-network", "--no-open"}); err != nil {
		t.Fatal(err)
	}
	r := e.doctorReport()
	for _, id := range []string{"codex", "claude-code"} {
		c := status(t, r, id)
		if c.Status != doctor.Warn || !strings.Contains(c.Summary, "not installed") || c.Fix == "" {
			t.Errorf("%s = %+v: a missing agent is a warning that says how to install it", id, c)
		}
	}
	if c := status(t, r, "agent"); c.Status != doctor.Fail || !strings.Contains(c.Fix, "never asks for an API key") {
		t.Fatalf("agent = %+v", c)
	}
	e.out.Reset()
	err := e.app.cmdDoctor(bg, nil)
	if _, silent := err.(silentError); !silent {
		t.Fatalf("doctor with a failure must exit non-zero: %v", err)
	}
	if !strings.Contains(e.output(), "✗") || !strings.Contains(e.output(), "problem(s) to fix") {
		t.Fatalf("output:\n%s", e.output())
	}
	// One agent is enough.
	e.fakeAgent("codex", fakeCodex)
	_ = e.app.cmdRestart(bg, nil)
	if r := e.doctorReport(); func() bool { _, ok := r.Find("agent"); return ok }() {
		t.Fatalf("one working agent should be enough: %+v", r.Checks)
	}
}

func TestDoctorWhenTheControllerIsNotRunningStillChecksWhatItCan(t *testing.T) {
	e := newTestEnv(t)
	e.fakeAgent("codex", fakeCodex)
	r := e.doctorReport()
	if c := status(t, r, "controller"); c.Status != doctor.Fail || !strings.Contains(c.Fix, "werkbord start") {
		t.Fatalf("controller = %+v", c)
	}
	if c := status(t, r, "database"); c.Status != doctor.Warn || !strings.Contains(c.Summary, "not created yet") {
		t.Fatalf("database = %+v", c)
	}
	if c := status(t, r, "git"); c.Status != doctor.OK {
		t.Fatalf("git = %+v", c)
	}
	if c := status(t, r, "codex"); c.Status != doctor.OK {
		t.Fatalf("codex = %+v: agents can be checked without the controller", c)
	}
	for _, id := range []string{"network", "runner", "projects"} {
		if c := status(t, r, id); c.Status != doctor.Skip || !strings.Contains(c.Summary, "controller running") {
			t.Errorf("%s = %+v: it needs the controller, and should say so", id, c)
		}
	}
}

func TestDoctorFindsABrokenDatabaseAndProject(t *testing.T) {
	e := newTestEnv(t)
	e.fakeAgent("codex", fakeCodex)
	if err := e.app.cmdSetup(bg, []string{"--no-network", "--no-open"}); err != nil {
		t.Fatal(err)
	}
	repo := newGitRepo(t)
	c, _ := e.app.client()
	if _, err := c.registerProject(bg, repo, "doomed"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	r := e.doctorReport()
	if p := status(t, r, "projects"); p.Status != doctor.Warn || !strings.Contains(p.Summary, "doomed") || !strings.Contains(p.Summary, "no longer exists") {
		t.Fatalf("projects = %+v", p)
	}

	// A database from a newer build, or a damaged one, is a failure with a way out.
	_ = e.app.cmdStop(bg, nil)
	db := filepath.Join(e.dataDir, "devboard.db")
	_ = os.Remove(db + "-wal")
	_ = os.Remove(db + "-shm")
	if err := os.WriteFile(db, []byte(strings.Repeat("not a database ", 100)), 0o600); err != nil {
		t.Fatal(err)
	}
	if c := status(t, e.doctorReport(), "database"); c.Status != doctor.Fail || !strings.Contains(c.Fix, "backups") {
		t.Fatalf("database = %+v", c)
	}
}

func TestPathCheckCatchesAnAgentTheServiceCannotSee(t *testing.T) {
	e := newTestEnv(t)
	// The user's shell finds claude; the controller (run by a service with a different PATH) does not.
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "claude"), []byte(fakeClaude), 0o755)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin") // no real codex or claude
	e.app.cfg.Agents = nil                                                                            // the default command names: claude, codex
	inner := doctor.Report{Checks: []doctor.Check{{ID: "claude-code", Name: "claude-code", Status: doctor.Warn, Summary: "Claude Code is not installed (optional if another agent works)"}}}
	cs := e.app.pathCheck(inner)
	if len(cs) != 1 || cs[0].Status != doctor.Warn || !strings.Contains(cs[0].Summary, "your shell finds claude") || !strings.Contains(cs[0].Fix, "werkbord setup") {
		t.Fatalf("path check = %+v", cs)
	}
	// An agent the shell does not find either gets no extra finding.
	inner2 := doctor.Report{Checks: []doctor.Check{{ID: "codex", Name: "codex", Status: doctor.Warn, Summary: "Codex is not installed"}}}
	if cs := e.app.pathCheck(inner2); len(cs) != 0 {
		t.Fatalf("path check = %+v", cs)
	}
}
