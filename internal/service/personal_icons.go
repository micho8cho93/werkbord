package service

import (
	"context"
	"fmt"
	"werkbord/internal/domain"
	"werkbord/internal/store"
)

// Icon assignment is done before preview, and again atomically for direct API writes.
// Approved proposals already contain their exact library icon; a concurrent collision fails closed.
func preparePersonalIcon(ctx context.Context, tx store.Tx, id string, spec *domain.PersonalWorkspaceSpec) error {
	if !domain.ValidPersonalIcon(spec.Icon) {
		return fmt.Errorf("%w: invalid workspace icon", domain.ErrInvalid)
	}
	icons, err := tx.Personal().WorkspaceIcons(ctx)
	if err != nil {
		return err
	}
	used := map[string]bool{}
	for _, existing := range icons {
		if existing.ID == id {
			// A request for a replacement must not select the same icon again.
			if spec.Icon == "auto" {
				used[domain.DisplayPersonalIcon(existing.Icon, existing.Name)] = true
			}
			continue
		}
		used[domain.DisplayPersonalIcon(existing.Icon, existing.Name)] = true
	}
	if domain.LibraryPersonalIcon(spec.Icon) {
		if used[spec.Icon] {
			return fmt.Errorf("%w: that workspace icon is already used; choose a new icon or ask Salie for a fresh preview", domain.ErrConflict)
		}
		return nil
	}
	for _, candidate := range domain.PersonalIconCandidates(spec.Name, domain.PersonalIconContext(*spec)) {
		if !used[candidate] {
			spec.Icon = candidate
			return nil
		}
	}
	return fmt.Errorf("%w: no unused workspace icon is available", domain.ErrConflict)
}
