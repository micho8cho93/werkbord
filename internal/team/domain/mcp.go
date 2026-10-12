package domain

import "time"

// MCPGrant is one revocable, project- and operation-scoped client credential.
// The bearer is never stored; TokenHash is only its SHA-256 digest.
type MCPGrant struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspaceId"`
	MemberID    string     `json:"memberId"`
	Name        string     `json:"name"`
	Projects    []string   `json:"projects"`
	Operations  []string   `json:"operations"`
	TokenHash   string     `json:"-"`
	CreatedAt   time.Time  `json:"createdAt"`
	RevokedAt   *time.Time `json:"revokedAt,omitempty"`
}

// MCPTaskPlanProposal is a pending or resolved external batch. PlanJSON is the
// editable plan; the original request digest remains fixed for replay safety.
type MCPTaskPlanProposal struct {
	ID             string     `json:"id"`
	WorkspaceID    string     `json:"workspaceId"`
	MemberID       string     `json:"memberId"`
	GrantID        string     `json:"grantId"`
	ProjectID      string     `json:"projectId"`
	IdempotencyKey string     `json:"idempotencyKey"`
	RequestDigest  string     `json:"-"`
	PlanDigest     string     `json:"-"`
	PlanJSON       string     `json:"-"`
	State          string     `json:"state"`
	ResultJSON     string     `json:"-"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	ResolvedAt     *time.Time `json:"resolvedAt,omitempty"`
}
