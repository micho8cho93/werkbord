package service

import (
	"context"
	"encoding/json"
	"fmt"

	"devboard/internal/domain"
	"devboard/internal/store"
)

// Personal is private local owner data. Only the authenticated loopback owner
// API and owner assistant proposals wire this service. It is absent from MCP and the
// desktop/Team bridges; a display location grants no access to another product.
type Personal struct{ Deps }

func personalIDCheck(id, prefix string) error {
	if !domain.ValidPersonalID(id, prefix) {
		return fmt.Errorf("%w: invalid personal identifier", domain.ErrInvalid)
	}
	return nil
}
func personalRevision(got, want int64) error {
	if want < 1 {
		return fmt.Errorf("%w: a positive revision is required", domain.ErrInvalid)
	}
	if got != want {
		return domain.ErrConflict
	}
	return nil
}
func (s *Personal) meta(prefix string) domain.PersonalMeta {
	now := s.now()
	return domain.PersonalMeta{ID: domain.NewID(prefix), CreatedAt: now, UpdatedAt: now}
}

// Events announce invalidation only: private definitions and values never enter
// the existing broadly consumed event stream.
func personalEvent(em *emitter, kind, action string, meta domain.PersonalMeta) error {
	return em.emit(newEvent(domain.EventType("personal."+kind+"."+action), struct {
		ID       string `json:"id"`
		Revision int64  `json:"revision"`
	}{meta.ID, meta.Revision}))
}
func (s *Personal) validateWorkspace(ctx context.Context, tx store.Tx, spec domain.PersonalWorkspaceSpec) error {
	// Bound reference lookups before touching storage.
	if len(spec.CollectionIDs) > 32 || len(spec.ReadBindings) > 16 {
		return domain.ErrInvalid
	}
	collections := map[string]domain.PersonalCollectionSpec{}
	for _, id := range spec.CollectionIDs {
		if err := personalIDCheck(id, domain.PrefixPersonalCollection); err != nil {
			return err
		}
		c, err := tx.Personal().GetCollection(ctx, id)
		if err != nil {
			return err
		}
		collections[id] = c.PersonalCollectionSpec
	}
	if err := domain.ValidatePersonalWorkspace(spec, collections); err != nil {
		return err
	}
	for _, b := range spec.ReadBindings {
		if _, err := tx.Projects().Get(ctx, b.ProjectID); err != nil {
			return err
		}
	}
	return nil
}
func (s *Personal) CreateWorkspace(ctx context.Context, spec domain.PersonalWorkspaceSpec) (*domain.PersonalWorkspace, error) {
	w := &domain.PersonalWorkspace{PersonalMeta: s.meta(domain.PrefixPersonalWorkspace), PersonalWorkspaceSpec: spec}
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		if err := s.validateWorkspace(ctx, tx, spec); err != nil {
			return err
		}
		if err := tx.Personal().CreateWorkspace(ctx, w); err != nil {
			return err
		}
		return personalEvent(em, "workspace", "created", w.PersonalMeta)
	})
	return w, err
}
func (s *Personal) UpdateWorkspace(ctx context.Context, id string, revision int64, spec domain.PersonalWorkspaceSpec) (*domain.PersonalWorkspace, error) {
	if err := personalIDCheck(id, domain.PrefixPersonalWorkspace); err != nil {
		return nil, err
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
		if err = s.validateWorkspace(ctx, tx, spec); err != nil {
			return err
		}
		w.PersonalWorkspaceSpec, w.UpdatedAt = spec, s.now()
		if err = tx.Personal().UpdateWorkspace(ctx, w); err != nil {
			return err
		}
		return personalEvent(em, "workspace", "updated", w.PersonalMeta)
	})
	return w, err
}
func (s *Personal) GetWorkspace(ctx context.Context, id string) (*domain.PersonalWorkspace, error) {
	if err := personalIDCheck(id, domain.PrefixPersonalWorkspace); err != nil {
		return nil, err
	}
	var w *domain.PersonalWorkspace
	err := s.Store.View(ctx, func(tx store.Tx) error { var err error; w, err = tx.Personal().GetWorkspace(ctx, id); return err })
	return w, err
}
func (s *Personal) ListWorkspaces(ctx context.Context, q domain.PersonalQuery) ([]domain.PersonalWorkspace, error) {
	if err := domain.ValidatePersonalQuery(q, domain.PrefixPersonalWorkspace); err != nil {
		return nil, err
	}
	var out []domain.PersonalWorkspace
	err := s.Store.View(ctx, func(tx store.Tx) error { var err error; out, err = tx.Personal().ListWorkspaces(ctx, q); return err })
	return out, err
}
func (s *Personal) DeleteWorkspace(ctx context.Context, id string, revision int64) error {
	if err := personalIDCheck(id, domain.PrefixPersonalWorkspace); err != nil {
		return err
	}
	return s.update(ctx, func(tx store.Tx, em *emitter) error {
		w, err := tx.Personal().GetWorkspace(ctx, id)
		if err != nil {
			return err
		}
		if err = personalRevision(w.Revision, revision); err != nil {
			return err
		}
		if err = tx.Personal().DeleteWorkspace(ctx, id, revision); err != nil {
			return err
		}
		return personalEvent(em, "workspace", "deleted", w.PersonalMeta)
	})
}
func (s *Personal) CreateCollection(ctx context.Context, spec domain.PersonalCollectionSpec) (*domain.PersonalCollection, error) {
	if err := domain.ValidatePersonalCollection(spec); err != nil {
		return nil, err
	}
	c := &domain.PersonalCollection{PersonalMeta: s.meta(domain.PrefixPersonalCollection), PersonalCollectionSpec: spec}
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		if err := tx.Personal().CreateCollection(ctx, c); err != nil {
			return err
		}
		return personalEvent(em, "collection", "created", c.PersonalMeta)
	})
	return c, err
}
func (s *Personal) UpdateCollection(ctx context.Context, id string, revision int64, spec domain.PersonalCollectionSpec) (*domain.PersonalCollection, error) {
	if err := personalIDCheck(id, domain.PrefixPersonalCollection); err != nil {
		return nil, err
	}
	var c *domain.PersonalCollection
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var err error
		c, err = tx.Personal().GetCollection(ctx, id)
		if err != nil {
			return err
		}
		if err = personalRevision(c.Revision, revision); err != nil {
			return err
		}
		if err = domain.ValidatePersonalEvolution(c.PersonalCollectionSpec, spec); err != nil {
			return err
		}
		c.PersonalCollectionSpec, c.UpdatedAt = spec, s.now()
		if err = tx.Personal().UpdateCollection(ctx, c); err != nil {
			return err
		}
		return personalEvent(em, "collection", "updated", c.PersonalMeta)
	})
	return c, err
}
func (s *Personal) GetCollection(ctx context.Context, id string) (*domain.PersonalCollection, error) {
	if err := personalIDCheck(id, domain.PrefixPersonalCollection); err != nil {
		return nil, err
	}
	var c *domain.PersonalCollection
	err := s.Store.View(ctx, func(tx store.Tx) error { var err error; c, err = tx.Personal().GetCollection(ctx, id); return err })
	return c, err
}
func (s *Personal) ListCollections(ctx context.Context, q domain.PersonalQuery) ([]domain.PersonalCollection, error) {
	if err := domain.ValidatePersonalQuery(q, domain.PrefixPersonalCollection); err != nil {
		return nil, err
	}
	var out []domain.PersonalCollection
	err := s.Store.View(ctx, func(tx store.Tx) error { var err error; out, err = tx.Personal().ListCollections(ctx, q); return err })
	return out, err
}
func (s *Personal) DeleteCollection(ctx context.Context, id string, revision int64) error {
	if err := personalIDCheck(id, domain.PrefixPersonalCollection); err != nil {
		return err
	}
	return s.update(ctx, func(tx store.Tx, em *emitter) error {
		c, err := tx.Personal().GetCollection(ctx, id)
		if err != nil {
			return err
		}
		if err = personalRevision(c.Revision, revision); err != nil {
			return err
		}
		if err = tx.Personal().DeleteCollection(ctx, id, revision); err != nil {
			return err
		}
		return personalEvent(em, "collection", "deleted", c.PersonalMeta)
	})
}
func personalRecordIDs(collection, id string) error {
	if err := personalIDCheck(collection, domain.PrefixPersonalCollection); err != nil {
		return err
	}
	return personalIDCheck(id, domain.PrefixPersonalRecord)
}
func (s *Personal) CreateRecord(ctx context.Context, collection string, values map[string]json.RawMessage) (*domain.PersonalRecord, error) {
	if err := personalIDCheck(collection, domain.PrefixPersonalCollection); err != nil {
		return nil, err
	}
	v := &domain.PersonalRecord{PersonalMeta: s.meta(domain.PrefixPersonalRecord), CollectionID: collection, Values: values}
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		c, err := tx.Personal().GetCollection(ctx, collection)
		if err != nil {
			return err
		}
		if err = domain.ValidatePersonalValues(c.PersonalCollectionSpec, values); err != nil {
			return err
		}
		if err = tx.Personal().CreateRecord(ctx, v); err != nil {
			return err
		}
		return personalEvent(em, "record", "created", v.PersonalMeta)
	})
	return v, err
}
func (s *Personal) UpdateRecord(ctx context.Context, collection, id string, revision int64, values map[string]json.RawMessage) (*domain.PersonalRecord, error) {
	if err := personalRecordIDs(collection, id); err != nil {
		return nil, err
	}
	var v *domain.PersonalRecord
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		var err error
		v, err = tx.Personal().GetRecord(ctx, collection, id)
		if err != nil {
			return err
		}
		if err = personalRevision(v.Revision, revision); err != nil {
			return err
		}
		c, err := tx.Personal().GetCollection(ctx, collection)
		if err != nil {
			return err
		}
		if err = domain.ValidatePersonalValues(c.PersonalCollectionSpec, values); err != nil {
			return err
		}
		v.Values, v.UpdatedAt = values, s.now()
		if err = tx.Personal().UpdateRecord(ctx, v); err != nil {
			return err
		}
		return personalEvent(em, "record", "updated", v.PersonalMeta)
	})
	return v, err
}
func (s *Personal) GetRecord(ctx context.Context, collection, id string) (*domain.PersonalRecord, error) {
	if err := personalRecordIDs(collection, id); err != nil {
		return nil, err
	}
	var v *domain.PersonalRecord
	err := s.Store.View(ctx, func(tx store.Tx) error {
		var err error
		v, err = tx.Personal().GetRecord(ctx, collection, id)
		return err
	})
	return v, err
}
func (s *Personal) ListRecords(ctx context.Context, collection string, q domain.PersonalQuery) ([]domain.PersonalRecord, error) {
	if err := personalIDCheck(collection, domain.PrefixPersonalCollection); err != nil {
		return nil, err
	}
	if err := domain.ValidatePersonalQuery(q, domain.PrefixPersonalRecord); err != nil {
		return nil, err
	}
	var out []domain.PersonalRecord
	err := s.Store.View(ctx, func(tx store.Tx) error {
		if _, err := tx.Personal().GetCollection(ctx, collection); err != nil {
			return err
		}
		var err error
		out, err = tx.Personal().ListRecords(ctx, collection, q)
		return err
	})
	return out, err
}
func (s *Personal) DeleteRecord(ctx context.Context, collection, id string, revision int64) error {
	if err := personalRecordIDs(collection, id); err != nil {
		return err
	}
	return s.update(ctx, func(tx store.Tx, em *emitter) error {
		v, err := tx.Personal().GetRecord(ctx, collection, id)
		if err != nil {
			return err
		}
		if err = personalRevision(v.Revision, revision); err != nil {
			return err
		}
		if err = tx.Personal().DeleteRecord(ctx, collection, id, revision); err != nil {
			return err
		}
		return personalEvent(em, "record", "deleted", v.PersonalMeta)
	})
}
