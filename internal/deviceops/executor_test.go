package deviceops

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"werkbord/internal/clientaccess"
	"werkbord/internal/devicewire"
	"werkbord/internal/domain"
	"werkbord/internal/gitrepo"
)

func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", root}, args...)...)
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %s %v", args, b, e)
	}
	return strings.TrimSpace(string(b))
}
func fixture(t *testing.T) (*Executor, string) {
	t.Helper()
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	git(t, root, "init", "-q", "-b", "main")
	git(t, root, "config", "user.name", "Test")
	git(t, root, "config", "user.email", "test@example.invalid")
	write(t, root, "a.txt", "initial\n")
	write(t, root, "b.txt", "initial\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-qm", "initial")
	e := &Executor{Dir: t.TempDir(), RunnerID: "rnr_test", Git: &gitrepo.CLI{}, Resolve: func(context.Context, devicewire.Operation) (string, error) { return root, nil }}
	return e, root
}
func write(t *testing.T, root, path, data string) {
	t.Helper()
	if e := os.WriteFile(filepath.Join(root, path), []byte(data), 0600); e != nil {
		t.Fatal(e)
	}
}
func operation(kind string) devicewire.Operation {
	return devicewire.Operation{ID: domain.NewID("op"), Actor: "client_a", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute), Request: devicewire.Request{RequestID: domain.NewID("request"), RunnerID: "rnr_test", ProjectID: "prj_test", Kind: kind}}
}
func prepare(t *testing.T, e *Executor, action string, files ...string) *devicewire.GitSnapshot {
	t.Helper()
	op := operation("git.prepare")
	op.Request.Action = action
	op.Request.Files = files
	op.Request.Message = "reviewed"
	out := e.Run(context.Background(), op)
	if out.State != "done" || out.Git == nil {
		t.Fatalf("prepare %+v", out)
	}
	return out.Git
}
func TestConfirmedCommitPreservesStagingAndCannotReplay(t *testing.T) {
	e, root := fixture(t)
	write(t, root, "a.txt", "selected\n")
	write(t, root, "b.txt", "unrelated staged\n")
	git(t, root, "add", "b.txt")
	write(t, root, "new.txt", "new selected\n")
	// A repository-controlled hook must never run.
	write(t, root, ".git/hooks/pre-commit", "#!/bin/sh\ntouch hook-ran\nexit 1\n")
	os.Chmod(filepath.Join(root, ".git/hooks/pre-commit"), 0700)
	p := prepare(t, e, "commit", "a.txt", "new.txt")
	op := operation("git.commit")
	op.Request.PreviewID = p.PreviewID
	op.Request.Confirmed = true
	out := e.Run(context.Background(), op)
	if out.State != "done" {
		t.Fatalf("commit %+v", out)
	}
	head := git(t, root, "rev-parse", "HEAD")
	if got := git(t, root, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD"); got != "a.txt\nnew.txt" {
		t.Fatal(got)
	}
	if got := git(t, root, "diff", "--cached", "--name-only"); got != "b.txt" {
		t.Fatalf("unrelated staging lost: %s", got)
	}
	if _, err := os.Stat(filepath.Join(root, "hook-ran")); !os.IsNotExist(err) {
		t.Fatal("hook executed")
	}
	// Reconstructed executor simulates restart after a lost acknowledgement.
	again := &Executor{Dir: e.Dir, RunnerID: e.RunnerID, Git: e.Git, Resolve: e.Resolve}
	r := again.Run(context.Background(), op)
	if r.State != "done" || git(t, root, "rev-parse", "HEAD") != head {
		t.Fatalf("replay %+v", r)
	}
	op.ID = domain.NewID("op")
	if r := again.Run(context.Background(), op); r.State != "failed" {
		t.Fatalf("spent preview accepted %+v", r)
	}
}
func TestPreviewBindsBytesActorAndTarget(t *testing.T) {
	for _, mode := range []string{"bytes", "actor", "project", "run", "expiry", "revoked", "busy"} {
		t.Run(mode, func(t *testing.T) {
			e, root := fixture(t)
			write(t, root, "a.txt", "reviewed\n")
			p := prepare(t, e, "commit", "a.txt")
			op := operation("git.commit")
			op.Request.PreviewID = p.PreviewID
			op.Request.Confirmed = true
			switch mode {
			case "bytes":
				write(t, root, "a.txt", "different same status\n")
			case "actor":
				op.Actor = "client_b"
			case "project":
				op.Request.ProjectID = "prj_other"
			case "run":
				op.Request.RunID = "run_other"
			case "expiry":
				e.Now = func() time.Time { return time.Now().Add(6 * time.Minute) }
			case "revoked":
				e.Authorize = func(context.Context, devicewire.Operation) error { return domain.ErrForbidden }
			case "busy":
				e.Busy = func(context.Context, string) bool { return true }
			}
			before := git(t, root, "rev-parse", "HEAD")
			r := e.Run(context.Background(), op)
			if r.State != "failed" || git(t, root, "rev-parse", "HEAD") != before {
				t.Fatalf("unsafe command %+v", r)
			}
		})
	}
}
func TestInterruptedReceiptNeverReexecutes(t *testing.T) {
	e, root := fixture(t)
	write(t, root, "a.txt", "change\n")
	p := prepare(t, e, "commit", "a.txt")
	op := operation("git.commit")
	op.Request.PreviewID = p.PreviewID
	op.Request.Confirmed = true
	path := filepath.Join(e.Dir, "operations", op.ID+".json")
	if err := clientaccess.WriteJSON(path, receipt{digest(op), devicewire.Result{ID: op.ID, State: "executing"}}); err != nil {
		t.Fatal(err)
	}
	before := git(t, root, "rev-parse", "HEAD")
	r := e.Run(context.Background(), op)
	if r.State != "uncertain" || git(t, root, "rev-parse", "HEAD") != before {
		t.Fatalf("reexecuted %+v", r)
	}
	op.Request.Message = "changed request"
	r = e.Run(context.Background(), op)
	if r.State != "uncertain" {
		t.Fatal(r)
	}
	raw, _ := os.ReadFile(path)
	var receipt map[string]any
	if json.Unmarshal(raw, &receipt) != nil {
		t.Fatal("receipt corrupted")
	}
}
func TestFilteredFileAndEscapingPathRefused(t *testing.T) {
	e, root := fixture(t)
	write(t, root, "a.txt", "change\n")
	write(t, root, ".gitattributes", "a.txt filter=custom\n")
	git(t, root, "config", "filter.custom.clean", "touch filter-ran")
	p := prepare(t, e, "commit", "a.txt")
	op := operation("git.commit")
	op.Request.PreviewID = p.PreviewID
	op.Request.Confirmed = true
	if r := e.Run(context.Background(), op); r.State != "failed" {
		t.Fatal(r)
	}
	if _, err := os.Stat(filepath.Join(root, "filter-ran")); !os.IsNotExist(err) {
		t.Fatal("filter executed")
	}
	if _, err := e.Git.WorkingFingerprint(context.Background(), root, []string{"../escape"}); err == nil {
		t.Fatal("escaping path accepted")
	}
}
func TestConfirmedPushUsesOwningRepository(t *testing.T) {
	e, root := fixture(t)
	remote := t.TempDir()
	git(t, remote, "init", "--bare", "-q")
	git(t, root, "remote", "add", "origin", remote)
	op := operation("git.prepare")
	op.RemoteURL = remote
	op.Request.Action = "push"
	p := e.Run(context.Background(), op)
	if p.State != "done" {
		t.Fatal(p)
	}
	push := operation("git.push")
	push.RemoteURL = remote
	push.Request.Confirmed = true
	push.Request.PreviewID = p.Git.PreviewID
	r := e.Run(context.Background(), push)
	if r.State != "done" || r.Outcome != domain.OutcomeDone {
		t.Fatalf("push %+v", r)
	}
	if git(t, remote, "rev-parse", "refs/heads/main") != git(t, root, "rev-parse", "HEAD") {
		t.Fatal("wrong commit pushed")
	}
}
