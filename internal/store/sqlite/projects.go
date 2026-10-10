package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"devboard/internal/domain"
)

type projectRepo struct{ q queryer }

const projectCols = `id, name, repo_path, created_at, updated_at, execution, kind`

// workPathPrefix starts the placeholder a work project keeps in repo_path (see migration 0013).
const workPathPrefix = "work:"

func scanProject(s interface{ Scan(...any) error }) (*domain.Project, error) {
	var p domain.Project
	var created, updated int64
	var execution string
	if err := s.Scan(&p.ID, &p.Name, &p.RepoPath, &created, &updated, &execution, &p.Kind); err != nil {
		return nil, err
	}
	if p.Kind == domain.ProjectWork {
		p.RepoPath = "" // the stored placeholder is not a path
	}
	p.CreatedAt, p.UpdatedAt = fromMS(created), fromMS(updated)
	var err error
	if p.Execution, err = decodeExecution(execution); err != nil {
		return nil, fmt.Errorf("project %s: %w", p.ID, err)
	}
	return &p, nil
}

func (r projectRepo) Create(ctx context.Context, p *domain.Project) error {
	execution, err := encodeExecution(p.Execution)
	if err != nil {
		return err
	}
	kind, path := p.Kind, p.RepoPath
	if kind == "" {
		kind = domain.ProjectRepository
	}
	if !kind.Valid() {
		return fmt.Errorf("%w: unknown project kind %q", domain.ErrInvalid, kind)
	}
	p.Kind = kind
	if kind == domain.ProjectWork {
		if path != "" {
			return fmt.Errorf("%w: a work project has no repository path", domain.ErrInvalid)
		}
		path = workPathPrefix + p.ID
	}
	_, err = r.q.ExecContext(ctx,
		`INSERT INTO projects (`+projectCols+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, path, ms(p.CreatedAt), ms(p.UpdatedAt), execution, kind)
	if isUniqueViolation(err) {
		return fmt.Errorf("project with path %s: %w", p.RepoPath, domain.ErrDuplicate)
	}
	return err
}

func (r projectRepo) Get(ctx context.Context, id string) (*domain.Project, error) {
	p, err := scanProject(r.q.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE id = ?`, id))
	return p, notFound(err, "project", id)
}

func (r projectRepo) GetByPath(ctx context.Context, repoPath string) (*domain.Project, error) {
	p, err := scanProject(r.q.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE repo_path = ?`, repoPath))
	return p, notFound(err, "project with path", repoPath)
}

func (r projectRepo) List(ctx context.Context) ([]domain.Project, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT `+projectCols+` FROM projects ORDER BY name COLLATE NOCASE, created_at`)
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
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (r projectRepo) SetExecution(ctx context.Context, id string, cfg domain.ExecutionConfig, at time.Time) error {
	execution, err := encodeExecution(cfg)
	if err != nil {
		return err
	}
	res, err := r.q.ExecContext(ctx, `UPDATE projects SET execution = ?, updated_at = ? WHERE id = ?`, execution, ms(at), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("project %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

type gitRepoRepo struct{ q queryer }

const gitRepoCols = `project_id, root_path, common_dir, current_branch, head_commit, default_branch, remotes, inspected_at`

func (r gitRepoRepo) Upsert(ctx context.Context, g *domain.GitRepository) error {
	remotes := g.Remotes
	if remotes == nil {
		remotes = []domain.GitRemote{}
	}
	rj, err := toJSON(remotes)
	if err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx, `
		INSERT INTO git_repositories (`+gitRepoCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (project_id) DO UPDATE SET
			root_path = excluded.root_path,
			common_dir = excluded.common_dir,
			current_branch = excluded.current_branch,
			head_commit = excluded.head_commit,
			default_branch = excluded.default_branch,
			remotes = excluded.remotes,
			inspected_at = excluded.inspected_at`,
		g.ProjectID, g.RootPath, g.CommonDir, g.CurrentBranch, g.HeadCommit, g.DefaultBranch, rj, ms(g.InspectedAt))
	switch {
	case isUniqueViolation(err):
		return fmt.Errorf("repository %s is already registered by another project: %w", g.CommonDir, domain.ErrDuplicate)
	case isFKViolation(err):
		return fmt.Errorf("project %s: %w", g.ProjectID, domain.ErrNotFound)
	}
	return err
}

func scanGitRepo(s interface{ Scan(...any) error }) (*domain.GitRepository, error) {
	var g domain.GitRepository
	var remotes string
	var inspected int64
	if err := s.Scan(&g.ProjectID, &g.RootPath, &g.CommonDir, &g.CurrentBranch, &g.HeadCommit, &g.DefaultBranch, &remotes, &inspected); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(remotes), &g.Remotes); err != nil {
		return nil, fmt.Errorf("decode remotes for project %s: %w", g.ProjectID, err)
	}
	g.InspectedAt = fromMS(inspected)
	return &g, nil
}

func (r gitRepoRepo) Get(ctx context.Context, projectID string) (*domain.GitRepository, error) {
	g, err := scanGitRepo(r.q.QueryRowContext(ctx, `SELECT `+gitRepoCols+` FROM git_repositories WHERE project_id = ?`, projectID))
	return g, notFound(err, "repository for project", projectID)
}

func (r gitRepoRepo) GetByCommonDir(ctx context.Context, commonDir string) (*domain.GitRepository, error) {
	if commonDir == "" {
		return nil, fmt.Errorf("repository with empty common dir: %w", domain.ErrNotFound) // '' means unknown, never a match
	}
	g, err := scanGitRepo(r.q.QueryRowContext(ctx, `SELECT `+gitRepoCols+` FROM git_repositories WHERE common_dir = ?`, commonDir))
	return g, notFound(err, "repository with common dir", commonDir)
}
