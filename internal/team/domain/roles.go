package domain

import "sort"

// Role is what a member is allowed to do in a workspace.
//
// Roles are stored as text and mean nothing but the permissions listed for them
// in rolePermissions below, so adding a role later is one new constant and one new
// row in that table: no schema change, and no handler has to learn about it,
// because handlers and services ask "may this member do X" (Can), never "is this
// member an Owner".
type Role string

const (
	// RoleOwner created the workspace and can do everything in it. A workspace
	// has exactly one.
	RoleOwner Role = "owner"
	// RoleMember takes part in the projects they are added to.
	RoleMember Role = "member"
)

// Permission is one thing a role may do.
type Permission string

const (
	PermWorkspaceView        Permission = "workspace.view"
	PermWorkspaceManage      Permission = "workspace.manage" // rename the workspace
	PermMembersView          Permission = "members.view"
	PermMembersManage        Permission = "members.manage"    // add and remove members, reissue their tokens
	PermProjectsViewAll      Permission = "projects.view_all" // see every project, not only the ones you belong to
	PermProjectsCreate       Permission = "projects.create"
	PermProjectsManage       Permission = "projects.manage"        // edit or archive any project
	PermProjectMembersManage Permission = "project_members.manage" // add and remove people on a project
)

// AllPermissions lists every permission, for tests and for the roles endpoint.
func AllPermissions() []Permission {
	return []Permission{
		PermWorkspaceView, PermWorkspaceManage,
		PermMembersView, PermMembersManage,
		PermProjectsViewAll, PermProjectsCreate, PermProjectsManage, PermProjectMembersManage,
	}
}

var rolePermissions = map[Role][]Permission{
	RoleOwner: AllPermissions(),
	RoleMember: {
		PermWorkspaceView,
		PermMembersView,
	},
}

// Roles lists the roles that exist, owner first.
func Roles() []Role {
	roles := make([]Role, 0, len(rolePermissions))
	for r := range rolePermissions {
		roles = append(roles, r)
	}
	sort.Slice(roles, func(i, j int) bool {
		if roles[i] == RoleOwner || roles[j] == RoleOwner {
			return roles[i] == RoleOwner
		}
		return roles[i] < roles[j]
	})
	return roles
}

// Valid reports whether r is a role that exists.
func (r Role) Valid() bool {
	_, ok := rolePermissions[r]
	return ok
}

// Permissions lists what r may do.
func (r Role) Permissions() []Permission {
	return append([]Permission(nil), rolePermissions[r]...)
}

// Can reports whether r may do p. An unknown role may do nothing.
func (r Role) Can(p Permission) bool {
	for _, have := range rolePermissions[r] {
		if have == p {
			return true
		}
	}
	return false
}

// ParseRole turns text into a Role that exists.
func ParseRole(s string) (Role, error) {
	r := Role(s)
	if !r.Valid() {
		return "", invalid("role %q does not exist (roles: %s)", s, rolesList())
	}
	return r, nil
}

func rolesList() string {
	s := ""
	for i, r := range Roles() {
		if i > 0 {
			s += ", "
		}
		s += string(r)
	}
	return s
}
