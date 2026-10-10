package teamlink

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devboard/desktop/internal/workspaces"
)

var bg = context.Background()

const key = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func writeKey(t *testing.T, mode os.FileMode, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "access.key")
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTheCredentialIsReadAndNeverMade(t *testing.T) {
	if k, err := ReadKey(writeKey(t, 0o600, key)); err != nil || k != key {
		t.Fatalf("%q %v", k, err)
	}
	for name, p := range map[string]string{
		"group readable": writeKey(t, 0o640, key),
		"short":          writeKey(t, 0o600, "abc"),
		"not hex":        writeKey(t, 0o600, strings.Repeat("z", 64)),
		"missing":        filepath.Join(t.TempDir(), "nope"),
	} {
		if _, err := ReadKey(p); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	missing := filepath.Join(t.TempDir(), "nope")
	_, _ = ReadKey(missing)
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("reading the credential made one")
	}
	// A symlink is not a private file of the person's.
	target := writeKey(t, 0o600, key)
	link := filepath.Join(t.TempDir(), "link")
	_ = os.Symlink(target, link)
	if _, err := ReadKey(link); err == nil {
		t.Error("a symlink was followed")
	}
}

func TestTheStatusTellsNotSetUpFromStoppedFromOutOfDate(t *testing.T) {
	installed := false
	l, err := New(DefaultBase, writeKey(t, 0o600, key), func() bool { return installed })
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		err       error
		installed bool
		want      string
	}{
		{nil, true, "ready"},
		{workspaces.ErrNoCredential, false, "not_installed"},
		{workspaces.ErrNotRunning, false, "not_installed"},
		{workspaces.ErrNotRunning, true, "stopped"},
		{workspaces.ErrOutdated, true, "outdated"},
		{workspaces.ErrRefused, true, "refused"},
	}
	for _, c := range cases {
		installed = c.installed
		if got := l.StatusOf(c.err); got.State != c.want {
			t.Errorf("%v installed=%v: %s, want %s", c.err, c.installed, got.State, c.want)
		}
	}
}

func TestSlotsAreAddedForgottenAndGivenGrantsOnlyByTheirOwnIdentity(t *testing.T) {
	var got []string
	var body map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			w.WriteHeader(401)
			return
		}
		got = append(got, r.Method+" "+r.URL.Path)
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/device/v1/workspaces":
			_, _ = w.Write([]byte(`{"slot":"ws_ab12cd34ef56","created":true}`))
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/runner/grant"):
			w.WriteHeader(204)
		case r.Method == "DELETE":
			w.WriteHeader(409)
			_, _ = w.Write([]byte(`{"error":{"code":"device","message":"leave this workspace safely before removing it from this computer"}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	l, err := New(srv.URL, writeKey(t, 0o600, key), nil)
	if err != nil {
		t.Fatal(err)
	}
	id, err := l.AddWorkspace(bg)
	if err != nil || id != "team:ws_ab12cd34ef56" {
		t.Fatalf("%q %v", id, err)
	}
	if err := l.DeliverGrant(bg, id, "wba_x", "http://127.0.0.1:7420"); err != nil {
		t.Fatal(err)
	}
	if body["token"] != "wba_x" || got[len(got)-1] != "POST /w/ws_ab12cd34ef56/api/device/v1/runner/grant" {
		t.Fatalf("%v %v", got, body)
	}
	// The service's own reason reaches the person.
	if err := l.ForgetWorkspace(bg, id); err == nil || !strings.Contains(err.Error(), "leave this workspace safely") {
		t.Fatalf("%v", err)
	}
	// Anything that is not a Team workspace identity goes nowhere, and cannot be smuggled into a path.
	for _, bad := range []string{"personal", "team:", "team:../x", "team:a/b", "team:a b", "team:" + strings.Repeat("a", 30)} {
		if err := l.DeliverGrant(bg, bad, "wba_x", ""); err == nil {
			t.Errorf("%q was used", bad)
		}
		if err := l.ForgetWorkspace(bg, bad); err == nil {
			t.Errorf("%q was forgotten", bad)
		}
	}
	srv.Close()
	if _, err := l.AddWorkspace(bg); !errors.Is(err, workspaces.ErrNotRunning) {
		t.Fatalf("%v", err)
	}
}

// ---- the installer ----

// fakeApp is an app bundle as a release has it: the executable, and Team's service beside the other helpers.
func fakeApp(t *testing.T, dir string, withService bool) string {
	t.Helper()
	exe := filepath.Join(dir, "Werkbord.app", "Contents", "MacOS", "Werkbord")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if withService {
		helper := filepath.Join(dir, "Werkbord.app", "Contents", "Helpers", "werkbord-team")
		if err := os.MkdirAll(filepath.Dir(helper), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(helper, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return exe
}

func TestOnlyTheAppsOwnInstallerIsRunAndOnlyWithFixedArguments(t *testing.T) {
	dir := t.TempDir()
	exe := fakeApp(t, dir, true)
	var ran [][]string
	i := &Installer{
		Program: exe,
		Run: func(_ context.Context, name string, args ...string) (string, error) {
			ran = append(ran, append([]string{name}, args...))
			return "noise from a library\n" + `{"ok":true,"version":"3.6.0","detail":"the Team service is running"}` + "\n", nil
		},
	}
	r, err := i.Activate(bg)
	if err != nil || !r.OK || r.Version != "3.6.0" {
		t.Fatalf("%+v %v", r, err)
	}
	if len(ran) != 1 || ran[0][0] != exe || ran[0][1] != "--activate" || len(ran[0]) != 2 {
		t.Fatalf("ran %v", ran)
	}
	for _, a := range []string{"start", "stop", "uninstall"} {
		if _, err := i.Service(bg, a); err != nil {
			t.Errorf("%s: %v", a, err)
		}
	}
	if len(ran) != 4 || ran[3][1] != "--service" || ran[3][2] != "uninstall" {
		t.Fatalf("ran %v", ran)
	}
	for _, bad := range []string{"", "install", "--activate", "stop; rm -rf /", "Stop"} {
		if _, err := i.Service(bg, bad); err == nil {
			t.Errorf("%q was run", bad)
		}
	}
	if len(ran) != 4 {
		t.Fatal("a refused action was run")
	}
	// A build that does not carry Team's service (run from source) cannot install it, and nothing runs.
	bare := fakeApp(t, t.TempDir(), false)
	for _, none := range []*Installer{{Program: bare, Run: i.Run}, {Program: filepath.Join(dir, "missing"), Run: i.Run}, {Run: i.Run}} {
		if _, err := none.Activate(bg); err == nil || !strings.Contains(err.Error(), "does not carry Team's service") {
			t.Fatalf("%v", err)
		}
	}
	if len(ran) != 4 {
		t.Fatal("an installer that is not there was run")
	}
	// The installer's own words reach the person when it fails.
	failing := &Installer{Program: exe, Run: func(context.Context, string, ...string) (string, error) {
		return `{"ok":false,"version":"3.6.0","detail":"service installation was cancelled; choose Try again when you are ready"}`, errors.New("exit status 1")
	}}
	if _, err := failing.Activate(bg); err == nil || !strings.Contains(err.Error(), "was cancelled") {
		t.Fatalf("%v", err)
	}
	garbled := &Installer{Program: exe, Run: func(context.Context, string, ...string) (string, error) { return "<html>", nil }}
	if _, err := garbled.Activate(bg); err == nil {
		t.Fatal("an answer that is not JSON was believed")
	}
}
