package service

import (
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"devboard/internal/deviceid"
	"devboard/internal/enrollment"
	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// Joining the workspace.
//
// An administrator makes an invitation: a signed, expiring, single-use statement of who
// may join as what, and where the workspace's own machines can be found. The person
// gives it to the joining device (a link, or a QR code). The device finds one of the
// workspace's machines at an address in the invitation, makes it prove it is the
// workspace, and presents the invitation's one-time credential with its own public
// keys. Depending on the workspace's policy it is then enrolled at once or waits for an
// administrator. Enrolling makes it a device of a member, gives it an address and a
// certificate on the private network, and a credential for the workspace's API.
//
// Nothing about the invitation outlives it: the workspace keeps only a hash of the
// credential, the credential is spent when it is used, and what the device receives
// (certificate, credential) is for that device alone and is collected once.

// EnrollInviteInput is what making an invitation takes.
type EnrollInviteInput struct {
	// Label says who it is for, for the administrators' own reference.
	Label string
	// ForMemberID names an existing member the new device will belong to. Empty makes a
	// new member, with Role.
	ForMemberID string
	// Role is what a new member becomes (member by default; admin needs the owner).
	Role domain.Role
	// Capabilities are what the device will do. The host capabilities need
	// devices.manage, as they do anywhere else.
	Capabilities []domain.Capability
	// TTL is how long the invitation works (a day by default, 14 days at most).
	TTL time.Duration
	// RequireApproval asks that an administrator approve the device even if the
	// workspace's policy would not. It cannot lower the policy.
	RequireApproval bool
}

// EnrollInviteResult is a new invitation. The link is shown here and nowhere else.
type EnrollInviteResult struct {
	Invitation domain.EnrollInvitation `json:"invitation"`
	// Link is the signed invitation, "werkbord://join/…", for a link or a QR code.
	Link string `json:"link"`
	// Fingerprint is the workspace's, to read out so the person joining can check it.
	Fingerprint string `json:"fingerprint"`
}

// Bounds on an invitation's life.
const (
	DefaultEnrollTTL = 24 * time.Hour
	MaxEnrollTTL     = 14 * 24 * time.Hour
	MinEnrollTTL     = time.Minute
)

// CreateEnrollInvitation makes an invitation.
func (s *Service) CreateEnrollInvitation(ctx context.Context, a Actor, in EnrollInviteInput) (EnrollInviteResult, error) {
	net, err := s.needNetwork()
	if err != nil {
		return EnrollInviteResult{}, err
	}
	label := strings.TrimSpace(in.Label)
	if len(label) > 120 {
		return EnrollInviteResult{}, fmt.Errorf("%w: a label is at most 120 characters", domain.ErrInvalid)
	}
	ttl := in.TTL
	if ttl == 0 {
		ttl = DefaultEnrollTTL
	}
	if ttl < MinEnrollTTL || ttl > MaxEnrollTTL {
		return EnrollInviteResult{}, fmt.Errorf("%w: an invitation lasts between a minute and %d days", domain.ErrInvalid, int(MaxEnrollTTL/(24*time.Hour)))
	}
	caps, err := domain.CleanCapabilities(in.Capabilities)
	if err != nil {
		return EnrollInviteResult{}, err
	}
	role := in.Role
	if role == "" {
		role = domain.RoleMember
	}
	if !role.Valid() || role == domain.RoleOwner {
		return EnrollInviteResult{}, fmt.Errorf("%w: an invitation can make someone a member or an admin, never the owner", domain.ErrInvalid)
	}
	for _, c := range caps {
		if c.Infrastructure() {
			if err := a.require(domain.PermDevicesManage, "invite a device that will be a "+string(c)); err != nil {
				return EnrollInviteResult{}, err
			}
		}
	}

	var out EnrollInviteResult
	err = s.db.Update(ctx, func(tx store.Tx) error {
		settings, err := tx.NetworkSettings(ctx, a.Workspace.ID)
		if err != nil {
			return errNoNetwork()
		}
		info := net.Info()
		if !enrollment.SameFingerprint(info.Fingerprint, settings.Fingerprint) {
			return fmt.Errorf("%w: this host does not hold the keys of this workspace's network", domain.ErrConflict)
		}
		inv := domain.EnrollInvitation{ID: enrollment.NewID(), WorkspaceID: a.Workspace.ID, Label: label, Role: role, Capabilities: caps, CreatedBy: a.Member.ID,
			RequireApproval: in.RequireApproval || settings.EnrollmentApproval == domain.ApprovalAdmin}
		switch {
		case in.ForMemberID == "":
			inv.Role = role
			if err := a.require(domain.PermMembersManage, "invite someone to the workspace"); err != nil {
				return err
			}
			if !a.Member.Role.CanManage(role) {
				return forbidden("invite someone as " + string(role))
			}
		default:
			target, err := tx.Member(ctx, a.Workspace.ID, in.ForMemberID)
			if err != nil {
				return err
			}
			inv.ForMemberID, inv.Role = target.ID, target.Role
			if target.ID == a.Member.ID {
				if err := a.require(domain.PermDevicesOwn, "add a device"); err != nil {
					return err
				}
			} else if !a.Member.Can(domain.PermMembersManage) || !a.Member.Role.CanManage(target.Role) {
				return forbidden("add a device for " + target.Name)
			}
		}
		endpoints, err := s.bootstrapEndpoints(ctx, tx, a.Workspace.ID)
		if err != nil {
			return err
		}
		if len(endpoints) == 0 {
			return fmt.Errorf("%w: no Workspace Host has said where it can be reached to enroll new devices yet; set WERKBORD_TEAM_ENDPOINTS on a host (docs/TEAM_NETWORK.md)", domain.ErrConflict)
		}
		now := s.stamp()
		inv.CreatedAt, inv.ExpiresAt, inv.State = now, now.Add(ttl), domain.InvitationOpen
		credential, hash := enrollment.NewCredential()
		capNames := make([]string, 0, len(caps))
		for _, c := range caps {
			capNames = append(capNames, string(c))
		}
		link, err := net.SignInvitation(enrollment.Invitation{ID: inv.ID, WorkspaceName: a.Workspace.Name, Endpoints: endpoints, Credential: credential,
			Role: string(inv.Role), Capabilities: capNames, IssuedAt: now.Unix(), ExpiresAt: inv.ExpiresAt.Unix()})
		if err != nil {
			return fmt.Errorf("%w: %v", domain.ErrInvalid, err)
		}
		if err := tx.InsertEnrollInvitation(ctx, inv, hash); err != nil {
			return err
		}
		out = EnrollInviteResult{Invitation: inv, Link: link, Fingerprint: settings.Fingerprint}
		return nil
	})
	return out, err
}

// bootstrapEndpoints are where the workspace's own machines answer new devices, the
// reachable ones first: Connectivity Hosts, then Workspace Hosts. More than one when
// there is more than one, so that no single host is the only way in.
func (s *Service) bootstrapEndpoints(ctx context.Context, tx store.Tx, workspaceID string) ([]string, error) {
	devs, err := tx.Devices(ctx, workspaceID, "")
	if err != nil {
		return nil, err
	}
	nets, err := tx.DeviceNetworks(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	byDev := map[string]domain.Device{}
	for _, d := range devs {
		byDev[d.ID] = d
	}
	type cand struct {
		ep   string
		rank int
	}
	var cands []cand
	seen := map[string]bool{}
	for _, n := range nets {
		d, ok := byDev[n.DeviceID]
		if !ok || d.Revoked() || !(d.Has(domain.CapabilityWorkspaceHost) || d.Has(domain.CapabilityConnectivityHost)) {
			continue
		}
		for _, e := range n.BootstrapEndpoints {
			if seen[e] {
				continue
			}
			seen[e] = true
			rank := 2
			if domain.ExternallyAddressable(domain.AddressKind(e)) {
				rank = 0
			} else if d.Has(domain.CapabilityConnectivityHost) {
				rank = 1
			}
			cands = append(cands, cand{e, rank})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].rank < cands[j].rank })
	var out []string
	for _, c := range cands {
		if len(out) < enrollment.MaxEndpoints {
			out = append(out, c.ep)
		}
	}
	return out, nil
}

// ListEnrollInvitations lists the workspace's invitations.
func (s *Service) ListEnrollInvitations(ctx context.Context, a Actor) ([]domain.EnrollInvitation, error) {
	if err := a.require(domain.PermMembersManage, "see invitations"); err != nil {
		return nil, err
	}
	var out []domain.EnrollInvitation
	err := s.db.View(ctx, func(tx store.Tx) (err error) { out, err = tx.EnrollInvitations(ctx, a.Workspace.ID); return })
	return out, err
}

// WithdrawEnrollInvitation stops an invitation that has not been used.
func (s *Service) WithdrawEnrollInvitation(ctx context.Context, a Actor, id string) error {
	if err := a.require(domain.PermMembersManage, "withdraw invitations"); err != nil {
		return err
	}
	return s.db.Update(ctx, func(tx store.Tx) error {
		ok, err := tx.WithdrawEnrollInvitation(ctx, a.Workspace.ID, id)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: invitation (it may already have been used)", domain.ErrNotFound)
		}
		return nil
	})
}

// ---- approving ----

// ListEnrollments lists the requests to join, pending ones by default.
func (s *Service) ListEnrollments(ctx context.Context, a Actor, state domain.EnrollmentState) ([]domain.Enrollment, error) {
	if err := a.require(domain.PermMembersManage, "see requests to join"); err != nil {
		return nil, err
	}
	var out []domain.Enrollment
	err := s.db.View(ctx, func(tx store.Tx) (err error) {
		out, err = tx.Enrollments(ctx, a.Workspace.ID, state)
		return
	})
	for i := range out {
		if k, err := deviceid.ParsePublicKey(out[i].DeviceKey); err == nil {
			out[i].Fingerprint = deviceid.Fingerprint(k)
		}
	}
	return out, err
}

func (s *Service) mayDecide(a Actor, e domain.Enrollment, inv domain.EnrollInvitation) error {
	if err := a.require(domain.PermMembersManage, "approve devices"); err != nil {
		return err
	}
	for _, c := range inv.Capabilities {
		if c.Infrastructure() {
			if err := a.require(domain.PermDevicesManage, "approve a "+string(c)); err != nil {
				return err
			}
		}
	}
	if inv.ForMemberID == "" && !a.Member.Role.CanManage(inv.Role) {
		return forbidden("approve someone as " + string(inv.Role))
	}
	return nil
}

// ApproveEnrollment approves a pending request. What the device is issued is minted
// when it next asks, so that its credential is shown to it and to no one else.
func (s *Service) ApproveEnrollment(ctx context.Context, a Actor, id string) (domain.Enrollment, error) {
	return s.decide(ctx, a, id, domain.EnrollmentApproved)
}

// DenyEnrollment refuses a pending request. The invitation is spent.
func (s *Service) DenyEnrollment(ctx context.Context, a Actor, id string) (domain.Enrollment, error) {
	return s.decide(ctx, a, id, domain.EnrollmentDenied)
}

func (s *Service) decide(ctx context.Context, a Actor, id string, state domain.EnrollmentState) (domain.Enrollment, error) {
	var out domain.Enrollment
	err := s.db.Update(ctx, func(tx store.Tx) error {
		e, err := tx.Enrollment(ctx, a.Workspace.ID, id)
		if err != nil {
			return err
		}
		inv, _, err := tx.EnrollInvitationByID(ctx, e.InvitationID)
		if err != nil {
			return err
		}
		if err := s.mayDecide(a, e, inv); err != nil {
			return err
		}
		if e.State != domain.EnrollmentPending {
			return fmt.Errorf("%w: this request has already been %s", domain.ErrConflict, e.State)
		}
		if state == domain.EnrollmentApproved {
			// Anything the joiner chose that cannot be accepted is for the administrator to
			// see now, not for the device to find out later.
			if err := s.checkFinalizable(ctx, tx, e, inv); err != nil {
				return fmt.Errorf("%w: %v", domain.ErrConflict, err)
			}
		}
		ok, err := tx.DecideEnrollment(ctx, a.Workspace.ID, id, state, "", a.Member.ID, s.stamp())
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: this request has already been decided", domain.ErrConflict)
		}
		out, err = tx.Enrollment(ctx, a.Workspace.ID, id)
		return err
	})
	s.changed(a.Workspace.ID, err)
	return out, err
}

// ---- the public side: what a device talks to ----

// EnrollAuthority is the workspace's side of the enrollment protocol for one
// workspace (enrollment.Authority). The server puts it behind the TLS endpoint.
func (s *Service) EnrollAuthority(workspaceID string) interface {
	enrollment.Authority
	PollKey(ctx context.Context, enrollmentID, deviceID string) (ed25519.PublicKey, error)
} {
	return &enrollAuthority{s: s, ws: workspaceID}
}

type enrollAuthority struct {
	s  *Service
	ws string
}

func (e *enrollAuthority) Identity(ctx context.Context) (enrollment.Hello, error) {
	var st domain.NetworkSettings
	err := e.s.db.View(ctx, func(tx store.Tx) (err error) { st, err = tx.NetworkSettings(ctx, e.ws); return })
	if err != nil {
		return enrollment.Hello{}, err
	}
	return enrollment.Hello{WorkspaceID: e.ws, WorkspaceKey: st.WorkspaceKey, Fingerprint: st.Fingerprint}, nil
}

func (e *enrollAuthority) PollKey(ctx context.Context, enrollmentID, deviceID string) (ed25519.PublicKey, error) {
	var key ed25519.PublicKey
	err := e.s.db.View(ctx, func(tx store.Tx) error {
		en, err := tx.EnrollmentByID(ctx, enrollmentID)
		if err != nil || en.WorkspaceID != e.ws || en.DeviceID != deviceID {
			return enrollment.ErrNotFound
		}
		key, err = deviceid.ParsePublicKey(en.DeviceKey)
		return err
	})
	return key, err
}

// Enroll handles a device's request to join. It answers every request that is not a
// valid use of an invitation in the same way, so that nothing tells a guess from an
// expired or spent invitation.
func (e *enrollAuthority) Enroll(ctx context.Context, req enrollment.AuthorityJoin) (enrollment.Response, error) {
	s := e.s
	net, err := s.needNetwork()
	if err != nil {
		return enrollment.Response{}, err
	}
	var resp enrollment.Response
	err = s.db.Update(ctx, func(tx store.Tx) error {
		now := s.stamp()
		inv, hash, err := tx.EnrollInvitationByID(ctx, req.InviteID)
		if err != nil || inv.WorkspaceID != e.ws {
			return notFoundBecause("no invitation %s in this workspace", req.InviteID)
		}
		if subtle.ConstantTimeCompare(hash, enrollment.HashCredential(req.Credential)) != 1 {
			return notFoundBecause("invitation %s: the credential does not match", inv.ID)
		}
		if !inv.Usable(now) {
			// Only the log says which: the person asking is told one thing for all of them.
			return notFoundBecause("invitation %s is not usable: it is %s, it ends %s and it is now %s",
				inv.ID, inv.State, inv.ExpiresAt.Format(time.RFC3339), now.Format(time.RFC3339))
		}
		// From here the credential is good, and the person holding it may be told what to change.
		name, err := deviceid.CleanName(req.DeviceName)
		if err != nil {
			return enrollment.Reject(err.Error())
		}
		hasHost := false
		for _, c := range inv.Capabilities {
			hasHost = hasHost || c == domain.CapabilityWorkspaceHost
		}
		if hasHost && req.SealingPublicKey == "" {
			return enrollment.Reject("a Workspace Host must present a sealing key")
		}
		if req.SealingPublicKey != "" {
			if b, err := base64.RawURLEncoding.DecodeString(req.SealingPublicKey); err != nil || len(b) != 32 {
				return enrollment.Reject("the sealing key is not an X25519 public key")
			}
		}
		memberName := ""
		if inv.ForMemberID == "" {
			if memberName, err = domain.CleanName("member", req.MemberName); err != nil {
				return enrollment.Reject("this invitation makes a new member: a name is required (" + err.Error() + ")")
			}
		}
		used, err := tx.UseEnrollInvitation(ctx, inv.WorkspaceID, inv.ID, now)
		if err != nil {
			return err
		}
		if !used {
			return notFoundBecause("invitation %s was used by another request at the same moment", inv.ID)
		}
		en := domain.Enrollment{ID: domain.NewID(domain.PrefixEnrollment), WorkspaceID: inv.WorkspaceID, InvitationID: inv.ID, MemberID: inv.ForMemberID, MemberName: memberName,
			DeviceID: req.DeviceID, DeviceName: name, Capabilities: inv.Capabilities, State: domain.EnrollmentPending, RemoteAddr: req.RemoteAddr, CreatedAt: now,
			DeviceKey: base64.RawURLEncoding.EncodeToString(req.Key), NetworkPublicKey: req.NetworkPublicKey, SealingKey: req.SealingPublicKey}
		if err := s.checkFinalizable(ctx, tx, en, inv); err != nil {
			return enrollment.Reject(err.Error())
		}
		if err := tx.InsertEnrollment(ctx, en); err != nil {
			return notFoundBecause("recording the request of device %s: %v", req.DeviceID, err)
		}
		if inv.RequireApproval {
			resp = enrollment.Response{State: enrollment.StatePending, EnrollmentID: en.ID, Message: "an administrator has to approve this device"}
			return nil
		}
		if ok, err := tx.DecideEnrollment(ctx, inv.WorkspaceID, en.ID, domain.EnrollmentApproved, "", inv.CreatedBy, now); err != nil || !ok {
			return fmt.Errorf("deciding an enrollment that was just made: %v", err)
		}
		en.State = domain.EnrollmentApproved
		resp, err = s.finalize(ctx, tx, net, en, inv, now)
		return err
	})
	if err != nil {
		return enrollment.Response{}, translate(err)
	}
	s.changed(e.ws, nil)
	return resp, nil
}

// Poll answers a device that is waiting, and gives an approved device what it was issued, once.
func (e *enrollAuthority) Poll(ctx context.Context, req enrollment.AuthorityPoll) (enrollment.Response, error) {
	s := e.s
	net, err := s.needNetwork()
	if err != nil {
		return enrollment.Response{}, err
	}
	var resp enrollment.Response
	err = s.db.Update(ctx, func(tx store.Tx) error {
		now := s.stamp()
		en, err := tx.EnrollmentByID(ctx, req.EnrollmentID)
		if err != nil || en.WorkspaceID != e.ws || en.DeviceID != req.DeviceID {
			return enrollment.ErrNotFound
		}
		switch en.State {
		case domain.EnrollmentPending:
			resp = enrollment.Response{State: enrollment.StatePending, EnrollmentID: en.ID, Message: "an administrator has to approve this device"}
			return nil
		case domain.EnrollmentDenied:
			resp = enrollment.Response{State: enrollment.StateDenied, EnrollmentID: en.ID}
			return nil
		}
		if en.Delivered {
			return enrollment.ErrNotFound // collected once; an administrator can issue another credential
		}
		inv, _, err := tx.EnrollInvitationByID(ctx, en.InvitationID)
		if err != nil {
			return enrollment.ErrNotFound
		}
		resp, err = s.finalize(ctx, tx, net, en, inv, now)
		return err
	})
	if err != nil {
		return enrollment.Response{}, translate(err)
	}
	if resp.State == enrollment.StateApproved {
		s.changed(e.ws, nil)
	}
	return resp, nil
}

// notFoundBecause is enrollment.ErrNotFound with the reason attached, for this host's log. The protocol
// answers every such refusal the same way, so the reason never reaches the caller.
func notFoundBecause(format string, a ...any) error {
	return fmt.Errorf("%w: %s", enrollment.ErrNotFound, fmt.Sprintf(format, a...))
}

// translate turns what the domain says into what the protocol does: a rejection stays one,
// a missing or refused thing is the single refusal, and anything else is a fault.
func translate(err error) error {
	var rej *enrollment.RejectedError
	switch {
	case errors.As(err, &rej), errors.Is(err, enrollment.ErrNotFound):
		return err
	case errors.Is(err, domain.ErrNotFound), errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrInvalid):
		return enrollment.Reject(strings.TrimPrefix(err.Error(), "conflict: "))
	}
	return err
}

// checkFinalizable says whether the enrollment could be completed: that what the joiner
// chose is free. It does not change anything.
func (s *Service) checkFinalizable(ctx context.Context, tx store.Tx, e domain.Enrollment, inv domain.EnrollInvitation) error {
	if inv.ForMemberID == "" {
		taken, err := tx.HasMemberNamed(ctx, e.WorkspaceID, e.MemberName)
		if err != nil {
			return err
		}
		if taken {
			return fmt.Errorf("a member named %q already exists in this workspace: choose another name", e.MemberName)
		}
	} else if _, err := tx.Member(ctx, e.WorkspaceID, inv.ForMemberID); err != nil {
		return errors.New("the member this invitation was for is no longer in the workspace")
	}
	if _, err := tx.Device(ctx, e.WorkspaceID, e.DeviceID); err == nil {
		return errors.New("a device with that ID is already registered")
	}
	return nil
}

// finalize makes the device a member's device, puts it on the network and mints its
// credential, then marks the enrollment collected. It runs in the caller's transaction.
func (s *Service) finalize(ctx context.Context, tx store.Tx, net NetworkAuthority, e domain.Enrollment, inv domain.EnrollInvitation, now time.Time) (enrollment.Response, error) {
	if err := s.checkFinalizable(ctx, tx, e, inv); err != nil {
		return enrollment.Response{}, enrollment.Reject(err.Error())
	}
	settings, err := tx.NetworkSettings(ctx, e.WorkspaceID)
	if err != nil {
		return enrollment.Response{}, err
	}
	var m domain.Member
	if inv.ForMemberID != "" {
		if m, err = tx.Member(ctx, e.WorkspaceID, inv.ForMemberID); err != nil {
			return enrollment.Response{}, err
		}
	} else {
		m = domain.Member{ID: domain.NewID(domain.PrefixMember), WorkspaceID: e.WorkspaceID, Name: e.MemberName, Role: inv.Role, CreatedAt: now}
		// A member who joins through a device has no sign-in token of their own: their
		// devices' credentials are how they reach the API. The token stored here is one
		// nobody was given, so it cannot be used; an administrator can issue a real one.
		_, unusable := domain.NewToken()
		if err := tx.InsertMember(ctx, m, unusable); err != nil {
			return enrollment.Response{}, err
		}
	}
	devs, err := tx.Devices(ctx, e.WorkspaceID, m.ID)
	if err != nil {
		return enrollment.Response{}, err
	}
	live := 0
	for _, d := range devs {
		if !d.Revoked() {
			live++
		}
	}
	if live >= domain.MaxDevicesPerMember {
		return enrollment.Response{}, enrollment.Reject(fmt.Sprintf("a member may have at most %d devices; revoke one first", domain.MaxDevicesPerMember))
	}
	caps := e.Capabilities
	dev := domain.Device{ID: e.DeviceID, WorkspaceID: e.WorkspaceID, MemberID: m.ID, Name: e.DeviceName, PublicKey: e.DeviceKey, Capabilities: caps,
		HostStatus: initialStatus(caps, domain.CapabilityWorkspaceHost), ConnectivityStatus: initialStatus(caps, domain.CapabilityConnectivityHost), CreatedAt: now, UpdatedAt: now}
	if err := dev.Validate(); err != nil {
		return enrollment.Response{}, enrollment.Reject(err.Error())
	}
	if err := tx.InsertDevice(ctx, dev); err != nil {
		return enrollment.Response{}, err
	}
	_, issued, err := s.placeOnNetwork(ctx, tx, net, net.Info(), dev, e.NetworkPublicKey, e.SealingKey, now)
	if err != nil {
		return enrollment.Response{}, err
	}
	token, hash := domain.NewToken()
	if err := tx.SetDeviceCredential(ctx, e.WorkspaceID, dev.ID, hash, now); err != nil {
		return enrollment.Response{}, err
	}
	bundle, err := s.bundleFor(ctx, tx, net, e.WorkspaceID, dev.ID, issued)
	if err != nil {
		return enrollment.Response{}, err
	}
	ws, err := tx.Workspace(ctx, e.WorkspaceID)
	if err != nil {
		return enrollment.Response{}, err
	}
	if err := tx.SetEnrollmentMember(ctx, e.WorkspaceID, e.ID, m.ID); err != nil {
		return enrollment.Response{}, err
	}
	if ok, err := tx.MarkEnrollmentDelivered(ctx, e.WorkspaceID, e.ID, now); err != nil || !ok {
		return enrollment.Response{}, fmt.Errorf("recording that an enrollment was collected: %v", err)
	}
	capNames := make([]string, 0, len(caps))
	for _, c := range caps {
		capNames = append(capNames, string(c))
	}
	return enrollment.Response{State: enrollment.StateApproved, EnrollmentID: e.ID, WorkspaceID: e.WorkspaceID, WorkspaceName: ws.Name, WorkspaceKey: settings.WorkspaceKey,
		MemberID: m.ID, MemberName: m.Name, Role: string(m.Role), DeviceID: dev.ID, DeviceToken: token, Capabilities: capNames, Network: toWire(bundle)}, nil
}
