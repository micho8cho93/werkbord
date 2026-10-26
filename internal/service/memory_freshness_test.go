package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"werkbord/internal/domain"
	"werkbord/internal/github"
	"werkbord/internal/gitrepo"
	"werkbord/internal/store"
	"werkbord/internal/store/sqlite"
)

func freshnessService(f *gcFixture) *MemoryFreshness {
	return &MemoryFreshness{Deps: f.deps, Control: f.gc}
}
func freshnessScope(f *gcFixture) domain.MemoryScope {
	return domain.MemoryScope{OwnerID: "local", ProjectID: f.project.ID, Repository: "github.com/acme/app"}
}
func gitMemory(t *testing.T, f *gcFixture, scope domain.MemoryScope, sha, branch, summary string, paths ...string) *domain.Memory {
	t.Helper()
	c := memoryContent(summary)
	c.State = domain.MemoryCurrent
	c.Source = domain.MemoryProvenance{Type: domain.MemorySourceGit, Reference: "git:" + sha, GitSHA: sha, Branch: branch, Paths: paths}
	return createMemory(t, memoryService(t, f.fixture, scope.OwnerID, domain.MemoryLimits{}), scope, c)
}
func reconcileMemory(t *testing.T, s *MemoryFreshness, scope domain.MemoryScope) bool {
	t.Helper()
	more, err := s.Reconcile(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	return more
}
func assertMemoryState(t *testing.T, f *gcFixture, m *domain.Memory, state domain.MemoryState, version int64) {
	t.Helper()
	got, err := memoryService(t, f.fixture, m.Scope.OwnerID, domain.MemoryLimits{}).Get(f.ctx, m.Scope, m.ID)
	if err != nil || got.State != state || got.Version != version {
		t.Fatalf("%s: got %+v, %v; want %s v%d", m.Summary, got, err, state, version)
	}
}
func memoryCheckpoint(t *testing.T, s *MemoryFreshness, scope domain.MemoryScope) *domain.MemoryFreshness {
	t.Helper()
	var out *domain.MemoryFreshness
	if err := s.Store.View(context.Background(), func(tx store.Tx) error {
		var err error
		out, err = tx.Memory().Freshness(context.Background(), scope)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMemoryFreshnessCodeChangesPathsAndReplay(t *testing.T) {
	f := newGC(t)
	s := freshnessService(f)
	scope := freshnessScope(f)
	base := commitIn(t, f.repo, "src/a.go", "old", "source")
	affected := gitMemory(t, f, scope, base, "refs/heads/main", "source fact", "src/a.go")
	unrelated := gitMemory(t, f, scope, base, "refs/heads/main", "unrelated fact", "docs")
	directory := gitMemory(t, f, scope, base, "refs/heads/main", "directory fact", "src")
	c := memoryContent("Keep the architectural decision")
	c.State = domain.MemoryCurrent
	c.Authority = domain.MemoryDecision
	c.Source = domain.MemoryProvenance{Type: domain.MemorySourceUser, Reference: "approval:1", ApprovedBy: "local", ApprovalRef: "decision:1", GitSHA: base, Paths: []string{"src"}}
	decision := createMemory(t, memoryService(t, f.fixture, "local", domain.MemoryLimits{}), scope, c)
	preferenceContent := memoryContent("Prefer concise explanations")
	preferenceContent.Authority, preferenceContent.State = domain.MemoryPreference, domain.MemoryCurrent
	preference := createMemory(t, memoryService(t, f.fixture, "local", domain.MemoryLimits{}), scope, preferenceContent)
	reconcileMemory(t, s, scope)
	assertMemoryState(t, f, affected, domain.MemoryCurrent, 1)
	// Uncommitted files never invalidate committed facts or count as verification.
	writeFile(t, f.repo, "src/a.go", "dirty")
	reconcileMemory(t, s, scope)
	assertMemoryState(t, f, affected, domain.MemoryCurrent, 1)
	head := commitIn(t, f.repo, "src/a.go", "new", "implementation changed")
	ev := newEvent(domain.EventGitPushed, domain.GitActionResult{})
	ev.ProjectID = f.project.ID
	if err := f.deps.update(f.ctx, func(tx store.Tx, em *emitter) error { return em.emit(ev) }); err != nil {
		t.Fatal(err)
	}
	reconcileMemory(t, s, scope)
	assertMemoryState(t, f, affected, domain.MemoryNeedsVerification, 2)
	assertMemoryState(t, f, directory, domain.MemoryNeedsVerification, 2)
	assertMemoryState(t, f, unrelated, domain.MemoryCurrent, 1)
	assertMemoryState(t, f, decision, domain.MemoryCurrent, 1)
	assertMemoryState(t, f, preference, domain.MemoryCurrent, 1)
	checkpoint := memoryCheckpoint(t, s, scope)
	if checkpoint.Revision.Head != head || checkpoint.EventSeq == 0 || checkpoint.SucceededAt == nil {
		t.Fatalf("checkpoint %+v", checkpoint)
	}
	// Rewind only the cursor to model replay after restoration. Same event and
	// source changes cannot create records or update an already stale fact twice.
	if err := s.Store.Update(f.ctx, func(tx store.Tx) error { checkpoint.EventSeq = 0; return tx.Memory().SaveFreshness(f.ctx, checkpoint) }); err != nil {
		t.Fatal(err)
	}
	reconcileMemory(t, s, scope)
	reconcileMemory(t, s, scope)
	assertMemoryState(t, f, affected, domain.MemoryNeedsVerification, 2)
	usage, err := memoryService(t, f.fixture, "local", domain.MemoryLimits{}).Usage(f.ctx)
	if err != nil || usage.Records != 5 {
		t.Fatal(usage, err)
	}
}

func TestMemoryFreshnessBranchesCheckoutRewriteAndDeletion(t *testing.T) {
	f := newGC(t)
	s := freshnessService(f)
	scope := freshnessScope(f)
	base := gitOut(t, f.repo, "rev-parse", "HEAD")
	git(t, f.repo, "branch", "feature")
	feature := gitMemory(t, f, scope, base, "refs/heads/feature", "feature remains stable", "README.md")
	main := gitMemory(t, f, scope, base, "refs/heads/main", "main moved", "README.md")
	floating := gitMemory(t, f, scope, base, "", "checkout follows HEAD", "README.md")
	reconcileMemory(t, s, scope)
	commitIn(t, f.repo, "README.md", "changed", "main change")
	reconcileMemory(t, s, scope)
	assertMemoryState(t, f, feature, domain.MemoryCurrent, 1)
	assertMemoryState(t, f, main, domain.MemoryNeedsVerification, 2)
	assertMemoryState(t, f, floating, domain.MemoryNeedsVerification, 2)
	// A force reset with an unchanged relevant file is still ambiguous history.
	stable := gitMemory(t, f, scope, base, "refs/heads/main", "reset conservatively invalidates", "untouched")
	reconcileMemory(t, s, scope)
	git(t, f.repo, "reset", "--hard", base)
	reconcileMemory(t, s, scope)
	assertMemoryState(t, f, stable, domain.MemoryNeedsVerification, 2)
	git(t, f.repo, "checkout", "-q", "feature")
	reconcileMemory(t, s, scope)
	cp := memoryCheckpoint(t, s, scope)
	if cp.Revision.Branch != "feature" {
		t.Fatal(cp.Revision)
	}
	git(t, f.repo, "checkout", "-q", "main")
	git(t, f.repo, "branch", "-D", "feature")
	reconcileMemory(t, s, scope)
	assertMemoryState(t, f, feature, domain.MemoryNeedsVerification, 2)
	if _, ok := memoryCheckpoint(t, s, scope).Revision.Refs["refs/heads/feature"]; ok {
		t.Fatal("deleted branch retained")
	}
}

func TestMemoryFreshnessMissedUpdatesReconcileAfterDatabaseRestart(t *testing.T) {
	f := newGC(t)
	path := filepath.Join(t.TempDir(), "memory.db")
	db, err := sqlite.Open(f.ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	deps := Deps{Store: db}
	projects := &Projects{Deps: deps, Git: f.git}
	p, err := projects.Register(f.ctx, f.repo, "restart")
	if err != nil {
		t.Fatal(err)
	}
	scope := domain.MemoryScope{OwnerID: "local", ProjectID: p.ID, Repository: "github.com/acme/app"}
	mem, err := NewMemory(deps, "local", domain.MemoryLimits{})
	if err != nil {
		t.Fatal(err)
	}
	base := gitOut(t, f.repo, "rev-parse", "HEAD")
	c := memoryContent("survives restart")
	c.State = domain.MemoryCurrent
	c.Source = domain.MemoryProvenance{Type: domain.MemorySourceGit, Reference: "git:source", GitSHA: base, Paths: []string{"README.md"}}
	m := createMemory(t, mem, scope, c)
	worker := &MemoryFreshness{Deps: deps, Control: &GitControl{Deps: deps, Git: f.git}}
	reconcileMemory(t, worker, scope)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// No Werkbord operation/event, while the database is closed.
	head := commitIn(t, f.repo, "README.md", "outside Werkbord", "terminal commit")
	db, err = sqlite.Open(f.ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	deps.Store = db
	worker = &MemoryFreshness{Deps: deps, Control: &GitControl{Deps: deps, Git: f.git}}
	reconcileMemory(t, worker, scope)
	mem, err = NewMemory(deps, "local", domain.MemoryLimits{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := mem.Get(f.ctx, scope, m.ID)
	if err != nil || got.State != domain.MemoryNeedsVerification || memoryCheckpoint(t, worker, scope).Revision.Head != head {
		t.Fatal(got, err)
	}
}

func TestMemoryFreshnessMissingEvidenceAndRemoteIdentity(t *testing.T) {
	f := newGC(t)
	s := freshnessService(f)
	scope := freshnessScope(f)
	base := gitOut(t, f.repo, "rev-parse", "HEAD")
	missing := gitMemory(t, f, scope, "1111111111111111111111111111111111111111", "refs/heads/main", "missing commit", "README.md")
	deleted := gitMemory(t, f, scope, base, "refs/heads/deleted", "missing branch", "elsewhere")
	unknown := memoryContent("unanchored repository claim")
	unknown.State = domain.MemoryCurrent
	unanchored := createMemory(t, memoryService(t, f.fixture, "local", domain.MemoryLimits{}), scope, unknown)
	reconcileMemory(t, s, scope)
	for _, m := range []*domain.Memory{missing, deleted, unanchored} {
		assertMemoryState(t, f, m, domain.MemoryNeedsVerification, 2)
	}
	valid := gitMemory(t, f, scope, base, "refs/heads/main", "remote identity changed", "README.md")
	old := memoryCheckpoint(t, s, scope)
	git(t, f.repo, "remote", "set-url", "origin", "https://github.com/other/repo.git")
	reconcileMemory(t, s, scope)
	assertMemoryState(t, f, valid, domain.MemoryNeedsVerification, 2)
	cp := memoryCheckpoint(t, s, scope)
	if cp.Problem == "" || cp.Revision.Head != old.Revision.Head || cp.EventSeq != old.EventSeq {
		t.Fatalf("failed read advanced success %+v", cp)
	}
}

type memoryLimitedGit struct {
	gitrepo.Control
	refs      int
	truncated bool
	fail      bool
}

func (g memoryLimitedGit) ListRefs(ctx context.Context, root string) ([]gitrepo.RefInfo, error) {
	if g.fail {
		return nil, errors.New("not accessible")
	}
	if g.refs > 0 {
		return make([]gitrepo.RefInfo, g.refs), nil
	}
	return g.Control.ListRefs(ctx, root)
}
func (g memoryLimitedGit) DiffFiles(ctx context.Context, root, from, to string, max int) ([]domain.GitDiffFile, bool, error) {
	if g.truncated {
		return []domain.GitDiffFile{{Path: "unrelated"}}, true, nil
	}
	return g.Control.DiffFiles(ctx, root, from, to, max)
}
func TestMemoryFreshnessLargeInaccessibleAndTruncatedRepositories(t *testing.T) {
	for _, mode := range []string{"refs", "inaccessible", "diff"} {
		t.Run(mode, func(t *testing.T) {
			f := newGC(t)
			s := freshnessService(f)
			scope := freshnessScope(f)
			base := gitOut(t, f.repo, "rev-parse", "HEAD")
			m := gitMemory(t, f, scope, base, "refs/heads/main", "conservative degradation", "not-changed")
			reconcileMemory(t, s, scope)
			g := memoryLimitedGit{Control: f.git}
			switch mode {
			case "refs":
				g.refs = memoryRefLimit + 1
			case "inaccessible":
				g.fail = true
			case "diff":
				g.truncated = true
				commitIn(t, f.repo, "README.md", "change", "source changed")
			}
			f.gc.Git = g
			reconcileMemory(t, s, scope)
			assertMemoryState(t, f, m, domain.MemoryNeedsVerification, 2)
		})
	}
}

func TestMemoryFreshnessBoundedPagesResume(t *testing.T) {
	f := newGC(t)
	s := freshnessService(f)
	scope := freshnessScope(f)
	base := gitOut(t, f.repo, "rev-parse", "HEAD")
	for i := 0; i < 205; i++ {
		gitMemory(t, f, scope, base, "refs/heads/main", fmt.Sprintf("bounded fact %d", i), "README.md")
	}
	// Establish baseline over multiple bounded pages.
	for i := 0; i < 3; i++ {
		reconcileMemory(t, s, scope)
	}
	commitIn(t, f.repo, "README.md", "changed", "many facts")
	if !reconcileMemory(t, s, scope) {
		t.Fatal("expected pending page")
	}
	cp := memoryCheckpoint(t, s, scope)
	if cp.After == nil || cp.Revision.Head != base {
		t.Fatal("checkpoint skipped unfinished page", cp)
	}
	// New worker, same durable traversal. No update can skip a record.
	s = freshnessService(f)
	for i := 0; i < 4; i++ {
		if !reconcileMemory(t, s, scope) {
			break
		}
	}
	page, err := memoryService(t, f.fixture, "local", domain.MemoryLimits{}).List(f.ctx, scope, domain.MemoryQuery{State: domain.MemoryCurrent, Limit: 100})
	if err != nil || len(page.Records) != 0 {
		t.Fatal("missed invalidations", len(page.Records), err)
	}
}

type memoryGH struct {
	prs []domain.GitHubPR
	err error
}

func (g *memoryGH) HostKnown(context.Context, string) bool { return true }
func (g *memoryGH) PullRequests(context.Context, github.Repo, int) ([]domain.GitHubPR, error) {
	return append([]domain.GitHubPR(nil), g.prs...), g.err
}
func (g *memoryGH) CreatePullRequest(context.Context, github.Repo, github.CreateRequest) (domain.GitHubPR, error) {
	return domain.GitHubPR{}, errors.New("not supported")
}
func TestMemoryFreshnessAuthenticatedPRAndCIChanges(t *testing.T) {
	f := newGC(t)
	s := freshnessService(f)
	scope := freshnessScope(f)
	sha := gitOut(t, f.repo, "rev-parse", "HEAD")
	gh := &memoryGH{prs: []domain.GitHubPR{{Number: 1, URL: "https://github.com/acme/app/pull/1", State: "open", HeadSha: sha, HeadBranch: "main", Checks: domain.GitHubChecks{State: "pending", Total: 1, Pending: 1}}}}
	f.gc.GitHub = gh
	reconcileMemory(t, s, scope)
	m := gitMemory(t, f, scope, sha, "refs/heads/main", "CI claim", "README.md")
	mem := memoryService(t, f.fixture, "local", domain.MemoryLimits{})
	content := m.MemoryContent
	content.Source.PullRequest = gh.prs[0].URL
	var updateErr error
	m, updateErr = mem.Update(f.ctx, scope, m.ID, m.Version, content)
	if updateErr != nil {
		t.Fatal(updateErr)
	}
	reconcileMemory(t, s, scope)
	assertMemoryState(t, f, m, domain.MemoryCurrent, 2)
	// Independently observed CI transition is evidence to recheck a claim,
	// never permission to upgrade it. Agent-reported tests are not consumed.
	gh.prs[0].Checks = domain.GitHubChecks{State: "passing", Total: 1, Passed: 1}
	reconcileMemory(t, s, scope)
	assertMemoryState(t, f, m, domain.MemoryNeedsVerification, 3)
	var observed int
	if err := s.Store.View(f.ctx, func(tx store.Tx) error {
		events, err := tx.Events().ListAfter(f.ctx, 0, 100)
		for _, ev := range events {
			if ev.Type == domain.EventGitPRObserved {
				observed++
			}
		}
		return err
	}); err != nil || observed != 2 {
		t.Fatal(observed, err)
	}
	m, updateErr = mem.Get(f.ctx, scope, m.ID)
	if updateErr != nil {
		t.Fatal(updateErr)
	}
	for _, state := range []string{"merged", "closed"} {
		content = m.MemoryContent
		content.State = domain.MemoryCurrent
		var err error
		m, err = mem.Update(f.ctx, scope, m.ID, m.Version, content)
		if err != nil {
			t.Fatal(err)
		}
		gh.prs[0].State = state
		reconcileMemory(t, s, scope)
		m, err = mem.Get(f.ctx, scope, m.ID)
		if err != nil || m.State != domain.MemoryNeedsVerification {
			t.Fatal(m, err)
		}
	}
	// An unavailable or truncated/omitted PR is unknown, never fresh.
	content = m.MemoryContent
	content.State = domain.MemoryCurrent
	var err error
	m, err = mem.Update(f.ctx, scope, m.ID, m.Version, content)
	if err != nil {
		t.Fatal(err)
	}
	gh.err = errors.New("offline")
	reconcileMemory(t, s, scope)
	got, err := mem.Get(f.ctx, scope, m.ID)
	if err != nil || got.State != domain.MemoryNeedsVerification {
		t.Fatal(got, err)
	}
}

func TestMemoryFreshnessWorkerReconcilesExternalChangesAndStops(t *testing.T) {
	f := newGC(t)
	s := freshnessService(f)
	s.PollInterval = 20 * time.Millisecond
	scope := freshnessScope(f)
	base := gitOut(t, f.repo, "rev-parse", "HEAD")
	m := gitMemory(t, f, scope, base, "refs/heads/main", "polled external change", "README.md")
	ctx, cancel := context.WithCancel(f.ctx)
	s.Watch(ctx, f.bus)
	defer func() { cancel(); s.Wait() }()
	commitIn(t, f.repo, "README.md", "outside", "external commit")
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("poll did not reconcile external commit")
		case <-tick.C:
			got, err := memoryService(t, f.fixture, "local", domain.MemoryLimits{}).Get(f.ctx, scope, m.ID)
			if err == nil && got.State == domain.MemoryNeedsVerification {
				return
			}
		}
	}
}

func TestMemoryFreshnessRepositoryOnlyAndUnavailableCheckout(t *testing.T) {
	f := newGC(t)
	s := freshnessService(f)
	scope := freshnessScope(f)
	scope.ProjectID = ""
	base := gitOut(t, f.repo, "rev-parse", "HEAD")
	m := gitMemory(t, f, scope, base, "refs/heads/main", "repository-only evidence", "README.md")
	reconcileMemory(t, s, scope)
	assertMemoryState(t, f, m, domain.MemoryCurrent, 1)
	// A missing checkout is not a new empty repository, and cannot advance success.
	if err := os.Rename(f.repo, f.repo+"-offline"); err != nil {
		t.Fatal(err)
	}
	reconcileMemory(t, s, scope)
	assertMemoryState(t, f, m, domain.MemoryNeedsVerification, 2)
	if memoryCheckpoint(t, s, scope).Problem == "" {
		t.Fatal("missing checkout was successful")
	}
}

func TestMemoryFreshnessCompletedRunAndReplayGap(t *testing.T) {
	f := newGC(t)
	s := freshnessService(f)
	scope := freshnessScope(f)
	sha := gitOut(t, f.repo, "rev-parse", "HEAD")
	task, err := f.tasks.Create(f.ctx, f.project.ID, "run evidence", "")
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.runs.Create(f.ctx, NewRun{TaskID: task.ID, AgentID: "fake", Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.runs.MarkStarted(f.ctx, run.ID, Started{}); err != nil {
		t.Fatal(err)
	}
	mem := memoryService(t, f.fixture, "local", domain.MemoryLimits{})
	c := memoryContent("The run's independently checked source")
	c.State = domain.MemoryCurrent
	c.Source = domain.MemoryProvenance{Type: domain.MemorySourceRun, Reference: "run:" + run.ID, RunID: run.ID, GitSHA: sha, Branch: "refs/heads/main", Paths: []string{"README.md"}}
	m := createMemory(t, mem, scope, c)
	reconcileMemory(t, s, scope)
	assertMemoryState(t, f, m, domain.MemoryCurrent, 1)
	if _, err = f.runs.End(f.ctx, run.ID, Ended{State: domain.RunCompleted}); err != nil {
		t.Fatal(err)
	}
	// An unrelated content/privacy edit after completion must not mask the
	// terminal event. Only an explicit verification timestamp is a watermark.
	mem.deps.Now = func() time.Time { return time.Now().Add(time.Second) }
	edited := m.MemoryContent
	edited.SharingEligible = true
	m, err = mem.Update(f.ctx, scope, m.ID, m.Version, edited)
	if err != nil {
		t.Fatal(err)
	}
	reconcileMemory(t, s, scope)
	assertMemoryState(t, f, m, domain.MemoryNeedsVerification, 3)
	// A successful agent exit never supplies a verification timestamp/test pass.
	got, err := mem.Get(f.ctx, scope, m.ID)
	if err != nil || got.VerifiedAt != nil {
		t.Fatal(got, err)
	}
	other := gitMemory(t, f, scope, sha, "refs/heads/main", "retention gap is ambiguous", "unrelated")
	if err := s.Store.Update(f.ctx, func(tx store.Tx) error {
		cp, err := tx.Memory().Freshness(f.ctx, scope)
		if err != nil {
			return err
		}
		cp.EventSeq = 0
		if err := tx.Memory().SaveFreshness(f.ctx, cp); err != nil {
			return err
		}
		return tx.Settings().Set(f.ctx, "events:replay-floor", cp.PendingSeq, s.now())
	}); err != nil {
		t.Fatal(err)
	}
	reconcileMemory(t, s, scope)
	assertMemoryState(t, f, other, domain.MemoryNeedsVerification, 2)
}

func TestMemoryFreshnessRenameRemoteRefsAndRecovery(t *testing.T) {
	f := newGC(t)
	s := freshnessService(f)
	scope := freshnessScope(f)
	sha := gitOut(t, f.repo, "rev-parse", "HEAD")
	renamed := gitMemory(t, f, scope, sha, "refs/heads/main", "old path tracks rename", "README.md")
	remote := gitMemory(t, f, scope, sha, "refs/remotes/origin/main", "remote tracking branch", "README.md")
	reconcileMemory(t, s, scope)
	git(t, f.repo, "mv", "README.md", "renamed.md")
	git(t, f.repo, "commit", "-qm", "rename")
	head := gitOut(t, f.repo, "rev-parse", "HEAD")
	// Local branch changes cannot claim a tracking branch moved without evidence.
	reconcileMemory(t, s, scope)
	assertMemoryState(t, f, renamed, domain.MemoryNeedsVerification, 2)
	assertMemoryState(t, f, remote, domain.MemoryCurrent, 1)
	// Model tracking refs changed by an explicit fetch/push elsewhere. Observer
	// must discover it, and never perform the network operation itself.
	git(t, f.repo, "update-ref", "refs/remotes/origin/main", head)
	reconcileMemory(t, s, scope)
	assertMemoryState(t, f, remote, domain.MemoryNeedsVerification, 2)
	old := memoryCheckpoint(t, s, scope)
	f.gc.Git = memoryLimitedGit{Control: f.git, fail: true}
	reconcileMemory(t, s, scope)
	failed := memoryCheckpoint(t, s, scope)
	if failed.Problem == "" || failed.Revision.Head != old.Revision.Head || failed.EventSeq != old.EventSeq {
		t.Fatal(failed)
	}
	f.gc.Git = f.git
	reconcileMemory(t, s, scope)
	if cp := memoryCheckpoint(t, s, scope); cp.Problem != "" || cp.SucceededAt == nil {
		t.Fatal(cp)
	}
	assertMemoryState(t, f, renamed, domain.MemoryNeedsVerification, 2)
}

func TestMemoryFreshnessMovingRepositoryCannotStarvePages(t *testing.T) {
	f := newGC(t)
	s := freshnessService(f)
	scope := freshnessScope(f)
	sha := gitOut(t, f.repo, "rev-parse", "HEAD")
	for i := 0; i < 205; i++ {
		gitMemory(t, f, scope, sha, "refs/heads/main", fmt.Sprintf("moving repository %d", i), "unchanged")
	}
	if !reconcileMemory(t, s, scope) {
		t.Fatal("expected more")
	}
	for i := 0; i < 3; i++ {
		commitIn(t, f.repo, "README.md", fmt.Sprintf("change %d", i), "new observation during scan")
		reconcileMemory(t, s, scope)
	}
	// Pages after the first change are conservative; the earlier page is reached
	// by the next pass even while the repository keeps moving.
	page, err := memoryService(t, f.fixture, "local", domain.MemoryLimits{}).List(f.ctx, scope, domain.MemoryQuery{State: domain.MemoryCurrent, Limit: 100})
	if err != nil || len(page.Records) > 100 || page.Next != nil {
		t.Fatalf("tail starved: %+v %v", page, err)
	}
}

func TestMemoryFreshnessObserverUsesNoGitWrites(t *testing.T) {
	f := newGC(t)
	s := freshnessService(f)
	scope := freshnessScope(f)
	sha := gitOut(t, f.repo, "rev-parse", "HEAD")
	gitMemory(t, f, scope, sha, "refs/heads/main", "observer is read only", "README.md")
	before := gitOut(t, f.repo, "show-ref")
	// Forbid every operation that could mutate refs/trees or contact a remote.
	f.git.Binary = failingGit(t, "fetch", "pull", "push", "merge", "branch", "checkout", "switch", "reset", "update-ref", "commit", "worktree")
	reconcileMemory(t, s, scope)
	cp := memoryCheckpoint(t, s, scope)
	if cp.Problem != "" || cp.Revision.Head != sha || gitOut(t, f.repo, "show-ref") != before {
		t.Fatal(cp)
	}
}

func TestMemoryFreshnessEventWakesRepositoryOnlyScopeWithoutPolling(t *testing.T) {
	f := newGC(t)
	s := freshnessService(f)
	s.PollInterval = time.Hour
	scope := freshnessScope(f)
	scope.ProjectID = ""
	sha := gitOut(t, f.repo, "rev-parse", "HEAD")
	m := gitMemory(t, f, scope, sha, "refs/heads/main", "event wake", "README.md")
	ctx, cancel := context.WithCancel(f.ctx)
	s.Watch(ctx, f.bus)
	defer func() { cancel(); s.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var ready bool
		err := s.Store.View(ctx, func(tx store.Tx) error {
			cp, err := tx.Memory().Freshness(ctx, scope)
			ready = err == nil && cp.SucceededAt != nil
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("startup did not observe scope")
		}
		time.Sleep(10 * time.Millisecond)
	}
	commitIn(t, f.repo, "README.md", "event commit", "outside change before event")
	if err := s.Deps.update(ctx, func(tx store.Tx, em *emitter) error {
		ev := newEvent(domain.EventGitPushed, domain.GitActionResult{})
		ev.ProjectID = f.project.ID
		return em.emit(ev)
	}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		got, err := memoryService(t, f.fixture, "local", domain.MemoryLimits{}).Get(ctx, scope, m.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.State == domain.MemoryNeedsVerification {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("event did not prioritize repository-only scope")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestMemoryFreshnessAmbiguousRepositoryOnlyScopeIsNeverFresh(t *testing.T) {
	f := newGC(t)
	s := freshnessService(f)
	scope := freshnessScope(f)
	scope.ProjectID = ""
	sha := gitOut(t, f.repo, "rev-parse", "HEAD")
	m := gitMemory(t, f, scope, sha, "refs/heads/main", "ambiguous checkout", "README.md")
	reconcileMemory(t, s, scope)
	assertMemoryState(t, f, m, domain.MemoryCurrent, 1)
	other := filepath.Join(t.TempDir(), "same-remote")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, other, "init", "-q", "-b", "main")
	commitIn(t, other, "README.md", "other", "init")
	git(t, other, "remote", "add", "origin", ghURL)
	if _, err := f.projects.Register(f.ctx, other, "second checkout"); err != nil {
		t.Fatal(err)
	}
	reconcileMemory(t, s, scope)
	assertMemoryState(t, f, m, domain.MemoryNeedsVerification, 2)
	if cp := memoryCheckpoint(t, s, scope); cp.Problem == "" || cp.Revision.Head != sha {
		t.Fatal(cp)
	}
}

func TestMemoryFreshnessProjectFactsNeedEvidenceButWorkFactsDoNot(t *testing.T) {
	f := newGC(t)
	s := freshnessService(f)
	mem := memoryService(t, f.fixture, "local", domain.MemoryLimits{})
	c := memoryContent("project-scoped source claim")
	c.State = domain.MemoryCurrent
	gitScope := domain.MemoryScope{OwnerID: "local", ProjectID: f.project.ID}
	gitFact := createMemory(t, mem, gitScope, c)
	reconcileMemory(t, s, gitScope)
	assertMemoryState(t, f, gitFact, domain.MemoryNeedsVerification, 2)
	work, err := f.projects.CreateWork(f.ctx, "Operations")
	if err != nil {
		t.Fatal(err)
	}
	workScope := domain.MemoryScope{OwnerID: "local", ProjectID: work.ID}
	c.Summary = "Operations planning fact"
	workFact := createMemory(t, mem, workScope, c)
	reconcileMemory(t, s, workScope)
	assertMemoryState(t, f, workFact, domain.MemoryCurrent, 1)
	if err := s.Store.View(f.ctx, func(tx store.Tx) error {
		scopes, err := tx.Memory().FreshnessScopes(f.ctx, nil, 32)
		if err != nil {
			return err
		}
		for _, scope := range scopes {
			if scope.ProjectID == work.ID {
				t.Fatal("work project entered Git observation")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryFreshnessEventCursorNeverRegressesAfterPruning(t *testing.T) {
	f := newGC(t)
	s := freshnessService(f)
	scope := freshnessScope(f)
	a := f.agent(t, "retained event cursor", 0)
	// Put a retained run lifecycle event above the task's later manual move.
	if err := s.Deps.update(f.ctx, func(tx store.Tx, em *emitter) error {
		ev := newEvent(domain.EventAgentCompleted, map[string]any{"run": a.run})
		ev.ProjectID, ev.RunID = f.project.ID, a.run.ID
		return em.emit(ev)
	}); err != nil {
		t.Fatal(err)
	}
	sha := gitOut(t, f.repo, "rev-parse", "HEAD")
	gitMemory(t, f, scope, sha, "refs/heads/main", "cursor high water mark", "README.md")
	reconcileMemory(t, s, scope)
	previous := memoryCheckpoint(t, s, scope).EventSeq
	if err := s.Store.Update(f.ctx, func(tx store.Tx) error {
		_, err := tx.Events().Prune(f.ctx, time.Now().Add(400*24*time.Hour))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.View(f.ctx, func(tx store.Tx) error {
		latest, err := tx.Events().LatestSeq(f.ctx)
		if err != nil {
			return err
		}
		if latest >= previous {
			t.Fatal("fixture did not remove the highest processed event")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reconcileMemory(t, s, scope)
	if current := memoryCheckpoint(t, s, scope).EventSeq; current < previous {
		t.Fatalf("event pruning regressed cursor for %s: %d -> %d", a.run.ID, previous, current)
	}
}
