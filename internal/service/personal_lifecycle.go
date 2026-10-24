package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"devboard/internal/domain"
	"devboard/internal/store"
)

func (s *Personal) CollectionUsage(ctx context.Context, id string) (*domain.PersonalCollectionUsage, error) {
	if err := personalIDCheck(id, domain.PrefixPersonalCollection); err != nil {
		return nil, err
	}
	var out *domain.PersonalCollectionUsage
	err := s.Store.View(ctx, func(tx store.Tx) error { var err error; out, err = tx.Personal().CollectionUsage(ctx, id); return err })
	return out, err
}
func (s *Personal) deletionPlan(ctx context.Context, tx store.Tx, id string, revision int64, mode string) (*domain.PersonalDeletion, error) {
	if mode != "keep_data" && mode != "delete_data" {
		return nil, domain.ErrInvalid
	}
	w, err := tx.Personal().GetWorkspace(ctx, id)
	if err != nil {
		return nil, err
	}
	if err = personalRevision(w.Revision, revision); err != nil {
		return nil, err
	}
	out := &domain.PersonalDeletion{WorkspaceID: id, Revision: revision, Name: w.Name, Mode: mode, Collections: []domain.PersonalDeletionCollection{}}
	for _, cid := range w.CollectionIDs {
		usage, err := tx.Personal().CollectionUsage(ctx, cid)
		if err != nil {
			return nil, err
		}
		out.Collections = append(out.Collections, domain.PersonalDeletionCollection{PersonalCollectionUsage: *usage, Delete: mode == "delete_data" && usage.OwnerWorkspaceID == id && usage.ReferenceCount == 1})
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	out.Fingerprint = hex.EncodeToString(sum[:])
	return out, nil
}
func (s *Personal) PreviewDeletion(ctx context.Context, id string, revision int64, mode string) (*domain.PersonalDeletion, error) {
	if err := personalIDCheck(id, domain.PrefixPersonalWorkspace); err != nil {
		return nil, err
	}
	var out *domain.PersonalDeletion
	err := s.Store.View(ctx, func(tx store.Tx) error {
		var err error
		out, err = s.deletionPlan(ctx, tx, id, revision, mode)
		return err
	})
	return out, err
}

// DeleteWorkspaceReviewed is the single UI/Salie destructive operation. The
// fingerprint binds approval to the exact record population, schemas and usage.
// A concurrent edit, new record, or new dependency invalidates the entire plan.
func (s *Personal) DeleteWorkspaceReviewed(ctx context.Context, id string, request domain.PersonalDeleteRequest) (*domain.PersonalDeletion, error) {
	if err := personalIDCheck(id, domain.PrefixPersonalWorkspace); err != nil {
		return nil, err
	}
	if !request.Confirmed || len(request.Fingerprint) != 64 {
		return nil, fmt.Errorf("%w: review and explicitly confirm workspace deletion", domain.ErrInvalid)
	}
	var out *domain.PersonalDeletion
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var err error
		out, err = s.deletionPlan(ctx, tx, id, request.Revision, request.Mode)
		if err != nil {
			return err
		}
		if out.Fingerprint != request.Fingerprint {
			return fmt.Errorf("%w: personal data or dependencies changed; review deletion again", domain.ErrConflict)
		}
		if err = tx.Personal().DeleteWorkspace(ctx, id, request.Revision); err != nil {
			return err
		}
		for _, c := range out.Collections {
			if !c.Delete {
				continue
			}
			if err = tx.Personal().DeleteCollectionRecords(ctx, c.CollectionID); err != nil {
				return err
			}
			if err = tx.Personal().DeleteCollection(ctx, c.CollectionID, c.Revision); err != nil {
				return err
			}
			if err = personalEvent(em, "collection", "deleted", domain.PersonalMeta{ID: c.CollectionID, Revision: c.Revision}); err != nil {
				return err
			}
		}
		return personalEvent(em, "workspace", "deleted", domain.PersonalMeta{ID: id, Revision: request.Revision})
	})
	return out, err
}
func (s *Personal) WorkspaceHistory(ctx context.Context, id string) ([]domain.PersonalMeta, error) {
	if err := personalIDCheck(id, domain.PrefixPersonalWorkspace); err != nil {
		return nil, err
	}
	var out []domain.PersonalMeta
	err := s.Store.View(ctx, func(tx store.Tx) error {
		if _, err := tx.Personal().GetWorkspace(ctx, id); err != nil {
			return err
		}
		var err error
		out, err = tx.Personal().WorkspaceHistory(ctx, id)
		return err
	})
	return out, err
}

// Rollback creates a new revision; it never rewinds collection schemas or records.
func (s *Personal) RollbackWorkspace(ctx context.Context, id string, revision, target int64) (*domain.PersonalWorkspace, error) {
	if err := personalIDCheck(id, domain.PrefixPersonalWorkspace); err != nil {
		return nil, err
	}
	if target < 1 {
		return nil, domain.ErrInvalid
	}
	var w *domain.PersonalWorkspace
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var err error
		w, err = tx.Personal().GetWorkspace(ctx, id)
		if err != nil {
			return err
		}
		if err = personalRevision(w.Revision, revision); err != nil {
			return err
		}
		old, err := tx.Personal().WorkspaceRevision(ctx, id, target)
		if err != nil {
			return err
		}
		if err = preparePersonalIcon(ctx, tx, id, &old.PersonalWorkspaceSpec); err != nil {
			return err
		}
		if err = s.validateWorkspace(ctx, tx, old.PersonalWorkspaceSpec); err != nil {
			return err
		}
		w.PersonalWorkspaceSpec, w.UpdatedAt = old.PersonalWorkspaceSpec, s.now()
		if err = tx.Personal().UpdateWorkspace(ctx, w); err != nil {
			return err
		}
		return personalEvent(em, "workspace", "updated", w.PersonalMeta)
	})
	return w, err
}
