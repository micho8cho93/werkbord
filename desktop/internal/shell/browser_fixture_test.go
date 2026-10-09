package shell

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/desktop/internal/migration"
	"devboard/desktop/internal/teamlink"
	"devboard/desktop/internal/workspaces"
)

// Browser-only transport for the real Shell. No fixture route ships in the app.
func TestWorkspaceShellBrowserFixture(t *testing.T) {
	dir := os.Getenv("WERKBORD_UNIFIED_BROWSER_FIXTURE")
	if dir == "" {
		t.Skip("browser harness only")
	}
	var meta map[string]string
	b, err := os.ReadFile(filepath.Join(dir, "team-ready.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &meta); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "shell-team.key")
	_ = os.WriteFile(keyPath, []byte(meta["firstKey"]), 0600)
	link, err := teamlink.New(meta["first"], keyPath, func() bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	personal := workspaces.NewPersonal(func(context.Context) (workspaces.Access, error) {
		return workspaces.Access{Base: os.Getenv("WERKBORD_BROWSER_PERSONAL"), Token: "disposable-browser-credential"}, nil
	})
	reg := workspaces.NewRegistry(workspaces.OpenState(filepath.Join(dir, "shell.json")), nil, personal, link.Source)
	legacy := filepath.Join(dir, "legacy-personal")
	_ = os.MkdirAll(legacy, 0700)
	_ = os.WriteFile(filepath.Join(legacy, "token"), []byte("disposable-migration-credential"), 0600)
	mig := &migration.Manager{Dir: filepath.Join(dir, "migration"), Roots: []migration.Installation{{Kind: "personal_data", Path: legacy}}}
	ui := &fakeUI{answers: []string{"Connect runner", "Connect runner", "Connect runner"}}
	sh := New(Options{Migration: mig, VerifyMigration: func(ctx context.Context) error { _, err := link.Source.List(ctx); return err }, Components: "Shell: v1.9.0-preview.1\nPersonal: v1.9.0-preview.1\nTeam: v3.8.0", Workspaces: reg, Team: link, TeamInstaller: &fakeInstaller{found: true}, Grants: personal, Invites: &Invites{}, UI: ui})
	mux := http.NewServeMux()
	dist := filepath.Join("..", "..", "frontend", "dist")
	mux.Handle("/", http.FileServer(http.Dir(dist)))
	// The shell's page with the fixture's stand-in for the native bridge, so a plain browser can open it.
	mux.HandleFunc("GET /shell/shell/index.html", func(w http.ResponseWriter, r *http.Request) {
		page, err := os.ReadFile(filepath.Join(dist, "shell", "shell", "index.html"))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(strings.Replace(string(page), "<head>", "<head><script>"+fixtureBridge+"</script>", 1)))
	})
	mux.HandleFunc("POST /fixture/call", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Method string
			Args   []json.RawMessage
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			w.WriteHeader(400)
			return
		}
		var out any
		var err error
		var id string
		if len(in.Args) > 0 {
			_ = json.Unmarshal(in.Args[0], &id)
		}
		switch in.Method {
		case "Info":
			out = sh.Info()
		case "MigrationStatus":
			out, err = sh.MigrationStatus()
		case "Migrate":
			// Substitute native dialog response only in this disposable browser fixture.
			ui.mu.Lock()
			if id == "adopt" {
				ui.answers = append([]string{"Back up and adopt"}, ui.answers...)
			} else {
				ui.answers = append([]string{"Roll back adoption"}, ui.answers...)
			}
			ui.mu.Unlock()
			out, err = sh.Migrate(id)
		case "Workspaces":
			out = sh.Workspaces()
		case "Overview":
			out = sh.Overview()
		case "OpenWorkspace":
			out, err = sh.OpenWorkspace(id)
		case "AddTeam":
			out, err = sh.AddTeam()
		case "ForgetWorkspace":
			err = sh.ForgetWorkspace(id)
		case "RememberPlace":
			var place string
			if len(in.Args) > 1 {
				_ = json.Unmarshal(in.Args[1], &place)
			}
			err = sh.RememberPlace(id, place)
		case "Relay":
			var method string
			var args []json.RawMessage
			if len(in.Args) == 3 {
				_ = json.Unmarshal(in.Args[1], &method)
				_ = json.Unmarshal(in.Args[2], &args)
			}
			out, err = sh.Relay(id, method, args)
		default:
			w.WriteHeader(400)
			return
		}
		if err != nil {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	ln, err := net.Listen("tcp", strings.TrimPrefix(os.Getenv("WERKBORD_BROWSER_SHELL_ORIGIN"), "http://"))
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go server.Serve(ln)
	_ = os.WriteFile(filepath.Join(dir, "shell-ready"), []byte("ready"), 0600)
	deadline := time.Now().Add(fixtureLifetime())
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "done")); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("shell browser harness did not finish")
}

// fixtureBridge answers the shell's native calls through the fixture's /fixture/call route.
const fixtureBridge = `if (window.parent === window && !window.go) window.go = { main: { App: new Proxy({}, { get: (_, method) => async (...args) => { const r = await fetch('/fixture/call', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ Method: method, Args: args }) }); const d = await r.json(); if (!r.ok) throw Error(d.error); return d; } }) } };`

// fixtureLifetime is how long the harness keeps serving: ten minutes, or WERKBORD_FIXTURE_MINUTES for a person exploring it.
func fixtureLifetime() time.Duration {
	if m, err := time.ParseDuration(os.Getenv("WERKBORD_FIXTURE_MINUTES") + "m"); err == nil && m > 0 {
		return m
	}
	return 10 * time.Minute
}
