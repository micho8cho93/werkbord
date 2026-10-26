package service

import (
	"testing"
	"time"
	"werkbord/internal/integration"
	"werkbord/internal/team/domain"
)

func TestRelationshipGraphApprovalAndAccessRevocation(t *testing.T) {
	tm, now := memoryGitProject(t)
	other := tm.project2()
	repo := "https://github.com/acme/other"
	if _, e := tm.svc.UpdateProject(bg, tm.owner, other.ID, ProjectPatch{Repository: &repo}); e != nil {
		t.Fatal(e)
	}
	if e := tm.svc.AddProjectMember(bg, tm.owner, other.ID, tm.bo.Member.ID, domain.ProjectContributor); e != nil {
		t.Fatal(e)
	}
	p := memoryProposal(tm.bo, tm.pid())
	r := integration.RepositoryRelationship{Source: integration.MemoryTarget{ProjectID: tm.pid(), Repository: "github.com/acme/shop"}, Target: integration.MemoryTarget{ProjectID: other.ID, Repository: "github.com/acme/other"}, Type: "api_consumer", Evidence: "manual", LastVerified: now}
	p.Scope = integration.SharedMemoryScope{WorkspaceID: tm.bo.Workspace.ID, Kind: "cross_repository", Targets: []integration.MemoryTarget{r.Source, r.Target}}
	p.Content.Relationship = &r
	m, e := tm.svc.ProposeMemory(bg, tm.bo, p)
	if e != nil {
		t.Fatal(e)
	}
	got, e := tm.svc.Relationships(bg, tm.bo, tm.pid(), 4)
	if e != nil || len(got) != 0 {
		t.Fatal("unapproved graph", got, e)
	}
	m, e = tm.svc.ChangeMemory(bg, tm.owner, m.ID, memoryCommand(m, "approve"))
	if e != nil {
		t.Fatal(e)
	}
	m, e = tm.svc.ChangeMemory(bg, tm.owner, m.ID, memoryCommand(m, "publish"))
	if e != nil {
		t.Fatal(e)
	}
	got, e = tm.svc.Relationships(bg, tm.bo, tm.pid(), 4)
	if e != nil || len(got) != 1 {
		t.Fatal(got, e)
	}
	name := "Renamed display name"
	if _, e = tm.svc.UpdateProject(bg, tm.owner, other.ID, ProjectPatch{Name: &name}); e != nil {
		t.Fatal(e)
	}
	got, e = tm.svc.Relationships(bg, tm.bo, tm.pid(), 4)
	if e != nil || len(got) != 1 {
		t.Fatal("display rename lost identity", got, e)
	}
	renamed := "https://github.com/acme/renamed"
	if _, e = tm.svc.UpdateProject(bg, tm.owner, other.ID, ProjectPatch{Repository: &renamed}); e != nil {
		t.Fatal(e)
	}
	got, e = tm.svc.Relationships(bg, tm.bo, tm.pid(), 4)
	if e != nil || len(got) != 0 {
		t.Fatal("remote rename must require rebinding", got, e)
	}
	if _, e = tm.svc.UpdateProject(bg, tm.owner, other.ID, ProjectPatch{Repository: &repo}); e != nil {
		t.Fatal(e)
	}
	archived := true
	if _, e = tm.svc.UpdateProject(bg, tm.owner, other.ID, ProjectPatch{Archived: &archived}); e != nil {
		t.Fatal(e)
	}
	got, e = tm.svc.Relationships(bg, tm.bo, tm.pid(), 4)
	if e != nil || len(got) != 0 {
		t.Fatal("archived repository returned", got, e)
	}
	archived = false
	if _, e = tm.svc.UpdateProject(bg, tm.owner, other.ID, ProjectPatch{Archived: &archived}); e != nil {
		t.Fatal(e)
	}
	*now = now.Add(25 * time.Hour)
	got, e = tm.svc.Relationships(bg, tm.bo, tm.pid(), 4)
	if e != nil || len(got) != 1 || got[0].Status != "stale" {
		t.Fatal(got, e)
	}
	restrictedProposal := p
	restrictedProposal.ClientID = "restricted_relationship"
	restrictedProposal.Content.Classification = "restricted"
	restrictedProposal.Content.Summary = "Restricted API dependency"
	restrictedRecord, err := tm.svc.ProposeMemory(bg, tm.bo, restrictedProposal)
	if err != nil {
		t.Fatal(err)
	}
	restrictedRecord, err = tm.svc.ChangeMemory(bg, tm.owner, restrictedRecord.ID, memoryCommand(restrictedRecord, "approve"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = tm.svc.ChangeMemory(bg, tm.owner, restrictedRecord.ID, memoryCommand(restrictedRecord, "publish"))
	if err != nil {
		t.Fatal(err)
	}
	got, e = tm.svc.Relationships(bg, tm.bo, tm.pid(), 4)
	if e != nil || len(got) != 1 {
		t.Fatal("restricted relationship leaked", got, e)
	}
	if e = tm.svc.RemoveProjectMember(bg, tm.owner, other.ID, tm.bo.Member.ID); e != nil {
		t.Fatal(e)
	}
	got, e = tm.svc.Relationships(bg, tm.bo, tm.pid(), 4)
	if e != nil || len(got) != 0 {
		t.Fatal("hidden relationship leaked", got, e)
	}
	// A cached actor and a starting project cannot recover inaccessible neighbours.
	restricted := tm.bo
	restricted.MCPGrantID = "grant"
	if _, e = tm.svc.Relationships(bg, restricted, tm.pid(), 1); e != domain.ErrForbidden {
		t.Fatal(e)
	}
}
