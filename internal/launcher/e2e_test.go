//go:build !windows

package launcher

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"devboard/internal/update"
)

// These tests run the real thing: the werkbord program is built from this source tree
// (twice, as 1.0.0 and 1.1.0), and the launcher starts, finds and updates real
// controllers with it, each in its own process. They never touch the computer they run
// on: HOME and the data directory are temporary, the address is a free port, and
// Werkbord is set up without a login service, so no launchd job or systemd unit is made.

func moduleDir(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		t.Fatalf("go list -m: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func build(t *testing.T, version, out string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", out, "-ldflags", "-X main.version="+version, "./cmd/werkbord")
	cmd.Dir = moduleDir(t)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", version, err, b)
	}
}

// releaseServer serves a releases page whose newest release is tag, with one archive for
// this platform holding the program at binary, laid out as GitHub's releases are.
func releaseServer(t *testing.T, tag, binary string) *httptest.Server {
	t.Helper()
	asset := fmt.Sprintf("werkbord_%s_%s_%s.tar.gz", strings.TrimPrefix(tag, "werkbord-v"), runtime.GOOS, runtime.GOARCH)
	var archive []byte
	{
		body, err := os.ReadFile(binary)
		if err != nil {
			t.Fatal(err)
		}
		var buf strings.Builder
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(&tar.Header{Name: "werkbord", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write(body)
		_ = tw.Close()
		_ = gz.Close()
		archive = []byte(buf.String())
	}
	sum := sha256.Sum256(archive)
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/tag/"+tag, http.StatusFound) })
	mux.HandleFunc("/download/"+tag+"/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), asset)
	})
	mux.HandleFunc("/download/"+tag+"/"+asset, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archive) })
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func pidOf(t *testing.T, dataDir string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dataDir, "controller.pid"))
	if err != nil {
		t.Fatalf("no controller.pid: %v", err)
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

func alive(pid int) bool { return pid > 0 && syscall.Kill(pid, 0) == nil }

func get(t *testing.T, url, token string) int {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestTheDesktopLifecycleWithTheRealProgram(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the real program")
	}
	tmp := t.TempDir()
	// What a release would contain, and what the "app" ships. Built first: once HOME is a
	// temporary directory, the go command would fetch its whole module cache into it.
	v10, v11 := filepath.Join(tmp, "werkbord-1.0.0"), filepath.Join(tmp, "werkbord-1.1.0")
	build(t, "v1.0.0", v10)
	build(t, "v1.1.0", v11)

	home := filepath.Join(tmp, "home")
	data := filepath.Join(home, "data")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("WERKBORD_DATA_DIR", data)
	t.Setenv("DEVBOARD_DATA_DIR", "")
	addr := freeAddr(t)
	t.Setenv("WERKBORD_ADDR", addr)
	t.Setenv("DEVBOARD_ADDR", "")

	releases := releaseServer(t, "werkbord-v1.1.0", v11)
	t.Setenv("WERKBORD_RELEASE_URL", releases.URL)

	newLauncher := func() *Launcher {
		return New(Options{
			Version: "v1.0.0", Bundled: v10, NoService: true, Home: home,
			ShellPATH: func(context.Context) string { return "" },
			Source:    update.Source{Base: releases.URL},
		})
	}
	installed := filepath.Join(home, ".local", "bin", "werkbord")
	t.Cleanup(func() { // whatever happens, no controller outlives the test
		if alive(pidOf0(data)) {
			_ = exec.Command(installed, "stop").Run()
		}
		if pid := pidOf0(data); alive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	// 1. A computer Werkbord was never on: the app installs its program, sets up, and starts the controller.
	conn, err := newLauncher().Connect(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if conn.URL != "http://"+addr || conn.Version != "v1.0.0" {
		t.Fatalf("connection = %+v", conn)
	}
	if fi, err := os.Stat(installed); err != nil || fi.Mode()&0o111 == 0 {
		t.Fatalf("the program was not installed at %s: %v", installed, err)
	}
	pid := pidOf(t, data)
	if !alive(pid) {
		t.Fatal("the controller is not running")
	}

	// 2. Authentication is the controller's own, and is untouched.
	tok, err := os.ReadFile(filepath.Join(data, "token"))
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(string(tok))
	if conn.SignInURL != conn.URL+"/#token="+token {
		t.Fatalf("sign-in link %q", conn.SignInURL)
	}
	if fi, _ := os.Stat(filepath.Join(data, "token")); fi.Mode().Perm() != 0o600 {
		t.Fatalf("the token file is %v: it must be readable by the user only", fi.Mode().Perm())
	}
	for _, tc := range []struct {
		name, token string
		want        int
	}{{"no token", "", 401}, {"a wrong token", "not-the-token", 401}, {"the token", token, 200}} {
		if got := get(t, conn.URL+"/api/projects", tc.token); got != tc.want {
			t.Errorf("GET /api/projects with %s = %d, want %d", tc.name, got, tc.want)
		}
	}
	// The web app a browser (or a phone) is given is the same one, served by the same controller.
	if got := get(t, conn.URL+"/", ""); got != 200 {
		t.Errorf("GET / = %d", got)
	}

	// 3. Opening the app again, from another window or another launch, starts nothing.
	for i := 0; i < 2; i++ {
		if _, err := newLauncher().Connect(ctx, nil); err != nil {
			t.Fatal(err)
		}
	}
	if got := pidOf(t, data); got != pid || !alive(pid) {
		t.Fatalf("the controller was replaced (pid %d → %d)", pid, got)
	}
	// Nor can a second controller be started on the same data, by the program or by hand.
	out, err := exec.CommandContext(ctx, installed, "start").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "already running") || pidOf(t, data) != pid {
		t.Fatalf("`werkbord start` beside a running controller: %v\n%s", err, out)
	}
	if out, err := exec.CommandContext(ctx, installed, "serve").CombinedOutput(); err == nil || !strings.Contains(string(out), "already using") {
		t.Fatalf("a second `werkbord serve` on one data directory was not refused: %v\n%s", err, out)
	}

	// 4. The app has no way to stop the controller, and going away does not: it is not the app's child.
	conn2, err := newLauncher().Connect(ctx, nil)
	if err != nil || conn2.Version != "v1.0.0" {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if !alive(pid) || get(t, conn.URL+"/api/health", "") != 200 {
		t.Fatal("the controller stopped")
	}

	// 5. There is an update, and the installed program is what says so.
	l := newLauncher()
	if st := l.UpdateStatus(ctx, true); !st.Available || st.Current != "v1.0.0" || st.Latest != "v1.1.0" {
		t.Fatalf("update status = %+v", st)
	}

	// 6. Agents at work stop an update: the controller is not restarted under them.
	journal := filepath.Join(data, "runner", "runs", "r1.json")
	if err := os.MkdirAll(filepath.Dir(journal), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journal, []byte(`{"phase":"running"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := l.ApplyUpdate(ctx); err == nil || !strings.Contains(err.Error(), "finish or resolve it") {
		t.Fatalf("an update went ahead over unfinished work: %v", err)
	}
	if v := l.FindInstall(ctx).Version; v != "v1.0.0" || pidOf(t, data) != pid || !alive(pid) {
		t.Fatalf("a refused update changed something: %s, pid %d", v, pidOf(t, data))
	}
	if err := os.Remove(journal); err != nil {
		t.Fatal(err)
	}

	// 7. Updating: checksum-verified download, database snapshot, replace, restart, verify.
	out2, err := l.ApplyUpdate(ctx)
	if err != nil {
		t.Fatalf("update: %v\n%s", err, out2)
	}
	if v := l.FindInstall(ctx).Version; v != "v1.1.0" {
		t.Fatalf("installed version after the update: %s", v)
	}
	conn3, err := newLauncher().Connect(ctx, nil)
	if err != nil || conn3.Version != "v1.1.0" {
		t.Fatalf("after the update: %+v, %v", conn3, err)
	}
	if pidOf(t, data) == pid {
		t.Fatal("the controller was not restarted on the new program")
	}
	if backups, _ := filepath.Glob(filepath.Join(data, "backups", "before-update-*.db")); len(backups) != 1 {
		t.Fatalf("no database snapshot was taken before the update: %v", backups)
	}
	if get(t, conn3.URL+"/api/projects", token) != 200 {
		t.Fatal("the token no longer works after the update")
	}
	if st := l.UpdateStatus(ctx, true); st.Available {
		t.Fatalf("still offered the release it just installed: %+v", st)
	}

	// 8. An app that ships an older program than the one installed does not put it back.
	if _, err := newLauncher().Connect(ctx, nil); err != nil || l.FindInstall(ctx).Version != "v1.1.0" {
		t.Fatalf("an older app downgraded the install: %v", err)
	}
}

func pidOf0(dataDir string) int {
	b, err := os.ReadFile(filepath.Join(dataDir, "controller.pid"))
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

// A controller that was set up before the app existed (by the installer, from a terminal) is the
// one the app joins, wherever its program and data are: no second installation is made.
func TestTheAppJoinsAnInstallationMadeByTheTerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the real program")
	}
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	data := filepath.Join(tmp, "custom data") // the terminal chose it
	toolDir := filepath.Join(tmp, "tools")
	for _, d := range []string{home, toolDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cli := filepath.Join(toolDir, "werkbord")
	build(t, "v1.0.0", cli) // before HOME changes, for the reason above
	t.Setenv("HOME", home)
	t.Setenv("WERKBORD_DATA_DIR", data)
	addr := freeAddr(t)
	t.Setenv("WERKBORD_ADDR", addr)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if out, err := exec.CommandContext(ctx, cli, "setup", "--no-service", "--no-open", "--no-network").CombinedOutput(); err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command(cli, "stop").Run() })
	pid := pidOf(t, data)

	// The Finder gives the app none of that: only the files the installation left.
	t.Setenv("WERKBORD_DATA_DIR", "")
	t.Setenv("WERKBORD_ADDR", "")
	cfgFile := filepath.Join(data, "config.json")
	b, err := os.ReadFile(cfgFile)
	if err != nil || !strings.Contains(string(b), addr) {
		t.Fatalf("setup should have recorded the address in %s: %v %s", cfgFile, err, b)
	}
	l := New(Options{
		Version: "v1.1.0", Bundled: filepath.Join(tmp, "unused"), Home: home, DataDir: data,
		ShellPATH: func(context.Context) string { return "" },
	})
	conn, err := l.Connect(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if conn.URL != "http://"+addr || conn.Version != "v1.0.0" || pidOf(t, data) != pid {
		t.Fatalf("connection %+v, pid %d (was %d)", conn, pidOf(t, data), pid)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "bin", "werkbord")); err == nil {
		t.Fatal("the app installed a second copy of the program beside an installation that was running")
	}
}

// Installing a newer program while nothing is running is how a new disk image upgrades an old installation. The
// program keeps the old executable for whoever checks that the new controller came up; that has to be done, or the
// next update is refused for the leftover.
func TestAnUpgradeWhileNothingRunsLeavesNothingBehindToBlockTheNextUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the real program")
	}
	tmp := t.TempDir()
	v10, v11, v12, v13 := filepath.Join(tmp, "werkbord-1.0.0"), filepath.Join(tmp, "werkbord-1.1.0"), filepath.Join(tmp, "werkbord-1.2.0"), filepath.Join(tmp, "werkbord-1.3.0")
	for _, v := range []struct{ version, path string }{{"v1.0.0", v10}, {"v1.1.0", v11}, {"v1.2.0", v12}, {"v1.3.0", v13}} {
		build(t, v.version, v.path) // all before HOME changes: see TestTheDesktopLifecycleWithTheRealProgram
	}
	home := filepath.Join(tmp, "home")
	data := filepath.Join(home, "data")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("WERKBORD_DATA_DIR", data)
	addr := freeAddr(t)
	t.Setenv("WERKBORD_ADDR", addr)
	releases := releaseServer(t, "werkbord-v1.2.0", v12)
	t.Setenv("WERKBORD_RELEASE_URL", releases.URL)
	installed := filepath.Join(home, ".local", "bin", "werkbord")
	t.Cleanup(func() {
		if alive(pidOf0(data)) {
			_ = exec.Command(installed, "stop").Run()
		}
	})
	launcherFor := func(bundled, version string) *Launcher {
		return New(Options{Version: version, Bundled: bundled, NoService: true, Home: home, ShellPATH: func(context.Context) string { return "" }, Source: update.Source{Base: releases.URL}})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	// An installation made by an older app, then stopped (the computer was restarted, say).
	if _, err := launcherFor(v10, "v1.0.0").Connect(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.CommandContext(ctx, installed, "stop").CombinedOutput(); err != nil {
		t.Fatalf("stop: %v\n%s", err, out)
	}

	// A newer app is opened: it upgrades the program (nothing is running to interrupt), and starts it.
	l := launcherFor(v11, "v1.1.0")
	conn, err := l.Connect(ctx, nil)
	if err != nil || conn.Version != "v1.1.0" || conn.Notice != "" {
		t.Fatalf("after the upgrade: %+v, %v", conn, err)
	}
	if v := l.FindInstall(ctx).Version; v != "v1.1.0" {
		t.Fatalf("installed version = %s", v)
	}
	if _, err := os.Stat(installed + ".prev"); err == nil {
		t.Fatal("the old program was left beside the new one: the next update would be refused for it")
	}
	if snaps, _ := filepath.Glob(filepath.Join(data, "backups", "before-update-*.db")); len(snaps) == 0 {
		t.Fatal("the database was not snapshotted before the upgrade")
	}

	// And so the next update, from the app, goes through.
	if out, err := l.ApplyUpdate(ctx); err != nil {
		t.Fatalf("the update after an upgrade was refused: %v\n%s", err, out)
	}
	if v := l.FindInstall(ctx).Version; v != "v1.2.0" {
		t.Fatalf("installed version after the update = %s", v)
	}

	// The same when the update itself is applied while nothing is running (its menu item, from the error screen):
	// it installs and leaves the controller stopped, and the connection that follows finishes it.
	releases13 := releaseServer(t, "werkbord-v1.3.0", v13)
	t.Setenv("WERKBORD_RELEASE_URL", releases13.URL)
	if out, err := exec.CommandContext(ctx, installed, "stop").CombinedOutput(); err != nil {
		t.Fatalf("stop: %v\n%s", err, out)
	}
	if out, err := l.ApplyUpdate(ctx); err != nil {
		t.Fatalf("update while stopped: %v\n%s", err, out)
	}
	if _, err := os.Stat(installed + ".prev"); err != nil {
		t.Fatalf("the old program should be kept until the new controller has been seen to start: %v", err)
	}
	conn, err = l.Connect(ctx, nil)
	if err != nil || conn.Version != "v1.3.0" || conn.Notice != "" {
		t.Fatalf("after updating while stopped: %+v, %v", conn, err)
	}
	if _, err := os.Stat(installed + ".prev"); err == nil {
		t.Fatal("the old program was left beside the new one after the controller came up on it")
	}
}
