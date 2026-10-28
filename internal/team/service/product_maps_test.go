package service

import (
	"errors"
	"strings"
	"testing"
	"time"
	"werkbord/internal/integration"
	"werkbord/internal/team/domain"
)

func sampleMap() integration.ProductMapSnapshot {
	return integration.ProductMapSnapshot{Schema: integration.ProductMapSchema, ID: "map_sample", Version: 1, Repository: "github.com/acme/shop", Branch: "main", Commit: strings.Repeat("a", 40), Tree: strings.Repeat("b", 40), SourceDigest: strings.Repeat("c", 64), Rules: 1, Provider: "codex", Model: "test", CreatedAt: time.Now().UTC(), FilesRead: 1, FilesAvailable: 1, Content: integration.ProductMapContent{Features: []integration.ProductFeature{{ID: "checkout", Name: "Checkout", Description: "Purchase items", Scenarios: []string{"Purchases create orders"}, Evidence: []integration.ProductEvidence{{Path: "checkout.go", Start: 1, End: 3}}}}, Relationships: []integration.ProductRelationship{}, Limitations: []string{"Runtime not executed"}}}
}
func setupMapTeam(t *testing.T) *team {
	tm := newTeam(t)
	repo := "https://github.com/acme/shop.git"
	p, err := tm.svc.UpdateProject(bg, tm.owner, tm.pid(), ProjectPatch{Repository: &repo})
	if err != nil {
		t.Fatal(err)
	}
	tm.project = p
	return tm
}
func TestProductMapPublicationIsReviewedScopedAndIdempotent(t *testing.T) {
	tm := setupMapTeam(t)
	snapshot := sampleMap()
	proposal := ProductMapProposal{ID: "mapshare_1", Snapshot: snapshot}
	r, err := tm.svc.ProposeProductMap(bg, tm.bo, tm.pid(), proposal)
	if err != nil {
		t.Fatal(err)
	}
	again, err := tm.svc.ProposeProductMap(bg, tm.bo, tm.pid(), proposal)
	if err != nil || again.ID != r.ID {
		t.Fatal("retry was not idempotent", err)
	}
	changed := proposal
	changed.Snapshot.Model = "different"
	if _, err = tm.svc.ProposeProductMap(bg, tm.bo, tm.pid(), changed); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("replayed ID replaced document", err)
	}
	view, err := tm.svc.ProductMaps(bg, tm.cy, tm.pid())
	if err != nil || len(view.Records) != 0 {
		t.Fatal("unpublished proposal visible to another contributor", err)
	}
	decision := ProductMapDecision{Decision: "publish", Digest: ProductMapDigest(snapshot)}
	if _, err = tm.svc.ReviewProductMap(bg, tm.bo, tm.pid(), r.ID, decision); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("contributor published", err)
	}
	bad := decision
	bad.Digest = strings.Repeat("0", 64)
	if _, err = tm.svc.ReviewProductMap(bg, tm.di, tm.pid(), r.ID, bad); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("unseen digest published", err)
	}
	published, err := tm.svc.ReviewProductMap(bg, tm.di, tm.pid(), r.ID, decision)
	if err != nil || published.State != "published" || published.Version != 1 {
		t.Fatalf("publication: %+v %v", published, err)
	}
	view, err = tm.svc.ProductMaps(bg, tm.cy, tm.pid())
	if err != nil || len(view.Records) != 1 || view.Freshness != "unknown" {
		t.Fatalf("shared view: %+v %v", view, err)
	}
	if _, err = tm.svc.ReviewProductMap(bg, tm.di, tm.pid(), r.ID, decision); err != nil {
		t.Fatal("publish retry", err)
	}
	if err = tm.svc.RemoveProjectMember(bg, tm.owner, tm.pid(), tm.cy.Member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = tm.svc.ProductMaps(bg, tm.cy, tm.pid()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("removed member retained access", err)
	}
}
func TestProductMapsRejectWrongRepositoriesAndConcurrentPublication(t *testing.T) {
	tm := setupMapTeam(t)
	bad := sampleMap()
	bad.Repository = "github.com/other/repo"
	if _, err := tm.svc.ProposeProductMap(bg, tm.bo, tm.pid(), ProductMapProposal{ID: "wrong", Snapshot: bad}); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("cross-repository snapshot", err)
	}
	first := ProductMapProposal{ID: "first", Snapshot: sampleMap()}
	second := ProductMapProposal{ID: "second", Snapshot: sampleMap()}
	a, err := tm.svc.ProposeProductMap(bg, tm.bo, tm.pid(), first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := tm.svc.ProposeProductMap(bg, tm.di, tm.pid(), second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tm.svc.ReviewProductMap(bg, tm.owner, tm.pid(), a.ID, ProductMapDecision{Decision: "publish", Digest: ProductMapDigest(a.Snapshot)}); err != nil {
		t.Fatal(err)
	}
	if _, err = tm.svc.ReviewProductMap(bg, tm.di, tm.pid(), b.ID, ProductMapDecision{Decision: "publish", Digest: ProductMapDigest(b.Snapshot)}); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("stale map overwrote published map", err)
	}
}

func TestMapPublicationRechecksPremiumAndAllowsExplicitSelfReview(t *testing.T) {
	tm := setupMapTeam(t)
	proposal := ProductMapProposal{ID: "self_review", Snapshot: sampleMap()}
	record, err := tm.svc.ProposeProductMap(bg, tm.di, tm.pid(), proposal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tm.svc.ReviewProductMap(bg, tm.di, tm.pid(), record.ID, ProductMapDecision{Decision: "publish", Digest: ProductMapDigest(record.Snapshot)}); err != nil {
		t.Fatal("explicit self review refused", err)
	}
	tm.svc.EnforcePlan(func(string) (string, error) { return "", nil })
	next := ProductMapProposal{ID: "premium_required", ExpectedVersion: 1, Snapshot: sampleMap()}
	if _, err = tm.svc.ProposeProductMap(bg, tm.di, tm.pid(), next); !errors.Is(err, domain.ErrPremiumRequired) {
		t.Fatal("map write bypassed Premium gate", err)
	}
	if _, err = tm.svc.ProductMaps(bg, tm.di, tm.pid()); err != nil {
		t.Fatal("Premium gate blocked reading", err)
	}
}
