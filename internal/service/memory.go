package service

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"

	"devboard/internal/domain"
	"devboard/internal/integration"
	"devboard/internal/store"
)

// Memory is a private, in-process local service bound to one individual owner.
// The caller supplies a stable local principal, not a display name or Team ID.
// Owner APIs use this service locally. Publication exposes only separately
// authorised projections through the workspace-bound journal, never this store.
type Memory struct {
	deps   Deps
	owner  string
	limits domain.MemoryLimits
}

func NewMemory(deps Deps, owner string, limits domain.MemoryLimits) (*Memory, error) {
	if deps.Store == nil {
		return nil, fmt.Errorf("%w: memory store is required", domain.ErrInvalid)
	}
	if err := (domain.MemoryScope{OwnerID: owner}).Validate(); err != nil {
		return nil, err
	}
	if limits == (domain.MemoryLimits{}) {
		limits = domain.DefaultMemoryLimits()
	}
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	return &Memory{deps: deps, owner: owner, limits: limits}, nil
}

// scope accepts credential-free Git addresses or canonical identities returned
// by this service. Both paths reuse the existing integration validator.
func (s *Memory) scope(in domain.MemoryScope) (domain.MemoryScope, error) {
	if in.OwnerID == "" {
		in.OwnerID = s.owner
	}
	if in.OwnerID != s.owner {
		return in, domain.ErrForbidden
	}
	if err := in.Validate(); err != nil {
		return in, err
	}
	if in.Repository != "" {
		raw := in.Repository
		id, err := integration.RepositoryIdentity(raw)
		if err != nil && !strings.Contains(raw, "://") && !strings.HasPrefix(raw, "git@") {
			// Canonical identities omit the transport. Try both transports so
			// a significant SSH port 443 is not mistaken for default HTTPS.
			address := raw
			host, path, found := strings.Cut(raw, "/")
			if found && net.ParseIP(host) != nil {
				address = "[" + host + "]/" + path
			}
			for _, prefix := range []string{"https://", "ssh://git@"} {
				candidate, e := integration.RepositoryIdentity(prefix + address)
				if e == nil && candidate == raw {
					id, err = candidate, nil
					break
				}
			}
		}
		if err != nil {
			return in, fmt.Errorf("%w: invalid memory repository identity", domain.ErrInvalid)
		}
		in.Repository = id
	}
	return in, nil
}

func (s *Memory) checkProject(ctx context.Context, tx store.Tx, scope domain.MemoryScope) error {
	if scope.ProjectID == "" {
		return nil
	}
	p, err := tx.Projects().Get(ctx, scope.ProjectID)
	if err != nil {
		return err
	}
	if scope.Repository == "" {
		return nil
	}
	if !p.HasRepository() {
		return fmt.Errorf("%w: work project has no repository", domain.ErrInvalid)
	}
	repo, err := tx.Repositories().Get(ctx, scope.ProjectID)
	if err != nil {
		return err
	}
	for _, remote := range repo.Remotes {
		id, err := integration.RepositoryIdentity(remote.URL)
		if err == nil && id == scope.Repository {
			return nil
		}
	}
	return fmt.Errorf("%w: memory repository does not belong to project", domain.ErrInvalid)
}

func (s *Memory) checkEvidence(ctx context.Context, tx store.Tx, m *domain.Memory) error {
	p := m.Source
	if p.GitSHA != "" || p.PullRequest != "" || p.Branch != "" || len(p.Paths) > 0 || p.Type == domain.MemorySourceGit {
		if m.Scope.Repository == "" {
			return fmt.Errorf("%w: Git evidence requires a repository scope", domain.ErrInvalid)
		}
	}
	if p.PullRequest != "" {
		u, err := url.Parse(p.PullRequest)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("%w: pull request must be a credential-free HTTPS reference", domain.ErrInvalid)
		}
	}
	if p.RunID != "" {
		if m.Scope.ProjectID == "" {
			return fmt.Errorf("%w: local run evidence requires a project scope", domain.ErrInvalid)
		}
		run, err := tx.Runs().Get(ctx, p.RunID)
		if err != nil {
			return err
		}
		if run.ProjectID != m.Scope.ProjectID {
			return domain.ErrNotFound
		}
		if p.HandoffID != "" && (run.Handoff == nil || p.HandoffID != run.ID) {
			return fmt.Errorf("%w: handoff reference must name an existing run handoff", domain.ErrInvalid)
		}
	}
	if m.VerifiedAt != nil && (m.VerifiedAt.IsZero() || m.VerifiedAt.After(m.UpdatedAt)) {
		return fmt.Errorf("%w: invalid verification timestamp", domain.ErrInvalid)
	}
	// Links stay in the exact owner/project/repository and authority partition.
	next := m.SupersededBy
	if next == "" {
		next = m.DuplicateOf
	}
	for depth := 0; next != ""; depth++ {
		if next == m.ID || depth >= 100 {
			return fmt.Errorf("%w: cyclic or excessive memory reference chain", domain.ErrInvalid)
		}
		target, err := tx.Memory().Get(ctx, m.Scope, next)
		if err != nil {
			return err
		}
		if target.Type != m.Type || target.Authority != m.Authority {
			return fmt.Errorf("%w: memory links must preserve type and authority", domain.ErrInvalid)
		}
		next = target.SupersededBy
		if next == "" {
			next = target.DuplicateOf
		}
	}
	return nil
}

// Create inserts a normalized record. Exact evidence duplicates return
// ErrDuplicate; FindDuplicate returns the original without mutating its state.
func (s *Memory) Create(ctx context.Context, scope domain.MemoryScope, in domain.MemoryContent) (*domain.Memory, error) {
	scope, err := s.scope(scope)
	if err != nil {
		return nil, err
	}
	in, err = in.Normalized()
	if err != nil {
		return nil, err
	}
	now := s.deps.now()
	m := &domain.Memory{ID: domain.NewID("mem"), Scope: scope, MemoryContent: in, Fingerprint: domain.MemoryFingerprint(in), SchemaVersion: domain.MemorySchemaVersion, Version: 1, CreatedAt: now, UpdatedAt: now}
	err = s.deps.Store.Update(ctx, func(tx store.Tx) error {
		if err := s.checkProject(ctx, tx, scope); err != nil {
			return err
		}
		if err := s.checkEvidence(ctx, tx, m); err != nil {
			return err
		}
		if err := tx.Memory().Create(ctx, m); err != nil {
			return err
		}
		return s.checkCapacity(ctx, tx)
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}
func (s *Memory) checkCapacity(ctx context.Context, tx store.Tx) error {
	u, err := tx.Memory().Usage(ctx, s.owner)
	if err != nil {
		return err
	}
	evidenceBytes, err := tx.Memory().EvidenceUsage(ctx, s.owner)
	if err != nil {
		return err
	}
	if u.Records > s.limits.Records || u.Bytes+evidenceBytes > s.limits.Bytes {
		return fmt.Errorf("%w: owner memory capacity exceeded; delete or prune records explicitly", domain.ErrConflict)
	}
	return nil
}
func (s *Memory) Get(ctx context.Context, scope domain.MemoryScope, id string) (*domain.Memory, error) {
	scope, err := s.scope(scope)
	if err != nil {
		return nil, err
	}
	var m *domain.Memory
	err = s.deps.Store.View(ctx, func(tx store.Tx) error {
		var err error
		m, err = tx.Memory().Get(ctx, scope, id)
		return err
	})
	return m, err
}
func (s *Memory) FindDuplicate(ctx context.Context, scope domain.MemoryScope, in domain.MemoryContent) (*domain.Memory, error) {
	scope, err := s.scope(scope)
	if err != nil {
		return nil, err
	}
	in, err = in.Normalized()
	if err != nil {
		return nil, err
	}
	var m *domain.Memory
	err = s.deps.Store.View(ctx, func(tx store.Tx) error {
		var err error
		m, err = tx.Memory().FindFingerprint(ctx, scope, domain.MemoryFingerprint(in))
		return err
	})
	return m, err
}
func (s *Memory) List(ctx context.Context, scope domain.MemoryScope, q domain.MemoryQuery) (domain.MemoryPage, error) {
	scope, err := s.scope(scope)
	if err != nil {
		return domain.MemoryPage{}, err
	}
	if err := domain.ValidateMemoryQuery(q); err != nil {
		return domain.MemoryPage{}, err
	}
	var out domain.MemoryPage
	err = s.deps.Store.View(ctx, func(tx store.Tx) error {
		var err error
		out, err = tx.Memory().List(ctx, scope, q)
		return err
	})
	return out, err
}

// Update replaces content with compare-and-swap protection. Identity, scope,
// type, authority, source type and creation time are immutable. A new authority
// requires a new record with its own explicit evidence, not an upgrade in place.
func (s *Memory) Update(ctx context.Context, scope domain.MemoryScope, id string, version int64, in domain.MemoryContent) (*domain.Memory, error) {
	scope, err := s.scope(scope)
	if err != nil {
		return nil, err
	}
	in, err = in.Normalized()
	if err != nil {
		return nil, err
	}
	if version < 1 {
		return nil, fmt.Errorf("%w: memory version required", domain.ErrInvalid)
	}
	var result *domain.Memory
	err = s.deps.Store.Update(ctx, func(tx store.Tx) error {
		old, err := tx.Memory().Get(ctx, scope, id)
		if err != nil {
			return err
		}
		if old.Version != version {
			return domain.ErrConflict
		}
		if old.Type != in.Type || old.Authority != in.Authority || old.Source.Type != in.Source.Type {
			return fmt.Errorf("%w: memory type, authority and source type are immutable", domain.ErrInvalid)
		}
		if (old.Authority == domain.MemoryDecision || old.Authority == domain.MemoryPolicy) &&
			(old.Summary != in.Summary || !bytes.Equal(old.Details, in.Details)) && old.Source.ApprovalRef == in.Source.ApprovalRef {
			return fmt.Errorf("%w: decision or policy content changes require a new approval reference", domain.ErrInvalid)
		}
		fingerprint := domain.MemoryFingerprint(in)
		if fingerprint != old.Fingerprint && in.VerifiedAt != nil && old.VerifiedAt != nil && in.VerifiedAt.Equal(*old.VerifiedAt) {
			in.VerifiedAt = nil
		}
		old.MemoryContent = in
		old.Fingerprint = fingerprint
		at := s.deps.now()
		if at.After(old.UpdatedAt) {
			old.UpdatedAt = at
		}
		if err := s.checkEvidence(ctx, tx, old); err != nil {
			return err
		}
		if err := tx.Memory().Update(ctx, old); err != nil {
			return err
		}
		if err := s.checkCapacity(ctx, tx); err != nil {
			return err
		}
		result = old
		return nil
	})
	return result, err
}

// Delete physically removes one version of one record, including its FTS entry.
// Incoming links cause ErrConflict; unlink them explicitly before deletion.
func (s *Memory) Delete(ctx context.Context, scope domain.MemoryScope, id string, version int64) error {
	scope, err := s.scope(scope)
	if err != nil {
		return err
	}
	if version < 1 {
		return fmt.Errorf("%w: memory version required", domain.ErrInvalid)
	}
	return s.deps.Store.Update(ctx, func(tx store.Tx) error { return tx.Memory().Delete(ctx, scope, id, version) })
}

// Prune is bounded maintenance across this owner's scopes. Knowledge schedules
// it using the configured retention; reads and capacity failures never prune.
func (s *Memory) Prune(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 1000 {
		return 0, fmt.Errorf("%w: prune limit must be 1..1000", domain.ErrInvalid)
	}
	var n int
	err := s.deps.Store.Update(ctx, func(tx store.Tx) error {
		var err error
		n, err = tx.Memory().Prune(ctx, s.owner, s.deps.now().Add(-s.limits.HistoryRetention), limit)
		return err
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}
func (s *Memory) Usage(ctx context.Context) (domain.MemoryUsage, error) {
	var u domain.MemoryUsage
	err := s.deps.Store.View(ctx, func(tx store.Tx) error {
		var err error
		u, err = tx.Memory().Usage(ctx, s.owner)
		return err
	})
	return u, err
}
