package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"devboard/internal/domain"
	"devboard/internal/store"
)

func memoryContent(summary string) domain.MemoryContent {
	return domain.MemoryContent{Type: "build_command", Authority: domain.MemoryObservation, Source: domain.MemoryProvenance{Type: domain.MemorySourceImport, Reference: "manual:1"}, Summary: summary}
}
func memoryService(t *testing.T, f *fixture, owner string, limits domain.MemoryLimits) *Memory {
	t.Helper()
	s, err := NewMemory(f.deps, owner, limits)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func createMemory(t *testing.T, s *Memory, scope domain.MemoryScope, c domain.MemoryContent) *domain.Memory {
	t.Helper()
	m, err := s.Create(context.Background(), scope, c)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func TestMemoryCRUDIsolationAndCanonicalRepository(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := memoryService(t, f, "individual_a", domain.MemoryLimits{})
	p, err := f.projects.Register(ctx, "/repos/memory-a", "Same name")
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.projects.Register(ctx, "/repos/memory-b", "Same name")
	if err != nil {
		t.Fatal(err)
	}
	err = f.deps.Store.Update(ctx, func(tx store.Tx) error {
		for _, x := range []struct{ id, remote string }{{p.ID, "git@GitHub.com:acme/a.git"}, {other.ID, "https://github.com/acme/b.git"}} {
			r, err := tx.Repositories().Get(ctx, x.id)
			if err != nil {
				return err
			}
			r.Remotes = []domain.GitRemote{{Name: "origin", URL: x.remote}}
			if err := tx.Repositories().Upsert(ctx, r); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	scope := domain.MemoryScope{ProjectID: p.ID, Repository: "https://GITHUB.com/acme/a.git"}
	m := createMemory(t, s, scope, memoryContent("Use SQLite WAL"))
	if m.Scope.Repository != "github.com/acme/a" || m.Scope.OwnerID != "individual_a" {
		t.Fatalf("scope: %+v", m.Scope)
	}
	for _, wrong := range []domain.MemoryScope{{}, {ProjectID: other.ID, Repository: scope.Repository}, {ProjectID: p.ID, Repository: "https://github.com/acme/b"}, {Repository: scope.Repository}, {ProjectID: p.ID}} {
		if _, err := s.Get(ctx, wrong, m.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("cross-scope get %+v: %v", wrong, err)
		}
		page, err := s.List(ctx, wrong, domain.MemoryQuery{Text: "SQLite"})
		if err != nil || len(page.Records) != 0 {
			t.Fatalf("cross-scope search: %+v %v", page, err)
		}
		if err := s.Delete(ctx, wrong, m.ID, m.Version); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("cross-scope delete: %v", err)
		}
		if _, err := s.Update(ctx, wrong, m.ID, m.Version, m.MemoryContent); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("cross-scope update: %v", err)
		}
	}
	b := memoryService(t, f, "individual_b", domain.MemoryLimits{})
	if _, err := b.Get(ctx, scope, m.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := b.Get(ctx, m.Scope, m.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, domain.MemoryScope{ProjectID: other.ID, Repository: scope.Repository}, memoryContent("wrong repository")); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	for _, repo := range []string{"/repos/memory-a", "Same name", "file:///repos/a", "https://token@github.com/acme/a", "https://github.com/acme/../a", "github.com/acme/a.git"} {
		if _, err := s.Create(ctx, domain.MemoryScope{Repository: repo}, memoryContent("bad")); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("accepted %q: %v", repo, err)
		}
	}
	got, err := s.Get(ctx, m.Scope, m.ID)
	if err != nil || got.ID != m.ID {
		t.Fatal(got, err)
	}
	c := got.MemoryContent
	c.Summary = "Use transactions"
	updated, err := s.Update(ctx, m.Scope, m.ID, m.Version, c)
	if err != nil || updated.Version != 2 {
		t.Fatal(updated, err)
	}
	if _, err := s.Update(ctx, m.Scope, m.ID, m.Version, c); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, m.Scope, m.ID, m.Version); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, m.Scope, m.ID, updated.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, m.Scope, m.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
}
func TestMemoryDeduplicationAndAuthority(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := memoryService(t, f, "owner", domain.MemoryLimits{})
	scope := domain.MemoryScope{}
	c := memoryContent("Build with make")
	c.Details = json.RawMessage(`{"b":2,"a":1}`)
	m := createMemory(t, s, scope, c)
	c.Details = json.RawMessage(`{ "a":1, "b":2 }`)
	if _, err := s.Create(ctx, scope, c); !errors.Is(err, domain.ErrDuplicate) {
		t.Fatal(err)
	}
	dup, err := s.FindDuplicate(ctx, scope, c)
	if err != nil || dup.ID != m.ID {
		t.Fatal(dup, err)
	}
	c.Authority = domain.MemoryPreference
	pref := createMemory(t, s, scope, c)
	c = memoryContent("different summary")
	second := createMemory(t, s, scope, c)
	c.DuplicateOf = m.ID
	alias := createMemory(t, s, scope, c)
	if alias.DuplicateOf != m.ID {
		t.Fatal(alias)
	}
	c = m.MemoryContent
	if _, err := s.Update(ctx, scope, second.ID, second.Version, c); !errors.Is(err, domain.ErrDuplicate) {
		t.Fatalf("must not collide with canonical evidence: %v", err)
	}
	// A direct cycle is refused even when it spans distinct link kinds.
	c = second.MemoryContent
	c.State = domain.MemorySuperseded
	c.SupersededBy = alias.ID
	if _, err := s.Update(ctx, scope, second.ID, second.Version, c); err != nil {
		t.Fatal(err)
	}
	c = m.MemoryContent
	c.State = domain.MemorySuperseded
	c.SupersededBy = second.ID
	if _, err := s.Update(ctx, scope, m.ID, m.Version, c); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("cycle: %v", err)
	}
	c = pref.MemoryContent
	c.DuplicateOf = m.ID
	if _, err := s.Update(ctx, scope, pref.ID, pref.Version, c); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("cross authority: %v", err)
	}
	c = pref.MemoryContent
	c.Authority = domain.MemoryPolicy
	c.Source = domain.MemoryProvenance{Type: domain.MemorySourceUser, Reference: "user:1", ApprovedBy: "owner", ApprovalRef: "approval:1"}
	if _, err := s.Update(ctx, scope, pref.ID, pref.Version, c); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("authority escalation: %v", err)
	}
	if err := s.Delete(ctx, scope, m.ID, m.Version); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("referenced deletion: %v", err)
	}
	policy := memoryContent("Never push automatically")
	policy.Authority = domain.MemoryPolicy
	policy.Source = c.Source
	approved := createMemory(t, s, scope, policy)
	if approved.SharingEligible {
		t.Fatal("approved policy was shared")
	}
}
func TestMemorySearchPaginationAndFTSLifecycle(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := memoryService(t, f, "owner", domain.MemoryLimits{})
	scope := domain.MemoryScope{}
	var ids []string
	for i := 0; i < 5; i++ {
		m := createMemory(t, s, scope, memoryContent(fmt.Sprintf("SQLite café transaction %d", i)))
		ids = append(ids, m.ID)
	}
	createMemory(t, s, domain.MemoryScope{Repository: "https://example.com/other/repo"}, memoryContent("SQLite cafe transaction isolated"))
	q := domain.MemoryQuery{Text: "sqlite cafe", Limit: 2}
	seen := map[string]bool{}
	for {
		page, err := s.List(ctx, scope, q)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range page.Records {
			if seen[m.ID] {
				t.Fatal("duplicate across pages")
			}
			seen[m.ID] = true
		}
		if page.Next == nil {
			break
		}
		q.After = page.Next
	}
	if len(seen) != 5 {
		t.Fatalf("got %d results", len(seen))
	}
	for _, text := range []string{`" OR *`, `sqlite NOT cafe`, `sqlite*`, `summary:sqlite`, `"`, `*`, `sqlite') OR 1=1 --`} {
		if _, err := s.List(ctx, scope, domain.MemoryQuery{Text: text}); err != nil {
			t.Fatalf("literal search %q: %v", text, err)
		}
	}
	got, err := s.Get(ctx, scope, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	c := got.MemoryContent
	c.Summary = "Postgres backup"
	got, err = s.Update(ctx, scope, got.ID, got.Version, c)
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.List(ctx, scope, domain.MemoryQuery{Text: "Postgres"})
	if err != nil || len(page.Records) != 1 {
		t.Fatal(page, err)
	}
	if err := s.Delete(ctx, scope, got.ID, got.Version); err != nil {
		t.Fatal(err)
	}
	page, err = s.List(ctx, scope, domain.MemoryQuery{Text: "Postgres"})
	if err != nil || len(page.Records) != 0 {
		t.Fatal(page, err)
	}
	page, err = s.List(ctx, scope, domain.MemoryQuery{Text: "sqlite", State: domain.MemoryNeedsVerification, Authority: domain.MemoryObservation, Type: "build_command"})
	if err != nil || len(page.Records) != 4 {
		t.Fatal(page, err)
	}
}
func TestMemoryConcurrentUpdates(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := memoryService(t, f, "owner", domain.MemoryLimits{})
	m := createMemory(t, s, domain.MemoryScope{}, memoryContent("initial"))
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := m.MemoryContent
			c.Summary = fmt.Sprintf("writer %d", i)
			_, err := s.Update(ctx, m.Scope, m.ID, m.Version, c)
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	success, conflicts := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, domain.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
}
func TestMemoryRetentionAndCapacity(t *testing.T) {
	f := newFixture(t)
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f.deps.Now = func() time.Time { return clock }
	limits := domain.MemoryLimits{Records: 8, Bytes: 1 << 20, HistoryRetention: 24 * time.Hour}
	s := memoryService(t, f, "owner", limits)
	ctx := context.Background()
	scope := domain.MemoryScope{}
	keep := createMemory(t, s, scope, memoryContent("current evidence"))
	historical := memoryContent("historical target")
	historical.State = domain.MemoryHistorical
	target := createMemory(t, s, scope, historical)
	clock = clock.Add(time.Millisecond)
	old := memoryContent("superseded source")
	old.State = domain.MemorySuperseded
	old.SupersededBy = target.ID
	createMemory(t, s, scope, old)
	clock = clock.Add(time.Millisecond)
	historical.Summary = "old unreferenced"
	createMemory(t, s, scope, historical)
	other := memoryService(t, f, "other", limits)
	createMemory(t, other, scope, historical)
	clock = clock.Add(48 * time.Hour)
	historical.Summary = "recent history"
	createMemory(t, s, scope, historical)
	if n, err := s.Prune(ctx, 1); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err := s.Get(ctx, scope, target.ID); err != nil {
		t.Fatalf("referenced target pruned: %v", err)
	}
	if n, err := s.Prune(ctx, 100); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	if _, err := s.Get(ctx, scope, keep.ID); err != nil {
		t.Fatal(err)
	}
	if u, err := other.Usage(ctx); err != nil || u.Records != 1 {
		t.Fatal(u, err)
	}
	if u, err := s.Usage(ctx); err != nil || u.Records != 2 {
		t.Fatal(u, err)
	}
	small := limits
	small.Records = 2
	limited := memoryService(t, f, "owner", small)
	if _, err := limited.Create(ctx, scope, memoryContent("over capacity")); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	if u, _ := s.Usage(ctx); u.Records != 2 {
		t.Fatal("capacity failure did not roll back")
	}
	// Payload budget is transactional too, with valid individual record sizes.
	byteLimits := limits
	byteLimits.Bytes = domain.MaxMemoryRecordBytes
	byteLimits.Records = 8
	bytesSvc := memoryService(t, f, "bytes_owner", byteLimits)
	big := memoryContent(strings.Repeat("x", 4000))
	big.Details = json.RawMessage(`{"blob":"` + strings.Repeat("a", 15000) + `"}`)
	first := createMemory(t, bytesSvc, scope, big)
	big.Summary = strings.Repeat("y", 4000)
	if _, err := bytesSvc.Create(ctx, scope, big); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	if err := bytesSvc.Delete(ctx, scope, first.ID, first.Version); err != nil {
		t.Fatal(err)
	}
	if u, _ := bytesSvc.Usage(ctx); u.Records != 0 || u.Bytes != 0 {
		t.Fatal(u)
	}
}
func TestMemoryVerificationAndProvenance(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := memoryService(t, f, "owner", domain.MemoryLimits{})
	scope := domain.MemoryScope{}
	future := time.Now().Add(time.Hour)
	c := memoryContent("verified")
	c.VerifiedAt = &future
	if _, err := s.Create(ctx, scope, c); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	verified := time.Now().Add(-time.Hour)
	c.VerifiedAt = &verified
	m := createMemory(t, s, scope, c)
	c = m.MemoryContent
	c.Summary = "new evidence"
	m, err := s.Update(ctx, scope, m.ID, m.Version, c)
	if err != nil || m.VerifiedAt != nil {
		t.Fatal(m, err)
	}
	c = memoryContent("git evidence")
	c.Source.Type = domain.MemorySourceGit
	c.Source.GitSHA = strings.Repeat("a", 40)
	if _, err := s.Create(ctx, scope, c); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	repoScope := domain.MemoryScope{Repository: "git@github.com:acme/a.git"}
	createMemory(t, s, repoScope, c)
	c.Source.PullRequest = "https://user:pass@example.com/acme/a/pull/1"
	if _, err := s.Create(ctx, repoScope, c); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	c = memoryContent("run")
	c.Source.Type = domain.MemorySourceRun
	c.Source.RunID = "run_absent"
	if _, err := s.Create(ctx, scope, c); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	p, err := f.projects.Register(ctx, "/repos/source", "source")
	if err != nil {
		t.Fatal(err)
	}
	scope.ProjectID = p.ID
	if _, err := s.Create(ctx, scope, c); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestMemoryTotalRecordBoundAndUpdateQuotaRollback(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := memoryService(t, f, "owner", domain.MemoryLimits{})
	tooBig := memoryContent(strings.Repeat(`"`, 4000))
	tooBig.Source.Reference = strings.Repeat("<", 2000)
	tooBig.Details = json.RawMessage(`{"blob":"` + strings.Repeat("a", 16000) + `"}`)
	if _, err := s.Create(ctx, domain.MemoryScope{}, tooBig); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("total record bound: %v", err)
	}
	if usage, err := s.Usage(ctx); err != nil || usage.Records != 0 {
		t.Fatal(usage, err)
	}
	limits := domain.MemoryLimits{Records: 10, Bytes: domain.MaxMemoryRecordBytes, HistoryRetention: 24 * time.Hour}
	limited := memoryService(t, f, "limited", limits)
	initial := memoryContent(strings.Repeat("i", 4000))
	initial.Details = json.RawMessage(`{"blob":"` + strings.Repeat("a", 15000) + `"}`)
	createMemory(t, limited, domain.MemoryScope{}, initial)
	small := createMemory(t, limited, domain.MemoryScope{}, memoryContent("small"))
	before, err := limited.Usage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	initial.Summary = strings.Repeat("s", 4000)
	if _, err := limited.Update(ctx, small.Scope, small.ID, small.Version, initial); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("update quota: %v", err)
	}
	got, err := limited.Get(ctx, small.Scope, small.ID)
	if err != nil || got.Version != small.Version || got.Summary != "small" {
		t.Fatal(got, err)
	}
	after, err := limited.Usage(ctx)
	if err != nil || after != before {
		t.Fatalf("quota rollback counters before=%+v after=%+v err=%v", before, after, err)
	}
	page, err := limited.List(ctx, small.Scope, domain.MemoryQuery{Text: "small"})
	if err != nil || len(page.Records) != 1 {
		t.Fatal(page, err)
	}
}

func TestMemoryLocalRunHandoffEvidenceAndRemoteChanges(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := memoryService(t, f, "owner", domain.MemoryLimits{})
	p, err := f.projects.Register(ctx, "/repos/handoff-memory", "handoff")
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.projects.Register(ctx, "/repos/handoff-other", "other")
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.tasks.Create(ctx, p.ID, "evidence", "")
	if err != nil {
		t.Fatal(err)
	}
	at := f.deps.now()
	run := &domain.Run{ID: domain.NewID(domain.PrefixRun), TaskID: task.ID, ProjectID: p.ID, AgentID: "test", State: domain.RunCompleted, CreatedAt: at, UpdatedAt: at, Handoff: &domain.Handoff{Summary: "recorded"}}
	err = f.deps.Store.Update(ctx, func(tx store.Tx) error { return tx.Runs().Create(ctx, run) })
	if err != nil {
		t.Fatal(err)
	}
	c := memoryContent("handoff evidence")
	c.Source = domain.MemoryProvenance{Type: domain.MemorySourceHandoff, Reference: "run-artifact:1", RunID: run.ID, HandoffID: run.ID}
	createMemory(t, s, domain.MemoryScope{ProjectID: p.ID}, c)
	if _, err := s.Create(ctx, domain.MemoryScope{ProjectID: other.ID}, c); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-project run: %v", err)
	}
	c.Source.HandoffID = "run_wrong"
	if _, err := s.Create(ctx, domain.MemoryScope{ProjectID: p.ID}, c); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("wrong handoff: %v", err)
	}
	scope := domain.MemoryScope{ProjectID: p.ID, Repository: "https://github.com/acme/original"}
	err = f.deps.Store.Update(ctx, func(tx store.Tx) error {
		r, err := tx.Repositories().Get(ctx, p.ID)
		if err != nil {
			return err
		}
		r.Remotes = []domain.GitRemote{{Name: "origin", URL: scope.Repository}}
		return tx.Repositories().Upsert(ctx, r)
	})
	if err != nil {
		t.Fatal(err)
	}
	m := createMemory(t, s, scope, memoryContent("original repository evidence"))
	err = f.deps.Store.Update(ctx, func(tx store.Tx) error {
		r, err := tx.Repositories().Get(ctx, p.ID)
		if err != nil {
			return err
		}
		r.Remotes = []domain.GitRemote{{Name: "origin", URL: "https://github.com/acme/replaced"}}
		return tx.Repositories().Upsert(ctx, r)
	})
	if err != nil {
		t.Fatal(err)
	}
	c = m.MemoryContent
	c.State = domain.MemoryHistorical
	updated, err := s.Update(ctx, m.Scope, m.ID, m.Version, c)
	if err != nil {
		t.Fatalf("old partition update: %v", err)
	}
	if err := s.Delete(ctx, m.Scope, m.ID, updated.Version); err != nil {
		t.Fatalf("old partition delete: %v", err)
	}
}

func TestMemoryCanonicalRepositoryRoundTrip(t *testing.T) {
	f := newFixture(t)
	s := memoryService(t, f, "owner", domain.MemoryLimits{})
	ctx := context.Background()
	for i, raw := range []string{"ssh://git@example.com:443/Acme/repo.git", "https://example.com:22/Acme/repo", "ssh://git@example.com:2222/Acme/repo", "https://[::1]/Acme/repo", "ssh://git@[::1]:443/Acme/repo"} {
		t.Run(raw, func(t *testing.T) {
			m := createMemory(t, s, domain.MemoryScope{Repository: raw}, memoryContent(fmt.Sprintf("canonical roundtrip %d", i)))
			if _, err := s.Get(ctx, m.Scope, m.ID); err != nil {
				t.Fatalf("returned identity cannot be read: %+v %v", m.Scope, err)
			}
			if err := s.Delete(ctx, m.Scope, m.ID, m.Version); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMemoryApprovedContentRequiresFreshApproval(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := memoryService(t, f, "owner", domain.MemoryLimits{})
	for _, authority := range []domain.MemoryAuthority{domain.MemoryDecision, domain.MemoryPolicy} {
		c := memoryContent("approved content")
		c.Authority = authority
		c.Source = domain.MemoryProvenance{Type: domain.MemorySourceUser, Reference: "user:1", ApprovedBy: "owner", ApprovalRef: "approval:1"}
		m := createMemory(t, s, domain.MemoryScope{}, c)
		c = m.MemoryContent
		c.Summary = "different content"
		if _, err := s.Update(ctx, m.Scope, m.ID, m.Version, c); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("old approval used for new content: %v", err)
		}
		c.Source.ApprovalRef = "approval:2"
		updated, err := s.Update(ctx, m.Scope, m.ID, m.Version, c)
		if err != nil {
			t.Fatal(err)
		}
		c = updated.MemoryContent
		c.Sensitivity = domain.MemorySecret
		if _, err := s.Update(ctx, m.Scope, m.ID, updated.Version, c); err != nil {
			t.Fatalf("privacy tightening should not require reapproval: %v", err)
		}
	}
}
