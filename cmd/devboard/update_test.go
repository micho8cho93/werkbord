package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"devboard/internal/daemon"
	"devboard/internal/remote"
	"devboard/internal/update"
)

// publishRelease serves a releases page whose one release is a "devboard" that
// prints its own version, like the real one does for `devboard version`.
func publishRelease(t *testing.T, tag string, tamper bool) *httptest.Server {
	t.Helper()
	script := "#!/bin/sh\n[ \"$1\" = version ] && echo " + update.VersionOf(tag) + "\nexit 0\n"
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "devboard", Mode: 0o755, Size: int64(len(script)), Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte(script))
	_ = tw.Close()
	_ = gz.Close()
	archive := buf.Bytes()
	h := sha256.Sum256(archive)
	asset := update.AssetName(tag, runtime.GOOS, runtime.GOARCH)
	sums := hex.EncodeToString(h[:]) + "  " + asset + "\n"
	if tamper {
		archive = append(archive, 'x')
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/tag/"+tag, http.StatusFound) })
	mux.HandleFunc("/download/"+tag+"/checksums.txt", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, sums) })
	mux.HandleFunc("/download/"+tag+"/"+asset, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archive) })
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	t.Setenv("DEVBOARD_RELEASE_URL", ts.URL)
	return ts
}

func setVersion(t *testing.T, v string) {
	t.Helper()
	old := version
	version = v
	t.Cleanup(func() { version = old })
}

func TestUpdateInstallsTheNewReleaseAndRestartsTheController(t *testing.T) {
	e := newTestEnv(t)
	if err := e.app.cmdSetup(bg, []string{"--no-network", "--no-open"}); err != nil {
		t.Fatal(err)
	}
	setVersion(t, "v1.0.0")
	publishRelease(t, "v1.1.0", false)
	bin, _ := e.app.executable()
	startsBefore := e.mgr.starts

	// --check says so and changes nothing.
	e.out.Reset()
	if err := e.app.cmdUpdate(bg, []string{"--check"}); err != nil || !strings.Contains(e.output(), "Update available: v1.0.0 → v1.1.0") {
		t.Fatalf("check: %v\n%s", err, e.output())
	}
	if b, _ := os.ReadFile(bin); !strings.Contains(string(b), "echo old") {
		t.Fatal("--check replaced the binary")
	}

	e.out.Reset()
	if err := e.app.cmdUpdate(bg, nil); err != nil {
		t.Fatalf("update: %v\n%s", err, e.output())
	}
	if b, _ := os.ReadFile(bin); !strings.Contains(string(b), "v1.1.0") {
		t.Fatalf("the binary was not replaced: %s", b)
	}
	if e.mgr.starts != startsBefore+1 || !e.running() {
		t.Fatalf("the controller was not restarted once: starts %d -> %d, running %v", startsBefore, e.mgr.starts, e.running())
	}
	if !strings.Contains(e.output(), "Installed v1.1.0 over v1.0.0") || !strings.Contains(e.output(), "running again") {
		t.Fatalf("output:\n%s", e.output())
	}
	if _, err := os.Stat(bin + ".prev"); err == nil {
		t.Fatal("the rollback copy was kept after a good update")
	}

	// Already current.
	setVersion(t, "v1.1.0")
	e.out.Reset()
	if err := e.app.cmdUpdate(bg, nil); err != nil || !strings.Contains(e.output(), "Already up to date") {
		t.Fatalf("%v\n%s", err, e.output())
	}
}

func TestUpdateRefusesATamperedDownloadAndTouchesNothing(t *testing.T) {
	e := newTestEnv(t)
	setVersion(t, "v1.0.0")
	publishRelease(t, "v1.1.0", true)
	bin, _ := e.app.executable()
	err := e.app.cmdUpdate(bg, nil)
	if err == nil || !strings.Contains(err.Error(), "does not match its checksum") {
		t.Fatalf("update: %v", err)
	}
	if b, _ := os.ReadFile(bin); !strings.Contains(string(b), "echo old") {
		t.Fatal("a tampered download replaced the binary")
	}
}

func TestUpdateRefusesAnExecutableThatDoesNotReportTheVersion(t *testing.T) {
	e := newTestEnv(t)
	setVersion(t, "v1.0.0")
	ts := publishRelease(t, "v1.1.0", false)
	// The release's archive contains a devboard that claims to be another version.
	_ = ts
	t.Setenv("DEVBOARD_RELEASE_URL", publishLiar(t).URL)
	bin, _ := e.app.executable()
	err := e.app.cmdUpdate(bg, nil)
	if err == nil || !strings.Contains(err.Error(), "did not report v1.2.0") {
		t.Fatalf("update: %v", err)
	}
	if b, _ := os.ReadFile(bin); !strings.Contains(string(b), "echo old") {
		t.Fatal("the binary was replaced by something that was not what it said")
	}
}

func publishLiar(t *testing.T) *httptest.Server {
	t.Helper()
	// Same layout as publishRelease, but the executable says v9.9.9.
	script := "#!/bin/sh\n[ \"$1\" = version ] && echo v9.9.9\nexit 0\n"
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "devboard", Mode: 0o755, Size: int64(len(script)), Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte(script))
	_ = tw.Close()
	_ = gz.Close()
	h := sha256.Sum256(buf.Bytes())
	asset := update.AssetName("v1.2.0", runtime.GOOS, runtime.GOARCH)
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/tag/v1.2.0", http.StatusFound) })
	mux.HandleFunc("/download/v1.2.0/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, hex.EncodeToString(h[:])+"  "+asset+"\n")
	})
	mux.HandleFunc("/download/v1.2.0/"+asset, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(buf.Bytes()) })
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestUpdateRollsBackWhenTheNewVersionDoesNotStart(t *testing.T) {
	e := newTestEnv(t)
	if err := e.app.cmdSetup(bg, []string{"--no-network", "--no-open"}); err != nil {
		t.Fatal(err)
	}
	setVersion(t, "v1.0.0")
	publishRelease(t, "v1.1.0", false)
	bin, _ := e.app.executable()

	// The new version cannot start: the service manager fails while the new binary is in place.
	e.mgr.mu.Lock()
	e.mgr.startCheck = func() error {
		if strings.Contains(b(bin), "v1.1.0") {
			return errors.New("exec format error")
		}
		return nil
	}
	e.mgr.mu.Unlock()
	err := e.app.cmdUpdate(bg, nil)
	if err == nil || !strings.Contains(err.Error(), "failed and v1.0.0 is running again") {
		t.Fatalf("update: %v\n%s", err, e.output())
	}
	if got := b(bin); !strings.Contains(got, "echo old") {
		t.Fatalf("the old binary was not put back: %s", got)
	}
	if !e.running() {
		t.Fatal("the old controller is not running again")
	}
}

func b(path string) string { x, _ := os.ReadFile(path); return string(x) }

type updateRunnerManager struct {
	daemon.Manager
	statusErr error
	startErr  error
	running   bool
	onStart   func()
}

func (m *updateRunnerManager) Status(context.Context) (daemon.State, error) {
	return daemon.State{Running: m.running}, m.statusErr
}

func (m *updateRunnerManager) Stop(context.Context) error {
	m.running = false
	return nil
}

func (m *updateRunnerManager) Start(context.Context) error {
	if m.startErr != nil {
		return m.startErr
	}
	m.running = true
	if m.onStart != nil {
		m.onStart()
	}
	return nil
}

func TestUpdateRetainsRollbackUntilRunnerRestartSucceeds(t *testing.T) {
	for _, failure := range []string{"status", "restart", "crash", "none"} {
		t.Run(failure, func(t *testing.T) {
			e := newTestEnv(t)
			if err := e.app.cmdSetup(bg, []string{"--no-network", "--no-open"}); err != nil {
				t.Fatal(err)
			}
			setVersion(t, "v1.0.0")
			publishRelease(t, "v1.1.0", false)
			bin, _ := e.app.executable()
			dir := runnerDir(e.app.cfg)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			id := remote.Identity{RunnerID: "run_update", Controller: "https://controller.example.test"}
			if err := remote.AtomicJSON(filepath.Join(dir, "identity.json"), id); err != nil {
				t.Fatal(err)
			}
			m := &updateRunnerManager{running: true}
			m.onStart = func() {
				if !strings.Contains(b(bin+".prev"), "echo old") {
					t.Error("old executable deleted before runner restart")
				}
				if failure == "crash" {
					m.running = false
					return
				}
				if err := remote.AtomicJSON(filepath.Join(dir, "health.json"), remote.Health{Version: "v1.1.0", RunnerID: id.RunnerID, Controller: id.Controller, SyncedAt: time.Now(), Sequence: 42}); err != nil {
					t.Fatal(err)
				}
			}
			if failure == "status" {
				m.statusErr = errors.New("runner status failed")
			} else if failure == "restart" {
				m.startErr = errors.New("runner restart failed")
			}
			e.app.runnerDaemon = func(context.Context) daemon.Manager { return m }
			err := e.app.cmdUpdate(bg, nil)
			if failure == "none" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(bin + ".prev"); !os.IsNotExist(err) {
					t.Fatal("rollback executable retained after successful restarts")
				}
			} else if failure == "status" {
				if err == nil || !strings.Contains(b(bin), "echo old") {
					t.Fatalf("preflight status failure changed the executable: %v", err)
				}
				return
			} else if err == nil || !strings.Contains(err.Error(), bin+".prev") || !strings.Contains(b(bin+".prev"), "echo old") {
				t.Fatalf("runner failure lost rollback executable: %v", err)
			}
			if !e.running() || !strings.Contains(b(bin), "v1.1.0") {
				t.Fatal("updated controller is not running")
			}
		})
	}
}

func TestUpdateFromASourceBuildNeedsForce(t *testing.T) {
	e := newTestEnv(t)
	for _, v := range []string{"dev", "v0.6.0-4-g1234abc", "v0.6.0-dirty"} {
		setVersion(t, v)
		err := e.app.cmdUpdate(bg, nil)
		if err == nil || !strings.Contains(err.Error(), "built from source") {
			t.Errorf("%s: %v", v, err)
		}
	}
	// --check works anyway: it only reads.
	publishRelease(t, "v1.1.0", false)
	e.out.Reset()
	setVersion(t, "dev")
	if err := e.app.cmdUpdate(bg, []string{"--check"}); err != nil || !strings.Contains(e.output(), "Update available") {
		t.Fatalf("%v\n%s", err, e.output())
	}
}

func TestUpdateDowngradeAndBadVersionNeedCare(t *testing.T) {
	e := newTestEnv(t)
	setVersion(t, "v1.5.0")
	publishRelease(t, "v1.1.0", false)
	if err := e.app.cmdUpdate(bg, []string{"--version", "v1.1.0"}); err == nil || !strings.Contains(err.Error(), "older") {
		t.Fatalf("downgrade: %v", err)
	}
	if err := e.app.cmdUpdate(bg, []string{"--version", "1.1"}); err == nil || !strings.Contains(err.Error(), "not a version") {
		t.Fatalf("bad version: %v", err)
	}
	// The latest being older than what is installed is "nothing to do", not a downgrade.
	e.out.Reset()
	if err := e.app.cmdUpdate(bg, nil); err != nil || !strings.Contains(e.output(), "newer than the latest release") {
		t.Fatalf("%v\n%s", err, e.output())
	}
}

// Releases are named by a product tag, but the program reports the bare version.
func TestUpdateInstallsAReleaseNamedByAProductTag(t *testing.T) {
	e := newTestEnv(t)
	if err := e.app.cmdSetup(bg, []string{"--no-network", "--no-open"}); err != nil {
		t.Fatal(err)
	}
	setVersion(t, "v0.8.0")
	publishRelease(t, "werkbord-v0.8.1", false)
	bin, _ := e.app.executable()

	e.out.Reset()
	if err := e.app.cmdUpdate(bg, []string{"--check"}); err != nil || !strings.Contains(e.output(), "Update available: v0.8.0 → v0.8.1") {
		t.Fatalf("check: %v\n%s", err, e.output())
	}
	e.out.Reset()
	if err := e.app.cmdUpdate(bg, nil); err != nil {
		t.Fatalf("update: %v\n%s", err, e.output())
	}
	if b, _ := os.ReadFile(bin); !strings.Contains(string(b), "v0.8.1") {
		t.Fatalf("the binary was not replaced: %s", b)
	}
	if !strings.Contains(e.output(), "Installed v0.8.1 over v0.8.0") {
		t.Fatalf("output:\n%s", e.output())
	}

	// --version takes the bare version too, and finds the release by its product tag.
	setVersion(t, "v0.8.0")
	e.out.Reset()
	if err := e.app.cmdUpdate(bg, []string{"--version", "v0.8.1", "--force"}); err != nil {
		t.Fatalf("update --version: %v\n%s", err, e.output())
	}
}

func TestUpdatedRunnerRequiresFreshMatchingSync(t *testing.T) {
	for _, invalid := range []string{"missing", "stale", "wrong version", "wrong runner", "wrong controller", "old sequence", "future timestamp"} {
		t.Run(invalid, func(t *testing.T) {
			e := newTestEnv(t)
			id := remote.Identity{RunnerID: "run_required", Controller: "https://controller.example.test", ControllerSequence: 10}
			dir := runnerDir(e.app.cfg)
			if err := remote.AtomicJSON(filepath.Join(dir, "identity.json"), id); err != nil {
				t.Fatal(err)
			}
			m := &updateRunnerManager{running: true, onStart: func() {
				h := remote.Health{Version: "v1.1.0", RunnerID: id.RunnerID, Controller: id.Controller, SyncedAt: time.Now(), Sequence: 11}
				switch invalid {
				case "missing":
					return
				case "stale":
					h.SyncedAt = time.Now().Add(-time.Minute)
				case "wrong version":
					h.Version = "v1.0.0"
				case "wrong runner":
					h.RunnerID = "another_runner"
				case "wrong controller":
					h.Controller = "https://other.example.test"
				case "old sequence":
					h.Sequence = 10
				case "future timestamp":
					h.SyncedAt = time.Now().Add(time.Hour)
				}
				if err := remote.AtomicJSON(filepath.Join(dir, "health.json"), h); err != nil {
					t.Fatal(err)
				}
			}}
			ctx, cancel := context.WithTimeout(bg, 60*time.Millisecond)
			defer cancel()
			if err := e.app.restartUpdatedRunner(ctx, m, "v1.1.0"); err == nil || !strings.Contains(err.Error(), "fresh controller sync") {
				t.Fatalf("accepted %s runner health: %v", invalid, err)
			}
		})
	}
}

func TestUpdatePreservesExistingProjectsTasksIdentityAndGit(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollback=%v", rollback), func(t *testing.T) {
			e := newTestEnv(t)
			if err := e.app.cmdSetup(bg, []string{"--no-network", "--no-open"}); err != nil {
				t.Fatal(err)
			}
			repo := newGitRepo(t)
			c, err := e.app.client()
			if err != nil {
				t.Fatal(err)
			}
			p, err := c.registerProject(bg, repo, "Existing project")
			if err != nil {
				t.Fatal(err)
			}
			var task map[string]any
			if err := c.do(bg, "POST", "/api/projects/"+p.ID+"/tasks", map[string]any{"title": "Existing ticket", "sourceRef": "existing:ticket", "workBranch": "existing-work", "baseBranch": "main"}, &task); err != nil {
				t.Fatal(err)
			}
			git := func(args ...string) string {
				t.Helper()
				out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
				if err != nil {
					t.Fatalf("git: %v %s", err, out)
				}
				return strings.TrimSpace(string(out))
			}
			git("branch", "existing-work")
			head := git("rev-parse", "existing-work")
			dirtyPath := filepath.Join(repo, "uncommitted-user-work.txt")
			if err := os.WriteFile(dirtyPath, []byte("preserved user work"), 0600); err != nil {
				t.Fatal(err)
			}
			identityPath := filepath.Join(runnerDir(e.app.cfg), "identity.json")
			id := remote.Identity{RunnerID: "run_existing", Controller: "https://controller.example.test", Sequence: 10, PrivateKey: []byte("private fixture key")}
			if err := remote.AtomicJSON(identityPath, id); err != nil {
				t.Fatal(err)
			}
			identityBefore, _ := os.ReadFile(identityPath)
			e.app.runnerDaemon = func(context.Context) daemon.Manager { return &updateRunnerManager{} }
			setVersion(t, "v1.0.0")
			publishRelease(t, "v1.1.0", false)
			bin, _ := e.app.executable()
			if rollback {
				e.mgr.startCheck = func() error {
					if strings.Contains(b(bin), "v1.1.0") {
						return errors.New("intentional upgrade failure")
					}
					return nil
				}
			}
			err = e.app.cmdUpdate(bg, nil)
			if rollback && (err == nil || !strings.Contains(err.Error(), "running again")) || !rollback && err != nil {
				t.Fatalf("update: %v", err)
			}
			projects, err := c.listProjects(bg)
			if err != nil || len(projects) != 1 || projects[0].ID != p.ID {
				t.Fatalf("projects: %v %v", projects, err)
			}
			var savedTasks struct {
				Tasks []map[string]any `json:"tasks"`
			}
			if err := c.do(bg, "GET", "/api/projects/"+p.ID+"/tasks", nil, &savedTasks); err != nil {
				t.Fatal(err)
			}
			if len(savedTasks.Tasks) != 1 {
				t.Fatalf("tickets lost: %v", savedTasks.Tasks)
			}
			saved := savedTasks.Tasks[0]
			for _, key := range []string{"id", "title", "sourceRef", "workBranch", "baseBranch"} {
				if saved[key] != task[key] {
					t.Fatalf("task %s changed: %v", key, saved)
				}
			}
			identityAfter, _ := os.ReadFile(identityPath)
			if !bytes.Equal(identityBefore, identityAfter) || git("rev-parse", "existing-work") != head || b(dirtyPath) != "preserved user work" {
				t.Fatal("update changed runner identity or user Git work")
			}
		})
	}
}

func TestInstallerUpgradeUsesCurrentRecoveryOnExistingExecutable(t *testing.T) {
	for _, failure := range []string{"none", "controller", "active work"} {
		t.Run(failure, func(t *testing.T) {
			e := newTestEnv(t)
			self, _ := e.app.executable()
			if err := os.WriteFile(self, []byte("#!/bin/sh\necho v1.0.0\n"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := e.app.cmdSetup(bg, []string{"--no-network", "--no-open"}); err != nil {
				t.Fatal(err)
			}
			setVersion(t, "v1.1.0")
			publishRelease(t, "v1.1.0", false)
			dir := t.TempDir()
			archive, err := (update.Source{}).Download(bg, "v1.1.0", runtime.GOOS, runtime.GOARCH, dir)
			if err != nil {
				t.Fatal(err)
			}
			fresh, err := update.ExtractBinary(archive, runtime.GOOS, dir)
			if err != nil {
				t.Fatal(err)
			}
			e.app.executable = func() (string, error) { return fresh, nil }
			if failure == "controller" {
				e.mgr.startCheck = func() error {
					if strings.Contains(b(self), "v1.1.0") {
						return errors.New("installer startup failure")
					}
					return nil
				}
			} else if failure == "active work" {
				if err := remote.AtomicJSON(filepath.Join(runnerDir(e.app.cfg), "runs", "active.json"), map[string]any{"phase": "active"}); err != nil {
					t.Fatal(err)
				}
			}
			err = e.app.cmdInstallRelease(bg, []string{self})
			if failure == "none" {
				if err != nil || !strings.Contains(b(self), "v1.1.0") {
					t.Fatalf("installer did not update destination: %v", err)
				}
			} else if err == nil || !strings.Contains(b(self), "v1.0.0") {
				t.Fatalf("installer lost original executable: %v", err)
			}
			if !e.running() {
				t.Fatal("installer failed to preserve/recover controller")
			}
		})
	}
}
