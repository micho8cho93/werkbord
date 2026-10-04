package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"devboard/internal/domain"
)

// scriptedGit answers the nth Inspect call with heads[n-1], i.e. the state of
// the repository at the moment that call reads it. Call number `hold` stalls
// after reading, before returning, until release is closed: a slow inspection.
type scriptedGit struct {
	mu      sync.Mutex
	calls   int
	heads   []string
	hold    int
	entered chan int
	release chan struct{}
}

func newScriptedGit(hold int, heads ...string) *scriptedGit {
	return &scriptedGit{heads: heads, hold: hold, entered: make(chan int, 16), release: make(chan struct{})}
}

func (g *scriptedGit) Inspect(ctx context.Context, path string) (*domain.GitRepository, error) {
	g.mu.Lock()
	g.calls++
	n := g.calls
	g.mu.Unlock()
	head := g.heads[n-1]
	g.entered <- n
	if n == g.hold {
		select {
		case <-g.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &domain.GitRepository{RootPath: path, HeadCommit: head, CurrentBranch: "main", Remotes: []domain.GitRemote{}, InspectedAt: time.Now().UTC()}, nil
}

func (g *scriptedGit) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

func goRefresh(f *fixture, id string) chan error {
	done := make(chan error, 1)
	go func() { _, err := f.projects.Refresh(context.Background(), id); done <- err }()
	return done
}

// TestConcurrentRefreshesNeverLeaveAnOlderSnapshot: refresh A reads the
// repository, then stalls; the repository changes; refresh B reads the new
// state and finishes first. A must not then overwrite B's newer snapshot with
// what it read earlier.
func TestConcurrentRefreshesNeverLeaveAnOlderSnapshot(t *testing.T) {
	f := newFixture(t)
	g := newScriptedGit(2, "h0", "old", "new") // call 1 registers, 2 = refresh A, 3 = refresh B
	f.projects.Git = g
	ctx := context.Background()
	p, err := f.projects.Register(ctx, "/repos/app", "app")
	if err != nil {
		t.Fatal(err)
	}
	<-g.entered

	a := goRefresh(f, p.ID)
	if n := <-g.entered; n != 2 {
		t.Fatalf("expected refresh A to be Inspect call 2, got %d", n)
	}
	b := goRefresh(f, p.ID)
	select { // without serialisation B completes here, before A
	case err := <-b:
		b <- err // keep the result for the check below
	case <-time.After(300 * time.Millisecond):
	}
	close(g.release)

	for name, ch := range map[string]chan error{"A": a, "B": b} {
		select {
		case err := <-ch:
			if err != nil {
				t.Fatalf("refresh %s: %v", name, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("refresh %s did not finish", name)
		}
	}
	got, err := f.projects.Get(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Repository.HeadCommit != "new" {
		t.Errorf("stored HEAD = %q, want %q: an older inspection overwrote a newer one", got.Repository.HeadCommit, "new")
	}
}

// TestRefreshWaitingForTheProjectHonoursContext: a refresh queued behind a slow
// one must give up when its caller does, without starting a second inspection.
func TestRefreshWaitingForTheProjectHonoursContext(t *testing.T) {
	f := newFixture(t)
	g := newScriptedGit(2, "h0", "slow", "never")
	f.projects.Git = g
	p, err := f.projects.Register(context.Background(), "/repos/app", "app")
	if err != nil {
		t.Fatal(err)
	}
	<-g.entered
	slow := goRefresh(f, p.ID)
	<-g.entered

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := f.projects.Refresh(ctx, p.ID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if n := g.callCount(); n != 2 {
		t.Errorf("Inspect ran %d times; the queued refresh must not have started one", n)
	}
	close(g.release)
	if err := <-slow; err != nil {
		t.Fatal(err)
	}
}

// TestRefreshOfOneProjectDoesNotBlockAnother guards against over-locking.
func TestRefreshOfOneProjectDoesNotBlockAnother(t *testing.T) {
	f := newFixture(t)
	g := newScriptedGit(3, "h0", "h0", "slow", "other") // 1,2 register; 3 = refresh of x (held); 4 = refresh of y
	f.projects.Git = g
	ctx := context.Background()
	x, err := f.projects.Register(ctx, "/repos/x", "x")
	if err != nil {
		t.Fatal(err)
	}
	y, err := f.projects.Register(ctx, "/repos/y", "y")
	if err != nil {
		t.Fatal(err)
	}
	<-g.entered
	<-g.entered
	slow := goRefresh(f, x.ID)
	<-g.entered

	other := goRefresh(f, y.ID)
	select {
	case err := <-other:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("refreshing project y waited for project x")
	}
	close(g.release)
	<-slow
}
