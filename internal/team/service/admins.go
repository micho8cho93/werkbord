package service

import (
	"context"
	"fmt"

	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// A workspace has one Owner, any number of Admins, and many Members.
//
// The Owner holds the workspace's ownership (and is the only person who appoints Admins). An Admin runs
// the day-to-day: members, projects, invitations, device approvals, and the workspace's hosts. What an
// Admin may do is a table (domain/roles.go), not a name check, so appointing a second, third or tenth
// Admin changes nothing about how any one of them is checked.
//
// Appointing an Admin gives a person authority. It gives their devices nothing: a device is a Workspace
// Host or a Connectivity Host only because someone who manages devices granted that capability to the
// device, and an Admin's laptop is not one by being an Admin's. Nor does a host make its owner an Admin.

// SetMemberRole appoints a member as an Admin, or returns an Admin to being a member. Only the owner may
// (admins.manage). The owner's own role never changes, and a workspace never has a second owner: both are
// refused here and again in the write.
//
// It takes effect on the person's very next request, devices included: a device's credential is checked
// against its owner's role every time it is used.
func (s *Service) SetMemberRole(ctx context.Context, a Actor, memberID string, role domain.Role) (domain.Member, error) {
	if err := a.require(domain.PermAdminsManage, "appoint or remove admins"); err != nil {
		return domain.Member{}, err
	}
	if role != domain.RoleAdmin && role != domain.RoleMember {
		if role == domain.RoleOwner {
			return domain.Member{}, fmt.Errorf("%w: a workspace has one owner, the person who created it", domain.ErrInvalid)
		}
		_, err := domain.ParseRole(string(role))
		if err == nil {
			err = fmt.Errorf("%w: a person can be made an admin or a member", domain.ErrInvalid)
		}
		return domain.Member{}, err
	}
	var out domain.Member
	update := s.db.Update
	if role == domain.RoleMember {
		update = s.containmentUpdate
	}
	err := update(ctx, func(tx store.Tx) error {
		m, err := tx.Member(ctx, a.Workspace.ID, memberID)
		if err != nil {
			return err
		}
		if m.Role == domain.RoleOwner {
			return fmt.Errorf("%w: the owner's role does not change", domain.ErrConflict)
		}
		if m.Role != role {
			if err := tx.SetMemberRole(ctx, a.Workspace.ID, memberID, role); err != nil {
				return err
			}
			m.Role = role
		}
		out = m
		return nil
	})
	s.changed(a.Workspace.ID, err)
	return out, err
}
