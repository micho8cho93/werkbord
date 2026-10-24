package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	"devboard/internal/domain"
	"devboard/internal/store"
)

type PersonalActionRecord struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
}
type PersonalActionRequest struct {
	WorkspaceRevision  int64                  `json:"workspaceRevision"`
	CollectionRevision int64                  `json:"collectionRevision"`
	Records            []PersonalActionRecord `json:"records"`
	Confirmed          bool                   `json:"confirmed"`
}
type PersonalActionResult struct {
	Saved   []domain.PersonalRecord `json:"saved"`
	Deleted []string                `json:"deleted"`
}

// ExecuteAction reloads the allowlisted definition and performs all steps and all
// selected records in one transaction. No client-supplied operation or values run.
func (s *Personal) ExecuteAction(ctx context.Context, wid, vid, aid string, in PersonalActionRequest) (*PersonalActionResult, error) {
	if err := personalIDCheck(wid, domain.PrefixPersonalWorkspace); err != nil {
		return nil, err
	}
	if !in.Confirmed || len(in.Records) > 100 {
		return nil, fmt.Errorf("%w: review and confirm the action first", domain.ErrInvalid)
	}
	out := &PersonalActionResult{Saved: []domain.PersonalRecord{}, Deleted: []string{}}
	err := s.update(ctx, func(tx store.Tx, em *emitter) error {
		w, err := tx.Personal().GetWorkspace(ctx, wid)
		if err != nil {
			return err
		}
		if err = personalRevision(w.Revision, in.WorkspaceRevision); err != nil {
			return err
		}
		var v *domain.PersonalView
		for i := range w.Definition.Views {
			if w.Definition.Views[i].ID == vid {
				v = &w.Definition.Views[i]
			}
		}
		if v == nil {
			return domain.ErrNotFound
		}
		c, err := tx.Personal().GetCollection(ctx, v.CollectionID)
		if err != nil {
			return err
		}
		if err = personalRevision(c.Revision, in.CollectionRevision); err != nil {
			return err
		}
		var a *domain.PersonalAction
		for i := range v.Actions {
			if v.Actions[i].ID == aid {
				a = &v.Actions[i]
			}
		}
		if a != nil && a.Prompt != "" {
			return domain.ErrForbidden
		}
		if a == nil {
			return domain.ErrNotFound
		}
		// Recheck the complete saved grammar against all current schemas.
		if len(w.CollectionIDs) > 32 {
			return domain.ErrInvalid
		}
		schemas := map[string]domain.PersonalCollectionSpec{}
		for _, id := range w.CollectionIDs {
			schema, err := tx.Personal().GetCollection(ctx, id)
			if err != nil {
				return err
			}
			schemas[id] = schema.PersonalCollectionSpec
		}
		if err = domain.ValidatePersonalWorkspace(w.PersonalWorkspaceSpec, schemas); err != nil {
			return err
		}
		needsRecords := false
		for _, step := range a.Steps {
			if step.Operation != "create" {
				needsRecords = true
			}
		}
		if needsRecords && len(in.Records) == 0 || !needsRecords && len(in.Records) != 0 {
			return domain.ErrInvalid
		}
		targets := []*domain.PersonalRecord{}
		seen := map[string]bool{}
		for _, ref := range in.Records {
			if seen[ref.ID] || !domain.ValidPersonalID(ref.ID, domain.PrefixPersonalRecord) {
				return domain.ErrInvalid
			}
			seen[ref.ID] = true
			r, err := tx.Personal().GetRecord(ctx, c.ID, ref.ID)
			if err != nil {
				return err
			}
			if err = personalRevision(r.Revision, ref.Revision); err != nil {
				return err
			}
			r.Values = domain.PersonalCalculatedValues(c.PersonalCollectionSpec, r.Values)
			if !domain.PersonalMatches(r.Values, v.Filters) || !domain.PersonalMatches(r.Values, a.When) {
				return fmt.Errorf("%w: record no longer matches this action", domain.ErrConflict)
			}
			targets = append(targets, r)
		}
		if !needsRecords {
			targets = append(targets, nil)
		}
		final := map[string]domain.PersonalRecord{}
		for _, target := range targets {
			deleted := false
			for _, step := range a.Steps {
				if !slices.Contains(v.Operations, step.Operation) {
					return domain.ErrForbidden
				}
				if step.Operation == "create" {
					values := domain.PersonalApplyRules(c.PersonalCollectionSpec, step.Values, s.now(), false)
					if err = domain.ValidatePersonalValues(c.PersonalCollectionSpec, values); err != nil {
						return err
					}
					r := &domain.PersonalRecord{PersonalMeta: s.meta(domain.PrefixPersonalRecord), CollectionID: c.ID, Values: values}
					if err = tx.Personal().CreateRecord(ctx, r); err != nil {
						return err
					}
					final[r.ID] = *r
					if err = personalEvent(em, "record", "created", r.PersonalMeta); err != nil {
						return err
					}
					continue
				}
				if target == nil || deleted {
					return fmt.Errorf("%w: action uses a deleted record", domain.ErrInvalid)
				}
				if step.Operation == "delete" {
					if err = tx.Personal().DeleteRecord(ctx, c.ID, target.ID, target.Revision); err != nil {
						return err
					}
					deleted = true
					delete(final, target.ID)
					out.Deleted = append(out.Deleted, target.ID)
					if err = personalEvent(em, "record", "deleted", target.PersonalMeta); err != nil {
						return err
					}
					continue
				}
				values := map[string]json.RawMessage{}
				for k, val := range target.Values {
					values[k] = val
				}
				for k, val := range step.Values {
					values[k] = val
				}
				for _, k := range step.Clear {
					delete(values, k)
				}
				values = domain.PersonalApplyRules(c.PersonalCollectionSpec, values, s.now(), false)
				if err = domain.ValidatePersonalValues(c.PersonalCollectionSpec, values); err != nil {
					return err
				}
				target.Values, target.UpdatedAt = values, s.now()
				if err = tx.Personal().UpdateRecord(ctx, target); err != nil {
					return err
				}
				final[target.ID] = *target
				if err = personalEvent(em, "record", "updated", target.PersonalMeta); err != nil {
					return err
				}
			}
		}
		ids := []string{}
		for id := range final {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			out.Saved = append(out.Saved, final[id])
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type PersonalBindingResult struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Total      int                 `json:"total"`
	Unresolved int                 `json:"unresolved"`
	Tasks      []PersonalBoundTask `json:"tasks"`
}
type PersonalBoundTask struct {
	ID    string           `json:"id"`
	Title string           `json:"title"`
	State domain.TaskState `json:"state"`
}

// ResolveBindings reads only existing local owner projects. Display associations
// never resolve Team IDs or add authority. Missing resources fail independently.
func (s *Personal) ResolveBinding(ctx context.Context, wid, bid string) (*PersonalBindingResult, error) {
	if err := personalIDCheck(wid, domain.PrefixPersonalWorkspace); err != nil {
		return nil, err
	}
	var out *PersonalBindingResult
	err := s.Store.View(ctx, func(tx store.Tx) error {
		w, err := tx.Personal().GetWorkspace(ctx, wid)
		if err != nil {
			return err
		}
		var b *domain.PersonalReadBinding
		for i := range w.ReadBindings {
			if w.ReadBindings[i].ID == bid {
				b = &w.ReadBindings[i]
			}
		}
		if b == nil {
			return domain.ErrNotFound
		}
		p, err := tx.Projects().Get(ctx, b.ProjectID)
		if err != nil {
			return err
		}
		if p.DeletedAt != nil {
			return domain.ErrNotFound
		}
		out = &PersonalBindingResult{ID: b.ID, Name: p.Name, Tasks: []PersonalBoundTask{}}
		tasks, err := tx.Tasks().ListByProject(ctx, p.ID)
		if err != nil {
			return err
		}
		out.Total = len(tasks)
		for _, t := range tasks {
			if t.State != domain.TaskDone {
				out.Unresolved++
			}
			if b.Kind == "individual.tasks" && len(out.Tasks) < 100 {
				out.Tasks = append(out.Tasks, PersonalBoundTask{ID: t.ID, Title: t.Title, State: t.State})
			}
		}
		return nil
	})
	return out, err
}

// Maintain runs inside the existing scheduler's minute maintenance callback.
// A keyset page per collection bounds work and avoids starving later records.
// Watermarks live in records and commit with the rule, surviving restart/retry.
func (s *Personal) Maintain(ctx context.Context) error {
	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()
	if s.maintenanceAfter == nil {
		s.maintenanceAfter = map[string]string{}
	}
	collections, err := s.ListCollections(ctx, domain.PersonalQuery{Limit: 100, After: s.maintenanceCollection})
	if err != nil {
		return err
	}
	if len(collections) < 100 {
		s.maintenanceCollection = ""
	} else {
		s.maintenanceCollection = collections[len(collections)-1].ID
	}
	for _, c := range collections {
		scheduled := false
		for _, r := range c.Rules {
			scheduled = scheduled || r.Schedule != nil
		}
		if !scheduled {
			continue
		}
		var after string
		err := s.update(ctx, func(tx store.Tx, em *emitter) error {
			current, err := tx.Personal().GetCollection(ctx, c.ID)
			if err != nil {
				return err
			}
			records, err := tx.Personal().ListRecords(ctx, c.ID, domain.PersonalQuery{Limit: 100, After: s.maintenanceAfter[c.ID]})
			if err != nil {
				return err
			}
			if len(records) == 100 {
				after = records[len(records)-1].ID
			}
			for _, r := range records {
				values := domain.PersonalApplyRules(current.PersonalCollectionSpec, r.Values, s.now(), true)
				if reflect.DeepEqual(values, r.Values) {
					continue
				}
				// Scheduled status changes also run the same single-pass status rules.
				values = domain.PersonalApplyRules(current.PersonalCollectionSpec, values, s.now(), false)
				if err = domain.ValidatePersonalValues(current.PersonalCollectionSpec, values); err != nil {
					return err
				}
				r.Values, r.UpdatedAt = values, s.now()
				if err = tx.Personal().UpdateRecord(ctx, &r); err != nil {
					return err
				}
				if err = personalEvent(em, "record", "updated", r.PersonalMeta); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		s.maintenanceAfter[c.ID] = after
	}
	return nil
}
