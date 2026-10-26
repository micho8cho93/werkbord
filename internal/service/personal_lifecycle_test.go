package service

import (
	"context"
	"errors"
	"testing"
	"werkbord/internal/domain"
	"werkbord/internal/store"
)

func reviewed(p *domain.PersonalDeletion) domain.PersonalDeleteRequest {
	return domain.PersonalDeleteRequest{Revision: p.Revision, Mode: p.Mode, Fingerprint: p.Fingerprint, Confirmed: true}
}
func TestPersonalReviewedDeletionOwnershipRetentionAndFreshness(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	s := &Personal{Deps: f.deps}
	project, err := f.projects.Register(ctx, "/repos/deletion-isolation", "Core project")
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.tasks.Create(ctx, project.ID, "Core task", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateCollection(ctx, personalCollectionSpec())
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateWorkspace(ctx, personalWorkspaceSpec(c.ID))
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateRecord(ctx, c.ID, personalValues("Keep this task"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.PreviewDeletion(ctx, w.ID, w.Revision, "delete_data")
	if err != nil || !p.Collections[0].Delete || p.Collections[0].RecordCount != 1 {
		t.Fatal(p, err)
	}
	req := reviewed(p)
	req.Confirmed = false
	if _, err = s.DeleteWorkspaceReviewed(ctx, w.ID, req); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	// Same population size, different revision: stale approval must fail.
	if _, err = s.UpdateRecord(ctx, c.ID, r.ID, r.Revision, personalValues("Changed task")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DeleteWorkspaceReviewed(ctx, w.ID, reviewed(p)); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("stale records accepted", err)
	}
	p, _ = s.PreviewDeletion(ctx, w.ID, w.Revision, "delete_data")
	other, err := s.CreateWorkspace(ctx, personalWorkspaceSpec(c.ID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DeleteWorkspaceReviewed(ctx, w.ID, reviewed(p)); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("new dependency accepted", err)
	}
	p, _ = s.PreviewDeletion(ctx, w.ID, w.Revision, "delete_data")
	if p.Collections[0].Delete || p.Collections[0].ReferenceCount != 2 {
		t.Fatal("shared data not protected", p)
	}
	if _, err = s.DeleteWorkspaceReviewed(ctx, w.ID, reviewed(p)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetRecord(ctx, c.ID, r.ID); err != nil {
		t.Fatal("shared record deleted", err)
	}
	if _, err = s.GetWorkspace(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	p, _ = s.PreviewDeletion(ctx, other.ID, other.Revision, "keep_data")
	if _, err = s.DeleteWorkspaceReviewed(ctx, other.ID, reviewed(p)); err != nil {
		t.Fatal(err)
	}
	usage, err := s.CollectionUsage(ctx, c.ID)
	if err != nil || usage.ReferenceCount != 0 || usage.OwnerWorkspaceID != "" || usage.RecordCount != 1 {
		t.Fatal(usage, err)
	}
	recovered, err := s.CreateWorkspace(ctx, personalWorkspaceSpec(c.ID))
	if err != nil {
		t.Fatal(err)
	}
	p, _ = s.PreviewDeletion(ctx, recovered.ID, recovered.Revision, "delete_data")
	if !p.Collections[0].Delete {
		t.Fatal("recovered unowned collection not claimed", p)
	}
	if _, err = s.DeleteWorkspaceReviewed(ctx, recovered.ID, reviewed(p)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetCollection(ctx, c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = s.GetWorkspace(ctx, recovered.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = f.projects.Get(ctx, project.ID); err != nil {
		t.Fatal("core project affected", err)
	}
	if _, err = f.tasks.Get(ctx, task.ID); err != nil {
		t.Fatal("core task affected", err)
	}

}

// Inject failure after record deletion to prove the entire transaction rolls back.
type failingPersonalStore struct{ store.Store }
type failingPersonalTx struct{ store.Tx }
type failingPersonalRepo struct{ store.PersonalRepo }

func (s failingPersonalStore) Update(ctx context.Context, fn func(store.Tx) error) error {
	return s.Store.Update(ctx, func(tx store.Tx) error { return fn(failingPersonalTx{tx}) })
}
func (t failingPersonalTx) Personal() store.PersonalRepo { return failingPersonalRepo{t.Tx.Personal()} }
func (r failingPersonalRepo) DeleteCollection(context.Context, string, int64) error {
	return errors.New("injected collection failure")
}
func TestPersonalDeletionFailureAndDefinitionRollbackPreserveIntegrity(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	s := &Personal{Deps: f.deps}
	c, _ := s.CreateCollection(ctx, personalCollectionSpec())
	w, _ := s.CreateWorkspace(ctx, personalWorkspaceSpec(c.ID))
	r, _ := s.CreateRecord(ctx, c.ID, personalValues("Durable"))
	spec := w.PersonalWorkspaceSpec
	spec.Name = "Renamed"
	spec.Visibility = domain.PersonalVisibility{}
	w, err := s.UpdateWorkspace(ctx, w.ID, w.Revision, spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetRecord(ctx, c.ID, r.ID); err != nil {
		t.Fatal(err)
	}
	w, err = s.RollbackWorkspace(ctx, w.ID, w.Revision, 1)
	if err != nil || w.Revision != 3 || w.Name != "My tasks" || !w.Visibility.Universal {
		t.Fatal(w, err)
	}
	history, err := s.WorkspaceHistory(ctx, w.ID)
	if err != nil || len(history) != 3 {
		t.Fatal(history, err)
	}
	p, _ := s.PreviewDeletion(ctx, w.ID, w.Revision, "delete_data")
	broken := &Personal{Deps: f.deps}
	broken.Store = failingPersonalStore{s.Store}
	if _, err = broken.DeleteWorkspaceReviewed(ctx, w.ID, reviewed(p)); err == nil {
		t.Fatal("expected failure")
	}
	if _, err = s.GetWorkspace(ctx, w.ID); err != nil {
		t.Fatal("definition lost", err)
	}
	if _, err = s.GetRecord(ctx, c.ID, r.ID); err != nil {
		t.Fatal("records lost", err)
	}
	usage, err := s.CollectionUsage(ctx, c.ID)
	if err != nil || usage.ReferenceCount != 1 || usage.OwnerWorkspaceID != w.ID {
		t.Fatal("references lost", usage, err)
	}
	if _, err = s.DeleteWorkspaceReviewed(ctx, w.ID, reviewed(p)); err != nil {
		t.Fatal("rollback changed fingerprint", err)
	}
}
