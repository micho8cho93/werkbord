package store

import (
	"context"
	"werkbord/internal/domain"
)

// PersonalRepo belongs exclusively to the current Individual controller owner.
// Queries use bounded keyset pagination. Record operations always include their
// collection; replacements and deletions compare revisions. Workspace references
// restrict collection deletion, but never cascade into personal records.
type PersonalRepo interface {
	WorkspaceIcons(context.Context) ([]domain.PersonalWorkspaceIcon, error)
	CollectionUsage(context.Context, string) (*domain.PersonalCollectionUsage, error)
	DeleteCollectionRecords(context.Context, string) error
	WorkspaceHistory(context.Context, string) ([]domain.PersonalMeta, error)
	WorkspaceRevision(context.Context, string, int64) (*domain.PersonalWorkspace, error)
	WorkspaceRequest(context.Context, string) (string, error)
	SaveWorkspaceRequest(context.Context, string, string) error
	CreateWorkspace(context.Context, *domain.PersonalWorkspace) error
	GetWorkspace(context.Context, string) (*domain.PersonalWorkspace, error)
	ListWorkspaces(context.Context, domain.PersonalQuery) ([]domain.PersonalWorkspace, error)
	UpdateWorkspace(context.Context, *domain.PersonalWorkspace) error
	DeleteWorkspace(context.Context, string, int64) error
	CreateCollection(context.Context, *domain.PersonalCollection) error
	GetCollection(context.Context, string) (*domain.PersonalCollection, error)
	ListCollections(context.Context, domain.PersonalQuery) ([]domain.PersonalCollection, error)
	UpdateCollection(context.Context, *domain.PersonalCollection) error
	DeleteCollection(context.Context, string, int64) error
	CreateRecord(context.Context, *domain.PersonalRecord) error
	GetRecord(context.Context, string, string) (*domain.PersonalRecord, error)
	ListRecords(context.Context, string, domain.PersonalQuery) ([]domain.PersonalRecord, error)
	UpdateRecord(context.Context, *domain.PersonalRecord) error
	DeleteRecord(context.Context, string, string, int64) error
}
