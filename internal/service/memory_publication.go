package service

import (
	"context"
	"devboard/internal/domain"
	"devboard/internal/integration"
	"devboard/internal/store"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// publicationContent is the export allowlist. No summary, details, branch,
// path, reference, run or handoff field is copied from an Individual memory.
func publicationContent(m domain.Memory) (integration.SharedMemoryContent, bool) {
	c := integration.SharedMemoryContent{Kind: "observation", Classification: "team", Verification: "verified", Source: integration.MemorySource{Kind: "git", ID: m.ID, Revision: m.Source.GitSHA}}
	if m.Sensitivity != domain.MemoryPrivate || m.Authority != domain.MemoryObservation || m.State != domain.MemoryCurrent || m.VerifiedAt == nil || m.Source.Type != domain.MemorySourceGit {
		return c, false
	}
	switch m.Type {
	case "git_head":
		c.Summary = "Observed Git commit " + m.Source.GitSHA
	case "git_pr", "pull_request":
		u, err := url.Parse(m.Source.PullRequest)
		if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return c, false
		}
		prefix := "https://" + m.Scope.Repository + "/pull/"
		if !strings.HasPrefix(m.Source.PullRequest, prefix) {
			return c, false
		}
		number := strings.TrimPrefix(m.Source.PullRequest, prefix)
		n, err := strconv.ParseUint(number, 10, 32)
		if err != nil || n == 0 || strconv.FormatUint(n, 10) != number {
			return c, false
		}
		c.Summary = fmt.Sprintf("Observed pull request #%d; repository commit %s", n, m.Source.GitSHA)
	default:
		return c, false
	}
	return c, c.Validate() == nil
}
func semanticCandidate(m domain.Memory) bool {
	if m.Sensitivity != domain.MemoryPrivate || m.State != domain.MemoryCurrent || m.VerifiedAt == nil || m.Authority == domain.MemoryPreference {
		return false
	}
	return m.Type == "decision" || m.Type == "solution" || m.Type == "architecture" || m.Type == "implementation_summary"
}
func publicationScope(g domain.PublicationGrant) integration.SharedMemoryScope {
	return integration.SharedMemoryScope{WorkspaceID: g.WorkspaceID, Kind: "repository", ProjectID: g.ProjectID, Repository: g.Repository}
}
func (s *Memory) publicationAssociation(ctx context.Context, tx store.Tx, g domain.PublicationGrant) error {
	if !integration.Identifier(g.LocalProjectID) || publicationScope(g).Validate() != nil {
		return domain.ErrInvalid
	}
	if err := s.checkProject(ctx, tx, domain.MemoryScope{OwnerID: s.owner, ProjectID: g.LocalProjectID, Repository: g.Repository}); err != nil {
		return err
	}
	f, e := tx.Memory().Freshness(ctx, domain.MemoryScope{OwnerID: s.owner, ProjectID: g.LocalProjectID, Repository: g.Repository})
	if e != nil && !errors.Is(e, domain.ErrNotFound) {
		return e
	}
	if e == nil && (f.Uncertain || f.Problem != "" || f.After != nil) {
		return fmt.Errorf("%w: repository verification is suspended", domain.ErrConflict)
	}
	repos, err := tx.Repositories().ListBounded(ctx, 65)
	if err != nil {
		return err
	}
	if len(repos) > 64 {
		return domain.ErrConflict
	}
	matches := 0
	for i := range repos {
		if memoryRepositoryMatches(&repos[i], g.Repository) {
			matches++
		}
	}
	if matches != 1 {
		return fmt.Errorf("%w: repository association is ambiguous or unavailable", domain.ErrConflict)
	}
	return nil
}
func (s *Memory) SetPublicationGrant(ctx context.Context, g domain.PublicationGrant) error {
	return s.deps.Store.Update(ctx, func(tx store.Tx) error {
		if publicationScope(g).Validate() != nil {
			return domain.ErrInvalid
		}
		if err := s.publicationAssociation(ctx, tx, g); err != nil && g.Enabled {
			return err
		}
		if !integration.Identifier(g.WorkspaceID) || !integration.Identifier(g.ProjectID) || !integration.Identifier(g.LocalProjectID) {
			return domain.ErrInvalid
		}
		gs, err := tx.Memory().PublicationGrants(ctx, s.owner)
		if err != nil {
			return err
		}
		for _, old := range gs {
			if old.WorkspaceID == g.WorkspaceID && old.ProjectID == g.ProjectID && (old.Repository != g.Repository || old.LocalProjectID != g.LocalProjectID) {
				return fmt.Errorf("%w: revoke the existing association before choosing a new destination", domain.ErrConflict)
			}
		}
		// Persist withdrawal intent with the setting itself, including offline
		// disable/re-enable sequences before the next maintenance tick.
		ps, e := tx.Memory().Publications(ctx, s.owner, g.WorkspaceID)
		if e != nil {
			return e
		}
		for i := range ps {
			p := &ps[i]
			if p.Grant.ProjectID != g.ProjectID {
				continue
			}
			semantic := p.Event != nil && p.Event.Proposal.Content.Source.Kind == "human"
			if !g.Enabled || !g.Semantic && semantic {
				if p.Event != nil && p.Event.Action != "revoke" {
					queuePublication(p, "revoke", p.Event.Proposal.Content)
				} else if p.Event == nil {
					p.State = "private"
				}
				if e = tx.Memory().SavePublication(ctx, s.owner, p); e != nil {
					return e
				}
			}
		}
		for _, old := range gs {
			if old.WorkspaceID == g.WorkspaceID && old.ProjectID == g.ProjectID && (!old.Enabled && g.Enabled || !old.Semantic && g.Semantic) {
				ps, e := tx.Memory().Publications(ctx, s.owner, g.WorkspaceID)
				if e != nil {
					return e
				}
				for i := range ps {
					p := &ps[i]
					if p.Grant.ProjectID == g.ProjectID && (!old.Enabled || p.Event == nil || p.Event.Proposal.Content.Source.Kind == "human") {
						p.Revision = 0
						p.State = "private"
						if e = tx.Memory().SavePublication(ctx, s.owner, p); e != nil {
							return e
						}
					}
				}
			}
		}
		return tx.Memory().SavePublicationGrant(ctx, s.owner, g)
	})
}
func (s *Memory) PublicationGrants(ctx context.Context) ([]domain.PublicationGrant, error) {
	var gs []domain.PublicationGrant
	err := s.deps.Store.View(ctx, func(tx store.Tx) error { var e error; gs, e = tx.Memory().PublicationGrants(ctx, s.owner); return e })
	return gs, err
}
func queuePublication(p *domain.MemoryPublication, action string, c integration.SharedMemoryContent) {
	previous := ""
	if p.Event != nil {
		previous = p.Event.ID
		if p.Event.Action == "revoke" {
			previous = ""
		}
	}
	id := domain.NewID("pub")
	p.Event = &integration.MemoryEvent{Schema: integration.MemorySchema, ID: id, SourceID: p.MemoryID, Revision: p.Revision, Action: action, Previous: previous, Proposal: integration.MemoryProposal{Schema: integration.MemorySchema, Scope: publicationScope(p.Grant), Content: c, ClientID: id}}
	if action == "revoke" {
		p.State = "revocation_pending"
	} else {
		p.State = "queued"
	}
	p.Summary = c.Summary
	p.Receipt = nil
}

// ReconcilePublication works without a Team connection. It atomically records
// ordered corrections and tombstones; transport cannot query private memory.
func (s *Memory) ReconcilePublication(ctx context.Context, w string) ([]domain.MemoryPublication, error) {
	var out []domain.MemoryPublication
	err := s.deps.Store.Update(ctx, func(tx store.Tx) error {
		gs, err := tx.Memory().PublicationGrants(ctx, s.owner)
		if err != nil {
			return err
		}
		existing, err := tx.Memory().Publications(ctx, s.owner, w)
		if err != nil {
			return err
		}
		for _, g := range gs {
			if g.WorkspaceID != w {
				continue
			}
			associated := s.publicationAssociation(ctx, tx, g) == nil
			byID := map[string]*domain.MemoryPublication{}
			for i := range existing {
				p := &existing[i]
				if p.Grant.ProjectID == g.ProjectID {
					byID[p.MemoryID] = p
				}
			}
			seen := map[string]bool{}
			if g.Enabled && associated {
				scopes := []domain.MemoryScope{{OwnerID: s.owner, ProjectID: g.LocalProjectID, Repository: g.Repository}}
				if g.Semantic {
					scopes = append(scopes, domain.MemoryScope{OwnerID: s.owner, ProjectID: g.LocalProjectID})
				}
				for _, scope := range scopes {
					q := domain.MemoryQuery{Limit: 100}
					for {
						page, e := tx.Memory().List(ctx, scope, q)
						if e != nil {
							return e
						}
						for _, m := range page.Records {
							c, metadata := publicationContent(m)
							if metadata {
								f, e := tx.Memory().Freshness(ctx, scope)
								metadata = e == nil && f.SucceededAt != nil && !f.Uncertain && f.Problem == "" && f.After == nil
								if metadata && m.Type == "git_head" {
									metadata = f.Revision.Head == m.Source.GitSHA
								}
								if metadata && m.Type != "git_head" {
									_, metadata = f.Revision.PullRequests[m.Source.PullRequest]
									commitKnown := f.Revision.Head == m.Source.GitSHA
									for _, sha := range f.Revision.Refs {
										if sha == m.Source.GitSHA {
											commitKnown = true
											break
										}
									}
									metadata = metadata && f.Revision.GitHubAvailable && commitKnown
								}
							}
							semantic := g.Semantic && semanticCandidate(m)
							if !metadata && !semantic {
								continue
							}
							seen[m.ID] = true
							p := byID[m.ID]
							if p == nil {
								p = &domain.MemoryPublication{Grant: g, MemoryID: m.ID}
								byID[m.ID] = p
							}
							if p.Revision == m.Version && p.State != "revoked" && p.State != "private" {
								continue
							}
							// An explicit user revocation persists until a new source revision.
							if p.Revision == m.Version && (p.State == "revoked" || p.State == "revocation_pending") {
								continue
							}
							if !metadata && p.Event != nil && p.Event.Action != "revoke" {
								old := p.Event.Proposal.Content
								queuePublication(p, "revoke", old)
								if e = tx.Memory().SavePublication(ctx, s.owner, p); e != nil {
									return e
								}
							}
							p.Grant = g
							p.SourceRepository = scope.Repository
							p.Revision = m.Version
							p.Summary = m.Summary
							p.State = "awaiting_approval"
							if metadata {
								queuePublication(p, "propose", c)
							}
							if e = tx.Memory().SavePublication(ctx, s.owner, p); e != nil {
								if errors.Is(e, domain.ErrPublicationQueueFull) {
									continue
								}
								return e
							}
						}
						if page.Next == nil {
							break
						}
						q.After = page.Next
					}
				}
			}
			for id, p := range byID {
				if seen[id] {
					continue
				}
				if p.Event != nil && p.Event.Action != "revoke" {
					queuePublication(p, "revoke", p.Event.Proposal.Content)
					if err = tx.Memory().SavePublication(ctx, s.owner, p); err != nil {
						return err
					}
				} else if p.Event == nil {
					p.State = "private"
					if err = tx.Memory().SavePublication(ctx, s.owner, p); err != nil {
						return err
					}
				}
			}
		}
		out, err = tx.Memory().Publications(ctx, s.owner, w)
		return err
	})
	return out, err
}
func (s *Memory) ReviewPublication(ctx context.Context, w, project, id string, version int64, summary string) error {
	return s.deps.Store.Update(ctx, func(tx store.Tx) error {
		gs, err := tx.Memory().PublicationGrants(ctx, s.owner)
		if err != nil {
			return err
		}
		var g domain.PublicationGrant
		for _, v := range gs {
			if v.WorkspaceID == w && v.ProjectID == project {
				g = v
			}
		}
		if !g.Enabled || !g.Semantic {
			return domain.ErrForbidden
		}
		if err = s.publicationAssociation(ctx, tx, g); err != nil {
			return err
		}
		ps, err := tx.Memory().Publications(ctx, s.owner, w)
		if err != nil {
			return err
		}
		for i := range ps {
			p := &ps[i]
			if p.MemoryID != id || p.Grant.ProjectID != project || p.State != "awaiting_approval" || p.Revision != version {
				continue
			}
			m, err := tx.Memory().Get(ctx, domain.MemoryScope{OwnerID: s.owner, ProjectID: g.LocalProjectID, Repository: p.SourceRepository}, id)
			if err != nil {
				return err
			}
			if m.Version != version || !semanticCandidate(*m) {
				return domain.ErrConflict
			}
			c := integration.SharedMemoryContent{Kind: "observation", Summary: strings.TrimSpace(summary), Classification: "team", Verification: "verified", Source: integration.MemorySource{Kind: "human", ID: m.ID}}
			if c.Validate() != nil {
				return domain.ErrInvalid
			}
			queuePublication(p, "propose", c)
			return tx.Memory().SavePublication(ctx, s.owner, p)
		}
		return domain.ErrConflict
	})
}
func (s *Memory) RevokePublication(ctx context.Context, w, project, id string) error {
	return s.deps.Store.Update(ctx, func(tx store.Tx) error {
		ps, err := tx.Memory().Publications(ctx, s.owner, w)
		if err != nil {
			return err
		}
		for i := range ps {
			p := &ps[i]
			if p.MemoryID == id && p.Grant.ProjectID == project {
				if p.Event == nil {
					p.State = "revoked"
				} else if p.Event.Action != "revoke" {
					queuePublication(p, "revoke", p.Event.Proposal.Content)
				}
				return tx.Memory().SavePublication(ctx, s.owner, p)
			}
		}
		return domain.ErrNotFound
	})
}
func (s *Memory) PublicationEvents(ctx context.Context, w string) (integration.MemoryEventPage, error) {
	var out integration.MemoryEventPage
	if !integration.Identifier(w) {
		return out, domain.ErrInvalid
	}
	ps, err := s.ReconcilePublication(ctx, w)
	if err != nil {
		return out, err
	}
	err = s.deps.Store.Update(ctx, func(tx store.Tx) error {
		var e error
		ps, e = tx.Memory().Publications(ctx, s.owner, w)
		if e != nil {
			return e
		}
		grants, e := tx.Memory().PublicationGrants(ctx, s.owner)
		if e != nil {
			return e
		}
		enabled := map[string]domain.PublicationGrant{}
		for _, g := range grants {
			if g.WorkspaceID == w && g.Enabled && s.publicationAssociation(ctx, tx, g) == nil {
				enabled[g.ProjectID] = g
			}
		}
		out, e = tx.Memory().PublicationEvents(ctx, s.owner, w)
		if e != nil {
			return e
		}
		for i := range out.Events {
			event := &out.Events[i]
			if event.Action != "propose" {
				continue
			}
			active := false
			for _, p := range ps {
				if p.Event != nil && p.Event.ID == event.ID && p.State == "queued" {
					g, on := enabled[p.Grant.ProjectID]
					if on {
						m, e := tx.Memory().Get(ctx, domain.MemoryScope{OwnerID: s.owner, ProjectID: g.LocalProjectID, Repository: p.SourceRepository}, p.MemoryID)
						active = e == nil && m.Version == event.Revision && m.State == domain.MemoryCurrent && (event.Proposal.Content.Source.Kind == "git" || g.Semantic)
					}
				}
			}
			if !active {
				event.Action = "cancel"
				event.Proposal.Content.Summary = "Publication withdrawn before delivery"
				if e = tx.Memory().CancelPublicationEvent(ctx, s.owner, *event); e != nil {
					return e
				}
			}
		}
		// Rotate status reconciliation independently from the delivery checkpoint.
		var cursor int64
		key := "memory_publication_status_" + s.owner + "_" + w
		if e = tx.Settings().Get(ctx, key, &cursor); e != nil && !errors.Is(e, domain.ErrNotFound) {
			return e
		}
		sort.Slice(ps, func(i, j int) bool {
			if ps[i].Event == nil {
				return ps[j].Event != nil
			}
			if ps[j].Event == nil {
				return false
			}
			return ps[i].Event.Sequence < ps[j].Event.Sequence
		})
		for pass := 0; pass < 2; pass++ {
			for _, p := range ps {
				if p.Receipt != nil && p.Event != nil && p.Event.Action == "propose" && p.State != "deleted" && p.State != "superseded" && p.Event.Sequence > cursor && len(out.Events)+len(out.Tracking) < integration.MaxMemoryBatch {
					out.Tracking = append(out.Tracking, *p.Event)
				}
			}
			if len(out.Tracking) > 0 || cursor == 0 {
				break
			}
			cursor = 0
		}
		// Bound encoded bytes too: JSON escaping can expand reviewed text.
		for {
			raw, e := json.Marshal(out)
			if e != nil {
				return e
			}
			if len(raw) <= 250<<10 {
				break
			}
			if len(out.Tracking) > 0 {
				out.Tracking = out.Tracking[:len(out.Tracking)-1]
			} else {
				out.Events = out.Events[:len(out.Events)-1]
			}
		}
		if len(out.Tracking) > 0 {
			cursor = out.Tracking[len(out.Tracking)-1].Sequence
		}
		return tx.Settings().Set(ctx, key, cursor, s.deps.now())
	})
	return out, err
}
func (s *Memory) AckPublication(ctx context.Context, w string, a integration.MemoryReceipt) error {
	return s.deps.Store.Update(ctx, func(tx store.Tx) error { return tx.Memory().AckPublication(ctx, s.owner, w, a) })
}
