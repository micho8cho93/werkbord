package service

import (
	"context"
	"devboard/internal/domain"
	"devboard/internal/store"
	"errors"
	"testing"
)

func iconWorkspace(name, icon string) domain.PersonalWorkspaceSpec {
	return domain.PersonalWorkspaceSpec{Name: name, Icon: icon, UISchemaVersion: 1, Visibility: domain.PersonalVisibility{Individual: true}, Definition: domain.PersonalUI{Layout: domain.PersonalNode{Type: "text", Text: "Personal notes"}}}
}

func TestPersonalWorkspaceIconsChooseUnusedLibraryMatches(t *testing.T) {
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
	if !domain.LibraryPersonalIcon(first.Icon) || !domain.LibraryPersonalIcon(second.Icon) || first.Icon == second.Icon {
		t.Fatalf("icons collide: %q %q", first.Icon, second.Icon)
	}
	teamSpec := iconWorkspace("My notes", "auto")
	teamSpec.Visibility = domain.PersonalVisibility{TeamWorkspaceIDs: []string{"team:main"}}
	team, err := s.CreateWorkspace(ctx, teamSpec)
	if err != nil || team.Icon == first.Icon || team.Icon == second.Icon {
		t.Fatalf("Team display reused another location's icon: %+v %v", team, err)
	}
	hiddenSpec := iconWorkspace("My notes", "auto")
	hiddenSpec.Visibility = domain.PersonalVisibility{}
	hidden, err := s.CreateWorkspace(ctx, hiddenSpec)
	if err != nil || hidden.Icon == first.Icon || hidden.Icon == second.Icon || hidden.Icon == team.Icon {
		t.Fatalf("hidden workspace reused a live icon: %+v %v", hidden, err)
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

func TestPersonalIconReplacementAndDeletedIdentityReuse(t *testing.T) {
	f := newFixture(t)
	s := &Personal{Deps: f.deps}
	ctx := context.Background()
	first, err := s.CreateWorkspace(ctx, iconWorkspace("Reading journal", "auto"))
	if err != nil {
		t.Fatal(err)
	}
	old := first.Icon
	spec := first.PersonalWorkspaceSpec
	spec.Icon = "auto"
	changed, err := s.UpdateWorkspace(ctx, first.ID, first.Revision, spec)
	if err != nil || changed.Icon == old {
		t.Fatalf("replacement did not change the icon: %+v %v", changed, err)
	}
	second, err := s.CreateWorkspace(ctx, iconWorkspace("Reading journal", "auto"))
	if err != nil || second.Icon != old {
		t.Fatalf("free icon was not reused: %+v %v", second, err)
	}
	if err := s.DeleteWorkspace(ctx, second.ID, second.Revision); err != nil {
		t.Fatal(err)
	}
	third, err := s.CreateWorkspace(ctx, iconWorkspace("Reading journal", "auto"))
	if err != nil || third.Icon != old {
		t.Fatalf("deleted identity reserved its icon: %+v %v", third, err)
	}
}

func TestPersonalIconCatalogueExhaustionNeverDuplicates(t *testing.T) {
	f := newFixture(t)
	s := &Personal{Deps: f.deps}
	ctx := context.Background()
	if err := f.deps.Store.Update(ctx, func(tx store.Tx) error {
		for _, icon := range domain.PersonalIcons() {
			w := &domain.PersonalWorkspace{PersonalMeta: s.meta(domain.PrefixPersonalWorkspace), PersonalWorkspaceSpec: iconWorkspace("Used", icon)}
			if err := tx.Personal().CreateWorkspace(ctx, w); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateWorkspace(ctx, iconWorkspace("One too many", "auto")); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("full catalogue should refuse another assignment: %v", err)
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
