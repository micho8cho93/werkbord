package launcher

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"werkbord/internal/device"
)

func TestDesktopUpdateSafetyChecksRealRunsAndRunnerJournals(t *testing.T) {
	for _, tc := range []struct {
		name, state       string
		remote, wantError bool
	}{
		{"active", "running", false, true}, {"waiting", "waiting_for_user", false, true}, {"blocked", "blocked", false, true}, {"complete", "completed", false, false}, {"remote", "running", true, false}, {"unknown", "new_state", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			token := "disposable-safety-token"
			os.WriteFile(filepath.Join(dir, "token"), []byte(token), 0600)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/health" {
					w.Write([]byte(`{"status":"ok","version":"v1.8.0"}`))
					return
				}
				if r.Header.Get("Authorization") != "Bearer "+token {
					t.Error("safety check unauthenticated")
					w.WriteHeader(401)
					return
				}
				remote := "false"
				if tc.remote {
					remote = "true"
				}
				w.Write([]byte(`{"runs":[{"run":{"state":"` + tc.state + `","remote":` + remote + `}}]}`))
			}))
			defer srv.Close()
			os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"addr":"`+strings.TrimPrefix(srv.URL, "http://")+`"}`), 0600)
			l := New(Options{DataDir: dir})
			err := l.UpdateSafety(context.Background())
			if (err != nil) != tc.wantError {
				t.Fatalf("safety %s: %v", tc.name, err)
			}
			os.MkdirAll(filepath.Join(dir, "runner", "runs"), 0700)
			os.WriteFile(filepath.Join(dir, "runner", "runs", "run.json"), []byte(`{"phase":"active"}`), 0600)
			if err = l.UpdateSafety(context.Background()); err == nil {
				t.Fatal("runner's unresolved work ignored")
			}
		})
	}
}

// terminalController is a controller that answers for its native terminals: running holds what it says is open, and
// stopping one removes it.
func terminalController(t *testing.T, dir, token string, running *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/health":
			w.Write([]byte(`{"status":"ok","version":"v1.8.0"}`))
		case r.Header.Get("Authorization") != "Bearer "+token:
			w.WriteHeader(401)
		case r.URL.Path == "/api/terminals":
			list := []string{}
			for _, id := range *running {
				list = append(list, `{"id":"`+id+`","projectId":"p1","agentId":"codex","repository":"test-repo","state":"running","createdAt":"2026-10-27T10:43:38Z"}`)
			}
			w.Write([]byte("[" + strings.Join(list, ",") + "]"))
		case strings.HasSuffix(r.URL.Path, "/stop") && r.Method == "POST":
			id := strings.Split(r.URL.Path, "/")[5]
			kept := (*running)[:0]
			for _, r := range *running {
				if r != id {
					kept = append(kept, r)
				}
			}
			*running = kept
			w.Write([]byte(`{"stopped":true}`))
		default:
			w.Write([]byte(`{"runs":[]}`))
		}
	}))
	t.Cleanup(srv.Close)
	os.WriteFile(filepath.Join(dir, "token"), []byte(token), 0600)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"addr":"`+strings.TrimPrefix(srv.URL, "http://")+`"}`), 0600)
	return srv
}

func TestUpdateSafetyTrustsTheControllersTerminalsOverItsFile(t *testing.T) {
	dir := t.TempDir()
	// The file says a terminal is running, as one left behind by a controller that did not shut down cleanly does.
	os.WriteFile(filepath.Join(dir, "terminals.json"), []byte(`[{"id":"old","state":"running"}]`), 0600)
	running := []string{}
	terminalController(t, dir, "tok", &running)
	l := New(Options{DataDir: dir})
	if err := l.UpdateSafety(context.Background()); err != nil {
		t.Fatalf("a stale record blocked the update: %v", err)
	}
	running = []string{"t1"}
	err := l.UpdateSafety(context.Background())
	var open *device.TerminalsRunning
	if !errors.As(err, &open) || len(open.Terminals) != 1 || open.Terminals[0].ID != "t1" || open.Terminals[0].ProjectID != "p1" {
		t.Fatalf("an open terminal was not reported as one: %v", err)
	}
	if !strings.Contains(err.Error(), "Codex in test-repo has been running since Oct 27") {
		t.Fatalf("the refusal does not name the terminal: %v", err)
	}
}

func TestStopTerminalsEndsThemThroughTheController(t *testing.T) {
	dir := t.TempDir()
	running := []string{"t1", "t2"}
	terminalController(t, dir, "tok", &running)
	l := New(Options{DataDir: dir})
	err := l.StopTerminals(context.Background(), []device.RunningTerminal{{ID: "t1", ProjectID: "p1"}, {ID: "t2", ProjectID: "p1"}})
	if err != nil || len(running) != 0 {
		t.Fatalf("err %v, still running %v", err, running)
	}
	if err := l.StopTerminals(context.Background(), []device.RunningTerminal{{ID: "t1"}}); err == nil {
		t.Fatal("a terminal with no project was sent to a stop address")
	}
}
