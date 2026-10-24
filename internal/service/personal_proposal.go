package service

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"devboard/internal/domain"
	"devboard/internal/store"
)

// personalCreationKey is stable across model retries, sessions and controller restarts.
// A person can create a distinct workspace by choosing a distinct name.
func personalCreationKey(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}
func personalDraftID(prefix, key string) string {
	hash := sha256.Sum256([]byte(key))
	return prefix + "_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(hash[:10]))
}

// PrepareChange assigns stable IDs to new collection aliases before the preview
// is stored. Existing IDs and read bindings can never be replaced by an edit.
func (s *Personal) PrepareChange(ctx context.Context, change domain.PersonalChange) (domain.PersonalChange, error) {
	if change.WorkspaceID == "" {
		// Planning does not mutate the caller's draft slices.
		change.Collections = append([]domain.PersonalCollectionChange(nil), change.Collections...)
		change.Workspace.CollectionIDs = append([]string(nil), change.Workspace.CollectionIDs...)
		change.Workspace.Definition.Views = append([]domain.PersonalView(nil), change.Workspace.Definition.Views...)
		key := personalCreationKey(change.Workspace.Name)
		aliases := map[string]string{}
		for i := range change.Collections {
			c := &change.Collections[i]
			if c.Revision > 0 {
				continue
			}
			if c.ID == "" || aliases[c.ID] != "" {
				return change, domain.ErrInvalid
			}
			aliases[c.ID] = personalDraftID(domain.PrefixPersonalCollection, key+"/"+c.ID)
			c.ID = aliases[c.ID]
		}
		for i, id := range change.Workspace.CollectionIDs {
			if mapped := aliases[id]; mapped != "" {
				change.Workspace.CollectionIDs[i] = mapped
			}
		}
		for i := range change.Workspace.Definition.Views {
			v := &change.Workspace.Definition.Views[i]
			if mapped := aliases[v.CollectionID]; mapped != "" {
				v.CollectionID = mapped
			}
		}
	}
	err := s.Store.View(ctx, func(tx store.Tx) error {
		if err := preparePersonalIcon(ctx, tx, change.WorkspaceID, &change.Workspace); err != nil {
			return err
		}
		return s.checkChange(ctx, tx, change)
	})
	return change, err
}
func (s *Personal) checkChange(ctx context.Context, tx store.Tx, ch domain.PersonalChange) error {
	if !domain.GeneratedPersonalIcon(ch.Workspace.Icon) {
		return fmt.Errorf("%w: ask Salie for a fresh workspace icon preview", domain.ErrConflict)
	}
	if err := preparePersonalIcon(ctx, tx, ch.WorkspaceID, &ch.Workspace); err != nil {
		return err
	}
	if len(ch.Collections) > 32 {
		return domain.ErrInvalid
	}
	for _, view := range ch.Workspace.Definition.Views {
		switch view.Kind {
		case "table", "list", "checklist", "progress", "status", "chart":
		default:
			return fmt.Errorf("%w: view is not registered in the personal renderer", domain.ErrInvalid)
		}
	}
	schemas := map[string]domain.PersonalCollectionSpec{}
	for _, c := range ch.Collections {
		if !domain.ValidPersonalID(c.ID, domain.PrefixPersonalCollection) {
			return domain.ErrInvalid
		}
		if _, duplicate := schemas[c.ID]; duplicate {
			return domain.ErrInvalid
		}
		if err := domain.ValidatePersonalCollection(c.Spec); err != nil {
			return err
		}
		schemas[c.ID] = c.Spec
	}
	if len(schemas) != len(ch.Workspace.CollectionIDs) {
		return fmt.Errorf("%w: include every bound collection schema", domain.ErrInvalid)
	}
	// Phase 2 has no renderer for external read bindings yet. Never generate an
	// unsupported interface or use a personal proposal to grant project access.
	if len(ch.Workspace.ReadBindings) != 0 {
		return fmt.Errorf("%w: external read bindings are not supported by this renderer", domain.ErrInvalid)
	}
	if ch.WorkspaceID == "" {
		if ch.Revision != 0 {
			return domain.ErrInvalid
		}
		if id, err := tx.Personal().WorkspaceRequest(ctx, personalCreationKey(ch.Workspace.Name)); err == nil {
			return fmt.Errorf("%w: workspace already saved as %s; reopen it with get_personal_workspace", domain.ErrDuplicate, id)
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		for _, c := range ch.Collections {
			if c.Revision > 0 {
				current, err := tx.Personal().GetCollection(ctx, c.ID)
				if err != nil {
					return err
				}
				if err = personalRevision(current.Revision, c.Revision); err != nil {
					return err
				}
				if !reflect.DeepEqual(current.PersonalCollectionSpec, c.Spec) {
					return fmt.Errorf("%w: reuse the saved collection schema unchanged", domain.ErrInvalid)
				}
			} else if c.Revision != 0 {
				return domain.ErrInvalid
			}
		}
	} else {
		if err := personalIDCheck(ch.WorkspaceID, domain.PrefixPersonalWorkspace); err != nil {
			return err
		}
		old, err := tx.Personal().GetWorkspace(ctx, ch.WorkspaceID)
		if err != nil {
			return err
		}
		if err = personalRevision(old.Revision, ch.Revision); err != nil {
			return err
		}
		if !slices.Equal(old.CollectionIDs, ch.Workspace.CollectionIDs) || !slices.Equal(old.ReadBindings, ch.Workspace.ReadBindings) {
			return fmt.Errorf("%w: preserve saved collection IDs, order and data bindings", domain.ErrInvalid)
		}
		for _, c := range ch.Collections {
			current, err := tx.Personal().GetCollection(ctx, c.ID)
			if err != nil {
				return err
			}
			if err = personalRevision(current.Revision, c.Revision); err != nil {
				return err
			}
			if err = domain.ValidatePersonalEvolution(current.PersonalCollectionSpec, c.Spec); err != nil {
				return fmt.Errorf("%w; destructive schema changes require explicit migration review and are not supported by Salie", err)
			}
		}
	}
	return domain.ValidatePersonalWorkspace(ch.Workspace, schemas)
}

// ApplyChange revalidates everything in the same transaction as all writes.
// It has no record mutation capability. Failure rolls back collections and history.
func (s *Personal) ApplyChange(ctx context.Context, ch domain.PersonalChange) (*domain.PersonalWorkspace, error) {
	var w *domain.PersonalWorkspace
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		if err := s.checkChange(ctx, tx, ch); err != nil {
			return err
		}
		for _, draft := range ch.Collections {
			c := &domain.PersonalCollection{PersonalMeta: s.meta(domain.PrefixPersonalCollection), PersonalCollectionSpec: draft.Spec}
			c.ID = draft.ID
			if ch.WorkspaceID == "" {
				if draft.Revision > 0 {
					continue
				}
				if err := tx.Personal().CreateCollection(ctx, c); err != nil {
					return err
				}
			} else {
				current, err := tx.Personal().GetCollection(ctx, c.ID)
				if err != nil {
					return err
				}
				if reflect.DeepEqual(current.PersonalCollectionSpec, draft.Spec) {
					continue
				}
				current.PersonalCollectionSpec, current.UpdatedAt = draft.Spec, s.now()
				if err = tx.Personal().UpdateCollection(ctx, current); err != nil {
					return err
				}
				c = current
			}
			action := "updated"
			if ch.WorkspaceID == "" {
				action = "created"
			}
			if err := personalEvent(em, "collection", action, c.PersonalMeta); err != nil {
				return err
			}
		}
		if ch.WorkspaceID == "" {
			w = &domain.PersonalWorkspace{PersonalMeta: s.meta(domain.PrefixPersonalWorkspace), PersonalWorkspaceSpec: ch.Workspace}
			if err := tx.Personal().CreateWorkspace(ctx, w); err != nil {
				return err
			}
			if err := tx.Personal().SaveWorkspaceRequest(ctx, personalCreationKey(ch.Workspace.Name), w.ID); err != nil {
				return err
			}
			return personalEvent(em, "workspace", "created", w.PersonalMeta)
		}
		var err error
		w, err = tx.Personal().GetWorkspace(ctx, ch.WorkspaceID)
		if err != nil {
			return err
		}
		w.PersonalWorkspaceSpec, w.UpdatedAt = ch.Workspace, s.now()
		if err = tx.Personal().UpdateWorkspace(ctx, w); err != nil {
			return err
		}
		return personalEvent(em, "workspace", "updated", w.PersonalMeta)
	})
	return w, err
}

// Snapshot always loads SQLite, including on a new model's first turn. History
// can be used as a proposed rollback with the current workspace/schema revisions.
func (s *Personal) Snapshot(ctx context.Context, id string, revision int64) (*domain.PersonalSnapshot, error) {
	if err := personalIDCheck(id, domain.PrefixPersonalWorkspace); err != nil {
		return nil, err
	}
	out := &domain.PersonalSnapshot{Collections: []domain.PersonalCollection{}}
	err := s.Store.View(ctx, func(tx store.Tx) error {
		var err error
		if revision > 0 {
			out.Workspace, err = tx.Personal().WorkspaceRevision(ctx, id, revision)
		} else {
			out.Workspace, err = tx.Personal().GetWorkspace(ctx, id)
		}
		if err != nil {
			return err
		}
		for _, cid := range out.Workspace.CollectionIDs {
			c, err := tx.Personal().GetCollection(ctx, cid)
			if err != nil {
				return err
			}
			out.Collections = append(out.Collections, *c)
		}
		return nil
	})
	return out, err
}
