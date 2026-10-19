package service

import (
	"context"
	"devboard/internal/domain"
	"devboard/internal/store"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func publicationFixture(t *testing.T) (*fixture, *Memory, domain.PublicationGrant, domain.MemoryScope) {
	t.Helper()
	f := newFixture(t)
	ctx := context.Background()
	p, err := f.projects.Register(ctx, "/repos/publication", "Publication")
	if err != nil {
		t.Fatal(err)
	}
	scope := domain.MemoryScope{OwnerID: "individual", ProjectID: p.ID, Repository: "github.com/acme/share"}
	err = f.deps.Store.Update(ctx, func(tx store.Tx) error {
		r, e := tx.Repositories().Get(ctx, p.ID)
		if e != nil {
			return e
		}
		r.Remotes = []domain.GitRemote{{Name: "origin", URL: "https://github.com/acme/share"}}
		if e = tx.Repositories().Upsert(ctx, r); e != nil {
			return e
		}
		now := f.deps.now()
		return tx.Memory().SaveFreshness(ctx, &domain.MemoryFreshness{Scope: scope, Revision: domain.MemoryRepositoryRevision{Head: strings.Repeat("a", 40)}, CheckedAt: now, SucceededAt: &now})
	})
	if err != nil {
		t.Fatal(err)
	}
	m := memoryService(t, f, "individual", domain.MemoryLimits{})
	g := domain.PublicationGrant{WorkspaceID: "ws_one", ProjectID: "prj_team", LocalProjectID: p.ID, Repository: scope.Repository, Enabled: true}
	if err = m.SetPublicationGrant(ctx, g); err != nil {
		t.Fatal(err)
	}
	return f, m, g, scope
}
func TestMemoryPublicationAllowlistAndSemanticReview(t *testing.T) {
	f, s, g, scope := publicationFixture(t)
	ctx := context.Background()
	now := f.deps.now()
	c := domain.MemoryContent{Type: "git_head", Authority: domain.MemoryObservation, State: domain.MemoryCurrent, Source: domain.MemoryProvenance{Type: domain.MemorySourceGit, Reference: "git:head", GitSHA: strings.Repeat("a", 40), Branch: "refs/heads/secret-name", Paths: []string{"private/file.go"}}, Summary: "PRIVATE raw source code and sk-secret environment: PRIVATE_ENV=value", VerifiedAt: &now, Details: json.RawMessage(`{"private":"transcript"}`)}
	git := createMemory(t, s, scope, c)
	c.Type = "decision"
	c.Authority = domain.MemoryDecision
	c.Source = domain.MemoryProvenance{Type: domain.MemorySourceUser, Reference: "user:decision", ApprovedBy: "individual", ApprovalRef: "review:decision"}
	c.Summary = "Private architecture context"
	c.Details = nil
	semantic := createMemory(t, s, scope, c)
	c.Type = "preference"
	c.Authority = domain.MemoryPreference
	c.Summary = "My personal preference"
	createMemory(t, s, scope, c)
	events, err := s.PublicationEvents(ctx, g.WorkspaceID)
	if err != nil || len(events.Events) != 1 || events.Events[0].SourceID != git.ID {
		t.Fatalf("allowlist: %+v %v", events, err)
	}
	raw, _ := json.Marshal(events)
	for _, secret := range []string{"PRIVATE", "secret-name", "private/file", "transcript", "Private architecture", "personal preference"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("export contains %s", secret)
		}
	}
	if err = s.ReviewPublication(ctx, g.WorkspaceID, g.ProjectID, semantic.ID, semantic.Version, "Use explicit transactions"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("semantic opt-in missing: %v", err)
	}
	g.Semantic = true
	if err = s.SetPublicationGrant(ctx, g); err != nil {
		t.Fatal(err)
	}
	ps, err := s.ReconcilePublication(ctx, g.WorkspaceID)
	if err != nil || len(ps) != 2 {
		t.Fatalf("candidate: %+v %v", ps, err)
	}
	if err = s.ReviewPublication(ctx, g.WorkspaceID, g.ProjectID, semantic.ID, semantic.Version+1, "Use explicit transactions"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale approval accepted: %v", err)
	}
	if err = s.ReviewPublication(ctx, g.WorkspaceID, g.ProjectID, semantic.ID, semantic.Version, "Use explicit transactions"); err != nil {
		t.Fatal(err)
	}
	events, err = s.PublicationEvents(ctx, g.WorkspaceID)
	if err != nil || len(events.Events) != 2 || events.Events[1].Proposal.Content.Summary != "Use explicit transactions" {
		t.Fatalf("reviewed projection: %+v %v", events, err)
	}
	// Withdrawing before delivery cancels the previously reviewed content too.
	if err = s.RevokePublication(ctx, g.WorkspaceID, g.ProjectID, semantic.ID); err != nil {
		t.Fatal(err)
	}
	events, err = s.PublicationEvents(ctx, g.WorkspaceID)
	if err != nil || events.Events[1].Action != "cancel" || events.Events[1].Proposal.Content.Summary != "Publication withdrawn before delivery" {
		t.Fatalf("revoked pending content leaked: %+v %v", events, err)
	}
}
func TestMemoryPublicationRejectsAmbiguitySuspensionAndRebinding(t *testing.T) {
	f, s, g, scope := publicationFixture(t)
	ctx := context.Background()
	bad := g
	bad.Repository = "github.com/acme/unauthorised"
	if err := s.SetPublicationGrant(ctx, bad); err == nil {
		t.Fatal("unmatched repository accepted")
	}
	p, err := f.projects.Register(ctx, "/repos/duplicate-publication", "Duplicate")
	if err != nil {
		t.Fatal(err)
	}
	err = f.deps.Store.Update(ctx, func(tx store.Tx) error {
		r, e := tx.Repositories().Get(ctx, p.ID)
		if e != nil {
			return e
		}
		r.Remotes = []domain.GitRemote{{Name: "origin", URL: "https://github.com/acme/share"}}
		return tx.Repositories().Upsert(ctx, r)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetPublicationGrant(ctx, g); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("ambiguous association: %v", err)
	}
	// Remove ambiguity and explicitly suspend repository verification.
	err = f.deps.Store.Update(ctx, func(tx store.Tx) error {
		r, e := tx.Repositories().Get(ctx, p.ID)
		if e != nil {
			return e
		}
		r.Remotes = nil
		if e = tx.Repositories().Upsert(ctx, r); e != nil {
			return e
		}
		state, e := tx.Memory().Freshness(ctx, scope)
		if e != nil {
			return e
		}
		state.Problem = "repository_unavailable_or_ambiguous"
		state.CheckedAt = time.Now()
		return tx.Memory().SaveFreshness(ctx, state)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetPublicationGrant(ctx, g); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("suspended association: %v", err)
	}
	g.Enabled = false
	if err = s.SetPublicationGrant(ctx, g); err != nil {
		t.Fatalf("cannot disable suspended association: %v", err)
	}
}
func TestMemoryPublicationWorkspacesHaveIndependentStreams(t *testing.T) {
	f, s, g, scope := publicationFixture(t)
	ctx := context.Background()
	now := f.deps.now()
	createMemory(t, s, scope, domain.MemoryContent{Type: "git_head", Authority: domain.MemoryObservation, State: domain.MemoryCurrent, Source: domain.MemoryProvenance{Type: domain.MemorySourceGit, Reference: "git:head", GitSHA: strings.Repeat("a", 40)}, Summary: "private summary", VerifiedAt: &now})
	other := g
	other.WorkspaceID = "ws_two"
	if err := s.SetPublicationGrant(ctx, other); err != nil {
		t.Fatal(err)
	}
	a, ea := s.PublicationEvents(ctx, g.WorkspaceID)
	b, eb := s.PublicationEvents(ctx, other.WorkspaceID)
	if ea != nil || eb != nil || len(a.Events) != 1 || len(b.Events) != 1 || a.Events[0].Sequence != 1 || b.Events[0].Sequence != 1 || a.Events[0].ID == b.Events[0].ID {
		t.Fatalf("streams overlap: %+v %+v %v %v", a, b, ea, eb)
	}
	g.Enabled = false
	if err := s.SetPublicationGrant(ctx, g); err != nil {
		t.Fatal(err)
	}
	a, ea = s.PublicationEvents(ctx, g.WorkspaceID)
	b, eb = s.PublicationEvents(ctx, other.WorkspaceID)
	if ea != nil || eb != nil || a.Events[0].Action != "cancel" || b.Events[0].Action != "propose" {
		t.Fatalf("grant leaked across workspaces: %+v %+v %v %v", a, b, ea, eb)
	}
}

func TestMemoryPublicationProjectDecisionNeedsExactReviewAndOfflineMaintenance(t *testing.T) {
	f, s, g, scope := publicationFixture(t)
	ctx := context.Background()
	g.Semantic = true
	if err := s.SetPublicationGrant(ctx, g); err != nil {
		t.Fatal(err)
	}
	k := &Knowledge{Memory: s}
	decision, err := k.SaveManual(ctx, scope.ProjectID, ManualMemory{Kind: "decision", Summary: "Local architectural context"})
	if err != nil {
		t.Fatal(err)
	}
	if err = k.Maintain(ctx); err != nil {
		t.Fatal(err)
	}
	ps, err := s.ReconcilePublication(ctx, g.WorkspaceID)
	if err != nil || len(ps) != 1 || ps[0].State != "awaiting_approval" || ps[0].SourceRepository != "" {
		t.Fatalf("project candidate not reviewable: %+v %v", ps, err)
	}
	if err = s.ReviewPublication(ctx, g.WorkspaceID, g.ProjectID, decision.ID, decision.Version, "Use transactional storage for shared coordination"); err != nil {
		t.Fatal(err)
	}
	// Check the journal directly, without a connection or export-triggered reconcile.
	err = f.deps.Store.View(ctx, func(tx store.Tx) error {
		page, e := tx.Memory().PublicationEvents(ctx, s.owner, g.WorkspaceID)
		if e != nil {
			return e
		}
		if len(page.Events) != 1 || page.Events[0].Proposal.Scope.Repository != g.Repository || strings.Contains(page.Events[0].Proposal.Content.Summary, "Local architectural") {
			t.Fatalf("offline reviewed projection: %+v", page)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
