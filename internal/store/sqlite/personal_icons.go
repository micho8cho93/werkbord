package sqlite

import (
	"context"
	"fmt"
	"time"

	"werkbord/internal/domain"
	"werkbord/internal/store"
)

// Upgrade live legacy identities together before serving requests. Existing
// library choices win, including hidden workspaces and every display location.
// Each changed identity gets a normal revision and history entry; records,
// collection references and old history remain intact. Reopening is idempotent.
func (d *DB) upgradePersonalIcons(ctx context.Context) error {
	return d.Update(ctx, func(tx store.Tx) error {
		icons, err := tx.Personal().WorkspaceIcons(ctx)
		if err != nil {
			return err
		}
		used := map[string]bool{}
		for _, icon := range icons {
			if domain.LibraryPersonalIcon(icon.Icon) {
				used[icon.Icon] = true
			}
		}
		for _, icon := range icons {
			if domain.LibraryPersonalIcon(icon.Icon) {
				continue
			}
			w, err := tx.Personal().GetWorkspace(ctx, icon.ID)
			if err != nil {
				return err
			}
			next := ""
			for _, candidate := range domain.PersonalIconCandidates(w.Name, domain.PersonalIconContext(w.PersonalWorkspaceSpec)) {
				if !used[candidate] {
					next = candidate
					break
				}
			}
			if next == "" {
				// A full catalogue must never prevent opening an existing library.
				// Keep remaining legacy identities; new assignments fail explicitly.
				d.log.Warn("workspace icon catalogue is full; retained remaining legacy identities")
				break
			}
			w.Icon = next
			if now := time.Now().UTC().Truncate(time.Millisecond); now.After(w.UpdatedAt) {
				w.UpdatedAt = now
			}
			// No collection changes: do not unlink/reclaim ownership provenance.
			repo := personalRepo{tx.(*txn).q}
			if err := repo.update(ctx, "personal_workspaces", &w.PersonalMeta, w.PersonalWorkspaceSpec, ""); err != nil {
				return fmt.Errorf("upgrade workspace icons: %w", err)
			}
			used[next] = true
		}
		return nil
	})
}
