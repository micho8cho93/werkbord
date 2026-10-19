package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"devboard/internal/domain"
	"devboard/internal/integration"
	"devboard/internal/store"
)

// Knowledge extracts only structured fields. No model, transcript mining or
// prompt authority is involved. Memory remains bound to one local principal.
type Knowledge struct {
	Memory    *Memory
	Freshness *MemoryFreshness
	// GitInterval is the least time between Git observations of one repository by
	// the five-second worker (default 30 seconds, matching MemoryFreshness).
	GitInterval time.Duration
	wg          sync.WaitGroup
	gitMu       sync.Mutex
	gitSeen     map[string]time.Time
}

// gitDue reports whether the repository may be observed again, so an idle
// repository costs no Git processes on most worker ticks.
func (s *Knowledge) gitDue(scope domain.MemoryScope) bool {
	interval := s.GitInterval
	if interval == 0 {
		interval = 30 * time.Second
	}
	key := scope.ProjectID + "\x00" + scope.Repository
	s.gitMu.Lock()
	defer s.gitMu.Unlock()
	if last, ok := s.gitSeen[key]; ok && time.Since(last) < interval {
		return false
	}
	if s.gitSeen == nil {
		s.gitSeen = map[string]time.Time{}
	}
	s.gitSeen[key] = time.Now()
	return true
}

func concise(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 1024 {
		s = s[:1024]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}

// observe compacts identical, conservatively normalised claims across sources.
// Each summary links at most 64 immutable evidence items; another bounded shard
// is created after that. Neither repetition nor pinning upgrades verification.
func (s *Knowledge) observe(ctx context.Context, tx store.Tx, scope domain.MemoryScope, c domain.MemoryContent, key string) error {
	seen, err := tx.Memory().EvidenceSeen(ctx, s.Memory.owner, key)
	if err != nil {
		return err
	}
	if seen {
		return tx.Memory().ReactivateEvidence(ctx, s.Memory.owner, key)
	}
	c.Summary = concise(c.Summary)
	if c.Summary == "" {
		return nil
	}
	// Agent text and task titles can carry a pasted credential. It is kept as local
	// evidence but classified secret, so retrieval and sharing never serve it.
	if integration.LooksSensitive(c.Summary) {
		c.Sensitivity = domain.MemorySecret
	}
	c, err = c.Normalized()
	if err != nil {
		return err
	}
	m, err := tx.Memory().KnowledgeMatch(ctx, scope, c)
	if errors.Is(err, domain.ErrNotFound) {
		now := s.Memory.deps.now()
		m = &domain.Memory{ID: domain.NewID("mem"), Scope: scope, MemoryContent: c, Fingerprint: domain.MemoryFingerprint(c), SchemaVersion: domain.MemorySchemaVersion, Version: 1, CreatedAt: now, UpdatedAt: now}
		if err = s.Memory.checkEvidence(ctx, tx, m); err != nil {
			return err
		}
		if err = tx.Memory().Create(ctx, m); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	// Original source and verification remain unchanged when more reports agree.
	return tx.Memory().AddEvidence(ctx, domain.MemoryEvidence{OwnerID: scope.OwnerID, Key: key, MemoryID: m.ID, Source: c.Source, Excerpt: c.Summary, ObservedAt: s.Memory.deps.now()})
}

// ExtractRun atomically saves all bounded claims and the run-version checkpoint.
// An interruption or quota failure rolls back both; startup polling retries it.
func (s *Knowledge) ExtractRun(ctx context.Context, id string) error {
	return s.Memory.deps.Store.Update(ctx, func(tx store.Tx) error {
		r, err := tx.Runs().Get(ctx, id)
		if err != nil {
			return err
		}
		if r.State.Active() {
			return domain.ErrConflict
		}
		scope := domain.MemoryScope{OwnerID: s.Memory.owner, ProjectID: r.ProjectID}
		task, err := tx.Tasks().Get(ctx, r.TaskID)
		if err != nil {
			return err
		}
		now := s.Memory.deps.now()
		summary := fmt.Sprintf("Task %s (%s): run %s %s", truncate(concise(task.Title), 256), r.TaskID, r.ID, r.State)
		if r.ExitCode != nil {
			summary += fmt.Sprintf("; process exit code %d", *r.ExitCode)
		}
		source := domain.MemoryProvenance{Type: domain.MemorySourceRun, Reference: "run:" + r.ID, RunID: r.ID}
		c := domain.MemoryContent{Type: "run_lifecycle", Authority: domain.MemoryObservation, Source: source, Summary: summary, State: domain.MemoryCurrent, VerifiedAt: &now}
		if err = s.observe(ctx, tx, scope, c, memoryDigest([]string{r.ID, "lifecycle", string(r.State)})); err != nil {
			return err
		}
		if err = tx.Memory().DeactivateRunEvidence(ctx, s.Memory.owner, r.ID); err != nil {
			return err
		}
		if h := r.Handoff; h != nil {
			source = domain.MemoryProvenance{Type: domain.MemorySourceHandoff, Reference: "handoff:" + r.ID, RunID: r.ID, HandoffID: r.ID}
			fields := []struct {
				kind   string
				values []string
			}{
				{"decision", h.Decisions}, {"test", h.Tests}, {"failure", h.KnownIssues}, {"blocker", h.Blockers}, {"question", h.Questions}, {"next_step", []string{h.NextAction}},
			}
			// Results can carry solutions, but the free-text summary can be a raw final
			// transcript. Extract only the structured results payload after its marker.
			if _, v, ok := strings.Cut(h.Results, "Agent-reported results: "); ok {
				fields = append(fields, struct {
					kind   string
					values []string
				}{"solution", []string{v}})
			} else if h.Results != "" && !strings.HasPrefix(h.Results, "Run state:") {
				fields = append(fields, struct {
					kind   string
					values []string
				}{"solution", []string{h.Results}})
			}
			for _, field := range fields {
				for i, v := range field.values {
					if i >= 8 {
						break
					}
					if len(v) > 1024 {
						continue
					}
					v = concise(v)
					if v == "" || v == "Inspect the work and record tests before continuing." {
						continue
					}
					source.Reference = fmt.Sprintf("handoff:%s:%s:v%d", r.ID, field.kind, r.Version)
					c = domain.MemoryContent{Type: field.kind, Authority: domain.MemoryObservation, Source: source, Summary: v, State: domain.MemoryNeedsVerification}
					// Identity excludes run version and array position: reordering/re-saving
					// the same handoff does not duplicate its evidence.
					key := memoryDigest([]string{r.ID, field.kind, v})
					if err = s.observe(ctx, tx, scope, c, key); err != nil {
						return err
					}
				}
			}
		}
		if err = tx.Memory().RetireObservations(ctx, s.Memory.owner, s.Memory.deps.now()); err != nil {
			return err
		}
		if err = s.Memory.checkCapacity(ctx, tx); err != nil {
			return err
		}
		return tx.Memory().MarkExtracted(ctx, s.Memory.owner, r.ID, r.Version)
	})
}

// ExtractGit records only independently observed committed metadata. Handoff
// GitState/FilesChanged and remote-run self-reports are deliberately not parsed.
func (s *Knowledge) ExtractGit(ctx context.Context, scope domain.MemoryScope) error {
	if s.Freshness == nil {
		return nil
	}
	var err error
	scope, err = s.Memory.scope(scope)
	if err != nil {
		return err
	}
	o := s.Freshness.observeSource(ctx, scope, false)
	if o.problem != "" {
		return domain.ErrConflict
	}
	if !validCommit(o.revision.Head) {
		return domain.ErrInvalid
	}
	now := s.Memory.deps.now()
	source := domain.MemoryProvenance{Type: domain.MemorySourceGit, Reference: "git:" + o.revision.Head, GitSHA: o.revision.Head}
	branch := o.revision.Branch
	if branch != "" && !strings.HasPrefix(branch, "refs/") {
		branch = "refs/heads/" + branch
	}
	source.Branch = branch
	branchLabel := branch
	if branchLabel == "" {
		branchLabel = "detached HEAD"
	}
	c := domain.MemoryContent{Type: "git_head", Authority: domain.MemoryObservation, Source: source, Summary: fmt.Sprintf("Observed repository HEAD %s on %s", o.revision.Head, branchLabel), State: domain.MemoryCurrent, VerifiedAt: &now}
	return s.Memory.deps.Store.Update(ctx, func(tx store.Tx) error {
		if err := s.Memory.checkProject(ctx, tx, scope); err != nil {
			return err
		}
		if err := tx.Memory().RetireGitHeads(ctx, scope, o.revision.Head, now); err != nil {
			return err
		}
		if err := s.observe(ctx, tx, scope, c, memoryDigest([]any{scope, "git_head", branch, o.revision.Head})); err != nil {
			return err
		}
		normalized, e := c.Normalized()
		if e != nil {
			return e
		}
		if e = tx.Memory().RefreshGitObservation(ctx, scope, memoryDigest([]any{scope, "git_head", branch, o.revision.Head}), domain.MemoryFingerprint(normalized), now); e != nil {
			return e
		}
		return s.Memory.checkCapacity(ctx, tx)
	})
}

// Maintain handles four runs and four repositories per tick. The repository
// cursor is durable; all I/O is serial and deadline/cancellation bounded.
func (s *Knowledge) Maintain(ctx context.Context) error {
	var runs []domain.Run
	var runAfter string
	runCursorKey := "memory_knowledge_run_cursor_" + s.Memory.owner
	if err := s.Memory.deps.Store.View(ctx, func(tx store.Tx) error {
		var e error
		if e = tx.Settings().Get(ctx, runCursorKey, &runAfter); e != nil && !errors.Is(e, domain.ErrNotFound) {
			return e
		}
		runs, e = tx.Memory().KnowledgeRuns(ctx, s.Memory.owner, runAfter, 4)
		return e
	}); err != nil {
		return err
	}
	var first error
	for _, r := range runs {
		if e := s.ExtractRun(ctx, r.ID); e != nil && first == nil {
			first = e
		}
	}
	nextRun := ""
	if len(runs) == 4 {
		nextRun = runs[len(runs)-1].ID
	}
	// An idle tick must not commit: each write is a durable (fsynced) transaction on the single writer.
	if nextRun != runAfter {
		if e := s.Memory.deps.Store.Update(ctx, func(tx store.Tx) error { return tx.Settings().Set(ctx, runCursorKey, nextRun, s.Memory.deps.now()) }); e != nil {
			return e
		}
	}
	// Registered snapshots bootstrap extraction even when no memory exists yet.
	if s.Freshness != nil {
		key := "memory_knowledge_repository_cursor_" + s.Memory.owner
		var after string
		var repos []domain.GitRepository
		err := s.Memory.deps.Store.View(ctx, func(tx store.Tx) error {
			if e := tx.Settings().Get(ctx, key, &after); e != nil && !errors.Is(e, domain.ErrNotFound) {
				return e
			}
			var e error
			repos, e = tx.Memory().KnowledgeRepositories(ctx, after, 4)
			return e
		})
		if err != nil {
			return err
		}
		for _, repo := range repos {
			// One canonical origin, or a single unambiguous remote. Never guess among
			// different remotes; the existing observer rechecks the live identity.
			identity := ""
			ambiguous := false
			for _, remote := range repo.Remotes {
				v, e := integration.RepositoryIdentity(remote.URL)
				if e != nil {
					continue
				}
				if remote.Name == "origin" {
					identity = v
					ambiguous = false
					break
				}
				if identity != "" && identity != v {
					ambiguous = true
				}
				identity = v
			}
			if scope := (domain.MemoryScope{OwnerID: s.Memory.owner, ProjectID: repo.ProjectID, Repository: identity}); identity != "" && !ambiguous && s.gitDue(scope) {
				_ = s.ExtractGit(ctx, scope)
			}
		}
		next := ""
		if len(repos) == 4 {
			next = repos[len(repos)-1].ProjectID
		}
		if next != after {
			if e := s.Memory.deps.Store.Update(ctx, func(tx store.Tx) error { return tx.Settings().Set(ctx, key, next, s.Memory.deps.now()) }); e != nil {
				return e
			}
		}
	}
	// Publication observes only approved local destinations and needs no Team connection.
	// Journal pressure is reported without interrupting Individual extraction or pruning.
	grants, grantErr := s.Memory.PublicationGrants(ctx)
	if grantErr == nil {
		seen := map[string]bool{}
		for _, g := range grants {
			if seen[g.WorkspaceID] {
				continue
			}
			seen[g.WorkspaceID] = true
			if _, e := s.Memory.ReconcilePublication(ctx, g.WorkspaceID); e != nil && first == nil {
				first = e
			}
		}
	} else if first == nil {
		first = grantErr
	}
	_, err := s.Memory.Prune(ctx, 100)
	if first != nil {
		return first
	}
	return err
}

// Watch polls durable state, including old task history, after startup. No event
// subscription is necessary for correctness. Wait joins before the DB closes.
func (s *Knowledge) Watch(ctx context.Context) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			batch, cancel := context.WithTimeout(ctx, 15*time.Second)
			err := s.Maintain(batch)
			cancel()
			if err != nil && ctx.Err() == nil {
				s.Memory.deps.log().Debug("memory maintenance deferred", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
func (s *Knowledge) Wait() { s.wg.Wait() }

// SaveManual is a human-only entry point: HTTP clients cannot provide authority,
// verification timestamps, owner IDs, Git proof or arbitrary approval metadata.
// Corrections archive the prior recommendation atomically, preserving evidence.
type ManualMemory struct {
	Summary    string `json:"summary"`
	Kind       string `json:"kind"`
	Corrects   string `json:"corrects,omitempty"`
	Version    int64  `json:"version,omitempty"`
	Repository string `json:"repository,omitempty"`
}

func (s *Knowledge) SaveManual(ctx context.Context, project string, in ManualMemory) (*domain.Memory, error) {
	if integration.LooksSensitive(in.Summary) {
		return nil, fmt.Errorf("%w: this looks like a credential, token or local path; memory is not a place to keep secrets", domain.ErrInvalid)
	}
	scope, err := s.Memory.scope(domain.MemoryScope{ProjectID: project, Repository: in.Repository})
	if err != nil {
		return nil, err
	}
	authority := domain.MemoryDecision
	switch in.Kind {
	case "decision":
	case "preference":
		authority = domain.MemoryPreference
	case "policy":
		authority = domain.MemoryPolicy
	case "fact":
		authority = domain.MemoryObservation
	default:
		return nil, domain.ErrInvalid
	}
	now := s.Memory.deps.now()
	ref := domain.NewID("confirmation")
	c := domain.MemoryContent{Type: in.Kind, Authority: authority, Source: domain.MemoryProvenance{Type: domain.MemorySourceUser, Reference: "manual:" + ref, ApprovedBy: s.Memory.owner, ApprovalRef: ref}, Summary: in.Summary, State: domain.MemoryCurrent, VerifiedAt: &now}
	c, err = c.Normalized()
	if err != nil {
		return nil, err
	}
	m := &domain.Memory{ID: domain.NewID("mem"), Scope: scope, MemoryContent: c, Fingerprint: domain.MemoryFingerprint(c), SchemaVersion: domain.MemorySchemaVersion, Version: 1, CreatedAt: now, UpdatedAt: now}
	err = s.Memory.deps.Store.Update(ctx, func(tx store.Tx) error {
		if err := s.Memory.checkProject(ctx, tx, scope); err != nil {
			return err
		}
		var original *domain.Memory
		if in.Corrects != "" {
			old, e := tx.Memory().Get(ctx, scope, in.Corrects)
			if e != nil {
				return e
			}
			if old.Version != in.Version {
				return domain.ErrConflict
			}
			original = old
			// A wording correction is not permission to declassify private evidence.
			m.Sensitivity = old.Sensitivity
			old.MemoryContent = archiveMemoryContent(old.MemoryContent)
			old.Fingerprint = domain.MemoryFingerprint(old.MemoryContent)
			old.UpdatedAt = now
			if e = tx.Memory().Update(ctx, old); e != nil {
				return e
			}
			m.Details, _ = json.Marshal(map[string]string{"corrects": old.ID})
		}
		m.Fingerprint = domain.MemoryFingerprint(m.MemoryContent)
		if e := tx.Memory().Create(ctx, m); e != nil {
			return e
		}
		if original != nil {
			if e := tx.Memory().AddEvidence(ctx, domain.MemoryEvidence{OwnerID: scope.OwnerID, Key: memoryDigest([]string{ref, "correction"}), MemoryID: m.ID, Source: original.Source, Excerpt: concise(original.Summary), ObservedAt: now}); e != nil {
				return e
			}
		}
		if e := tx.Memory().AddEvidence(ctx, domain.MemoryEvidence{OwnerID: scope.OwnerID, Key: memoryDigest(ref), MemoryID: m.ID, Source: m.Source, Excerpt: concise(m.Summary), ObservedAt: now}); e != nil {
			return e
		}
		return s.Memory.checkCapacity(ctx, tx)
	})
	return m, err
}

func (s *Knowledge) List(ctx context.Context, scope domain.MemoryScope, q domain.MemoryQuery) ([]domain.Memory, error) {
	scope, err := s.Memory.scope(scope)
	if err != nil {
		return nil, err
	}
	var out []domain.Memory
	err = s.Memory.deps.Store.View(ctx, func(tx store.Tx) error {
		if e := s.Memory.checkProject(ctx, tx, scope); e != nil {
			return e
		}
		var e error
		out, e = tx.Memory().KnowledgeList(ctx, scope, q, false)
		return e
	})
	return out, err
}
func (s *Knowledge) Sources(ctx context.Context, scope domain.MemoryScope, id string) ([]domain.MemoryEvidence, error) {
	scope, err := s.Memory.scope(scope)
	if err != nil {
		return nil, err
	}
	var out []domain.MemoryEvidence
	err = s.Memory.deps.Store.View(ctx, func(tx store.Tx) error {
		if e := s.Memory.checkProject(ctx, tx, scope); e != nil {
			return e
		}
		var e error
		out, e = tx.Memory().Evidence(ctx, scope, id, 64)
		return e
	})
	return out, err
}

// Archiving is a lifecycle action. Only mechanically re-verifiable HEAD facts
// need an explicit suppression marker; decision/policy content stays immutable.
func archiveMemoryContent(in domain.MemoryContent) domain.MemoryContent {
	in.State = domain.MemoryHistorical
	in.SupersededBy = ""
	if in.Source.Type == domain.MemorySourceGit && in.Type == "git_head" {
		var details map[string]any
		_ = json.Unmarshal(in.Details, &details)
		if details == nil {
			details = map[string]any{}
		}
		details["archivedBy"] = "user"
		in.Details, _ = json.Marshal(details)
	}
	return in
}
func (s *Knowledge) Change(ctx context.Context, scope domain.MemoryScope, id string, version int64, pinned *bool, archive bool) (*domain.Memory, error) {
	m, e := s.Memory.Get(ctx, scope, id)
	if e != nil {
		return nil, e
	}
	if pinned != nil {
		m.Pinned = *pinned
	}
	if archive {
		m.MemoryContent = archiveMemoryContent(m.MemoryContent)
	}
	return s.Memory.Update(ctx, scope, id, version, m.MemoryContent)
}
