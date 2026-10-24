package service

import (
	"context"
	"devboard/internal/domain"
	"devboard/internal/store"
	"fmt"
)

// Icon assignment is done before preview, and again atomically for direct API writes.
// Approved proposals already contain their exact glyph; a concurrent collision fails closed.
func preparePersonalIcon(ctx context.Context, tx store.Tx, id string, spec *domain.PersonalWorkspaceSpec) error {
	if !domain.ValidPersonalIcon(spec.Icon) {
		return fmt.Errorf("%w: invalid workspace icon", domain.ErrInvalid)
	}
	icons, err := tx.Personal().WorkspaceIcons(ctx)
	if err != nil {
		return err
	}
	used := map[string]bool{}
	name := spec.Name
	for _, existing := range icons {
		if existing.ID == id {
			if spec.Icon == existing.Icon {
				name = existing.Name
			}
			continue
		}
		used[domain.DisplayPersonalIcon(existing.Icon, existing.Name)] = true
	}
	if domain.GeneratedPersonalIcon(spec.Icon) {
		if used[spec.Icon] {
			return fmt.Errorf("%w: that workspace icon is already used; choose a new icon or ask Salie for a fresh preview", domain.ErrConflict)
		}
		return nil
	}
	for attempt := uint32(0); attempt <= domain.PersonalIconMask; attempt++ {
		candidate := domain.PersonalIconCandidate(name, spec.Icon, attempt)
		if domain.GeneratedPersonalIcon(candidate) && !used[candidate] {
			spec.Icon = candidate
			return nil
		}
	}
	return fmt.Errorf("%w: no unused workspace icon is available", domain.ErrConflict)
}
