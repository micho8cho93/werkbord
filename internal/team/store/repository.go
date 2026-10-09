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
	Progress(context.Context, string, string, string, string) (domain.ProgressRecord, error)
	SaveProgress(context.Context, string, string, string, domain.ProgressRecord) error
	TicketProgress(context.Context, string, string, string) ([]domain.ProgressRecord, error)
	SecurityQueries
	WorkspaceQueries
	MemberQueries
	ProjectQueries
	TicketQueries
	ActivityQueries
	InviteQueries
	DeviceQueries
	NetworkQueries
	MessageQueries
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
	// SetMemberRole changes a person's role. It never makes or unmakes the owner: the guard is in the write.
	SetMemberRole(ctx context.Context, workspaceID, id string, role domain.Role) error
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
	// SearchTickets returns bounded summaries, restricted to onlyMemberID's projects
	// unless it is empty (the service has authorized viewing all projects).
	SearchTickets(ctx context.Context, workspaceID, onlyMemberID, query string, limit int) ([]TicketMatch, error)
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
	// SaveDeviceProfile records what a device says about the kind of machine it is; DeviceProfiles lists them by device ID.
	SaveDeviceProfile(ctx context.Context, workspaceID string, p domain.DeviceProfile) error
	DeviceProfiles(ctx context.Context, workspaceID string) (map[string]domain.DeviceProfile, error)
}

// NetworkQueries are the customer-owned private network's records: its public
// settings, where each device sits on it, the certificates issued (by fingerprint),
// the credential each device uses for the API, and the invitations and requests by
// which devices join. None of it is secret: credentials and invitation credentials are
// hashes, and the keys that sign for the workspace are not in storage at all.
type NetworkQueries interface {
	InsertNetworkSettings(ctx context.Context, s domain.NetworkSettings) error
	// NetworkSettings returns the workspace's network settings, or domain.ErrNotFound
	// when it has no private network.
	NetworkSettings(ctx context.Context, workspaceID string) (domain.NetworkSettings, error)
	SetEnrollmentApproval(ctx context.Context, workspaceID string, p domain.ApprovalPolicy, at time.Time) error

	// InsertDeviceNetwork places a device on the network. An address already used in the
	// workspace is domain.ErrConflict: the schema guarantees no two devices share one.
	InsertDeviceNetwork(ctx context.Context, n domain.DeviceNetwork) error
	DeviceNetwork(ctx context.Context, workspaceID, deviceID string) (domain.DeviceNetwork, error)
	DeviceNetworks(ctx context.Context, workspaceID string) ([]domain.DeviceNetwork, error)
	// SaveDeviceNetwork writes a device's groups, roles and endpoints.
	SaveDeviceNetwork(ctx context.Context, n domain.DeviceNetwork) error
	// SetReachability records the outcome of a check of a device. A check that succeeded
	// from outside also sets when that last happened.
	SetReachability(ctx context.Context, workspaceID, deviceID string, r domain.Reachability, at time.Time, externalOK bool) error
	// SetProvision keeps secrets sealed to a device until it collects them; ClearProvision
	// removes them; Provision returns them (nil when there are none).
	SetProvision(ctx context.Context, workspaceID, deviceID string, sealed []byte, at time.Time) error
	Provision(ctx context.Context, workspaceID, deviceID string) ([]byte, error)
	ClearProvision(ctx context.Context, workspaceID, deviceID string, at time.Time) error

	InsertCertificate(ctx context.Context, workspaceID string, c domain.NetworkCertificate) error
	// RevokeCertificates marks every certificate of a device revoked, once, and returns how many it marked.
	RevokeCertificates(ctx context.Context, workspaceID, deviceID string, at time.Time) (int, error)
	// Blocklist returns the fingerprints of revoked certificates that have not yet expired
	// (an expired certificate is refused anyway).
	Blocklist(ctx context.Context, workspaceID string, now time.Time) ([]string, error)

	// SetDeviceCredential stores the hash of a device's API token, replacing any other.
	SetDeviceCredential(ctx context.Context, workspaceID, deviceID, tokenHash string, at time.Time) error
	// DeviceByCredentialHash finds the device a token belongs to, revoked or not.
	DeviceByCredentialHash(ctx context.Context, tokenHash string) (domain.Device, error)
	DeleteDeviceCredential(ctx context.Context, workspaceID, deviceID string) error

	InsertEnrollInvitation(ctx context.Context, inv domain.EnrollInvitation, credentialHash []byte) error
	// EnrollInvitationByID finds an invitation by its (public) ID in any workspace, with
	// the hash of its credential, for the public enrollment endpoint.
	EnrollInvitationByID(ctx context.Context, id string) (domain.EnrollInvitation, []byte, error)
	EnrollInvitations(ctx context.Context, workspaceID string) ([]domain.EnrollInvitation, error)
	// UseEnrollInvitation marks an open, unexpired invitation used. It reports false when
	// it was not open or had expired: the guard is in the write, so two requests with one
	// credential cannot both succeed.
	UseEnrollInvitation(ctx context.Context, workspaceID, id string, now time.Time) (bool, error)
	WithdrawEnrollInvitation(ctx context.Context, workspaceID, id string) (bool, error)

	InsertEnrollment(ctx context.Context, e domain.Enrollment) error
	Enrollment(ctx context.Context, workspaceID, id string) (domain.Enrollment, error)
	EnrollmentByID(ctx context.Context, id string) (domain.Enrollment, error)
	Enrollments(ctx context.Context, workspaceID string, state domain.EnrollmentState) ([]domain.Enrollment, error)
	// DecideEnrollment approves or denies a pending enrollment; false when it was not pending.
	DecideEnrollment(ctx context.Context, workspaceID, id string, state domain.EnrollmentState, memberID, by string, at time.Time) (bool, error)
	// SetEnrollmentMember records the member an approved enrollment became a device of.
	SetEnrollmentMember(ctx context.Context, workspaceID, id, memberID string) error
	// MarkEnrollmentDelivered records that the device collected what it was issued, once;
	// false when it already had.
	MarkEnrollmentDelivered(ctx context.Context, workspaceID, id string, at time.Time) (bool, error)
}

// MessageQueries are the mailbox of signed requests between a person's own devices. The workspace stores and
// routes what a device signed; it never makes or changes one.
type MessageQueries interface {
	// InsertMessage stores a request. The same message twice is domain.ErrConflict.
	InsertMessage(ctx context.Context, m domain.DeviceMessage) error
	Message(ctx context.Context, workspaceID, id string) (domain.DeviceMessage, error)
	// QueuedMessagesFor lists the unexpired requests waiting for a device, oldest first.
	QueuedMessagesFor(ctx context.Context, workspaceID, deviceID string, now time.Time, limit int) ([]domain.DeviceMessage, error)
	CountQueuedMessagesFor(ctx context.Context, workspaceID, deviceID string, now time.Time) (int, error)
	// MessagesOfMember lists a person's most recent requests, newest first.
	MessagesOfMember(ctx context.Context, workspaceID, memberID string, limit int) ([]domain.DeviceMessage, error)
	// DecideMessage records what the device a request was for did with it, once, and only if that device says so and the
	// request has not expired. It reports whether it decided it.
	DecideMessage(ctx context.Context, workspaceID, id, toDeviceID string, state domain.MessageState, result string, now time.Time) (bool, error)
	// PurgeMessages removes messages decided or expired before the cutoff.
	PurgeMessages(ctx context.Context, workspaceID string, before time.Time) (int, error)
}
