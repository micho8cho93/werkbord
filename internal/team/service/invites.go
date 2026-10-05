package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// InviteInput is what creating an invite takes.
type InviteInput struct {
	// Role is what people who join with it become on the project: member (the
	// default) or reviewer. Nobody is invited in as an owner.
	Role domain.ProjectRole
	// TTL is how long it works for; 0 means domain.DefaultInviteTTL.
	TTL time.Duration
	// MaxUses is how many people can use it; 0 means one.
	MaxUses int
}

// InviteWithCode is a new invite and its code, shown once.
type InviteWithCode struct {
	Invite domain.Invite `json:"invite"`
	Code   string        `json:"code"`
}

// CreateInvite makes a code a person can use to join a project. Only a project
// owner can. The code is returned here and nowhere else: only its hash is stored.
func (s *Service) CreateInvite(ctx context.Context, a Actor, projectID string, in InviteInput) (InviteWithCode, error) {
	role := in.Role
	if role == "" {
		role = domain.ProjectContributor
	}
	if !role.Valid() || role == domain.ProjectOwner {
		return InviteWithCode{}, fmt.Errorf("%w: an invite can make someone a member or a reviewer, not an owner", domain.ErrInvalid)
	}
	ttl, uses := in.TTL, in.MaxUses
	if ttl == 0 {
		ttl = domain.DefaultInviteTTL
	}
	if uses == 0 {
		uses = domain.DefaultInviteUses
	}
	if ttl < time.Minute || ttl > domain.MaxInviteTTL {
		return InviteWithCode{}, fmt.Errorf("%w: an invite must last between a minute and %d days", domain.ErrInvalid, int(domain.MaxInviteTTL/(24*time.Hour)))
	}
	if uses < 1 || uses > domain.MaxInviteUses {
		return InviteWithCode{}, fmt.Errorf("%w: an invite can be used between 1 and %d times", domain.ErrInvalid, domain.MaxInviteUses)
	}
	var out InviteWithCode
	err := s.mutate(ctx, a, projectID, func(tx *store.Tx, x access) error {
		if err := x.require(domain.PPInvitesManage, "invite people to this project"); err != nil {
			return err
		}
		if x.Project.Archived {
			return fmt.Errorf("%w: the project is archived", domain.ErrConflict)
		}
		now := s.stamp()
		code, hash := domain.NewInviteCode()
		inv := domain.Invite{ID: domain.NewID(domain.PrefixInvite), ProjectID: projectID, Role: role, CreatedBy: a.Member.ID, CreatedAt: now, ExpiresAt: now.Add(ttl), MaxUses: uses}
		if err := tx.InsertInvite(ctx, a.Workspace.ID, inv, hash); err != nil {
			return err
		}
		out = InviteWithCode{Invite: inv, Code: code}
		return nil
	})
	return out, err
}

// ListInvites lists a project's invites (never their codes).
func (s *Service) ListInvites(ctx context.Context, a Actor, projectID string) ([]domain.Invite, error) {
	var out []domain.Invite
	err := s.view(ctx, a, projectID, func(tx *store.Tx, x access) (err error) {
		if err := x.require(domain.PPInvitesManage, "see this project's invites"); err != nil {
			return err
		}
		out, err = tx.Invites(ctx, a.Workspace.ID, projectID)
		return err
	})
	return out, err
}

// RevokeInvite stops an invite from being used. People who already joined stay.
func (s *Service) RevokeInvite(ctx context.Context, a Actor, projectID, inviteID string) error {
	return s.mutate(ctx, a, projectID, func(tx *store.Tx, x access) error {
		if err := x.require(domain.PPInvitesManage, "revoke invites"); err != nil {
			return err
		}
		return tx.RevokeInvite(ctx, a.Workspace.ID, projectID, inviteID, s.stamp())
	})
}

// Joined is the result of using an invite.
type Joined struct {
	Project domain.Project     `json:"project"`
	Member  domain.Member      `json:"member"`
	Role    domain.ProjectRole `json:"role"`
	// Token signs a new member in. It is set only when the invite created the
	// member (RedeemInvite), is shown once, and is not stored.
	Token string `json:"token,omitempty"`
}

func unusable() error {
	return fmt.Errorf("%w: %v", domain.ErrNotFound, store.ErrInviteUnusable)
}

// RedeemInvite uses an invite code to become a new member of the workspace and of
// the invite's project. It needs no sign-in, which is the point of an invite; the
// code is the credential, so it is 128 random bits, expires, counts its uses
// and is single-use unless its creator said otherwise. If anything fails (the
// name is taken, say) the use is not counted.
func (s *Service) RedeemInvite(ctx context.Context, code, name, email string) (Joined, error) {
	name, err := domain.CleanName("member", name)
	if err != nil {
		return Joined{}, err
	}
	if email, err = domain.CleanEmail(email); err != nil {
		return Joined{}, err
	}
	var out Joined
	var projectID string
	err = s.db.Update(ctx, func(tx *store.Tx) error {
		now := s.stamp()
		workspaceID, inv, err := tx.UseInvite(ctx, domain.HashInviteCode(code), now)
		if errors.Is(err, store.ErrInviteUnusable) {
			return unusable()
		}
		if err != nil {
			return err
		}
		m := domain.Member{ID: domain.NewID(domain.PrefixMember), WorkspaceID: workspaceID, Name: name, Email: email, Role: domain.RoleMember, CreatedAt: now}
		token, hash := domain.NewToken()
		if err := tx.InsertMember(ctx, m, hash); err != nil {
			return err
		}
		if err := tx.AddProjectMember(ctx, workspaceID, domain.ProjectMember{ProjectID: inv.ProjectID, MemberID: m.ID, Role: inv.Role, AddedBy: inv.CreatedBy, AddedAt: now}); err != nil {
			return err
		}
		p, err := tx.Project(ctx, workspaceID, inv.ProjectID)
		if err != nil {
			return err
		}
		if err := tx.AddActivity(ctx, workspaceID, domain.Activity{ProjectID: inv.ProjectID, ActorID: m.ID, Kind: domain.ActMemberJoined, Detail: "with an invite", CreatedAt: now}); err != nil {
			return err
		}
		if _, err := tx.BumpRevision(ctx, workspaceID, inv.ProjectID); err != nil {
			return err
		}
		projectID = inv.ProjectID
		out = Joined{Project: p, Member: m, Role: inv.Role, Token: token}
		return nil
	})
	if err == nil {
		s.hub.notify(projectID)
	}
	return out, err
}

// JoinWithInvite lets a member who already has an account in the workspace use an
// invite to join its project.
func (s *Service) JoinWithInvite(ctx context.Context, a Actor, code string) (Joined, error) {
	var out Joined
	var projectID string
	err := s.db.Update(ctx, func(tx *store.Tx) error {
		now := s.stamp()
		workspaceID, inv, err := tx.UseInvite(ctx, domain.HashInviteCode(code), now)
		if errors.Is(err, store.ErrInviteUnusable) || (err == nil && workspaceID != a.Workspace.ID) {
			return unusable() // an invite for another workspace is indistinguishable from none
		}
		if err != nil {
			return err
		}
		if _, on, err := tx.ProjectRole(ctx, workspaceID, inv.ProjectID, a.Member.ID); err != nil {
			return err
		} else if on {
			return fmt.Errorf("%w: you are already on this project", domain.ErrConflict) // rolls the use back
		}
		if err := tx.AddProjectMember(ctx, workspaceID, domain.ProjectMember{ProjectID: inv.ProjectID, MemberID: a.Member.ID, Role: inv.Role, AddedBy: inv.CreatedBy, AddedAt: now}); err != nil {
			return err
		}
		p, err := tx.Project(ctx, workspaceID, inv.ProjectID)
		if err != nil {
			return err
		}
		if err := tx.AddActivity(ctx, workspaceID, domain.Activity{ProjectID: inv.ProjectID, ActorID: a.Member.ID, Kind: domain.ActMemberJoined, Detail: "with an invite", CreatedAt: now}); err != nil {
			return err
		}
		if _, err := tx.BumpRevision(ctx, workspaceID, inv.ProjectID); err != nil {
			return err
		}
		projectID = inv.ProjectID
		out = Joined{Project: p, Member: a.Member, Role: inv.Role}
		return nil
	})
	if err == nil {
		s.hub.notify(projectID)
	}
	return out, err
}
