package service

import (
	"context"
	"devboard/internal/integration"
	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// Relationships uses the same transaction, principal and full-scope checks as
// Sally memory search. Hidden edges never become traversal vertices or cursors.
func (s *Service) Relationships(ctx context.Context, a Actor, project string, depth int) ([]integration.RelationshipResult, error) {
	out := []integration.RelationshipResult{}
	if a.MCPGrantID != "" {
		return out, domain.ErrForbidden
	}
	if depth < 1 || depth > 4 {
		return out, domain.ErrInvalid
	}
	err := s.db.View(AuthenticatedContext(ctx, a), func(tx store.Tx) error {
		frontier := []string{project}
		seen := map[string]bool{project: true}
		used := map[string]bool{}
		for d := 0; d < depth; d++ {
			next := []string{}
			for _, pid := range frontier {
				after := ""
				for pages := 0; pages < 200; pages++ {
					page, e := tx.SearchMemories(ctx, memoryPrincipal(a), pid, "", after, "", s.stamp())
					if e != nil {
						return e
					}
					for _, m := range page.Records {
						r := m.Content.Relationship
						if r == nil || used[m.ID] || m.State != "published" || m.Approval == nil || m.Content.Verification == "rejected" || !r.MatchesScope(m.Scope) {
							continue
						}
						available := true
						for _, target := range []integration.MemoryTarget{r.Source, r.Target} {
							p, e := tx.Project(ctx, a.Workspace.ID, target.ProjectID)
							if e != nil || p.Archived {
								available = false
								break
							}
						}
						if !available {
							continue
						}
						used[m.ID] = true
						status := integration.RelationshipStatus(*r, s.stamp())
						if m.Content.Verification != "verified" {
							status = "provisional"
						}
						if status == "verified" && (m.Freshness == nil || m.Freshness.Status == "uncertain") {
							status = "uncertain"
						}
						if m.Freshness != nil && (m.Freshness.Conflict || m.Freshness.Status == "stale") {
							status = "stale"
						}
						out = append(out, integration.RelationshipResult{ID: m.ID, Version: m.Version, Relationship: *r, Status: status, Recommendation: integration.RelationshipRecommendation})
						for _, t := range []integration.MemoryTarget{r.Source, r.Target} {
							if !seen[t.ProjectID] {
								seen[t.ProjectID] = true
								next = append(next, t.ProjectID)
							}
						}
					}
					if !page.More {
						break
					}
					after = page.Next
				}
			}
			frontier = next
		}
		return nil
	})
	if err != nil {
		return []integration.RelationshipResult{}, err
	}
	return out, nil
}
