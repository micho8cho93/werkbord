// Package service is Werkbord Team's use cases: sign a member in by token, show a
// workspace, add and remove members, create projects and put members on them.
//
// Every operation takes the signed-in member (Actor) and asks the member's role
// whether it is allowed (domain.Role.Can), so what a role may do is decided in one
// place, internal/team/domain/roles.go. A thing the actor may not see is reported
// as not found, exactly like a thing that does not exist, so a member cannot probe
// for projects they are not on.
//
// This is a coordination service: nothing here starts a process, runs a command,
// or touches Git or an agent. See docs/PRODUCTS.md.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"devboard/internal/team/domain"
	"devboard/internal/team/store"
)

// Service is the Team application.
type Service struct {
	db  store.Store
	now func() time.Time
	hub *hub
	// net is what makes the workspace's private network's certificates and
	// configuration; nil when this host has none (SetNetwork).
	net NetworkAuthority
}

// SetClock replaces the clock the service reads (for tests, which move time to see an
// invitation expire; time.Now otherwise).
func (s *Service) SetClock(now func() time.Time) {
	if now == nil {
		now = time.Now
	}
	s.now = now
}

// New builds a Service on a database.
func New(db store.Store) *Service { return &Service{db: db, now: time.Now, hub: &hub{}} }

// changed wakes the clients waiting for the workspace to change, once a write has committed.
func (s *Service) changed(workspaceID string, err error) {
	if err == nil {
		s.hub.notify(workspaceKey(workspaceID))
	}
}

// stamp is the current time as stored: UTC, to the millisecond, so what a call
// returns is exactly what a later read returns.
func (s *Service) stamp() time.Time { return s.now().UTC().Truncate(time.Millisecond) }

// Actor is the signed-in member and the workspace they are in.
type Actor struct {
	Member    domain.Member
	Workspace domain.Workspace
	// Device is the device the request came from, when the caller signed in with a
	// device's own credential (what an enrolled device holds) rather than a member's
	// token. A revoked device has no credential that works, so a request that arrives
	// with one never reaches a handler, however it got to the host.
	Device *domain.Device
}

func forbidden(what string) error {
	return fmt.Errorf("%w: your role cannot %s", domain.ErrForbidden, what)
}

func (a Actor) require(p domain.Permission, what string) error {
	if !a.Member.Can(p) {
		return forbidden(what)
	}
	return nil
}

// ---- starting a workspace, and signing in ----

// Created is a new workspace, its owner, and the owner's token. The token is
// returned here and nowhere else: only its hash is stored.
type Created struct {
	Workspace domain.Workspace `json:"workspace"`
	Owner     domain.Member    `json:"owner"`
	Token     string           `json:"token"`
}

// CreateWorkspace starts a workspace with its owner. It is the one way a workspace
// comes to exist, and it is not reachable over HTTP: it is what
// `werkbord-team workspace create` does on the computer that hosts the server.
func (s *Service) CreateWorkspace(ctx context.Context, name, ownerName, ownerEmail string) (Created, error) {
	name, err := domain.CleanName("workspace", name)
	if err != nil {
		return Created{}, err
	}
	ownerName, err = domain.CleanName("owner", ownerName)
	if err != nil {
		return Created{}, err
	}
	ownerEmail, err = domain.CleanEmail(ownerEmail)
	if err != nil {
		return Created{}, err
	}
	now := s.stamp()
	ws := domain.Workspace{ID: domain.NewID(domain.PrefixWorkspace), Name: name, CreatedAt: now}
	owner := domain.Member{ID: domain.NewID(domain.PrefixMember), WorkspaceID: ws.ID, Name: ownerName, Email: ownerEmail, Role: domain.RoleOwner, CreatedAt: now}
	token, hash := domain.NewToken()
	err = s.db.Update(ctx, func(tx store.Tx) error {
		if err := tx.InsertWorkspace(ctx, ws); err != nil {
			return err
		}
		return tx.InsertMember(ctx, owner, hash)
	})
	if err != nil {
		return Created{}, err
	}
	return Created{Workspace: ws, Owner: owner, Token: token}, nil
}

// Authenticate finds who a token belongs to. A token that matches nobody is
// ErrUnauthenticated: the same answer whether it never existed or was reissued.
func (s *Service) Authenticate(ctx context.Context, token string) (Actor, error) {
	if token == "" {
		return Actor{}, domain.ErrUnauthenticated
	}
	var a Actor
	hash := domain.HashToken(token)
	err := s.db.View(ctx, func(tx store.Tx) error {
		m, err := tx.MemberByTokenHash(ctx, hash)
		var dev *domain.Device
		if errors.Is(err, domain.ErrNotFound) {
			// Not a member's token: a device's own credential. The device must not be
			// revoked, and its owner must still be a member: both are checked here, on
			// every request, which is what makes revoking a device take effect at once.
			d, derr := tx.DeviceByCredentialHash(ctx, hash)
			if derr != nil {
				return derr
			}
			if d.Revoked() {
				return domain.ErrNotFound
			}
			if m, err = tx.Member(ctx, d.WorkspaceID, d.MemberID); err != nil {
				return err
			}
			dev = &d
		} else if err != nil {
			return err
		}
		ws, err := tx.Workspace(ctx, m.WorkspaceID)
		if err != nil {
			return err
		}
		a = Actor{Member: m, Workspace: ws, Device: dev}
		return nil
	})
	if errors.Is(err, domain.ErrNotFound) {
		return Actor{}, domain.ErrUnauthenticated
	}
	return a, err
}

// ---- the workspace ----

// Me is what the signed-in member is, and what their role lets them do.
type Me struct {
	Member      domain.Member       `json:"member"`
	Workspace   domain.Workspace    `json:"workspace"`
	Permissions []domain.Permission `json:"permissions"`
}

// Me describes the actor.
func (s *Service) Me(a Actor) Me {
	return Me{Member: a.Member, Workspace: a.Workspace, Permissions: a.Member.Role.Permissions()}
}

// RenameWorkspace changes the workspace's name.
func (s *Service) RenameWorkspace(ctx context.Context, a Actor, name string) (domain.Workspace, error) {
	if err := a.require(domain.PermWorkspaceManage, "rename the workspace"); err != nil {
		return domain.Workspace{}, err
	}
	name, err := domain.CleanName("workspace", name)
	if err != nil {
		return domain.Workspace{}, err
	}
	ws := a.Workspace
	ws.Name = name
	err = s.db.Update(ctx, func(tx store.Tx) error { return tx.RenameWorkspace(ctx, ws.ID, name) })
	s.changed(a.Workspace.ID, err)
	return ws, err
}

// ---- members ----

// ListMembers lists the people in the workspace.
func (s *Service) ListMembers(ctx context.Context, a Actor) ([]domain.Member, error) {
	if err := a.require(domain.PermMembersView, "see the members"); err != nil {
		return nil, err
	}
	var out []domain.Member
	err := s.db.View(ctx, func(tx store.Tx) (err error) { out, err = tx.Members(ctx, a.Workspace.ID); return })
	return out, err
}

// MemberWithToken is a member and the token that signs them in, shown once.
type MemberWithToken struct {
	Member domain.Member `json:"member"`
	Token  string        `json:"token"`
}

// AddMember adds a person to the workspace and returns the token that signs them
// in. A workspace has one owner, its creator, so a new member cannot be given the
// owner role; only the owner may appoint an admin.
func (s *Service) AddMember(ctx context.Context, a Actor, name, email string, role domain.Role) (MemberWithToken, error) {
	if err := a.require(domain.PermMembersManage, "add members"); err != nil {
		return MemberWithToken{}, err
	}
	name, err := domain.CleanName("member", name)
	if err != nil {
		return MemberWithToken{}, err
	}
	if email, err = domain.CleanEmail(email); err != nil {
		return MemberWithToken{}, err
	}
	if role == "" {
		role = domain.RoleMember
	}
	if !role.Valid() {
		_, err := domain.ParseRole(string(role))
		return MemberWithToken{}, err
	}
	if role == domain.RoleOwner {
		return MemberWithToken{}, fmt.Errorf("%w: a workspace has one owner, the person who created it", domain.ErrInvalid)
	}
	if !a.Member.Role.CanManage(role) {
		return MemberWithToken{}, forbidden("add a member with the " + string(role) + " role")
	}
	m := domain.Member{ID: domain.NewID(domain.PrefixMember), WorkspaceID: a.Workspace.ID, Name: name, Email: email, Role: role, CreatedAt: s.stamp()}
	token, hash := domain.NewToken()
	err = s.db.Update(ctx, func(tx store.Tx) error { return tx.InsertMember(ctx, m, hash) })
	s.changed(a.Workspace.ID, err)
	if err != nil {
		return MemberWithToken{}, err
	}
	return MemberWithToken{Member: m, Token: token}, nil
}

// RemoveMember removes a member, which signs them out and takes them off every
// project. The owner cannot be removed.
func (s *Service) RemoveMember(ctx context.Context, a Actor, memberID string) error {
	if err := a.require(domain.PermMembersManage, "remove members"); err != nil {
		return err
	}
	var changed []string
	err := s.db.Update(ctx, func(tx store.Tx) error {
		m, err := tx.Member(ctx, a.Workspace.ID, memberID)
		if err != nil {
			return err
		}
		if m.Role == domain.RoleOwner {
			return fmt.Errorf("%w: the owner cannot be removed from their workspace", domain.ErrConflict)
		}
		if !a.Member.Role.CanManage(m.Role) {
			return forbidden("remove a member with the " + string(m.Role) + " role")
		}
		// Their devices are revoked first, so that their certificates are refused by the
		// network and their credentials by the API: deleting the member deletes the
		// registry rows, and the refusal has to outlive them.
		if err := s.revokeDevicesOf(ctx, tx, a.Workspace.ID, memberID); err != nil {
			return err
		}
		// Whatever they were working on goes back on its board first.
		projects, err := tx.ProjectsOfMemberWithHeldTickets(ctx, a.Workspace.ID, memberID)
		if err != nil {
			return err
		}
		for _, pid := range projects {
			if err := s.releaseAll(ctx, tx, a.Workspace.ID, pid, memberID, a.Member.ID, "left the workspace"); err != nil {
				return err
			}
			if _, err := tx.BumpRevision(ctx, a.Workspace.ID, pid); err != nil {
				return err
			}
			changed = append(changed, pid)
		}
		return tx.DeleteMember(ctx, a.Workspace.ID, memberID)
	})
	if err == nil {
		for _, pid := range changed {
			s.hub.notify(pid)
		}
	}
	s.changed(a.Workspace.ID, err)
	return err
}

// ReissueToken gives a member a new token and invalidates the old one. Anyone may
// reissue their own (a token that leaked); reissuing someone else's takes
// members.manage.
func (s *Service) ReissueToken(ctx context.Context, a Actor, memberID string) (MemberWithToken, error) {
	if memberID != a.Member.ID {
		if err := a.require(domain.PermMembersManage, "reissue another member's token"); err != nil {
			return MemberWithToken{}, err
		}
	}
	token, hash := domain.NewToken()
	var m domain.Member
	err := s.db.Update(ctx, func(tx store.Tx) (err error) {
		if m, err = tx.Member(ctx, a.Workspace.ID, memberID); err != nil {
			return err
		}
		// Reissuing a token hands over the account, so someone who may manage
		// members still may not take an admin's or the owner's.
		if memberID != a.Member.ID && !a.Member.Role.CanManage(m.Role) {
			return forbidden("reissue the token of a member with the " + string(m.Role) + " role")
		}
		return tx.SetMemberToken(ctx, a.Workspace.ID, memberID, hash)
	})
	if err != nil {
		return MemberWithToken{}, err
	}
	return MemberWithToken{Member: m, Token: token}, nil
}

// ---- projects ----

// ListProjects lists the projects the actor can see: all of them with
// projects.view_all, otherwise those they are on.
func (s *Service) ListProjects(ctx context.Context, a Actor) ([]domain.Project, error) {
	only := a.Member.ID
	if a.Member.Can(domain.PermProjectsViewAll) {
		only = ""
	}
	var out []domain.Project
	err := s.db.View(ctx, func(tx store.Tx) (err error) { out, err = tx.Projects(ctx, a.Workspace.ID, only); return })
	return out, err
}

// visibleProject loads a project the actor may see, or reports it as not found.
func (s *Service) visibleProject(ctx context.Context, tx store.Tx, a Actor, id string) (domain.Project, error) {
	p, err := tx.Project(ctx, a.Workspace.ID, id)
	if err != nil {
		return p, err
	}
	if a.Member.Can(domain.PermProjectsViewAll) {
		return p, nil
	}
	on, err := tx.IsProjectMember(ctx, a.Workspace.ID, id, a.Member.ID)
	if err != nil {
		return p, err
	}
	if !on {
		return domain.Project{}, fmt.Errorf("%w: project", domain.ErrNotFound)
	}
	return p, nil
}

// GetProject returns a project the actor can see.
func (s *Service) GetProject(ctx context.Context, a Actor, id string) (domain.Project, error) {
	var p domain.Project
	err := s.db.View(ctx, func(tx store.Tx) (err error) { p, err = s.visibleProject(ctx, tx, a, id); return })
	return p, err
}

// ProjectInput is what creating a project takes.
type ProjectInput struct {
	Name        string
	Description string
	Repository  string
}

// CreateProject creates a project. Its creator is put on it.
func (s *Service) CreateProject(ctx context.Context, a Actor, in ProjectInput) (domain.Project, error) {
	if err := a.require(domain.PermProjectsCreate, "create projects"); err != nil {
		return domain.Project{}, err
	}
	name, err := domain.CleanName("project", in.Name)
	if err != nil {
		return domain.Project{}, err
	}
	desc, err := domain.CleanDescription(in.Description)
	if err != nil {
		return domain.Project{}, err
	}
	repo, err := domain.CleanRepository(in.Repository)
	if err != nil {
		return domain.Project{}, err
	}
	now := s.stamp()
	p := domain.Project{ID: domain.NewID(domain.PrefixProject), WorkspaceID: a.Workspace.ID, Name: name, Description: desc, Repository: repo, CreatedAt: now, UpdatedAt: now}
	err = s.db.Update(ctx, func(tx store.Tx) error {
		if err := tx.InsertProject(ctx, p); err != nil {
			return err
		}
		return tx.AddProjectMember(ctx, a.Workspace.ID, domain.ProjectMember{ProjectID: p.ID, MemberID: a.Member.ID, Role: domain.ProjectOwner, AddedBy: a.Member.ID, AddedAt: now})
	})
	s.changed(a.Workspace.ID, err)
	return p, err
}

// ProjectPatch is what updating a project may change; a nil field is left alone.
type ProjectPatch struct {
	Name        *string
	Description *string
	Repository  *string
	Archived    *bool
}

// UpdateProject edits or archives a project.
func (s *Service) UpdateProject(ctx context.Context, a Actor, id string, patch ProjectPatch) (domain.Project, error) {
	var p domain.Project
	err := s.db.Update(ctx, func(tx store.Tx) (err error) {
		if p, err = s.visibleProject(ctx, tx, a, id); err != nil {
			return err
		}
		if err := a.require(domain.PermProjectsManage, "change projects"); err != nil {
			return err
		}
		if patch.Name != nil {
			if p.Name, err = domain.CleanName("project", *patch.Name); err != nil {
				return err
			}
		}
		if patch.Description != nil {
			if p.Description, err = domain.CleanDescription(*patch.Description); err != nil {
				return err
			}
		}
		if patch.Repository != nil {
			if p.Repository, err = domain.CleanRepository(*patch.Repository); err != nil {
				return err
			}
		}
		if patch.Archived != nil {
			p.Archived = *patch.Archived
		}
		p.UpdatedAt = s.stamp()
		if err := tx.UpdateProject(ctx, p); err != nil {
			return err
		}
		// A rename or an archive changes what the project's board shows.
		p.Revision, err = tx.BumpRevision(ctx, a.Workspace.ID, id)
		return err
	})
	if err == nil {
		s.hub.notify(id)
	}
	s.changed(a.Workspace.ID, err)
	return p, err
}

// ---- project membership ----

// ListProjectMembers lists the members on a project the actor can see, as members.
func (s *Service) ListProjectMembers(ctx context.Context, a Actor, projectID string) ([]domain.Member, error) {
	var out []domain.Member
	err := s.db.View(ctx, func(tx store.Tx) error {
		if _, err := s.visibleProject(ctx, tx, a, projectID); err != nil {
			return err
		}
		pms, err := tx.ProjectMembers(ctx, a.Workspace.ID, projectID)
		if err != nil {
			return err
		}
		out = make([]domain.Member, 0, len(pms))
		for _, pm := range pms {
			m, err := tx.Member(ctx, a.Workspace.ID, pm.MemberID)
			if err != nil {
				return err
			}
			out = append(out, m)
		}
		return nil
	})
	return out, err
}

// Person is a member as seen on one project: who they are and their role there.
type Person struct {
	domain.Member
	ProjectRole domain.ProjectRole `json:"projectRole"`
}

// ListProjectPeople lists the people on a project with their project roles, the
// workspace owner first and then by name.
func (s *Service) ListProjectPeople(ctx context.Context, a Actor, projectID string) ([]Person, error) {
	var out []Person
	err := s.view(ctx, a, projectID, func(tx store.Tx, x access) (err error) {
		out, err = people(ctx, tx, a.Workspace.ID, projectID)
		return err
	})
	return out, err
}

func people(ctx context.Context, tx store.Tx, workspaceID, projectID string) ([]Person, error) {
	pms, err := tx.ProjectMembers(ctx, workspaceID, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]Person, 0, len(pms))
	for _, pm := range pms {
		m, err := tx.Member(ctx, workspaceID, pm.MemberID)
		if err != nil {
			return nil, err
		}
		out = append(out, Person{Member: m, ProjectRole: pm.Role})
	}
	return out, nil
}

// AddProjectMember puts a member of the workspace on a project, as a plain
// member unless role says otherwise. Repeating it does nothing, except that a
// role given for someone already on the project changes their role.
func (s *Service) AddProjectMember(ctx context.Context, a Actor, projectID, memberID string, role domain.ProjectRole) error {
	if role != "" && !role.Valid() {
		_, err := domain.ParseProjectRole(string(role))
		return err
	}
	return s.mutate(ctx, a, projectID, func(tx store.Tx, x access) error {
		if err := x.require(domain.PPMembersManage, "change who is on a project"); err != nil {
			return err
		}
		if _, err := tx.Member(ctx, a.Workspace.ID, memberID); err != nil {
			return err
		}
		_, on, err := tx.ProjectRole(ctx, a.Workspace.ID, projectID, memberID)
		if err != nil {
			return err
		}
		if !on {
			return tx.AddProjectMember(ctx, a.Workspace.ID, domain.ProjectMember{ProjectID: projectID, MemberID: memberID, Role: role, AddedBy: a.Member.ID, AddedAt: s.stamp()})
		}
		if role == "" {
			return nil
		}
		return tx.SetProjectMemberRole(ctx, a.Workspace.ID, projectID, memberID, role)
	})
}

// RemoveProjectMember takes a member off a project. Any ticket they were working
// on goes back on the board, available. The owner has a place on every project
// they create, but nothing forces them to keep it.
func (s *Service) RemoveProjectMember(ctx context.Context, a Actor, projectID, memberID string) error {
	return s.mutate(ctx, a, projectID, func(tx store.Tx, x access) error {
		if err := x.require(domain.PPMembersManage, "change who is on a project"); err != nil {
			return err
		}
		removed, err := tx.RemoveProjectMember(ctx, a.Workspace.ID, projectID, memberID)
		if err != nil {
			return err
		}
		if !removed {
			return fmt.Errorf("%w: that member is not on this project", domain.ErrNotFound)
		}
		return s.releaseAll(ctx, tx, a.Workspace.ID, projectID, memberID, a.Member.ID, "left the project")
	})
}

// releaseAll puts back the tickets a member holds in progress on a project and records it.
func (s *Service) releaseAll(ctx context.Context, tx store.Tx, workspaceID, projectID, memberID, actorID, why string) error {
	now := s.stamp()
	released, err := tx.ReleaseHeld(ctx, workspaceID, projectID, memberID, now)
	if err != nil {
		return err
	}
	for _, k := range released {
		if err := tx.AddActivity(ctx, workspaceID, domain.Activity{ProjectID: projectID, TicketID: k.ID, TicketKey: k.Key, ActorID: actorID, Kind: domain.ActTicketReleased, Detail: why, CreatedAt: now}); err != nil {
			return err
		}
	}
	return nil
}
