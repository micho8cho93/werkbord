package launcher

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"werkbord/internal/device"
)

type deviceCall struct {
	args  []string
	input string
}

func deviceRunner(calls *[]deviceCall, mu *sync.Mutex, release <-chan struct{}, fail error, noise string) func(context.Context, []string, string, []string, io.Reader, io.Writer) error {
	return func(_ context.Context, _ []string, _ string, args []string, in io.Reader, out io.Writer) error {
		raw, _ := io.ReadAll(in)
		mu.Lock()
		*calls = append(*calls, deviceCall{args, string(raw)})
		mu.Unlock()
		if release != nil {
			<-release
		}
		if noise != "" {
			_, _ = io.WriteString(out, noise)
		}
		return fail
	}
}

func waitJob(t *testing.T, l *Launcher) DeviceJob {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st, err := l.DeviceStatus(bg)
		if err != nil {
			t.Fatal(err)
		}
		if !st.Job.Running {
			return st.Job
		}
		if time.Now().After(deadline) {
			t.Fatal("job never finished")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestDeviceStatusDefaultsToTheLocalControllerAndReadsBindings(t *testing.T) {
	w := newWorld(t)
	l := w.launcher(Options{})
	st, err := l.DeviceStatus(bg)
	if err != nil || st.Mode != "local" || st.Runner || st.ClientPaired || st.RunnerPaired || len(st.Projects) != 0 {
		t.Fatalf("%+v %v", st, err)
	}
	dir := filepath.Join(w.cfg.DataDir, "runner")
	writeFile(t, filepath.Join(dir, "identity.json"), `{"controller":"http://home.test","projects":["prj_1"]}`)
	writeFile(t, filepath.Join(dir, "repositories.json"), `{"prj_1":"/code/app"}`)
	if err = device.Save(w.cfg.DataDir, device.State{Mode: "remote", Controller: "http://home.test", Runner: true}); err != nil {
		t.Fatal(err)
	}
	st, err = l.DeviceStatus(bg)
	if err != nil || st.Mode != "remote" || !st.Runner || !st.RunnerPaired || st.Bindings["prj_1"] != "/code/app" || st.Projects[0] != "prj_1" {
		t.Fatalf("%+v %v", st, err)
	}
	// A runner paired to some other controller does not count as this connection's.
	writeFile(t, filepath.Join(dir, "identity.json"), `{"controller":"http://elsewhere.test"}`)
	if st, _ = l.DeviceStatus(bg); st.RunnerPaired {
		t.Fatal("another controller's runner was reported as paired")
	}
	writeFile(t, filepath.Join(dir, "identity.json"), `{broken`)
	if _, err = l.DeviceStatus(bg); err == nil {
		t.Fatal("an unreadable identity was ignored")
	}
}

func TestSetupSendsCodesOnlyOnStandardInputAndRedactsFailures(t *testing.T) {
	w := newWorld(t)
	w.program(filepath.Join(w.installDir(), "werkbord"), "v5.5.3-preview.1")
	var (
		mu    sync.Mutex
		calls []deviceCall
	)
	code := strings.Repeat("ab", 32)
	l := w.launcher(Options{})
	l.opt.RunInput = deviceRunner(&calls, &mu, nil, errors.New("exit 1"), "pairing code "+code+" was rejected\n")
	req := device.SetupRequest{Controller: "https://home.tailnet.ts.net:8443", Name: "Laptop", Code: code, Runner: true, RunnerCode: "rnr-secret"}
	if _, err := l.StartDeviceSetup(bg, req); err != nil {
		t.Fatal(err)
	}
	job := waitJob(t, l)
	if job.Phase != "failed" || strings.Contains(job.Error, code) || strings.Contains(job.Error, "rnr-secret") {
		t.Fatalf("%+v", job)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 || strings.Join(calls[0].args, " ") != "device configure" || !strings.Contains(calls[0].input, code) {
		t.Fatalf("%+v", calls)
	}
	for _, a := range calls[0].args {
		if strings.Contains(a, code) || strings.Contains(a, "rnr-secret") {
			t.Fatal("a pairing code reached the command line")
		}
	}
	if _, err := l.StartDeviceSetup(bg, device.SetupRequest{Controller: "http://127.0.0.1:1"}); err == nil {
		t.Fatal("an invalid request started a job")
	}
}

func TestOnlyOneDeviceOperationRunsAtATime(t *testing.T) {
	w := newWorld(t)
	w.program(filepath.Join(w.installDir(), "werkbord"), "v5.5.3-preview.1")
	var (
		mu    sync.Mutex
		calls []deviceCall
	)
	release := make(chan struct{})
	l := w.launcher(Options{})
	l.opt.RunInput = deviceRunner(&calls, &mu, release, nil, "")
	if _, err := l.UseLocalController(bg); err != nil {
		t.Fatal(err)
	}
	if st, _ := l.DeviceStatus(bg); !st.Job.Running || st.Job.Action != "local" {
		t.Fatalf("%+v", st.Job)
	}
	if _, err := l.BindDeviceRepository(bg, "prj_1", filepath.Join(t.TempDir(), "app")); err == nil {
		t.Fatal("a second operation started while one was running")
	}
	close(release)
	if job := waitJob(t, l); job.Phase != "done" {
		t.Fatalf("%+v", job)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 || strings.Join(calls[0].args, " ") != "device local" {
		t.Fatalf("%+v", calls)
	}
}

func TestBindingRefusesProjectsAndPathsItCannotTrust(t *testing.T) {
	w := newWorld(t)
	l := w.launcher(Options{})
	abs := t.TempDir()
	for _, c := range [][2]string{{"", abs}, {"task_1", abs}, {"prj_1/../x", abs}, {"prj_1", "relative"}, {"prj_" + strings.Repeat("x", 130), abs}} {
		if _, err := l.BindDeviceRepository(bg, c[0], c[1]); err == nil {
			t.Errorf("accepted %q %q", c[0], c[1])
		}
	}
}

func TestSetupNeedsTheInstalledProgram(t *testing.T) {
	w := newWorld(t)
	l := w.launcher(Options{})
	var out bytes.Buffer
	l.opt.RunInput = func(context.Context, []string, string, []string, io.Reader, io.Writer) error {
		out.WriteString("ran")
		return nil
	}
	if _, err := l.UseLocalController(bg); err != nil {
		t.Fatal(err)
	}
	if job := waitJob(t, l); job.Phase != "failed" || !strings.Contains(job.Error, "Open Werkbord") && !strings.Contains(job.Error, "open Werkbord") || out.Len() != 0 {
		t.Fatalf("%+v %q", job, out.String())
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
