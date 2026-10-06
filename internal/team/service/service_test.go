package service

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

var bg = context.Background()

func newDB(t *testing.T) (store.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "team.db")
	db, err := store.Open(bg, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, path
}

type world struct {
	t   *testing.T
	svc *Service
	db  store.Store
}

func newWorld(t *testing.T) *world {
	db, _ := newDB(t)
	return &world{t: t, svc: New(db), db: db}
}

// workspace starts a workspace and returns its owner, signed in.
func (w *world) workspace(name, owner string) (Actor, string) {
	w.t.Helper()
	c, err := w.svc.CreateWorkspace(bg, name, owner, "")
	if err != nil {
		w.t.Fatal(err)
	}
	return w.signIn(c.Token), c.Token
}

func (w *world) signIn(token string) Actor {
	w.t.Helper()
	a, err := w.svc.Authenticate(bg, token)
	if err != nil {
		w.t.Fatalf("Authenticate: %v", err)
	}
	return a
}

// member adds a member as actor and returns them, signed in.
func (w *world) member(owner Actor, name string) (Actor, string) {
	w.t.Helper()
	mt, err := w.svc.AddMember(bg, owner, name, "", domain.RoleMember)
	if err != nil {
		w.t.Fatal(err)
	}
	return w.signIn(mt.Token), mt.Token
}

func (w *world) project(a Actor, name string) domain.Project {
	w.t.Helper()
	p, err := w.svc.CreateProject(bg, a, ProjectInput{Name: name})
	if err != nil {
		w.t.Fatal(err)
	}
	return p
}

func wantErr(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("error = %v, want %v", err, target)
	}
}

func TestCreateWorkspaceMakesAnOwnerWhoCanSignIn(t *testing.T) {
	w := newWorld(t)
	owner, token := w.workspace("Acme", "Ada")
	if owner.Workspace.Name != "Acme" || owner.Member.Name != "Ada" || owner.Member.Role != domain.RoleOwner {
		t.Fatalf("%+v", owner)
	}
	if !owner.Member.Can(domain.PermMembersManage) {
		t.Fatal("the owner cannot manage members")
	}
	if _, err := w.svc.Authenticate(bg, token+"x"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("a wrong token signed in: %v", err)
	}
	for _, bad := range []string{"", "wbt_nothing"} {
		if _, err := w.svc.Authenticate(bg, bad); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("token %q: %v", bad, err)
		}
	}
	if _, err := w.svc.CreateWorkspace(bg, " ", "Ada", ""); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("a workspace with no name: %v", err)
	}
	if _, err := w.svc.CreateWorkspace(bg, "X", "", ""); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("a workspace with no owner name: %v", err)
	}
}

func TestTokensAreNotStored(t *testing.T) {
	db, path := newDB(t)
	svc := New(db)
	c, err := svc.CreateWorkspace(bg, "Acme", "Ada", "")
	if err != nil {
		t.Fatal(err)
	}
	owner, _ := svc.Authenticate(bg, c.Token)
	mt, err := svc.AddMember(bg, owner, "Bo", "", domain.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{path, path + "-wal"} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if bytes.Contains(b, []byte(c.Token)) || bytes.Contains(b, []byte(mt.Token)) {
			t.Fatalf("a token is in %s: only its hash may be stored", filepath.Base(f))
		}
	}
}

func TestOnlyTheOwnerManagesMembersAndTheWorkspace(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	bo, _ := w.member(owner, "Bo")

	_, err := w.svc.AddMember(bg, bo, "Cy", "", domain.RoleMember)
	wantErr(t, err, domain.ErrForbidden)
	wantErr(t, w.svc.RemoveMember(bg, bo, owner.Member.ID), domain.ErrForbidden)
	_, err = w.svc.RenameWorkspace(bg, bo, "Mine now")
	wantErr(t, err, domain.ErrForbidden)
	_, err = w.svc.CreateProject(bg, bo, ProjectInput{Name: "P"})
	wantErr(t, err, domain.ErrForbidden)

	// What a member may do, they can.
	ms, err := w.svc.ListMembers(bg, bo)
	if err != nil || len(ms) != 2 || ms[0].Role != domain.RoleOwner {
		t.Fatalf("a member should see the team, owner first: %v %v", ms, err)
	}

	ws, err := w.svc.RenameWorkspace(bg, owner, "  Acme Corp ")
	if err != nil || ws.Name != "Acme Corp" {
		t.Fatalf("%+v %v", ws, err)
	}
	if got := w.signIn(mustToken(t, w, owner, bo)).Workspace.Name; got != "Acme Corp" {
		t.Fatalf("the rename was not stored: %q", got)
	}
}

// mustToken reissues bo's token as the owner, to sign in again.
func mustToken(t *testing.T, w *world, owner, bo Actor) string {
	t.Helper()
	mt, err := w.svc.ReissueToken(bg, owner, bo.Member.ID)
	if err != nil {
		t.Fatal(err)
	}
	return mt.Token
}

func TestAddMemberRules(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")

	mt, err := w.svc.AddMember(bg, owner, "Bo", "bo@example.com", "")
	if err != nil || mt.Member.Role != domain.RoleMember || mt.Member.Email != "bo@example.com" || mt.Token == "" {
		t.Fatalf("a member added with no role should be a Member: %+v %v", mt, err)
	}
	_, err = w.svc.AddMember(bg, owner, "bo", "", domain.RoleMember) // same name, other case
	wantErr(t, err, domain.ErrConflict)
	_, err = w.svc.AddMember(bg, owner, "Cy", "", domain.RoleOwner)
	wantErr(t, err, domain.ErrInvalid)
	_, err = w.svc.AddMember(bg, owner, "Cy", "", "superuser")
	wantErr(t, err, domain.ErrInvalid)
	_, err = w.svc.AddMember(bg, owner, "", "", domain.RoleMember)
	wantErr(t, err, domain.ErrInvalid)
	_, err = w.svc.AddMember(bg, owner, "Cy", "not-an-email", domain.RoleMember)
	wantErr(t, err, domain.ErrInvalid)
}

func TestRemovingAMemberSignsThemOutAndOffTheirProjects(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	bo, boToken := w.member(owner, "Bo")
	p := w.project(owner, "Billing")
	if err := w.svc.AddProjectMember(bg, owner, p.ID, bo.Member.ID, ""); err != nil {
		t.Fatal(err)
	}

	wantErr(t, w.svc.RemoveMember(bg, owner, owner.Member.ID), domain.ErrConflict) // the owner stays
	if err := w.svc.RemoveMember(bg, owner, bo.Member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Authenticate(bg, boToken); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("a removed member can still sign in: %v", err)
	}
	ms, err := w.svc.ListProjectMembers(bg, owner, p.ID)
	if err != nil || len(ms) != 1 || ms[0].ID != owner.Member.ID {
		t.Fatalf("a removed member is still on the project: %v %v", ms, err)
	}
	wantErr(t, w.svc.RemoveMember(bg, owner, bo.Member.ID), domain.ErrNotFound)
}

func TestReissuingATokenReplacesTheOldOne(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	bo, boToken := w.member(owner, "Bo")
	cy, _ := w.member(owner, "Cy")

	// A member may reissue their own, and not anyone else's.
	_, err := w.svc.ReissueToken(bg, bo, cy.Member.ID)
	wantErr(t, err, domain.ErrForbidden)
	mt, err := w.svc.ReissueToken(bg, bo, bo.Member.ID)
	if err != nil || mt.Token == boToken {
		t.Fatalf("%+v %v", mt, err)
	}
	if _, err := w.svc.Authenticate(bg, boToken); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("the old token still works after it was reissued")
	}
	w.signIn(mt.Token)
	// The owner may reissue anyone's; someone who is not in the workspace is not found.
	if _, err := w.svc.ReissueToken(bg, owner, cy.Member.ID); err != nil {
		t.Fatal(err)
	}
	_, err = w.svc.ReissueToken(bg, owner, "tmb_doesnotexist")
	wantErr(t, err, domain.ErrNotFound)
}

func TestProjectsAreVisibleToTheirPeopleAndTheOwner(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")
	bo, _ := w.member(owner, "Bo")
	cy, _ := w.member(owner, "Cy")
	billing := w.project(owner, "Billing")
	w.project(owner, "Search")

	if ps, _ := w.svc.ListProjects(bg, owner); len(ps) != 2 {
		t.Fatalf("the owner sees %d projects, want 2", len(ps))
	}
	if ps, err := w.svc.ListProjects(bg, bo); err != nil || len(ps) != 0 {
		t.Fatalf("a member on no project sees %v %v", ps, err)
	}
	if err := w.svc.AddProjectMember(bg, owner, billing.ID, bo.Member.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.AddProjectMember(bg, owner, billing.ID, bo.Member.ID, ""); err != nil {
		t.Fatalf("adding someone twice should do nothing, not fail: %v", err)
	}
	ps, err := w.svc.ListProjects(bg, bo)
	if err != nil || len(ps) != 1 || ps[0].ID != billing.ID {
		t.Fatalf("Bo should see only Billing: %v %v", ps, err)
	}
	if _, err := w.svc.GetProject(bg, bo, billing.ID); err != nil {
		t.Fatal(err)
	}
	// Cy is not on it: it does not exist, as far as Cy can tell.
	_, err = w.svc.GetProject(bg, cy, billing.ID)
	wantErr(t, err, domain.ErrNotFound)
	_, err = w.svc.ListProjectMembers(bg, cy, billing.ID)
	wantErr(t, err, domain.ErrNotFound)
	wantErr(t, w.svc.AddProjectMember(bg, cy, billing.ID, cy.Member.ID, ""), domain.ErrNotFound) // cannot add themselves
	wantErr(t, w.svc.RemoveProjectMember(bg, cy, billing.ID, bo.Member.ID), domain.ErrNotFound)
	// Bo is on it, so they know it exists, and may not change it.
	_, err = w.svc.UpdateProject(bg, bo, billing.ID, ProjectPatch{Name: ptr("Mine")})
	wantErr(t, err, domain.ErrForbidden)
	wantErr(t, w.svc.AddProjectMember(bg, bo, billing.ID, cy.Member.ID, ""), domain.ErrForbidden)
	wantErr(t, w.svc.RemoveProjectMember(bg, bo, billing.ID, owner.Member.ID), domain.ErrForbidden)

	ms, err := w.svc.ListProjectMembers(bg, bo, billing.ID)
	if err != nil || len(ms) != 2 || ms[0].ID != owner.Member.ID || ms[1].ID != bo.Member.ID {
		t.Fatalf("project members = %v %v", ms, err)
	}
	if err := w.svc.RemoveProjectMember(bg, owner, billing.ID, bo.Member.ID); err != nil {
		t.Fatal(err)
	}
	wantErr(t, w.svc.RemoveProjectMember(bg, owner, billing.ID, bo.Member.ID), domain.ErrNotFound)
	if ps, _ := w.svc.ListProjects(bg, bo); len(ps) != 0 {
		t.Fatal("Bo still sees the project after being taken off it")
	}
}

func ptr[T any](v T) *T { return &v }

func TestCreateAndUpdateProject(t *testing.T) {
	w := newWorld(t)
	owner, _ := w.workspace("Acme", "Ada")

	p, err := w.svc.CreateProject(bg, owner, ProjectInput{Name: " Billing ", Description: "Invoices", Repository: "https://github.com/acme/billing"})
	if err != nil || p.Name != "Billing" || p.Archived || p.Repository != "https://github.com/acme/billing" {
		t.Fatalf("%+v %v", p, err)
	}
	if ms, _ := w.svc.ListProjectMembers(bg, owner, p.ID); len(ms) != 1 || ms[0].ID != owner.Member.ID {
		t.Fatal("a project's creator should be on it")
	}
	_, err = w.svc.CreateProject(bg, owner, ProjectInput{Name: "billing"})
	wantErr(t, err, domain.ErrConflict)
	_, err = w.svc.CreateProject(bg, owner, ProjectInput{Name: "X", Repository: "https://me:pw@github.com/a/b"})
	wantErr(t, err, domain.ErrInvalid)
	_, err = w.svc.CreateProject(bg, owner, ProjectInput{Name: ""})
	wantErr(t, err, domain.ErrInvalid)

	q := w.project(owner, "Search")
	u, err := w.svc.UpdateProject(bg, owner, p.ID, ProjectPatch{Description: ptr(""), Archived: ptr(true), Repository: ptr("git@github.com:acme/billing.git")})
	if err != nil || !u.Archived || u.Description != "" || u.Name != "Billing" || u.Repository != "git@github.com:acme/billing.git" {
		t.Fatalf("%+v %v", u, err)
	}
	got, _ := w.svc.GetProject(bg, owner, p.ID)
	if !got.Archived || got.UpdatedAt.Before(p.UpdatedAt) {
		t.Fatalf("the update was not stored: %+v", got)
	}
	_, err = w.svc.UpdateProject(bg, owner, p.ID, ProjectPatch{Name: ptr(q.Name)})
	wantErr(t, err, domain.ErrConflict)
	_, err = w.svc.UpdateProject(bg, owner, p.ID, ProjectPatch{Name: ptr("")})
	wantErr(t, err, domain.ErrInvalid)
	_, err = w.svc.UpdateProject(bg, owner, "tpj_missing", ProjectPatch{})
	wantErr(t, err, domain.ErrNotFound)
	// A failed update changed nothing.
	if got, _ := w.svc.GetProject(bg, owner, p.ID); got.Name != "Billing" {
		t.Fatalf("a rejected update was partly applied: %+v", got)
	}
	ps, _ := w.svc.ListProjects(bg, owner)
	if ps[len(ps)-1].ID != p.ID {
		t.Fatal("archived projects should list after the active ones")
	}
}

func TestWorkspacesAreIsolatedFromEachOther(t *testing.T) {
	w := newWorld(t)
	acme, _ := w.workspace("Acme", "Ada")
	rival, _ := w.workspace("Rival", "Rex") // the same names are fine in another workspace
	if _, err := w.svc.AddMember(bg, rival, "Ada", "", domain.RoleMember); err != nil {
		t.Fatalf("names are unique per workspace, not globally: %v", err)
	}
	p := w.project(acme, "Billing")
	if _, err := w.svc.CreateProject(bg, rival, ProjectInput{Name: "Billing"}); err != nil {
		t.Fatalf("project names are unique per workspace: %v", err)
	}

	_, err := w.svc.GetProject(bg, rival, p.ID)
	wantErr(t, err, domain.ErrNotFound)
	_, err = w.svc.UpdateProject(bg, rival, p.ID, ProjectPatch{Archived: ptr(true)})
	wantErr(t, err, domain.ErrNotFound)
	wantErr(t, w.svc.AddProjectMember(bg, rival, p.ID, rival.Member.ID, ""), domain.ErrNotFound)
	wantErr(t, w.svc.RemoveMember(bg, rival, acme.Member.ID), domain.ErrNotFound)
	_, err = w.svc.ReissueToken(bg, rival, acme.Member.ID)
	wantErr(t, err, domain.ErrNotFound) // Rival's owner may reissue tokens, but only for Rival's members
	if ms, _ := w.svc.ListMembers(bg, rival); len(ms) != 2 {
		t.Fatalf("Rival sees %d members; only its own", len(ms))
	}

	// Putting Acme's member on Rival's project must fail even though both IDs are real.
	rp := w.project(rival, "Search")
	wantErr(t, w.svc.AddProjectMember(bg, rival, rp.ID, acme.Member.ID, ""), domain.ErrNotFound)
	// And the database refuses it too, whatever the service does.
	err = w.db.Update(bg, func(tx store.Tx) error {
		return tx.AddProjectMember(bg, rival.Workspace.ID, domain.ProjectMember{ProjectID: rp.ID, MemberID: acme.Member.ID, AddedBy: rival.Member.ID})
	})
	if err == nil {
		t.Fatal("the database let a member of one workspace onto another workspace's project")
	}
}
