package service

import (
	"context"
	"devboard/internal/domain"
	"devboard/internal/integration"
	"devboard/internal/store"
	"encoding/json"
	"errors"
	"fmt"
)

const relationshipMemoryType = "repository_relationship"

// SaveRelationship uses the existing versioned private memory store. The local
// owner must have both registered repository bindings; no filesystem is opened.
func (s *Knowledge) SaveRelationship(ctx context.Context, project string, r integration.RepositoryRelationship, accept bool) (*domain.Memory, error) {
	if r.Validate() != nil || r.Source.ProjectID != project {
		return nil, domain.ErrInvalid
	}
	if !accept {
		r.LastVerified = nil
	} else {
		now := s.Memory.deps.now()
		r.LastVerified = &now
	}
	err := s.Memory.deps.Store.View(ctx, func(tx store.Tx) error {
		for _, t := range []integration.MemoryTarget{r.Source, r.Target} {
			if e := s.Memory.checkProject(ctx, tx, domain.MemoryScope{OwnerID: s.Memory.owner, ProjectID: t.ProjectID, Repository: t.Repository}); e != nil {
				return domain.ErrNotFound
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(r)
	state := domain.MemoryNeedsVerification
	if accept {
		state = domain.MemoryCurrent
	}
	return s.Memory.Create(ctx, domain.MemoryScope{ProjectID: project}, domain.MemoryContent{Type: relationshipMemoryType, Authority: domain.MemoryObservation, Source: domain.MemoryProvenance{Type: domain.MemorySourceUser, Reference: "repository relationship"}, Summary: fmt.Sprintf("%s: %s → %s", r.Type, r.Source.Repository, r.Target.Repository), Details: raw, State: state, VerifiedAt: r.LastVerified})
}

// Relationships filters both endpoints BEFORE assembling results or traversing.
// Rebinding/removing a repository hides obsolete edges. Display-name changes do
// not affect canonical identity. No fallback to repository names is permitted.
func (s *Knowledge) Relationships(ctx context.Context, project string, depth int) ([]integration.RelationshipResult, error) {
	return s.RelationshipsScoped(ctx, project, depth, nil)
}

// RelationshipsScoped intersects an assistant's narrower grant before traversal.
// A nil allow-list means the local owner's registered projects; empty means none.
func (s *Knowledge) RelationshipsScoped(ctx context.Context, project string, depth int, projectsAllowed []string) ([]integration.RelationshipResult, error) {
	permitted := func(id string) bool {
		if projectsAllowed == nil {
			return true
		}
		for _, p := range projectsAllowed {
			if p == id {
				return true
			}
		}
		return false
	}
	if !permitted(project) {
		return nil, domain.ErrNotFound
	}

	if depth < 1 || depth > 4 {
		return nil, domain.ErrInvalid
	}
	out := []integration.RelationshipResult{}
	err := s.Memory.deps.Store.View(ctx, func(tx store.Tx) error {
		if _, e := tx.Projects().Get(ctx, project); e != nil {
			return e
		}
		projects, e := tx.Projects().List(ctx)
		if e != nil {
			return e
		}
		edges := []integration.RelationshipResult{}
		var starts []integration.MemoryTarget
		for _, p := range projects {
			if !permitted(p.ID) {
				continue
			}
			records, e := relationshipRecords(ctx, tx, domain.MemoryScope{OwnerID: s.Memory.owner, ProjectID: p.ID})
			if e != nil {
				return e
			}
			for _, m := range records {
				if m.State != domain.MemoryCurrent && m.State != domain.MemoryNeedsVerification || m.SupersededBy != "" || m.Sensitivity != domain.MemoryPrivate {
					continue
				}
				var r integration.RepositoryRelationship
				if json.Unmarshal(m.Details, &r) != nil || r.Validate() != nil || r.Source.ProjectID != p.ID {
					continue
				}
				if !permitted(r.Source.ProjectID) || !permitted(r.Target.ProjectID) {
					continue
				}
				allowed := true
				stale := false
				for _, t := range []integration.MemoryTarget{r.Source, r.Target} {
					if s.Memory.checkProject(ctx, tx, domain.MemoryScope{OwnerID: s.Memory.owner, ProjectID: t.ProjectID, Repository: t.Repository}) != nil {
						allowed = false
						break
					}
					repo, e := tx.Repositories().Get(ctx, t.ProjectID)
					if e != nil {
						allowed = false
						break
					}
					revision := r.SourceRevision
					if t == r.Target {
						revision = r.TargetRevision
					}
					if revision != "" && revision != repo.HeadCommit {
						stale = true
					}
				}
				if !allowed {
					continue
				}
				status := integration.RelationshipStatus(r, s.Memory.deps.now())
				if stale {
					status = "stale"
				}
				if m.State == domain.MemoryNeedsVerification || m.VerifiedAt == nil || r.LastVerified == nil || !m.VerifiedAt.Equal(*r.LastVerified) {
					status = "provisional"
				}
				edges = append(edges, integration.RelationshipResult{ID: m.ID, Version: m.Version, Relationship: r, Status: status})
				if r.Source.ProjectID == project {
					starts = append(starts, r.Source)
				}
				if r.Target.ProjectID == project {
					starts = append(starts, r.Target)
				}
			}
		}
		seen := map[string]bool{}
		for _, start := range starts {
			for _, r := range integration.TraverseRelationships(start, edges, depth) {
				if !seen[r.ID] {
					seen[r.ID] = true
					out = append(out, r)
				}
			}
		}
		return nil
	})
	return out, err
}

// DiscoverRelationships accepts bounded manifest bytes from the owner's existing
// permitted checkout reader. Only explicit Git addresses match known identities.
// Package names, registry names and local paths never identify another repository.
func (s *Knowledge) DiscoverRelationships(ctx context.Context, project string, source integration.MemoryTarget, manifest string, raw []byte, revision string) ([]domain.Memory, error) {
	refs, e := integration.ManifestRepositories(manifest, raw)
	if e != nil {
		return nil, domain.ErrInvalid
	}
	out := []domain.Memory{}
	e = s.Memory.deps.Store.View(ctx, func(tx store.Tx) error {
		return s.Memory.checkProject(ctx, tx, domain.MemoryScope{OwnerID: s.Memory.owner, ProjectID: project, Repository: source.Repository})
	})
	if e != nil || source.ProjectID != project {
		return nil, domain.ErrNotFound
	}
	var targets []integration.MemoryTarget
	e = s.Memory.deps.Store.View(ctx, func(tx store.Tx) error {
		repos, e := tx.Repositories().ListBounded(ctx, 65)
		if e != nil {
			return e
		}
		if len(repos) > 64 {
			return domain.ErrConflict
		}
		for _, ref := range refs {
			var matches []integration.MemoryTarget
			for _, repo := range repos {
				if memoryRepositoryMatches(&repo, ref) {
					matches = append(matches, integration.MemoryTarget{ProjectID: repo.ProjectID, Repository: ref})
				}
			}
			if len(matches) == 1 {
				targets = append(targets, matches[0])
			}
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	for _, target := range targets {
		if target.ProjectID == project {
			continue
		}
		m, e := s.SaveRelationship(ctx, project, integration.RepositoryRelationship{Source: source, Target: target, Type: "package_dependency", Evidence: manifest, SourceRevision: revision, Inferred: true}, false)
		if errors.Is(e, domain.ErrDuplicate) {
			continue
		}
		if e != nil {
			return out, e
		}
		out = append(out, *m)
	}
	return out, nil
}

func (s *Knowledge) AcceptRelationship(ctx context.Context, project, id string, version int64) (*domain.Memory, error) {
	scope := domain.MemoryScope{ProjectID: project}
	m, err := s.Memory.Get(ctx, scope, id)
	if err != nil {
		return nil, err
	}
	var r integration.RepositoryRelationship
	if m.Type != relationshipMemoryType || json.Unmarshal(m.Details, &r) != nil || r.Validate() != nil {
		return nil, domain.ErrInvalid
	}
	err = s.Memory.deps.Store.View(ctx, func(tx store.Tx) error {
		for _, t := range []integration.MemoryTarget{r.Source, r.Target} {
			if e := s.Memory.checkProject(ctx, tx, domain.MemoryScope{OwnerID: s.Memory.owner, ProjectID: t.ProjectID, Repository: t.Repository}); e != nil {
				return domain.ErrNotFound
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	now := s.Memory.deps.now()
	r.LastVerified = &now
	m.Details, _ = json.Marshal(r)
	m.State = domain.MemoryCurrent
	m.VerifiedAt = &now
	return s.Memory.Update(ctx, scope, id, version, m.MemoryContent)
}

// Keyset paging prevents older links disappearing behind the ordinary UI page.
func relationshipRecords(ctx context.Context, tx store.Tx, scope domain.MemoryScope) ([]domain.Memory, error) {
	records := []domain.Memory{}
	q := domain.MemoryQuery{Type: relationshipMemoryType, Limit: 100}
	for pages := 0; pages <= domain.MaxMemoryRecords/100; pages++ {
		page, err := tx.Memory().List(ctx, scope, q)
		if err != nil {
			return nil, err
		}
		records = append(records, page.Records...)
		if page.Next == nil {
			return records, nil
		}
		q.After = page.Next
	}
	return nil, domain.ErrConflict
}
