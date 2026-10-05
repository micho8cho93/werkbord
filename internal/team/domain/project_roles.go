package domain

// ProjectRole is what a member may do inside one project. It is separate from the
// workspace Role: the workspace role says what you may do to the workspace (add
// people, create projects), the project role what you may do to a project's work.
//
// As with Role, a project role means nothing but the permissions listed for it
// below, and callers ask "may this member do X here" (Can), never "is this member
// an owner".
type ProjectRole string

const (
	// ProjectOwner runs the project: invites people, assigns and releases any
	// ticket, reviews and completes work. A workspace member whose role carries
	// projects.manage (the workspace owner) is a project owner of every project.
	ProjectOwner ProjectRole = "owner"
	// ProjectReviewer reviews work: asks for changes and completes tickets.
	ProjectReviewer ProjectRole = "reviewer"
	// ProjectContributor claims tickets and does the work.
	ProjectContributor ProjectRole = "member"
)

// ProjectPermission is one thing a project role may do.
type ProjectPermission string

const (
	PPTicketsView     ProjectPermission = "tickets.view"
	PPTicketsCreate   ProjectPermission = "tickets.create"
	PPTicketsEdit     ProjectPermission = "tickets.edit"   // edit any ticket's text, promote or demote it (a creator may edit their own)
	PPTicketsClaim    ProjectPermission = "tickets.claim"  // take an available ticket
	PPTicketsAssign   ProjectPermission = "tickets.assign" // assign, reassign or release a ticket that someone else holds
	PPTicketsReview   ProjectPermission = "tickets.review" // send work back, or mark a reviewed ticket done
	PPTicketsReopen   ProjectPermission = "tickets.reopen" // reopen a finished ticket
	PPGitReport       ProjectPermission = "git.report"     // report branches, commits and pull requests
	PPGitReportAny    ProjectPermission = "git.report_any" // report them for a ticket that is someone else's
	PPInvitesManage   ProjectPermission = "invites.manage" // create, list and revoke invites
	PPMembersManage   ProjectPermission = "members.manage" // add, remove and change the role of people on the project
	PPActivityView    ProjectPermission = "activity.view"
	PPRepositoryView  ProjectPermission = "repository.view"
	PPHandoffOwnTasks ProjectPermission = "handoff.own" // open a ticket you hold in your own runner
)

// AllProjectPermissions lists every project permission.
func AllProjectPermissions() []ProjectPermission {
	return []ProjectPermission{
		PPTicketsView, PPTicketsCreate, PPTicketsEdit, PPTicketsClaim, PPTicketsAssign, PPTicketsReview, PPTicketsReopen,
		PPGitReport, PPGitReportAny, PPInvitesManage, PPMembersManage, PPActivityView, PPRepositoryView, PPHandoffOwnTasks,
	}
}

var contributorPermissions = []ProjectPermission{
	PPTicketsView, PPTicketsCreate, PPTicketsClaim, PPGitReport, PPActivityView, PPRepositoryView, PPHandoffOwnTasks,
}

var projectRolePermissions = map[ProjectRole][]ProjectPermission{
	ProjectOwner:       AllProjectPermissions(),
	ProjectReviewer:    append(append([]ProjectPermission(nil), contributorPermissions...), PPTicketsReview, PPGitReportAny),
	ProjectContributor: contributorPermissions,
}

// ProjectRoles lists the project roles, most powerful first.
func ProjectRoles() []ProjectRole {
	return []ProjectRole{ProjectOwner, ProjectReviewer, ProjectContributor}
}

// Valid reports whether r is a project role that exists.
func (r ProjectRole) Valid() bool {
	_, ok := projectRolePermissions[r]
	return ok
}

// Can reports whether r may do p. An unknown role may do nothing.
func (r ProjectRole) Can(p ProjectPermission) bool {
	for _, have := range projectRolePermissions[r] {
		if have == p {
			return true
		}
	}
	return false
}

// Permissions lists what r may do.
func (r ProjectRole) Permissions() []ProjectPermission {
	return append([]ProjectPermission(nil), projectRolePermissions[r]...)
}

// ParseProjectRole turns text into a project role that exists. Empty is a plain member.
func ParseProjectRole(s string) (ProjectRole, error) {
	if s == "" {
		return ProjectContributor, nil
	}
	r := ProjectRole(s)
	if !r.Valid() {
		return "", invalid("project role %q does not exist (roles: owner, reviewer, member)", s)
	}
	return r, nil
}
