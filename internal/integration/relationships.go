package integration

import (
	"errors"
	"time"
)

// RepositoryRelationship is bounded, sanitised metadata. Direction describes the
// consumer/dependent (Source) and provider/prerequisite (Target), never execution.
// Scope and read/restricted-read permissions are inherited from its memory record.
type RepositoryRelationship struct {
	Source         MemoryTarget `json:"source"`
	Target         MemoryTarget `json:"target"`
	Type           string       `json:"type"`
	Evidence       string       `json:"evidence"` // manual or a repository-relative manifest
	SourceRevision string       `json:"sourceRevision,omitempty"`
	TargetRevision string       `json:"targetRevision,omitempty"`
	Inferred       bool         `json:"inferred"`
	LastVerified   *time.Time   `json:"lastVerified,omitempty"`
}

func (r RepositoryRelationship) Validate() error {
	for _, t := range []MemoryTarget{r.Source, r.Target} {
		id, e := MemoryRepository(t.Repository)
		if !Identifier(t.ProjectID) || e != nil || id != t.Repository {
			return errors.New("invalid relationship endpoint")
		}
	}
	if r.Source.ProjectID == r.Target.ProjectID || r.Source.Repository == r.Target.Repository {
		return errors.New("relationship requires distinct repositories and projects")
	}
	switch r.Type {
	case "package_dependency", "api_consumer", "shared_authentication", "deployment_dependency", "manual_association":
	default:
		return errors.New("invalid relationship type")
	}
	switch r.Evidence {
	case "manual", "package.json", "go.mod":
	default:
		return errors.New("unsupported relationship evidence")
	}
	if r.Inferred && r.Evidence == "manual" {
		return errors.New("inference requires manifest evidence")
	}
	for _, sha := range []string{r.SourceRevision, r.TargetRevision} {
		if sha != "" && !fullSHA(sha) {
			return errors.New("invalid relationship revision")
		}
	}
	if r.LastVerified != nil && r.LastVerified.IsZero() {
		return errors.New("invalid relationship verification")
	}
	return nil
}
func fullSHA(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
func (r RepositoryRelationship) MatchesScope(s SharedMemoryScope) bool {
	return s.Kind == "cross_repository" && len(s.Targets) == 2 && ((s.Targets[0] == r.Source && s.Targets[1] == r.Target) || (s.Targets[1] == r.Source && s.Targets[0] == r.Target))
}

type RelationshipResult struct {
	Version        int64                  `json:"version"`
	ID             string                 `json:"id"`
	Relationship   RepositoryRelationship `json:"relationship"`
	Status         string                 `json:"status"`
	Recommendation string                 `json:"recommendation"`
}

const RelationshipRecommendation = "Check compatibility and whether coordinated changes are needed. This relationship is not evidence of a breaking change."

// TraverseRelationships only accepts pre-authorised records. It never discovers
// hidden vertices, counts skipped edges, executes Git, or follows supersession.
func TraverseRelationships(start MemoryTarget, edges []RelationshipResult, depth int) []RelationshipResult {
	if depth < 1 || depth > 4 {
		return []RelationshipResult{}
	}
	seen := map[MemoryTarget]bool{start: true}
	used := map[string]bool{}
	frontier := []MemoryTarget{start}
	out := []RelationshipResult{}
	for d := 0; d < depth && len(frontier) > 0; d++ {
		next := []MemoryTarget{}
		for _, node := range frontier {
			for _, e := range edges {
				r := e.Relationship
				if used[e.ID] || r.Validate() != nil {
					continue
				}
				var other MemoryTarget
				if r.Source == node {
					other = r.Target
				} else if r.Target == node {
					other = r.Source
				} else {
					continue
				}
				used[e.ID] = true
				e.Recommendation = RelationshipRecommendation
				out = append(out, e)
				if !seen[other] {
					seen[other] = true
					next = append(next, other)
				}
			}
		}
		frontier = next
	}
	return out
}

func RelationshipStatus(r RepositoryRelationship, now time.Time) string {
	if r.LastVerified == nil {
		return "provisional"
	}
	if r.LastVerified.After(now) || now.Sub(*r.LastVerified) > 24*time.Hour {
		return "stale"
	}
	return "verified"
}
