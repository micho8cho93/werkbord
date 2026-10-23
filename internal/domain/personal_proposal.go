package domain

// PersonalChange is a closed, record-free proposal. Collection revisions are
// required even for layout-only edits, so the preview cannot approve stale schemas.
type PersonalChange struct {
	WorkspaceID string                     `json:"workspaceId,omitempty"`
	Revision    int64                      `json:"revision,omitempty"`
	Workspace   PersonalWorkspaceSpec      `json:"workspace"`
	Collections []PersonalCollectionChange `json:"collections"`
}
type PersonalCollectionChange struct {
	ID       string                 `json:"id"`
	Revision int64                  `json:"revision"`
	Spec     PersonalCollectionSpec `json:"spec"`
}
type PersonalSnapshot struct {
	Workspace   *PersonalWorkspace   `json:"workspace"`
	Collections []PersonalCollection `json:"collections"`
}
