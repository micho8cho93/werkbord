package domain

import "sort"

// Role is what a member is allowed to do in a workspace. It is about a person.
// What a *device* does for the workspace (hosts its data, connects its members,
// runs their work) is a Capability of the device (device.go), granted to the
// device and never implied by, or implying, a person's role: an Admin need not
// own a host, and the owner of a host need not be an Admin.
//
// Roles are stored as text and mean nothing but the permissions listed for them
// in rolePermissions below, so adding a role later is one new constant and one new
// row in that table: no schema change, and no handler has to learn about it,
// because handlers and services ask "may this member do X" (Can), never "is this
// member an Owner".
type Role string

const (
	// RoleOwner created the workspace and can do everything in it. A workspace
	// has exactly one, and it alone holds the workspace's ownership: appointing
	// and removing admins, and whatever licence the workspace runs under.
	RoleOwner Role = "owner"
	// RoleAdmin administers the workspace: its members, projects and devices. An
	// admin cannot appoint or remove other admins, cannot touch the owner, and has
	// no ownership authority.
	RoleAdmin Role = "admin"
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

	// PermAdminsManage appoints and removes admins, and acts on their accounts. Only the owner has it.
	PermAdminsManage Permission = "admins.manage"
	// PermOwnership is the workspace's ownership authority: the one that holds its
	// licence and, in time, may hand it on. Only the owner has it.
	PermOwnership Permission = "workspace.ownership"

	// PermDevicesOwn registers, renames and revokes one's own devices.
	PermDevicesOwn Permission = "devices.own"
	// PermDevicesViewAll sees every device in the workspace, not only one's own.
	PermDevicesViewAll Permission = "devices.view_all"
	// PermDevicesManage grants a device the infrastructure capabilities (Workspace
	// Host, Connectivity Host), and revokes any device.
	PermDevicesManage Permission = "devices.manage"
)

// AllPermissions lists every permission, for tests and for the roles endpoint.
func AllPermissions() []Permission {
	return []Permission{
		PermWorkspaceView, PermWorkspaceManage,
		PermMembersView, PermMembersManage,
		PermProjectsViewAll, PermProjectsCreate, PermProjectsManage, PermProjectMembersManage,
		PermAdminsManage, PermOwnership,
		PermDevicesOwn, PermDevicesViewAll, PermDevicesManage,
	}
}

// ownerOnly are the permissions an admin does not have.
var ownerOnly = map[Permission]bool{PermAdminsManage: true, PermOwnership: true}

func adminPermissions() []Permission {
	var out []Permission
	for _, p := range AllPermissions() {
		if !ownerOnly[p] {
			out = append(out, p)
		}
	}
	return out
}

var rolePermissions = map[Role][]Permission{
	RoleOwner: AllPermissions(),
	RoleAdmin: adminPermissions(),
	RoleMember: {
		PermWorkspaceView,
		PermMembersView,
		PermDevicesOwn,
	},
}

// CanManage reports whether a member holding r may add, remove or reissue the
// token of a member holding target. It is what keeps the members.manage permission
// from being a way up: an admin can administer members, but not other admins and
// never the owner (reissuing the owner's token would be taking the workspace).
// A person acting on their own account is not "managing" and is checked elsewhere.
func (r Role) CanManage(target Role) bool {
	switch {
	case !r.Can(PermMembersManage), target == RoleOwner:
		return false
	case target == RoleAdmin:
		return r.Can(PermAdminsManage)
	}
	return true
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
