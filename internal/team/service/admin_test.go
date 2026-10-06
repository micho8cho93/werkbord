package service

import (
	"testing"

	"devboard/internal/team/domain"
)

// admin adds an admin as the owner and returns them, signed in.
func (w *world) admin(owner Actor, name string) Actor {
	w.t.Helper()
	mt, err := w.svc.AddMember(bg, owner, name, "", domain.RoleAdmin)
	if err != nil {
		w.t.Fatal(err)
	}
	return w.signIn(mt.Token)
}

func TestOnlyTheOwnerAppointsAdmins(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	admin := w.admin(owner, "Ann")
	if admin.Member.Role != domain.RoleAdmin {
		t.Fatalf("role = %s", admin.Member.Role)
	}
	member, _ := w.member(owner, "Bo")

	// An admin adds members, but not admins (that would let one admin make another).
	if _, err := w.svc.AddMember(bg, admin, "Cy", "", domain.RoleMember); err != nil {
		t.Fatalf("an admin could not add a member: %v", err)
	}
	_, err := w.svc.AddMember(bg, admin, "Dee", "", domain.RoleAdmin)
	wantErr(t, err, domain.ErrForbidden)
	_, err = w.svc.AddMember(bg, member, "Dee", "", domain.RoleMember)
	wantErr(t, err, domain.ErrForbidden)
	// Nobody adds a second owner.
	_, err = w.svc.AddMember(bg, owner, "Eve", "", domain.RoleOwner)
	wantErr(t, err, domain.ErrInvalid)
}

// members.manage must not be a way up: an admin cannot take over another admin's
// or the owner's account by reissuing its token, or remove either.
func TestAnAdminCannotReachTheOwnerOrOtherAdmins(t *testing.T) {
	w := newWorld(t)
	owner, ownerToken := w.workspace("Acme", "Ada")
	ann := w.admin(owner, "Ann")
	ben := w.admin(owner, "Ben")
	member, memberToken := w.member(owner, "Cy")

	_, err := w.svc.ReissueToken(bg, ann, owner.Member.ID)
	wantErr(t, err, domain.ErrForbidden)
	if _, err := w.svc.Authenticate(bg, ownerToken); err != nil {
		t.Fatal("the owner was signed out")
	}
	_, err = w.svc.ReissueToken(bg, ann, ben.Member.ID)
	wantErr(t, err, domain.ErrForbidden)
	wantErr(t, w.svc.RemoveMember(bg, ann, ben.Member.ID), domain.ErrForbidden)
	wantErr(t, w.svc.RemoveMember(bg, ann, owner.Member.ID), domain.ErrConflict) // the owner stays, whoever asks

	// What an admin may do for members: reissue and remove.
	fresh, err := w.svc.ReissueToken(bg, ann, member.Member.ID)
	if err != nil || fresh.Token == memberToken {
		t.Fatalf("an admin could not reissue a member's token: %v", err)
	}
	if _, err := w.svc.Authenticate(bg, memberToken); err == nil {
		t.Fatal("the old token still works")
	}
	if err := w.svc.RemoveMember(bg, ann, member.Member.ID); err != nil {
		t.Fatal(err)
	}

	// Anyone may reissue their own; the owner may act on admins.
	if _, err := w.svc.ReissueToken(bg, ann, ann.Member.ID); err != nil {
		t.Fatalf("an admin could not reissue their own token: %v", err)
	}
	if _, err := w.svc.ReissueToken(bg, owner, ben.Member.ID); err != nil {
		t.Fatalf("the owner could not reissue an admin's token: %v", err)
	}
	if err := w.svc.RemoveMember(bg, owner, ben.Member.ID); err != nil {
		t.Fatalf("the owner could not remove an admin: %v", err)
	}
}

// An admin administers the workspace's projects and sees all of them, as the
// owner does, without being a member of any of them first.
func TestAnAdminAdministersProjects(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	admin := w.admin(owner, "Ann")
	member, _ := w.member(owner, "Bo")

	p := w.project(owner, "Shop")
	if _, err := w.svc.GetProject(bg, admin, p.ID); err != nil {
		t.Fatalf("an admin cannot see a project they are not on: %v", err)
	}
	if _, err := w.svc.GetProject(bg, member, p.ID); err == nil {
		t.Fatal("a plain member sees a project they are not on")
	}
	if list, _ := w.svc.ListProjects(bg, admin); len(list) != 1 {
		t.Fatalf("an admin sees %d projects", len(list))
	}
	if _, err := w.svc.CreateProject(bg, admin, ProjectInput{Name: "Admin's"}); err != nil {
		t.Fatalf("an admin cannot create a project: %v", err)
	}
	if err := w.svc.AddProjectMember(bg, admin, p.ID, member.Member.ID, ""); err != nil {
		t.Fatalf("an admin cannot put someone on a project: %v", err)
	}
	if _, err := w.svc.RenameWorkspace(bg, admin, "Acme Inc"); err != nil {
		t.Fatalf("an admin cannot rename the workspace: %v", err)
	}
	me := w.svc.Me(admin)
	for _, perm := range me.Permissions {
		if perm == domain.PermOwnership || perm == domain.PermAdminsManage {
			t.Errorf("an admin's permissions include %s", perm)
		}
	}
}

// Members and tokens that existed before the Admin role keep working and mean what they did.
func TestExistingRolesKeepTheirMeaning(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	member, _ := w.member(owner, "Bo")
	if owner.Member.Role != domain.RoleOwner || member.Member.Role != domain.RoleMember {
		t.Fatal("roles changed")
	}
	for _, p := range []domain.Permission{domain.PermMembersManage, domain.PermProjectsCreate, domain.PermProjectsManage, domain.PermWorkspaceManage} {
		if member.Member.Can(p) {
			t.Errorf("a member can %s", p)
		}
	}
	if _, err := w.svc.CreateProject(bg, member, ProjectInput{Name: "x"}); err == nil {
		t.Fatal("a member created a project")
	}
}
