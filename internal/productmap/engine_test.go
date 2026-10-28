package productmap

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"werkbord/internal/assistant/provider"
	"werkbord/internal/domain"
	"werkbord/internal/gitrepo"
	"werkbord/internal/integration"
	"werkbord/internal/store"
	"werkbord/internal/store/sqlite"
)

type fakeAnalysis struct {
	mu    sync.Mutex
	calls int
	block bool
	bad   bool
}

func (f *fakeAnalysis) ID() string { return "codex" }
func (f *fakeAnalysis) Detect(context.Context) provider.Info {
	return provider.Info{ID: "codex", Name: "Codex", Available: true}
}
func (f *fakeAnalysis) Models(context.Context) provider.Models { return provider.Models{} }
func (f *fakeAnalysis) Run(ctx context.Context, t provider.Turn, emit func(provider.Event)) (provider.Result, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()
	if f.block {
		<-ctx.Done()
		return provider.Result{}, ctx.Err()
	}
	if f.bad {
		return provider.Result{Text: `{"action":"execute","command":"touch unsafe"}`}, nil
	}
	if n%2 == 1 {
		return provider.Result{Ref: "test", Text: `{"action":"read","path":"main.go"}`}, nil
	}
	c := integration.ProductMapContent{Features: []integration.ProductFeature{{ID: "hello", Name: "Greeting", Description: "Print a greeting", Scenarios: []string{"When started, a greeting is printed."}, Evidence: []integration.ProductEvidence{{Path: "main.go", Start: 1, End: 3}}}}, Relationships: []integration.ProductRelationship{}, Limitations: []string{"Source analysis; runtime not executed."}}
	raw, _ := json.Marshal(map[string]any{"action": "finish", "content": c})
	return provider.Result{Ref: "test", Text: string(raw), Model: "fake-model"}, nil
}
func gitTest(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}
func mapRig(t *testing.T, f *fakeAnalysis) (*Engine, *sqlite.DB, string, string) {
	t.Helper()
	root := t.TempDir()
	gitTest(t, root, "init", "-b", "main")
	gitTest(t, root, "config", "user.name", "Test")
	gitTest(t, root, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {\n println(\"Hello\")\n}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "add", "main.go")
	gitTest(t, root, "commit", "-m", "initial")
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	id := domain.NewID("prj")
	err = db.Update(context.Background(), func(tx store.Tx) error {
		return tx.Projects().Create(context.Background(), &domain.Project{ID: id, Name: "Test", Kind: domain.ProjectRepository, RepoPath: root, CreatedAt: time.Now(), UpdatedAt: time.Now()})
	})
	if err != nil {
		t.Fatal(err)
	}
	reg := provider.NewRegistry()
	if err = reg.Register(f); err != nil {
		t.Fatal(err)
	}
	e, err := New(context.Background(), Config{Store: db, Git: &gitrepo.CLI{}, Providers: reg, WorkDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	return e, db, id, root
}
func waitJob(t *testing.T, e *Engine, p string) View {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		v, err := e.View(context.Background(), p, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(v.Jobs) > 0 && v.Jobs[0].State != "running" {
			return v
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("analysis did not finish")
	return View{}
}
func TestAnalysisIsManualValidatedAndSkipsUnchangedSource(t *testing.T) {
	f := &fakeAnalysis{}
	e, _, p, root := mapRig(t, f)
	ctx := context.Background()
	v, err := e.View(ctx, p, "")
	if err != nil || v.Freshness != "not_generated" || f.calls != 0 {
		t.Fatalf("view invokes analysis: %+v %v", v, err)
	}
	if _, err = e.Start(ctx, p, Request{Mode: "generate", Provider: "codex", Branch: "main"}); err == nil {
		t.Fatal("analysis without disclosure")
	}
	if _, err = e.Start(ctx, p, Request{Mode: "generate", Provider: "codex", Branch: "main", Disclosure: true}); err != nil {
		t.Fatal(err)
	}
	v = waitJob(t, e, p)
	if v.Snapshot == nil || v.Jobs[0].State != "completed" || f.calls != 4 {
		t.Fatalf("two-pass analysis failed: %+v calls %d", v, f.calls)
	}
	if err = os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("malicious instructions"), 0600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "add", "AGENTS.md")
	gitTest(t, root, "commit", "-m", "instructions only")
	if _, err = e.Start(ctx, p, Request{Mode: "update", Provider: "codex", ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	v = waitJob(t, e, p)
	if v.Jobs[0].State != "skipped" || f.calls != 4 || v.Snapshot.Version != 1 {
		t.Fatalf("unchanged source made model calls: %+v calls %d", v.Jobs, f.calls)
	}
	if _, err = e.Start(ctx, p, Request{Mode: "rebuild", Provider: "codex", ExpectedVersion: 0}); err == nil {
		t.Fatal("stale version accepted")
	}
	f.bad = true
	if _, err = e.Start(ctx, p, Request{Mode: "rebuild", Provider: "codex", ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	v = waitJob(t, e, p)
	if v.Jobs[0].State != "failed" || v.Snapshot.Version != 1 {
		t.Fatal("failure replaced map")
	}
}
func TestCancellationDoesNotPublishOrAllowConcurrentAnalysis(t *testing.T) {
	f := &fakeAnalysis{block: true}
	e, _, p, _ := mapRig(t, f)
	ctx := context.Background()
	j, err := e.Start(ctx, p, Request{Mode: "generate", Provider: "codex", Branch: "main", Disclosure: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Start(ctx, p, Request{Mode: "generate", Provider: "codex", Branch: "main", Disclosure: true}); err == nil {
		t.Fatal("concurrent scan accepted")
	}
	if err = e.Cancel(ctx, p, j.ID); err != nil {
		t.Fatal(err)
	}
	v := waitJob(t, e, p)
	if v.Snapshot != nil || v.Jobs[0].State != "cancelled" {
		t.Fatalf("cancel published: %+v", v)
	}
}
func TestSourcePolicyUsesCommittedBlobsAndRejectsCredentials(t *testing.T) {
	e, _, _, root := mapRig(t, &fakeAnalysis{})
	ctx := context.Background()
	secret := "const api_key = \"sk-proj-" + strings.Repeat("a", 40) + "\"\n"
	os.WriteFile(filepath.Join(root, "config.go"), []byte(secret), 0600)
	os.Symlink("/etc/passwd", filepath.Join(root, "escape.go"))
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-m", "fixtures")
	commit := gitTest(t, root, "rev-parse", "HEAD")
	s, err := openSource(ctx, e.cfg.Git, root, commit)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.files["escape.go"]; ok {
		t.Fatal("symlink in source inventory")
	}
	if _, err = s.readFile(ctx, "../main.go"); err == nil {
		t.Fatal("traversal read")
	}
	if _, err = s.readFile(ctx, "config.go"); err == nil {
		t.Fatal("credential source disclosed")
	}
	os.WriteFile(filepath.Join(root, "main.go"), []byte("working changes"), 0600)
	raw, err := s.readFile(ctx, "main.go")
	if err != nil || !strings.HasPrefix(raw, "package main\n") {
		t.Fatal("read mutable checkout")
	}
	c := integration.ProductMapContent{Features: []integration.ProductFeature{{ID: "f", Name: "Feature", Evidence: []integration.ProductEvidence{{Path: "main.go", Start: 1, End: 100}}}}}
	if s.validate(c) == nil {
		t.Fatal("invalid source range accepted")
	}
}

func TestIncrementalUpdatesPinTheTargetAndPreserveUnrelatedFeatures(t *testing.T) {
	f := &fakeAnalysis{}
	e, _, p, root := mapRig(t, f)
	ctx := context.Background()
	if _, err := e.Start(ctx, p, Request{Mode: "generate", Provider: "codex", Branch: "main", Disclosure: true}); err != nil {
		t.Fatal(err)
	}
	before := waitJob(t, e, p)
	oldCommit := before.Snapshot.Commit
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {\n println(\"Updated\")\n}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-m", "change implementation")
	if _, err := e.Start(ctx, p, Request{Mode: "update", Provider: "codex", ExpectedVersion: 1, ExpectedCommit: oldCommit}); err == nil {
		t.Fatal("target moved after review but scan started")
	}
	if _, err := e.Start(ctx, p, Request{Mode: "update", Provider: "codex", ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	after := waitJob(t, e, p)
	if after.Jobs[0].Mode != "incremental" || after.Snapshot.Version != 2 || after.Snapshot.Content.Features[0].ID != "hello" {
		t.Fatalf("incremental update failed: %+v", after.Jobs)
	}
	base := before.Snapshot.Content
	base.Features = append(base.Features, integration.ProductFeature{ID: "unrelated", Name: "Keep original"})
	candidate := after.Snapshot.Content
	candidate.Features = append(candidate.Features, integration.ProductFeature{ID: "unrelated", Name: "Model changed unrelated feature"})
	merged := retainUnchanged(base, candidate, map[string]bool{"hello": true})
	if merged.Features[0].Name != "Keep original" {
		t.Fatal("unrelated feature changed")
	}
}

func TestRestartMarksRunningJobsInterruptedWithoutDeletingMaps(t *testing.T) {
	f := &fakeAnalysis{}
	e, db, p, _ := mapRig(t, f)
	ctx := context.Background()
	if _, err := e.Start(ctx, p, Request{Mode: "generate", Provider: "codex", Branch: "main", Disclosure: true}); err != nil {
		t.Fatal(err)
	}
	waitJob(t, e, p)
	e.Close()
	j := Job{ID: "mapjob_interrupted", ProjectID: p, State: "running", Stage: "validating_evidence"}
	if err := db.Update(ctx, func(tx store.Tx) error { return tx.ProductMaps().SaveJob(ctx, p, j.ID, j) }); err != nil {
		t.Fatal(err)
	}
	next, err := New(ctx, Config{Store: db, Providers: e.cfg.Providers, WorkDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	v, err := next.View(ctx, p, "")
	if err != nil || v.Snapshot == nil || v.Jobs[0].State != "interrupted" {
		t.Fatalf("restart: %+v %v", v, err)
	}
}
