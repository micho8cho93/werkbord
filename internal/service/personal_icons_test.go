package service

import (
	"context"
	"devboard/internal/domain"
	"errors"
	"testing"
)

func iconWorkspace(name, icon string) domain.PersonalWorkspaceSpec {
	return domain.PersonalWorkspaceSpec{Name: name, Icon: icon, UISchemaVersion: 1, Visibility: domain.PersonalVisibility{Individual: true}, Definition: domain.PersonalUI{Layout: domain.PersonalNode{Type: "text", Text: "Personal notes"}}}
}

func TestPersonalWorkspaceIconsCannotReuseAppOrWorkspaceIcons(t *testing.T) {
	f := newFixture(t)
	s := &Personal{Deps: f.deps}
	ctx := context.Background()
	first, err := s.CreateWorkspace(ctx, iconWorkspace("My notes", "check"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateWorkspace(ctx, iconWorkspace("My notes", "check"))
	if err != nil {
		t.Fatal(err)
	}
	if !domain.GeneratedPersonalIcon(first.Icon) || !domain.GeneratedPersonalIcon(second.Icon) || first.Icon == second.Icon {
		t.Fatalf("icons collide: %q %q", first.Icon, second.Icon)
	}
	spec := first.PersonalWorkspaceSpec
	spec.Icon = second.Icon
	if _, err := s.UpdateWorkspace(ctx, first.ID, first.Revision, spec); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate icon update: %v", err)
	}
	spec = first.PersonalWorkspaceSpec
	spec.Name = "Renamed notes"
	renamed, err := s.UpdateWorkspace(ctx, first.ID, first.Revision, spec)
	if err != nil || renamed.Icon != first.Icon {
		t.Fatalf("rename changed identity: %+v %v", renamed, err)
	}
}

func TestPersonalIconApprovalPreservesExactPreviewAndRejectsConcurrentCollision(t *testing.T) {
	f := newFixture(t)
	s := &Personal{Deps: f.deps}
	ctx := context.Background()
	first, err := s.PrepareChange(ctx, domain.PersonalChange{Workspace: iconWorkspace("One", "auto")})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.PrepareChange(ctx, domain.PersonalChange{Workspace: iconWorkspace("Two", first.Workspace.Icon)})
	if err != nil {
		t.Fatal(err)
	}
	approved := first.Workspace.Icon
	w, err := s.ApplyChange(ctx, first)
	if err != nil || w.Icon != approved {
		t.Fatalf("approved glyph changed: %+v %v", w, err)
	}
	if _, err := s.ApplyChange(ctx, second); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("concurrent preview reused glyph: %v", err)
	}
	workspaces, err := s.ListWorkspaces(ctx, domain.PersonalQuery{Limit: 100})
	if err != nil || len(workspaces) != 1 {
		t.Fatalf("partial writes after collision: %+v %v", workspaces, err)
	}
	fresh, err := s.PrepareChange(ctx, domain.PersonalChange{Workspace: iconWorkspace("Two", "auto")})
	if err != nil || fresh.Workspace.Icon == approved {
		t.Fatalf("fresh preview reused icon: %+v %v", fresh, err)
	}
}
