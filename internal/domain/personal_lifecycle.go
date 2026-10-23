package domain

// Ownership is local provenance. Display locations never convey ownership.
type PersonalCollectionUsage struct {
	CollectionID     string                       `json:"collectionId"`
	Name             string                       `json:"name"`
	Revision         int64                        `json:"revision"`
	OwnerWorkspaceID string                       `json:"ownerWorkspaceId"`
	RecordCount      int64                        `json:"recordCount"`
	Generation       int64                        `json:"generation"`
	ReferenceCount   int64                        `json:"referenceCount"`
	Workspaces       []PersonalWorkspaceReference `json:"workspaces"`
}
type PersonalWorkspaceReference struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type PersonalDeletion struct {
	WorkspaceID string                       `json:"workspaceId"`
	Revision    int64                        `json:"revision"`
	Name        string                       `json:"name"`
	Mode        string                       `json:"mode"` // keep_data or delete_data
	Collections []PersonalDeletionCollection `json:"collections"`
	Fingerprint string                       `json:"fingerprint"`
}
type PersonalDeletionCollection struct {
	PersonalCollectionUsage
	Delete bool `json:"delete"`
}
type PersonalDeleteRequest struct {
	Revision    int64  `json:"revision"`
	Mode        string `json:"mode"`
	Fingerprint string `json:"fingerprint"`
	Confirmed   bool   `json:"confirmed"`
}
