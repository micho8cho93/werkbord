package device

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExecutionSafetyRefusesInterruptedReceiptsAndNativeProcesses(t *testing.T) {
	for _, file := range []string{"runner/operations/op_test.json", "device-operations/operations/op_test.json", "runner/terminals.json", "terminals.json"} {
		t.Run(file, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, file)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			raw := `{"result":{"state":"executing"}}`
			safe := `{"result":{"state":"uncertain"}}`
			if filepath.Base(path) == "terminals.json" {
				raw = `[{"state":"running"}]`
				safe = `[{"state":"interrupted"}]`
			}
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if err := CheckExecutionSettled(dir); err == nil {
				t.Fatal("interrupted work permitted update")
			}
			if err := os.WriteFile(path, []byte(safe), 0600); err != nil {
				t.Fatal(err)
			}
			if err := CheckExecutionSettled(dir); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOpenTerminalsAreDescribedForAPerson(t *testing.T) {
	now := time.Date(2026, 11, 3, 15, 0, 0, 0, time.Local)
	week := now.AddDate(0, 0, -7)
	for name, tc := range map[string]struct {
		list []RunningTerminal
		want string
	}{
		"one":       {[]RunningTerminal{{Agent: "codex", Repository: "test-repo", Since: week}}, "Codex in test-repo has been running since Oct 27 and is still open"},
		"today":     {[]RunningTerminal{{Agent: "claude-code", Repository: "app", Since: now.Add(-time.Hour)}}, "Claude Code in app has been running since today and is still open"},
		"yesterday": {[]RunningTerminal{{Agent: "codex", Repository: "app", Since: now.AddDate(0, 0, -1)}}, "Codex in app has been running since yesterday and is still open"},
		"unknown":   {[]RunningTerminal{{}}, "A terminal has been running for some time and is still open"},
		"several": {[]RunningTerminal{{Agent: "codex", Repository: "a", Since: week}, {Agent: "claude-code", Repository: "b", Since: now}},
			"2 terminals are still open: Codex in a (since Oct 27), Claude Code in b (since today)"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := DescribeTerminals(tc.list, now); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func writeTerminals(t *testing.T, path, state string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	raw := `[{"id":"t1","projectId":"p1","agentId":"codex","repository":"test-repo","state":"` + state + `","createdAt":"2026-10-27T10:43:38Z"}]`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestTheControllersAnswerOutranksItsStaleFile(t *testing.T) {
	dir := t.TempDir()
	writeTerminals(t, filepath.Join(dir, "terminals.json"), "running")

	// The file alone blocks, and says which terminal.
	var open *TerminalsRunning
	if err := OpenTerminals(dir, nil, false); !errors.As(err, &open) || len(open.Terminals) != 1 || open.Terminals[0].ID != "t1" || open.Terminals[0].Repository != "test-repo" {
		t.Fatalf("the file should block: %v", err)
	}
	// The controller says nothing is open: its record was stale.
	if err := OpenTerminals(dir, nil, true); err != nil {
		t.Fatalf("a stale record blocked the update: %v", err)
	}
	// The controller says one is open: that is what blocks, once.
	live := []RunningTerminal{{ID: "t9", Agent: "codex"}}
	if err := OpenTerminals(dir, live, true); !errors.As(err, &open) || len(open.Terminals) != 1 || open.Terminals[0].ID != "t9" {
		t.Fatalf("the live answer should block: %v", err)
	}
}

func TestARunnersTerminalsAreAlwaysReadFromItsFile(t *testing.T) {
	dir := t.TempDir()
	writeTerminals(t, filepath.Join(dir, "runner", "terminals.json"), "running")
	// Nothing can ask the runner, so the controller's answer does not cover it.
	if err := OpenTerminals(dir, nil, true); err == nil {
		t.Fatal("a runner's open terminal was ignored")
	}
}

func TestLiveTerminalsKeepsOnlyRunningAndFallsBackOnFailure(t *testing.T) {
	var status = 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.URL.Path != "/api/terminals" {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`[{"id":"a","projectId":"p","agentId":"codex","repository":"r","state":"running"},{"id":"b","state":"exited"},{"id":"c","state":"stopped"}]`))
	}))
	defer srv.Close()
	got, ok := LiveTerminals(context.Background(), srv.Client(), srv.URL, "tok")
	if !ok || len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("%v %v", got, ok)
	}
	if _, ok := LiveTerminals(context.Background(), srv.Client(), srv.URL, "wrong"); ok {
		t.Fatal("an unauthorised answer was trusted")
	}
	status = 403 // terminals not available to this caller
	if _, ok := LiveTerminals(context.Background(), srv.Client(), srv.URL, "tok"); ok {
		t.Fatal("a refusal was read as an empty list")
	}
	srv.Close()
	if _, ok := LiveTerminals(context.Background(), srv.Client(), srv.URL, "tok"); ok {
		t.Fatal("an unreachable controller was read as an empty list")
	}
}
