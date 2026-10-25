//go:build darwin || linux

package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devboard/internal/agent"
	"devboard/internal/agent/fake"
	"devboard/internal/domain"
	"devboard/internal/gitrepo"
	"devboard/internal/service"
	"devboard/internal/store"
	"devboard/internal/store/sqlite"
	"golang.org/x/sys/unix"
)

func fixture(t *testing.T) (*Manager, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git: %v %s", err, out)
	}
	db, err := sqlite.Open(ctx, filepath.Join(dir, "db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	deps := service.Deps{Store: db}
	projects := &service.Projects{Deps: deps, Git: &gitrepo.CLI{}}
	project, err := projects.Register(ctx, repo, "Native repo")
	if err != nil {
		t.Fatal(err)
	}
	settings := &service.Settings{Deps: deps}
	_, err = settings.SetSelection(ctx, domain.SelectionSettings{Agents: map[string]domain.AgentSelectionPreference{"codex": {DirectCLI: true, Billing: "auto"}, "claude-code": {DirectCLI: true, Billing: "auto"}}})
	if err != nil {
		t.Fatal(err)
	}
	local, e := settings.RegisterRunner(ctx)
	if e != nil {
		t.Fatal(e)
	}
	registry := agent.NewRegistry()
	for _, id := range []string{"codex", "claude-code"} {
		if err := registry.Register(&fake.Adapter{Name: id, Info: domain.Agent{ID: id, Installed: true, Available: true, SignIn: domain.SignedIn}}); err != nil {
			t.Fatal(err)
		}
	}
	cli := filepath.Join(dir, "native-cli")
	// A real PTY fixture requires all three descriptors to be terminals; no pipe can pass.
	script := "#!/bin/sh\n[ -t 0 ] && [ -t 1 ] && [ -t 2 ] || exit 17\nprintf '\\033[32mREADY\\033[0m\\n'\nwhile IFS= read -r line; do\n case \"$line\" in exit) exit 0;; size) stty size;; *) printf 'REPLY:%s\\n' \"$line\";; esac\ndone\n"
	if err = os.WriteFile(cli, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	m, err := New(Options{Projects: projects, Settings: settings, Agents: registry, Commands: map[string]string{"codex": cli, "claude-code": cli}, Device: local.ID, StatePath: filepath.Join(dir, "terminals.json")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m, project.ID
}
func create(t *testing.T, m *Manager, project, id string) Metadata {
	t.Helper()
	s, e := m.Create(context.Background(), project, id, m.Device(), 80, 24)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func collect(t *testing.T, m *Manager, s Metadata, after uint64, want string) (string, uint64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	var b strings.Builder
	for {
		out, e := m.Output(ctx, s.ProjectID, s.ID, after)
		if e != nil {
			t.Fatalf("waiting for %q: %v; output %q", want, e, b.String())
		}
		after = out.Next
		b.Write(out.Data)
		if strings.Contains(b.String(), want) {
			return b.String(), after
		}
	}
}
func TestNativePTYIsolationReconnectionAndLifecycle(t *testing.T) {
	m, p := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	a, e := m.Create(ctx, p, "claude-code", m.Device(), 80, 24)
	if e != nil {
		t.Fatal(e)
	}
	cancel()
	b := create(t, m, p, "codex")
	first, offset := collect(t, m, a, 0, "READY")
	if !strings.Contains(first, "\x1b[32m") {
		t.Fatal("ANSI was lost")
	}
	_, otherOffset := collect(t, m, b, 0, "READY")
	if e = m.Input(p, a.ID, []byte("alpha\n")); e != nil {
		t.Fatal(e)
	}
	_, offset = collect(t, m, a, offset, "REPLY:alpha")
	pollCtx, detach := context.WithCancel(context.Background())
	detach()
	if _, err := m.Output(pollCtx, p, a.ID, offset); !errors.Is(err, context.Canceled) {
		t.Fatalf("detach: %v", err)
	}
	if e = m.Input(p, b.ID, []byte("beta\n")); e != nil {
		t.Fatal(e)
	}
	out, _ := collect(t, m, b, otherOffset, "REPLY:beta")
	if strings.Contains(out, "alpha") {
		t.Fatal("cross-session output")
	}
	// Reattachment from offset zero replays raw output; disconnect never stopped the process.
	replay, e := m.Output(context.Background(), p, a.ID, 0)
	if e != nil || replay.Metadata.State != "running" || !strings.Contains(string(replay.Data), "REPLY:alpha") {
		t.Fatalf("replay: %+v %v", replay, e)
	}
	if e = m.Resize(p, a.ID, 111, 37); e != nil {
		t.Fatal(e)
	}
	if e = m.Input(p, a.ID, []byte("size\n")); e != nil {
		t.Fatal(e)
	}
	_, offset = collect(t, m, a, offset, "37 111")
	if e = m.Input("other-project", a.ID, []byte("injection\n")); !errors.Is(e, domain.ErrNotFound) {
		t.Fatalf("cross project input: %v", e)
	}
	if _, e = m.Output(context.Background(), "other-project", a.ID, 0); !errors.Is(e, domain.ErrNotFound) {
		t.Fatalf("cross project attach: %v", e)
	}
	if e = m.Stop(p, a.ID); e != nil {
		t.Fatal(e)
	}
	if e = m.Input(p, a.ID, []byte("late\n")); !errors.Is(e, domain.ErrConflict) {
		t.Fatalf("input after stop: %v", e)
	}
	if e = m.Input(p, b.ID, []byte("exit\n")); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		ended, _ := m.Output(context.Background(), p, b.ID, 0)
		if ended.Metadata.State == "exited" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("natural exit not recorded")
}
func TestTerminalValidationAndLimits(t *testing.T) {
	m, p := fixture(t)
	for _, in := range []struct {
		project, agent, device string
		c, r                   int
	}{{p, "amp-acp", m.Device(), 80, 24}, {p, "/bin/sh", m.Device(), 80, 24}, {p, "codex", "remote", 80, 24}, {"missing", "codex", m.Device(), 80, 24}, {p, "codex", m.Device(), 0, 24}} {
		if _, e := m.Create(context.Background(), in.project, in.agent, in.device, in.c, in.r); e == nil {
			t.Fatalf("accepted %+v", in)
		}
	}
	for i := 0; i < MaxSessions; i++ {
		create(t, m, p, "codex")
	}
	if _, e := m.Create(context.Background(), p, "codex", m.Device(), 80, 24); !errors.Is(e, domain.ErrConflict) {
		t.Fatalf("limit: %v", e)
	}
	s := m.List()[0]
	if e := m.Input(p, s.ID, make([]byte, MaxInput+1)); !errors.Is(e, domain.ErrInvalid) {
		t.Fatal(e)
	}
	if e := m.Resize(p, s.ID, 501, 24); !errors.Is(e, domain.ErrInvalid) {
		t.Fatal(e)
	}
}
func TestRestartMarksMissingSessionsInterrupted(t *testing.T) {
	m, p := fixture(t)
	s := create(t, m, p, "codex")
	// Snapshot persisted ownership, then stop service. Restore snapshot to model an interrupted device.
	b, e := os.ReadFile(m.opt.StatePath)
	if e != nil {
		t.Fatal(e)
	}
	m.Close()
	if e = os.WriteFile(m.opt.StatePath, b, 0600); e != nil {
		t.Fatal(e)
	}
	next, e := New(m.opt)
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	out, e := next.Output(context.Background(), p, s.ID, 0)
	if e != nil || out.Metadata.State != "interrupted" {
		t.Fatalf("restart: %+v %v", out, e)
	}
	if e = next.Input(p, s.ID, []byte("must not relaunch")); !errors.Is(e, domain.ErrConflict) {
		t.Fatal(e)
	}
}
func TestOutputOverflowSignalsReset(t *testing.T) {
	m, p := fixture(t)
	s := create(t, m, p, "codex")
	collect(t, m, s, 0, "READY")
	m.mu.Lock()
	live := m.sessions[s.ID]
	live.base = 100
	live.buffer = []byte("new output")
	m.mu.Unlock()
	out, e := m.Output(context.Background(), p, s.ID, 0)
	if e != nil || !out.Reset || out.Next != 110 || string(out.Data) != "new output" {
		t.Fatalf("overflow: %+v %v", out, e)
	}
}

func TestTerminalLaunchRequiresCurrentDevicePreferenceAndSignIn(t *testing.T) {
	m, p := fixture(t)
	ctx := context.Background()
	adapter, err := m.opt.Agents.Get("codex")
	if err != nil {
		t.Fatal(err)
	}
	detected := adapter.(*fake.Adapter)
	for _, info := range []domain.Agent{
		{ID: "codex", Installed: true, Available: true, SignIn: domain.SignedOut},
		{ID: "codex", Installed: true, Available: true, SignIn: domain.SignInUnknown},
		{ID: "codex", Installed: false, Available: true, SignIn: domain.SignedIn},
		{ID: "codex", Installed: true, Available: false, SignIn: domain.SignedIn},
	} {
		detected.Info = info
		if _, e := m.Create(ctx, p, "codex", m.Device(), 80, 24); !errors.Is(e, domain.ErrForbidden) {
			t.Fatalf("accepted %+v: %v", info, e)
		}
	}
	detected.Info = domain.Agent{ID: "codex", Installed: true, Available: true, SignIn: domain.SignedIn}
	prefs, e := m.opt.Settings.Selection(ctx)
	if e != nil {
		t.Fatal(e)
	}
	pref := prefs.Agents["codex"]
	pref.DirectCLI = false
	prefs.Agents["codex"] = pref
	if _, e = m.opt.Settings.SetSelection(ctx, prefs); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Create(ctx, p, "codex", m.Device(), 80, 24); !errors.Is(e, domain.ErrForbidden) {
		t.Fatalf("accepted disabled direct CLI: %v", e)
	}
	pref.DirectCLI = true
	prefs.Agents["codex"] = pref
	if _, e = m.opt.Settings.SetSelection(ctx, prefs); e != nil {
		t.Fatal(e)
	}
	if e = m.opt.Settings.Store.Update(ctx, func(tx store.Tx) error {
		r, e := tx.Runners().Get(ctx, m.Device())
		if e != nil {
			return e
		}
		r.Disabled = true
		return tx.Runners().Save(ctx, r)
	}); e != nil {
		t.Fatal(e)
	}
	if m.AuthorizedDevice(ctx) {
		t.Fatal("disabled local device advertised")
	}
	if _, e = m.Create(ctx, p, "codex", m.Device(), 80, 24); !errors.Is(e, domain.ErrForbidden) {
		t.Fatalf("accepted disabled device: %v", e)
	}
}
func TestTerminalRejectsRedirectedRepository(t *testing.T) {
	m, p := fixture(t)
	project, e := m.opt.Projects.Get(context.Background(), p)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Rename(project.RepoPath, project.RepoPath+"-original"); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(t.TempDir(), project.RepoPath); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Create(context.Background(), p, "codex", m.Device(), 80, 24); !errors.Is(e, domain.ErrForbidden) {
		t.Fatalf("accepted redirected path: %v", e)
	}
}

func TestPTYDescriptorCannotBeInheritedByAnotherSession(t *testing.T) {
	m, p := fixture(t)
	s := create(t, m, p, "codex")
	conn, e := m.sessions[s.ID].p.terminal.SyscallConn()
	if e != nil {
		t.Fatal(e)
	}
	flags := 0
	if e = conn.Control(func(fd uintptr) { flags, e = unix.FcntlInt(fd, unix.F_GETFD, 0) }); e != nil {
		t.Fatal(e)
	}
	if flags&unix.FD_CLOEXEC == 0 {
		t.Fatal("PTY master is inheritable by another process")
	}
}

func TestPhoneGrantPersistenceAssociationAndInputReceipts(t *testing.T) {
	m, p := fixture(t)
	s := create(t, m, p, "codex")
	ctx := context.Background()
	_, offset := collect(t, m, s, 0, "READY")
	probe, err := m.OutputNow(ctx, p, s.ID, offset)
	if err != nil || len(probe.Data) != 0 || probe.Next != offset || probe.Metadata.State != "running" {
		t.Fatalf("idle reconnect probe: %+v %v", probe, err)
	}
	if err := m.Access(ctx, p, s.ID, "phone-a"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("default access: %v", err)
	}
	if err := m.Grant(p, s.ID, "phone-a", true); err != nil {
		t.Fatal(err)
	}
	if err := m.Access(ctx, p, s.ID, "phone-a"); err != nil {
		t.Fatal(err)
	}
	if err := m.Access(ctx, p, s.ID, "phone-b"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("other phone: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := m.InputOnce(p, s.ID, "phone-client-1234", 1, []byte("once\n"), offset); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.InputOnce(p, s.ID, "phone-client-1234", 2, []byte("sentinel\n"), offset); err != nil {
		t.Fatal(err)
	}
	replay, err := m.Output(ctx, p, s.ID, 0)
	if err != nil || !replay.Replay || replay.Next != offset {
		t.Fatalf("acknowledged history: %+v %v", replay, err)
	}
	text, _ := collect(t, m, s, offset, "REPLY:sentinel")
	if strings.Count(text, "REPLY:once") != 1 {
		t.Fatalf("duplicate PTY write: %q", text)
	}
	if err := m.InputOnce(p, s.ID, "phone-client-1234", 1, []byte("once\n"), offset); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale receipt: %v", err)
	}
	// Persisted grants survive reopening metadata after the first manager has shut down.
	if err := m.Stop(p, s.ID); err != nil {
		t.Fatal(err)
	}
	m.Close()
	reopened, err := New(m.opt)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Access(ctx, p, s.ID, "phone-a"); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Grant(p, s.ID, "phone-a", false); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Access(ctx, p, s.ID, "phone-a"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("grant removal: %v", err)
	}
	if err := reopened.Grant(p, s.ID, "phone-a", true); err != nil {
		t.Fatal(err)
	}
	for _, disabled := range []bool{true, false} {
		if err := m.opt.Settings.Store.Update(ctx, func(tx store.Tx) error {
			r, e := tx.Runners().Get(ctx, m.Device())
			if e != nil {
				return e
			}
			r.Disabled = disabled
			return tx.Runners().Save(ctx, r)
		}); err != nil {
			t.Fatal(err)
		}
		err := reopened.Access(ctx, p, s.ID, "phone-a")
		if disabled && !errors.Is(err, domain.ErrForbidden) || !disabled && err != nil {
			t.Fatalf("runner disabled=%v: %v", disabled, err)
		}
	}
	// A repository that disappears or changes identity no longer authorises even its previous session.
	project, err := m.opt.Projects.Get(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(project.RepoPath, ".git"), filepath.Join(project.RepoPath, "old-git")); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Access(ctx, p, s.ID, "phone-a"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("repository identity lost: %v", err)
	}
}

// Many short-lived browser tabs must not lock a long-lived terminal out of input, and a duplicate must still not be written.
func TestInputReceiptsAreBoundedWithoutLockingAndNeverEvictedWhileFresh(t *testing.T) {
	clock := time.Now()
	m, p := fixture(t)
	m.now = func() time.Time { return clock }
	s := create(t, m, p, "codex")
	_, offset := collect(t, m, s, 0, "READY")
	client := func(i int) string { return fmt.Sprintf("tab-%02d-xxxxxxxxxxxxxxxx", i) }
	// The eight-entry queue drains at the terminal's pace; a full queue is the only conflict worth waiting out here.
	deliver := func(c string, seq uint64, data string) error {
		var err error
		for i := 0; i < 300; i++ {
			if err = m.InputOnce(p, s.ID, c, seq, []byte(data), offset); err == nil || !errors.Is(err, domain.ErrConflict) {
				return err
			}
			time.Sleep(10 * time.Millisecond)
		}
		return err
	}
	for i := 0; i < MaxInputClients; i++ {
		if err := deliver(client(i), 1, "x\n"); err != nil {
			t.Fatalf("client %d: %v", i, err)
		}
	}
	// Every receipt is fresh: a new client is refused rather than any receipt given up.
	if err := m.InputOnce(p, s.ID, client(MaxInputClients), 1, []byte("x\n"), offset); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("fresh receipts were not retained: %v", err)
	}
	if err := m.InputOnce(p, s.ID, client(0), 1, []byte("x\n"), offset); err != nil {
		t.Fatalf("duplicate of a retained receipt: %v", err)
	}
	// After the idle period a new client takes the longest-idle slot; the others keep theirs.
	clock = clock.Add(ReceiptIdle + time.Minute)
	if err := deliver(client(0), 2, "y\n"); err != nil {
		t.Fatalf("client 0 continues: %v", err)
	}
	if err := deliver(client(MaxInputClients), 1, "z\n"); err != nil {
		t.Fatalf("new client after idle period: %v", err)
	}
	m.mu.Lock()
	kept := len(m.sessions[s.ID].receipts)
	m.mu.Unlock()
	if kept != MaxInputClients {
		t.Fatalf("receipts = %d, want %d", kept, MaxInputClients)
	}
	// A refused request gives up nothing: the queue being full must not cost a receipt.
	clock = clock.Add(ReceiptIdle + time.Minute)
	m.mu.Lock()
	live := m.sessions[s.ID]
	live.State = "stopped"
	m.mu.Unlock()
	if err := m.InputOnce(p, s.ID, client(MaxInputClients+1), 1, []byte("q\n"), offset); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stopped session accepted input: %v", err)
	}
	m.mu.Lock()
	kept = len(live.receipts)
	live.State = "running" // so that Close still ends the process
	m.mu.Unlock()
	if kept != MaxInputClients {
		t.Fatalf("a refused request evicted a receipt: %d", kept)
	}
}

// A file cut short by a crash must not stop the controller; it is set aside and the service starts with no terminals.
func TestDamagedStateIsSetAsideInsteadOfStoppingTheController(t *testing.T) {
	m, p := fixture(t)
	create(t, m, p, "codex")
	m.Close()
	for name, damaged := range map[string][]byte{"empty": {}, "cut short": []byte(`[{"id":"abc","state":"run`), "wrong shape": []byte(`{"id":1}`)} {
		if err := os.WriteFile(m.opt.StatePath, damaged, 0600); err != nil {
			t.Fatal(err)
		}
		next, err := New(m.opt)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := next.List(); len(got) != 0 {
			t.Fatalf("%s: sessions %v", name, got)
		}
		kept, err := os.ReadFile(m.opt.StatePath + ".damaged")
		if err != nil || string(kept) != string(damaged) {
			t.Fatalf("%s: damaged file not kept: %q %v", name, kept, err)
		}
		if info, err := os.Stat(m.opt.StatePath); err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("%s: state not rewritten privately: %v %v", name, info, err)
		}
		next.Close()
	}
}

// More records than the limit keep the newest; none of them is allowed to fail the start.
func TestOversizedStateKeepsNewestRecords(t *testing.T) {
	m, _ := fixture(t)
	m.Close()
	now := time.Now().UTC()
	records := []record{}
	for i := 0; i < MaxRecords+6; i++ {
		records = append(records, record{Metadata: Metadata{ID: fmt.Sprintf("id-%03d", i), ProjectID: "p", State: "exited", CreatedAt: now.Add(time.Duration(i) * time.Second)}})
	}
	b, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(m.opt.StatePath, b, 0600); err != nil {
		t.Fatal(err)
	}
	next, err := New(m.opt)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	got := next.List()
	if len(got) != MaxRecords {
		t.Fatalf("kept %d", len(got))
	}
	for _, s := range got {
		if s.ID < "id-006" {
			t.Fatalf("kept an older record %s", s.ID)
		}
	}
}
