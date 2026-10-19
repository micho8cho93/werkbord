package service

import (
	"context"
	"devboard/internal/domain"
	"devboard/internal/integration"
	"devboard/internal/store"
	"strings"
	"testing"
)

func TestRepositoryRelationshipsLifecycleAndDiscovery(t *testing.T) {
	f, k, p := knowledgeFixture(t)
	ctx := context.Background()
	other, e := f.projects.Register(ctx, "/repos/frontend", "Renamable frontend")
	if e != nil {
		t.Fatal(e)
	}
	a := integration.MemoryTarget{ProjectID: p.ID, Repository: "github.com/acme/backend"}
	b := integration.MemoryTarget{ProjectID: other.ID, Repository: "github.com/acme/frontend"}
	set := func(target integration.MemoryTarget) {
		t.Helper()
		e := f.deps.Store.Update(ctx, func(tx store.Tx) error {
			repo, e := tx.Repositories().Get(ctx, target.ProjectID)
			if e != nil {
				return e
			}
			repo.Remotes = []domain.GitRemote{{Name: "origin", URL: "https://" + target.Repository}}
			repo.HeadCommit = strings.Repeat("a", 40)
			return tx.Repositories().Upsert(ctx, repo)
		})
		if e != nil {
			t.Fatal(e)
		}
	}
	set(a)
	set(b)
	r := integration.RepositoryRelationship{Source: a, Target: b, Type: "api_consumer", Evidence: "manual", SourceRevision: strings.Repeat("a", 40)}
	m, e := k.SaveRelationship(ctx, p.ID, r, false)
	if e != nil {
		t.Fatal(e)
	}
	query := func(status string, n int) {
		t.Helper()
		got, e := k.Relationships(ctx, p.ID, 4)
		if e != nil || len(got) != n {
			t.Fatal(got, e)
		}
		if n > 0 && got[0].Status != status {
			t.Fatal(got)
		}
	}
	query("provisional", 1)
	m, e = k.AcceptRelationship(ctx, p.ID, m.ID, m.Version)
	if e != nil {
		t.Fatal(e)
	}
	query("verified", 1)
	r.SourceRevision = strings.Repeat("b", 40)
	r.Type = "shared_authentication"
	stale, e := k.SaveRelationship(ctx, p.ID, r, true)
	if e != nil {
		t.Fatal(e)
	}
	got, e := k.Relationships(ctx, p.ID, 1)
	if e != nil || len(got) != 2 {
		t.Fatal(got, e)
	}
	found := false
	for _, v := range got {
		if v.ID == stale.ID {
			found = v.Status == "stale"
		}
	}
	if !found {
		t.Fatal(got)
	}
	// An upstream remote rename/replacement requires explicit re-binding: hide old edges.
	changed := b
	changed.Repository = "github.com/acme/frontend-renamed"
	set(changed)
	query("", 0)
	set(b)
	if e = k.Memory.Delete(ctx, domain.MemoryScope{ProjectID: p.ID}, m.ID, m.Version); e != nil {
		t.Fatal(e)
	}
	if e = k.Memory.Delete(ctx, domain.MemoryScope{ProjectID: p.ID}, stale.ID, stale.Version); e != nil {
		t.Fatal(e)
	}
	ms, e := k.DiscoverRelationships(ctx, p.ID, a, "package.json", []byte(`{"dependencies":{"ignored":"frontend","front":"git+https://github.com/acme/frontend.git#main"}}`), strings.Repeat("a", 40))
	if e != nil || len(ms) != 1 {
		t.Fatal(ms, e)
	}
	query("provisional", 1)
	ms, e = k.DiscoverRelationships(ctx, p.ID, a, "package.json", []byte(`{"dependencies":{"front":"git+https://github.com/acme/frontend.git#main"}}`), strings.Repeat("a", 40))
	if e != nil || len(ms) != 0 {
		t.Fatal(ms, e)
	}
	// Removed repository metadata makes the relationship unavailable, without opening a path.
	changed.Repository = "github.com/acme/unrelated"
	set(changed)
	query("", 0)
}

func TestRelationshipSupersessionDoesNotTraverseOldEdges(t *testing.T) {
	f, k, p := knowledgeFixture(t)
	ctx := context.Background()
	other, e := f.projects.Register(ctx, "/repos/replacement", "Replacement")
	if e != nil {
		t.Fatal(e)
	}
	a := integration.MemoryTarget{ProjectID: p.ID, Repository: "github.com/acme/a"}
	b := integration.MemoryTarget{ProjectID: other.ID, Repository: "github.com/acme/b"}
	e = f.deps.Store.Update(ctx, func(tx store.Tx) error {
		for _, target := range []integration.MemoryTarget{a, b} {
			repo, e := tx.Repositories().Get(ctx, target.ProjectID)
			if e != nil {
				return e
			}
			repo.Remotes = []domain.GitRemote{{Name: "origin", URL: "https://" + target.Repository}}
			if e = tx.Repositories().Upsert(ctx, repo); e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	old, e := k.SaveRelationship(ctx, p.ID, integration.RepositoryRelationship{Source: a, Target: b, Type: "manual_association", Evidence: "manual"}, false)
	if e != nil {
		t.Fatal(e)
	}
	replacement, e := k.SaveRelationship(ctx, p.ID, integration.RepositoryRelationship{Source: a, Target: b, Type: "api_consumer", Evidence: "manual"}, true)
	if e != nil {
		t.Fatal(e)
	}
	old.State = domain.MemorySuperseded
	old.SupersededBy = replacement.ID
	if _, e = k.Memory.Update(ctx, domain.MemoryScope{ProjectID: p.ID}, old.ID, old.Version, old.MemoryContent); e != nil {
		t.Fatal(e)
	}
	got, e := k.Relationships(ctx, p.ID, 4)
	if e != nil || len(got) != 1 || got[0].ID != replacement.ID {
		t.Fatal(got, e)
	}
}

func TestRelationshipTraversalIntersectsAssistantGrant(t *testing.T) {
	f, k, p := knowledgeFixture(t)
	ctx := context.Background()
	a := integration.MemoryTarget{ProjectID: p.ID, Repository: "github.com/acme/a"}
	targets := []integration.MemoryTarget{a}
	for _, name := range []string{"b", "c", "d"} {
		p, e := f.projects.Register(ctx, "/repos/grant-"+name, name)
		if e != nil {
			t.Fatal(e)
		}
		targets = append(targets, integration.MemoryTarget{ProjectID: p.ID, Repository: "github.com/acme/" + name})
	}
	e := f.deps.Store.Update(ctx, func(tx store.Tx) error {
		for _, target := range targets {
			repo, e := tx.Repositories().Get(ctx, target.ProjectID)
			if e != nil {
				return e
			}
			repo.Remotes = []domain.GitRemote{{Name: "origin", URL: "https://" + target.Repository}}
			if e = tx.Repositories().Upsert(ctx, repo); e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		_, e = k.SaveRelationship(ctx, targets[i].ProjectID, integration.RepositoryRelationship{Source: targets[i], Target: targets[i+1], Type: "package_dependency", Evidence: "manual"}, true)
		if e != nil {
			t.Fatal(e)
		}
	}
	allowed := []string{a.ProjectID, targets[2].ProjectID, targets[3].ProjectID}
	got, e := k.RelationshipsScoped(ctx, a.ProjectID, 4, allowed)
	if e != nil || len(got) != 0 {
		t.Fatal("hidden intermediary connected visible projects", got, e)
	}
}
