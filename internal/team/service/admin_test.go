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

// The owner appoints admins and takes the role back; nobody else does, and the owner's own role never moves.
func TestOnlyTheOwnerChangesRoles(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	ann := w.admin(owner, "Ann")
	bo, boToken := w.member(owner, "Bo")

	// A member cannot create a project; once appointed, with the very same token, they can.
	if _, err := w.svc.CreateProject(bg, bo, ProjectInput{Name: "Nope"}); err == nil {
		t.Fatal("a member created a project")
	}
	m, err := w.svc.SetMemberRole(bg, owner, bo.Member.ID, domain.RoleAdmin)
	if err != nil || m.Role != domain.RoleAdmin {
		t.Fatalf("appointing an admin: %+v %v", m, err)
	}
	boNow := w.signIn(boToken)
	if boNow.Member.Role != domain.RoleAdmin {
		t.Fatalf("the appointment did not reach the person's own token: %s", boNow.Member.Role)
	}
	if _, err := w.svc.CreateProject(bg, boNow, ProjectInput{Name: "Now allowed"}); err != nil {
		t.Fatalf("a new admin cannot create a project: %v", err)
	}
	// Doing it again changes nothing and is not an error.
	if _, err := w.svc.SetMemberRole(bg, owner, bo.Member.ID, domain.RoleAdmin); err != nil {
		t.Fatalf("appointing an admin twice: %v", err)
	}

	// Admins do not appoint or remove admins, whoever the target is, themselves included.
	for _, target := range []Actor{ann, boNow} {
		_, err := w.svc.SetMemberRole(bg, ann, target.Member.ID, domain.RoleMember)
		wantErr(t, err, domain.ErrForbidden)
	}
	_, err = w.svc.SetMemberRole(bg, boNow, ann.Member.ID, domain.RoleAdmin)
	wantErr(t, err, domain.ErrForbidden)
	cy, _ := w.member(owner, "Cy")
	_, err = w.svc.SetMemberRole(bg, cy, cy.Member.ID, domain.RoleAdmin)
	wantErr(t, err, domain.ErrForbidden) // nobody promotes themselves

	// The owner stays the owner; there is never a second one; a role that does not exist is not a role.
	_, err = w.svc.SetMemberRole(bg, owner, owner.Member.ID, domain.RoleMember)
	wantErr(t, err, domain.ErrConflict)
	_, err = w.svc.SetMemberRole(bg, owner, ann.Member.ID, domain.RoleOwner)
	wantErr(t, err, domain.ErrInvalid)
	_, err = w.svc.SetMemberRole(bg, owner, ann.Member.ID, domain.Role("superuser"))
	wantErr(t, err, domain.ErrInvalid)
	_, err = w.svc.SetMemberRole(bg, owner, "tmb_nobody", domain.RoleAdmin)
	wantErr(t, err, domain.ErrNotFound)

	// Taking the role back works at once, on the same token.
	if _, err := w.svc.SetMemberRole(bg, owner, bo.Member.ID, domain.RoleMember); err != nil {
		t.Fatal(err)
	}
	if got := w.signIn(boToken); got.Member.Role != domain.RoleMember || got.Member.Can(domain.PermProjectsCreate) {
		t.Fatalf("a demoted admin still has an admin's authority: %+v", got.Member)
	}
	members, _ := w.svc.ListMembers(bg, owner)
	admins := 0
	owners := 0
	for _, m := range members {
		switch m.Role {
		case domain.RoleAdmin:
			admins++
		case domain.RoleOwner:
			owners++
		}
	}
	if owners != 1 || admins != 1 {
		t.Fatalf("one owner and one admin (Ann) expected, found %d owners and %d admins", owners, admins)
	}
}

// With several admins, each administers the whole workspace, and none gains the owner's authority over another.
func TestSeveralAdminsEachAdministerAndNoneRulesAnother(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	admins := []Actor{w.admin(owner, "Ann"), w.admin(owner, "Ben"), w.admin(owner, "Cat")}
	for i, ad := range admins {
		if _, err := w.svc.CreateProject(bg, ad, ProjectInput{Name: "Project " + string(rune('A'+i))}); err != nil {
			t.Fatalf("admin %d cannot create a project: %v", i, err)
		}
		if _, err := w.svc.AddMember(bg, ad, "Member "+string(rune('A'+i)), "", domain.RoleMember); err != nil {
			t.Fatalf("admin %d cannot add a member: %v", i, err)
		}
	}
	for i, ad := range admins {
		// Each sees every project, theirs or not.
		if list, err := w.svc.ListProjects(bg, ad); err != nil || len(list) != 3 {
			t.Fatalf("admin %d sees %d projects: %v", i, len(list), err)
		}
		for j, other := range admins {
			if i == j {
				continue
			}
			_, err := w.svc.ReissueToken(bg, ad, other.Member.ID)
			wantErr(t, err, domain.ErrForbidden)
			wantErr(t, w.svc.RemoveMember(bg, ad, other.Member.ID), domain.ErrForbidden)
		}
	}
	// Removing one admin leaves the others, and the owner, exactly as they were.
	if err := w.svc.RemoveMember(bg, owner, admins[0].Member.ID); err != nil {
		t.Fatal(err)
	}
	for _, ad := range admins[1:] {
		if _, err := w.svc.ListMembers(bg, ad); err != nil {
			t.Fatalf("an admin lost access when another was removed: %v", err)
		}
	}
}
