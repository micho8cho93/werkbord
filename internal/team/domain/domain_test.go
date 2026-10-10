package domain

import (
	"errors"
	"strings"
	"testing"
)

// The permission tables are the whole of what a role means. They are written out
// here in full, so a change to what a role may do is a visible change to this test.
func TestEachRoleHasExactlyItsPermissions(t *testing.T) {
	for _, p := range AllPermissions() {
		if !RoleOwner.Can(p) {
			t.Errorf("the owner cannot %s: a permission added to AllPermissions must not be forgotten by Owner", p)
		}
	}
	tables := map[Role]map[Permission]bool{
		RoleAdmin: {
			PermWorkspaceView: true, PermWorkspaceManage: true, PermMembersView: true, PermMembersManage: true,
			PermProjectsViewAll: true, PermProjectsCreate: true, PermProjectsManage: true, PermProjectMembersManage: true, PermLabelsManage: true,
			PermDevicesOwn: true, PermDevicesViewAll: true, PermDevicesManage: true,
		},
		RoleMember: {PermWorkspaceView: true, PermMembersView: true, PermDevicesOwn: true},
	}
	for role, want := range tables {
		for _, p := range AllPermissions() {
			if role.Can(p) != want[p] {
				t.Errorf("%s.Can(%s) = %v, want %v", role, p, role.Can(p), want[p])
			}
		}
	}
	if Role("nobody").Can(PermWorkspaceView) || Role("").Can(PermWorkspaceView) {
		t.Error("a role that does not exist can do something")
	}
}

// What only the owner has stays with the owner: an admin administers, and does
// not own, appoint other admins, or reach into the owner's account.
func TestAnAdminAdministersButDoesNotOwn(t *testing.T) {
	for _, p := range []Permission{PermOwnership, PermAdminsManage} {
		if RoleAdmin.Can(p) || RoleMember.Can(p) || !RoleOwner.Can(p) {
			t.Errorf("%s must belong to the owner alone", p)
		}
	}
	for _, c := range []struct {
		actor, target Role
		want          bool
	}{
		{RoleOwner, RoleAdmin, true},
		{RoleOwner, RoleMember, true},
		{RoleOwner, RoleOwner, false}, // the owner is not managed by anyone
		{RoleAdmin, RoleMember, true},
		{RoleAdmin, RoleAdmin, false}, // an admin cannot touch another admin
		{RoleAdmin, RoleOwner, false}, // nor take over the owner's account
		{RoleMember, RoleMember, false},
		{RoleMember, RoleAdmin, false},
		{Role("nobody"), RoleMember, false},
	} {
		if got := c.actor.CanManage(c.target); got != c.want {
			t.Errorf("%s.CanManage(%s) = %v, want %v", c.actor, c.target, got, c.want)
		}
	}
}

func TestRolesAreListedAndParsed(t *testing.T) {
	rs := Roles()
	if len(rs) != 3 || rs[0] != RoleOwner || rs[1] != RoleAdmin || rs[2] != RoleMember {
		t.Fatalf("roles = %v", rs)
	}
	for _, name := range []string{"member", "admin", "owner"} {
		if r, err := ParseRole(name); err != nil || string(r) != name {
			t.Fatalf("%s: %v %v", name, r, err)
		}
	}
	if _, err := ParseRole("superuser"); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "owner, admin, member") {
		t.Fatalf("an unknown role should be refused, naming the roles: %v", err)
	}
	// What Permissions returns is a copy: changing it must not change the role.
	ps := RoleMember.Permissions()
	ps[0] = PermProjectsManage
	if RoleMember.Can(PermProjectsManage) {
		t.Fatal("Permissions() exposed the role's own list")
	}
}

func TestEveryPermissionIsDistinct(t *testing.T) {
	seen := map[Permission]bool{}
	for _, p := range AllPermissions() {
		if seen[p] {
			t.Errorf("%s is listed twice", p)
		}
		seen[p] = true
	}
}

func TestTokens(t *testing.T) {
	a, ha := NewToken()
	b, hb := NewToken()
	if a == b || ha == hb {
		t.Fatal("two tokens are the same")
	}
	if !strings.HasPrefix(a, TokenPrefix) || len(a) != len(TokenPrefix)+64 {
		t.Fatalf("token %q has the wrong shape", a)
	}
	if HashToken(a) != ha || ha == a || strings.Contains(ha, a) {
		t.Fatal("the stored hash must be a stable function of the token and not the token")
	}
}

func TestIDs(t *testing.T) {
	id := NewID(PrefixProject)
	if !strings.HasPrefix(id, "tpj_") || len(id) != len("tpj_")+16 || id == NewID(PrefixProject) {
		t.Fatalf("id %q", id)
	}
}

func TestCleanName(t *testing.T) {
	if got, err := CleanName("project", "  Billing  "); err != nil || got != "Billing" {
		t.Fatalf("%q %v", got, err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("x", MaxNameLen+1), "a\x00b", "line\nbreak", "tab\tbed"} {
		if _, err := CleanName("project", bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q was accepted", bad)
		}
	}
	if _, err := CleanName("project", strings.Repeat("é", MaxNameLen)); err != nil {
		t.Errorf("the limit counts characters, not bytes: %v", err)
	}
}

func TestCleanEmail(t *testing.T) {
	for in, ok := range map[string]bool{
		"": true, " ada@example.com ": true, "a@b": true,
		"ada": false, "@example.com": false, "ada@": false, "a b@example.com": false, "a@b@c": false, "<a@b.c>": false,
	} {
		if _, err := CleanEmail(in); (err == nil) != ok {
			t.Errorf("CleanEmail(%q) = %v, want ok=%v", in, err, ok)
		}
	}
}

func TestCleanRepository(t *testing.T) {
	good := []string{
		"", "https://github.com/acme/app", "https://github.com/acme/app.git", "ssh://git@github.com/acme/app.git",
		"git@github.com:acme/app.git", "git://example.com/app.git",
	}
	for _, s := range good {
		if _, err := CleanRepository(s); err != nil {
			t.Errorf("CleanRepository(%q): %v", s, err)
		}
	}
	bad := []string{
		"https://user:secret@github.com/acme/app", // a password
		"https://ghp_abcdef@github.com/acme/app",  // a token as the user name
		"http://github.com/acme/app",              // not encrypted
		"file:///home/ada/app", "javascript:alert(1)", "/home/ada/app", "github.com/acme/app",
		"https://github.com/acme/app?token=abc", "https://github.com/acme/app#frag",
		"https://github.com/acme app", "git@github.com:", "https://",
	}
	for _, s := range bad {
		_, err := CleanRepository(s)
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("CleanRepository(%q) = %v, want invalid", s, err)
			continue
		}
		if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "ghp_abcdef") {
			t.Errorf("the error for %q repeats the credential: %v", s, err)
		}
	}
}

func TestCleanDescription(t *testing.T) {
	if got, err := CleanDescription("  hello\nworld "); err != nil || got != "hello\nworld" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := CleanDescription(strings.Repeat("x", MaxDescriptionLen+1)); err == nil {
		t.Error("a very long description was accepted")
	}
	if _, err := CleanDescription("a\x00"); err == nil {
		t.Error("a control character was accepted")
	}
}
