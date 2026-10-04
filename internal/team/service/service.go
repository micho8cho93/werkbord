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
	db  *store.DB
	now func() time.Time
}

// New builds a Service on a database.
func New(db *store.DB) *Service { return &Service{db: db, now: time.Now} }

// stamp is the current time as stored: UTC, to the millisecond, so what a call
// returns is exactly what a later read returns.
func (s *Service) stamp() time.Time { return s.now().UTC().Truncate(time.Millisecond) }

// Actor is the signed-in member and the workspace they are in.
type Actor struct {
	Member    domain.Member
	Workspace domain.Workspace
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
	err = s.db.Update(ctx, func(tx *store.Tx) error {
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
	err := s.db.View(ctx, func(tx *store.Tx) error {
		m, err := tx.MemberByTokenHash(ctx, domain.HashToken(token))
		if err != nil {
			return err
		}
		ws, err := tx.Workspace(ctx, m.WorkspaceID)
		if err != nil {
			return err
		}
		a = Actor{Member: m, Workspace: ws}
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
	return ws, s.db.Update(ctx, func(tx *store.Tx) error { return tx.RenameWorkspace(ctx, ws.ID, name) })
}

// ---- members ----

// ListMembers lists the people in the workspace.
func (s *Service) ListMembers(ctx context.Context, a Actor) ([]domain.Member, error) {
	if err := a.require(domain.PermMembersView, "see the members"); err != nil {
		return nil, err
	}
	var out []domain.Member
	err := s.db.View(ctx, func(tx *store.Tx) (err error) { out, err = tx.Members(ctx, a.Workspace.ID); return })
	return out, err
}

// MemberWithToken is a member and the token that signs them in, shown once.
type MemberWithToken struct {
	Member domain.Member `json:"member"`
	Token  string        `json:"token"`
}

// AddMember adds a person to the workspace and returns the token that signs them
// in. A workspace has one owner, its creator, so a new member cannot be given the owner role.
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
	m := domain.Member{ID: domain.NewID(domain.PrefixMember), WorkspaceID: a.Workspace.ID, Name: name, Email: email, Role: role, CreatedAt: s.stamp()}
	token, hash := domain.NewToken()
	if err := s.db.Update(ctx, func(tx *store.Tx) error { return tx.InsertMember(ctx, m, hash) }); err != nil {
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
	return s.db.Update(ctx, func(tx *store.Tx) error {
		m, err := tx.Member(ctx, a.Workspace.ID, memberID)
		if err != nil {
			return err
		}
		if m.Role == domain.RoleOwner {
			return fmt.Errorf("%w: the owner cannot be removed from their workspace", domain.ErrConflict)
		}
		return tx.DeleteMember(ctx, a.Workspace.ID, memberID)
	})
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
	err := s.db.Update(ctx, func(tx *store.Tx) (err error) {
		if m, err = tx.Member(ctx, a.Workspace.ID, memberID); err != nil {
			return err
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
	err := s.db.View(ctx, func(tx *store.Tx) (err error) { out, err = tx.Projects(ctx, a.Workspace.ID, only); return })
	return out, err
}

// visibleProject loads a project the actor may see, or reports it as not found.
func (s *Service) visibleProject(ctx context.Context, tx *store.Tx, a Actor, id string) (domain.Project, error) {
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
	err := s.db.View(ctx, func(tx *store.Tx) (err error) { p, err = s.visibleProject(ctx, tx, a, id); return })
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
	err = s.db.Update(ctx, func(tx *store.Tx) error {
		if err := tx.InsertProject(ctx, p); err != nil {
			return err
		}
		return tx.AddProjectMember(ctx, a.Workspace.ID, domain.ProjectMember{ProjectID: p.ID, MemberID: a.Member.ID, AddedBy: a.Member.ID, AddedAt: now})
	})
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
	err := s.db.Update(ctx, func(tx *store.Tx) (err error) {
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
		return tx.UpdateProject(ctx, p)
	})
	return p, err
}

// ---- project membership ----

// ListProjectMembers lists the members on a project the actor can see, as members.
func (s *Service) ListProjectMembers(ctx context.Context, a Actor, projectID string) ([]domain.Member, error) {
	var out []domain.Member
	err := s.db.View(ctx, func(tx *store.Tx) error {
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

// AddProjectMember puts a member of the workspace on a project. Repeating it does nothing.
func (s *Service) AddProjectMember(ctx context.Context, a Actor, projectID, memberID string) error {
	return s.db.Update(ctx, func(tx *store.Tx) error {
		if _, err := s.visibleProject(ctx, tx, a, projectID); err != nil {
			return err
		}
		if err := a.require(domain.PermProjectMembersManage, "change who is on a project"); err != nil {
			return err
		}
		if _, err := tx.Member(ctx, a.Workspace.ID, memberID); err != nil {
			return err
		}
		return tx.AddProjectMember(ctx, a.Workspace.ID, domain.ProjectMember{ProjectID: projectID, MemberID: memberID, AddedBy: a.Member.ID, AddedAt: s.stamp()})
	})
}

// RemoveProjectMember takes a member off a project. The owner has a place on
// every project they create, but nothing forces them to keep it.
func (s *Service) RemoveProjectMember(ctx context.Context, a Actor, projectID, memberID string) error {
	return s.db.Update(ctx, func(tx *store.Tx) error {
		if _, err := s.visibleProject(ctx, tx, a, projectID); err != nil {
			return err
		}
		if err := a.require(domain.PermProjectMembersManage, "change who is on a project"); err != nil {
			return err
		}
		removed, err := tx.RemoveProjectMember(ctx, a.Workspace.ID, projectID, memberID)
		if err != nil {
			return err
		}
		if !removed {
			return fmt.Errorf("%w: that member is not on this project", domain.ErrNotFound)
		}
		return nil
	})
}
