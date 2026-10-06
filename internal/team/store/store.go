// Package store persists Werkbord Team's data. What the rest of Team sees of it
// is two interfaces (repository.go): Store, which runs a function in a read-only or
// a read-write transaction, and Tx, the queries available inside one. The service
// layer is written against those and never against SQL, so the storage under them
// can change without the use cases changing.
//
// The implementation today is SQLite (<data dir>/team.db), in this file and its
// neighbours. Opening, migrating and backing the file up is the shared
// internal/sqlitekit, exactly as for the individual product; the schema and the
// queries here are Team's alone.
//
// Every query that reads or changes a workspace's data is scoped by workspace ID,
// so code that holds one workspace's ID cannot reach another's rows.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"devboard/internal/sqlitekit"
	"devboard/internal/team/domain"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

const (
	productName  = "werkbord-team"
	backupPrefix = "team"
)

// Migrations returns Team's embedded migrations.
func Migrations() ([]sqlitekit.Migration, error) {
	return sqlitekit.LoadMigrations(migrationFS, "migrations")
}

// DB is Team's database.
type DB struct {
	pool *sqlitekit.Pool
}

// Open opens (creating if necessary) the database at path and applies any pending migrations.
func Open(ctx context.Context, path string, log *slog.Logger) (*DB, error) {
	ms, err := Migrations()
	if err != nil {
		return nil, err
	}
	pool, err := sqlitekit.Open(ctx, path, sqlitekit.Options{Migrations: ms, Product: productName, BackupPrefix: backupPrefix, Log: log})
	if err != nil {
		return nil, err
	}
	return &DB{pool: pool}, nil
}

// SchemaVersion reports the applied migration version.
func (d *DB) SchemaVersion(ctx context.Context) (int, error) { return d.pool.SchemaVersion(ctx) }

// Ping checks that the database answers.
func (d *DB) Ping(ctx context.Context) error { return d.pool.Ping(ctx) }

// Close closes the database.
func (d *DB) Close() error { return d.pool.Close() }

var _ Store = (*DB)(nil)

// View runs fn against a consistent read-only snapshot.
func (d *DB) View(ctx context.Context, fn func(Tx) error) error {
	return d.pool.View(ctx, func(tx *sql.Tx) error { return fn(&sqlTx{q: tx}) })
}

// Update runs fn in the one read-write transaction at a time; it commits if fn returns nil.
func (d *DB) Update(ctx context.Context, fn func(Tx) error) error {
	return d.pool.Update(ctx, func(tx *sql.Tx) error { return fn(&sqlTx{q: tx}) })
}

type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// sqlTx is a SQLite transaction's queries: the implementation of Tx.
type sqlTx struct{ q queryer }

var _ Tx = (*sqlTx)(nil)

func ms(t time.Time) int64     { return t.UTC().UnixMilli() }
func fromMS(v int64) time.Time { return time.UnixMilli(v).UTC() }
func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
func notFound(err error, what string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", domain.ErrNotFound, what)
	}
	return err
}

// ---- workspaces ----

// InsertWorkspace stores a new workspace.
func (t *sqlTx) InsertWorkspace(ctx context.Context, w domain.Workspace) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO workspaces (id, name, created_at) VALUES (?, ?, ?)`, w.ID, w.Name, ms(w.CreatedAt))
	return err
}

// Workspace returns a workspace.
func (t *sqlTx) Workspace(ctx context.Context, id string) (domain.Workspace, error) {
	var w domain.Workspace
	var created int64
	err := t.q.QueryRowContext(ctx, `SELECT id, name, created_at FROM workspaces WHERE id = ?`, id).Scan(&w.ID, &w.Name, &created)
	w.CreatedAt = fromMS(created)
	return w, notFound(err, "workspace")
}

// RenameWorkspace changes a workspace's name.
func (t *sqlTx) RenameWorkspace(ctx context.Context, id, name string) error {
	res, err := t.q.ExecContext(ctx, `UPDATE workspaces SET name = ? WHERE id = ?`, name, id)
	return affected(res, err, "workspace")
}

func affected(res sql.Result, err error, what string) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: %s", domain.ErrNotFound, what)
	}
	return nil
}

// ---- members ----

const memberCols = `id, workspace_id, name, email, role, created_at`

func scanMember(s interface{ Scan(...any) error }) (domain.Member, error) {
	var m domain.Member
	var role string
	var created int64
	err := s.Scan(&m.ID, &m.WorkspaceID, &m.Name, &m.Email, &role, &created)
	m.Role, m.CreatedAt = domain.Role(role), fromMS(created)
	return m, err
}

// InsertMember stores a new member with the hash of their token.
func (t *sqlTx) InsertMember(ctx context.Context, m domain.Member, tokenHash string) error {
	_, err := t.q.ExecContext(ctx,
		`INSERT INTO members (id, workspace_id, name, email, role, token_hash, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.WorkspaceID, m.Name, m.Email, string(m.Role), tokenHash, ms(m.CreatedAt))
	if isUnique(err) {
		return fmt.Errorf("%w: a member named %q (or an owner) already exists in this workspace", domain.ErrConflict, m.Name)
	}
	return err
}

// Member returns a member of a workspace.
func (t *sqlTx) Member(ctx context.Context, workspaceID, id string) (domain.Member, error) {
	m, err := scanMember(t.q.QueryRowContext(ctx, `SELECT `+memberCols+` FROM members WHERE workspace_id = ? AND id = ?`, workspaceID, id))
	return m, notFound(err, "member")
}

// MemberByTokenHash finds the member a token belongs to.
func (t *sqlTx) MemberByTokenHash(ctx context.Context, hash string) (domain.Member, error) {
	m, err := scanMember(t.q.QueryRowContext(ctx, `SELECT `+memberCols+` FROM members WHERE token_hash = ?`, hash))
	return m, notFound(err, "member")
}

// Members lists a workspace's members, owner first, then by name.
func (t *sqlTx) Members(ctx context.Context, workspaceID string) ([]domain.Member, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT `+memberCols+` FROM members WHERE workspace_id = ?
		ORDER BY (role = 'owner') DESC, name COLLATE NOCASE, id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Member{}
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeleteMember removes a member and, with them, their place on every project.
func (t *sqlTx) DeleteMember(ctx context.Context, workspaceID, id string) error {
	res, err := t.q.ExecContext(ctx, `DELETE FROM members WHERE workspace_id = ? AND id = ?`, workspaceID, id)
	return affected(res, err, "member")
}

// SetMemberToken replaces a member's token hash, which signs out whoever held the old token.
func (t *sqlTx) SetMemberToken(ctx context.Context, workspaceID, id, hash string) error {
	res, err := t.q.ExecContext(ctx, `UPDATE members SET token_hash = ? WHERE workspace_id = ? AND id = ?`, hash, workspaceID, id)
	return affected(res, err, "member")
}

// ---- projects ----

const projectCols = `id, workspace_id, name, description, repository, archived, created_at, updated_at, revision`

func scanProject(s interface{ Scan(...any) error }) (domain.Project, error) {
	var p domain.Project
	var archived int
	var created, updated int64
	err := s.Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.Description, &p.Repository, &archived, &created, &updated, &p.Revision)
	p.Archived, p.CreatedAt, p.UpdatedAt = archived == 1, fromMS(created), fromMS(updated)
	return p, err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// InsertProject stores a new project.
func (t *sqlTx) InsertProject(ctx context.Context, p domain.Project) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO projects (`+projectCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.WorkspaceID, p.Name, p.Description, p.Repository, b2i(p.Archived), ms(p.CreatedAt), ms(p.UpdatedAt), p.Revision)
	if isUnique(err) {
		return fmt.Errorf("%w: a project named %q already exists in this workspace", domain.ErrConflict, p.Name)
	}
	return err
}

// Project returns a project of a workspace.
func (t *sqlTx) Project(ctx context.Context, workspaceID, id string) (domain.Project, error) {
	p, err := scanProject(t.q.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE workspace_id = ? AND id = ?`, workspaceID, id))
	return p, notFound(err, "project")
}

// Projects lists a workspace's projects by name. If onlyMemberID is not empty,
// only the projects that member is on.
func (t *sqlTx) Projects(ctx context.Context, workspaceID, onlyMemberID string) ([]domain.Project, error) {
	q := `SELECT ` + projectCols + ` FROM projects WHERE workspace_id = ?`
	args := []any{workspaceID}
	if onlyMemberID != "" {
		q += ` AND id IN (SELECT project_id FROM project_members WHERE member_id = ? AND workspace_id = ?)`
		args = append(args, onlyMemberID, workspaceID)
	}
	rows, err := t.q.QueryContext(ctx, q+` ORDER BY archived, name COLLATE NOCASE, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateProject writes a project's editable fields.
func (t *sqlTx) UpdateProject(ctx context.Context, p domain.Project) error {
	res, err := t.q.ExecContext(ctx,
		`UPDATE projects SET name = ?, description = ?, repository = ?, archived = ?, updated_at = ? WHERE workspace_id = ? AND id = ?`,
		p.Name, p.Description, p.Repository, b2i(p.Archived), ms(p.UpdatedAt), p.WorkspaceID, p.ID)
	if isUnique(err) {
		return fmt.Errorf("%w: a project named %q already exists in this workspace", domain.ErrConflict, p.Name)
	}
	return affected(res, err, "project")
}

// ---- project membership ----

// AddProjectMember puts a member on a project. Adding someone already on it is a
// no-op, so the call can be repeated safely.
func (t *sqlTx) AddProjectMember(ctx context.Context, workspaceID string, pm domain.ProjectMember) error {
	_, err := t.q.ExecContext(ctx,
		`INSERT INTO project_members (project_id, member_id, workspace_id, added_by, added_at, role) VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (project_id, member_id) DO NOTHING`,
		pm.ProjectID, pm.MemberID, workspaceID, pm.AddedBy, ms(pm.AddedAt), string(roleOrDefault(pm.Role)))
	return err
}

func roleOrDefault(r domain.ProjectRole) domain.ProjectRole {
	if r == "" {
		return domain.ProjectContributor
	}
	return r
}

// SetProjectMemberRole changes a member's role on a project.
func (t *sqlTx) SetProjectMemberRole(ctx context.Context, workspaceID, projectID, memberID string, role domain.ProjectRole) error {
	res, err := t.q.ExecContext(ctx, `UPDATE project_members SET role = ? WHERE workspace_id = ? AND project_id = ? AND member_id = ?`,
		string(role), workspaceID, projectID, memberID)
	return affected(res, err, "project member")
}

// ProjectRole returns a member's role on a project and whether they are on it.
func (t *sqlTx) ProjectRole(ctx context.Context, workspaceID, projectID, memberID string) (domain.ProjectRole, bool, error) {
	var role string
	err := t.q.QueryRowContext(ctx, `SELECT role FROM project_members WHERE workspace_id = ? AND project_id = ? AND member_id = ?`,
		workspaceID, projectID, memberID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return domain.ProjectRole(role), err == nil, err
}

// RemoveProjectMember takes a member off a project; it reports whether they were on it.
func (t *sqlTx) RemoveProjectMember(ctx context.Context, workspaceID, projectID, memberID string) (bool, error) {
	res, err := t.q.ExecContext(ctx, `DELETE FROM project_members WHERE workspace_id = ? AND project_id = ? AND member_id = ?`, workspaceID, projectID, memberID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// IsProjectMember reports whether a member is on a project.
func (t *sqlTx) IsProjectMember(ctx context.Context, workspaceID, projectID, memberID string) (bool, error) {
	var n int
	err := t.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_members WHERE workspace_id = ? AND project_id = ? AND member_id = ?`,
		workspaceID, projectID, memberID).Scan(&n)
	return n > 0, err
}

// ProjectMembers lists who is on a project, owner first and then by name.
func (t *sqlTx) ProjectMembers(ctx context.Context, workspaceID, projectID string) ([]domain.ProjectMember, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT pm.project_id, pm.member_id, pm.role, pm.added_by, pm.added_at
		FROM project_members pm JOIN members m ON m.id = pm.member_id AND m.workspace_id = pm.workspace_id
		WHERE pm.workspace_id = ? AND pm.project_id = ?
		ORDER BY (m.role = 'owner') DESC, m.name COLLATE NOCASE, m.id`, workspaceID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ProjectMember{}
	for rows.Next() {
		var pm domain.ProjectMember
		var added int64
		var role string
		if err := rows.Scan(&pm.ProjectID, &pm.MemberID, &role, &pm.AddedBy, &added); err != nil {
			return nil, err
		}
		pm.Role, pm.AddedAt = domain.ProjectRole(role), fromMS(added)
		out = append(out, pm)
	}
	return out, rows.Err()
}
