package store

import (
	"context"
	"time"

	"devboard/internal/team/domain"
)

// Store is Team's storage as the service layer sees it. An implementation runs a
// function inside one transaction and gives it a Tx; that is the whole of the
// contract the use cases rely on:
//
//   - View sees one consistent snapshot and cannot change anything.
//   - Update is atomic and serialised against every other Update: the function's
//     reads and writes happen as if no other write happened in between, and either
//     all of its writes are kept (it returned nil) or none are.
//   - Every guard a use case depends on (a ticket is claimed by one person, an
//     invite is used up to its limit, a device is never un-revoked) is also
//     enforced inside the Tx method that writes it, not only by the use case's
//     earlier read, so an implementation does not need to be serialisable for those
//     to hold.
//   - Reads inside Update see the function's own earlier writes.
//   - A Tx must not be used after the function returns, and a function must have
//     no effect outside its Tx: an implementation is free to call it more than once.
//
// Errors a Tx returns for the domain's reasons are the domain's (domain.ErrNotFound,
// domain.ErrConflict, ErrInviteUnusable), never the storage's, so callers match
// them with errors.Is whatever is underneath.
type Store interface {
	View(ctx context.Context, fn func(Tx) error) error
	Update(ctx context.Context, fn func(Tx) error) error
	// SchemaVersion reports the storage format's version, for diagnostics.
	SchemaVersion(ctx context.Context) (int, error)
	// Ping checks that the storage answers.
	Ping(ctx context.Context) error
	Close() error
}

// Tx is a transaction's queries. It is deliberately a plain list of questions in
// the domain's terms, with no SQL, row or cursor in any signature.
type Tx interface {
	WorkspaceQueries
	MemberQueries
	ProjectQueries
	TicketQueries
	ActivityQueries
	InviteQueries
	DeviceQueries
}

// WorkspaceQueries are the workspace's own record and its change counter.
type WorkspaceQueries interface {
	InsertWorkspace(ctx context.Context, w domain.Workspace) error
	Workspace(ctx context.Context, id string) (domain.Workspace, error)
	RenameWorkspace(ctx context.Context, id, name string) error
	WorkspaceRevision(ctx context.Context, workspaceID string) (int64, error)
}

// MemberQueries are the people in a workspace.
type MemberQueries interface {
	InsertMember(ctx context.Context, m domain.Member, tokenHash string) error
	Member(ctx context.Context, workspaceID, id string) (domain.Member, error)
	MemberByTokenHash(ctx context.Context, hash string) (domain.Member, error)
	Members(ctx context.Context, workspaceID string) ([]domain.Member, error)
	HasMemberNamed(ctx context.Context, workspaceID, name string) (bool, error)
	DeleteMember(ctx context.Context, workspaceID, id string) error
	SetMemberToken(ctx context.Context, workspaceID, id, hash string) error
}

// ProjectQueries are projects and who is on them.
type ProjectQueries interface {
	InsertProject(ctx context.Context, p domain.Project) error
	Project(ctx context.Context, workspaceID, id string) (domain.Project, error)
	Projects(ctx context.Context, workspaceID, onlyMemberID string) ([]domain.Project, error)
	UpdateProject(ctx context.Context, p domain.Project) error
	BumpRevision(ctx context.Context, workspaceID, projectID string) (int64, error)
	Revision(ctx context.Context, workspaceID, projectID string) (int64, error)

	AddProjectMember(ctx context.Context, workspaceID string, pm domain.ProjectMember) error
	SetProjectMemberRole(ctx context.Context, workspaceID, projectID, memberID string, role domain.ProjectRole) error
	ProjectRole(ctx context.Context, workspaceID, projectID, memberID string) (domain.ProjectRole, bool, error)
	ProjectRoles(ctx context.Context, workspaceID, memberID string) (map[string]domain.ProjectRole, error)
	RemoveProjectMember(ctx context.Context, workspaceID, projectID, memberID string) (bool, error)
	IsProjectMember(ctx context.Context, workspaceID, projectID, memberID string) (bool, error)
	ProjectMembers(ctx context.Context, workspaceID, projectID string) ([]domain.ProjectMember, error)

	UpsertBranch(ctx context.Context, workspaceID string, b domain.ProjectBranch) error
	DeleteBranch(ctx context.Context, workspaceID, projectID, name string) (bool, error)
	Branches(ctx context.Context, workspaceID, projectID string) ([]domain.ProjectBranch, error)
}

// TicketQueries are the board.
type TicketQueries interface {
	NextTicketNumber(ctx context.Context, workspaceID string) (int, error)
	InsertTicket(ctx context.Context, workspaceID string, k domain.Ticket) error
	Ticket(ctx context.Context, workspaceID, projectID, id string) (domain.Ticket, error)
	Tickets(ctx context.Context, workspaceID, projectID string) ([]domain.Ticket, error)
	// ClaimTicket takes an available ticket for a member; it reports false when
	// someone else got there first. The guard is in the write itself.
	ClaimTicket(ctx context.Context, workspaceID, projectID, id, memberID, branch string, now time.Time) (bool, error)
	SaveTicket(ctx context.Context, workspaceID string, k domain.Ticket) (domain.Ticket, error)
	ReplaceCommits(ctx context.Context, ticketID string, commits []domain.Commit) error
	TicketCommits(ctx context.Context, ticketIDs []string) (map[string][]domain.Commit, error)
	ReleaseHeld(ctx context.Context, workspaceID, projectID, memberID string, now time.Time) ([]domain.Ticket, error)
	ProjectsOfMemberWithHeldTickets(ctx context.Context, workspaceID, memberID string) ([]string, error)
	TicketCounts(ctx context.Context, workspaceID string) (map[string]map[domain.TicketStatus]int, error)
	HeldTickets(ctx context.Context, workspaceID string) ([]domain.Ticket, error)
}

// ActivityQueries are the history.
type ActivityQueries interface {
	AddActivity(ctx context.Context, workspaceID string, a domain.Activity) error
	Activity(ctx context.Context, workspaceID, projectID string, before int64, limit int) ([]domain.Activity, error)
	ActivityAfter(ctx context.Context, workspaceID string, after int64, only []string, limit int) ([]domain.Activity, error)
	LatestActivityID(ctx context.Context, workspaceID string) (int64, error)
	LatestTicketActivity(ctx context.Context, workspaceID string, ticketIDs []string, kinds []domain.ActivityKind) (map[string]domain.Activity, error)
}

// InviteQueries are project invite links.
type InviteQueries interface {
	InsertInvite(ctx context.Context, workspaceID string, i domain.Invite, codeHash string) error
	Invites(ctx context.Context, workspaceID, projectID string) ([]domain.Invite, error)
	RevokeInvite(ctx context.Context, workspaceID, projectID, id string, now time.Time) error
	// UseInvite counts one use of an invite and returns it, or ErrInviteUnusable.
	UseInvite(ctx context.Context, codeHash string, now time.Time) (workspaceID string, inv domain.Invite, err error)
}

// DeviceQueries are the workspace's registry of devices: public, non-secret facts
// about the machines that take part, and never a private key, a credential, a
// path or an environment.
type DeviceQueries interface {
	// InsertDevice records a new device. A device ID or a public key already
	// registered in the workspace is domain.ErrConflict.
	InsertDevice(ctx context.Context, d domain.Device) error
	// Device returns a device of a workspace, revoked or not.
	Device(ctx context.Context, workspaceID, id string) (domain.Device, error)
	// Devices lists a workspace's devices; with a member ID, only theirs. Revoked
	// devices are included: they are history, and a verifier must be told.
	Devices(ctx context.Context, workspaceID, memberID string) ([]domain.Device, error)
	// DevicesWithCapability lists the devices that currently hold a capability:
	// not revoked, and listing it.
	DevicesWithCapability(ctx context.Context, workspaceID string, c domain.Capability) ([]domain.Device, error)
	// SaveDevice writes a device's name, capabilities and role statuses. It never
	// changes a revoked device (domain.ErrConflict): revocation is permanent.
	SaveDevice(ctx context.Context, d domain.Device) error
	// RecordDeviceSeen sets when a device was last seen, if later than recorded.
	RecordDeviceSeen(ctx context.Context, workspaceID, id string, at time.Time) error
	// RevokeDevice revokes a device and ends its host roles. It reports whether it
	// was the call that revoked it; revoking a revoked device changes nothing.
	RevokeDevice(ctx context.Context, workspaceID, id string, at time.Time) (bool, error)
}
