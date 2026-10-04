package sqlite

import (
	"context"
	"encoding/json"
	"fmt"

	"devboard/internal/domain"
)

type projectRepo struct{ q queryer }

const projectCols = `id, name, repo_path, created_at, updated_at`

func scanProject(s interface{ Scan(...any) error }) (*domain.Project, error) {
	var p domain.Project
	var created, updated int64
	if err := s.Scan(&p.ID, &p.Name, &p.RepoPath, &created, &updated); err != nil {
		return nil, err
	}
	p.CreatedAt, p.UpdatedAt = fromMS(created), fromMS(updated)
	return &p, nil
}

func (r projectRepo) Create(ctx context.Context, p *domain.Project) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO projects (`+projectCols+`) VALUES (?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.RepoPath, ms(p.CreatedAt), ms(p.UpdatedAt))
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

type gitRepoRepo struct{ q queryer }

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
		INSERT INTO git_repositories (project_id, root_path, current_branch, head_commit, default_branch, remotes, inspected_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (project_id) DO UPDATE SET
			root_path = excluded.root_path,
			current_branch = excluded.current_branch,
			head_commit = excluded.head_commit,
			default_branch = excluded.default_branch,
			remotes = excluded.remotes,
			inspected_at = excluded.inspected_at`,
		g.ProjectID, g.RootPath, g.CurrentBranch, g.HeadCommit, g.DefaultBranch, rj, ms(g.InspectedAt))
	if isFKViolation(err) {
		return fmt.Errorf("project %s: %w", g.ProjectID, domain.ErrNotFound)
	}
	return err
}

func (r gitRepoRepo) Get(ctx context.Context, projectID string) (*domain.GitRepository, error) {
	var g domain.GitRepository
	var remotes string
	var inspected int64
	err := r.q.QueryRowContext(ctx, `
		SELECT project_id, root_path, current_branch, head_commit, default_branch, remotes, inspected_at
		FROM git_repositories WHERE project_id = ?`, projectID).
		Scan(&g.ProjectID, &g.RootPath, &g.CurrentBranch, &g.HeadCommit, &g.DefaultBranch, &remotes, &inspected)
	if err != nil {
		return nil, notFound(err, "repository for project", projectID)
	}
	if err := json.Unmarshal([]byte(remotes), &g.Remotes); err != nil {
		return nil, fmt.Errorf("decode remotes for project %s: %w", projectID, err)
	}
	g.InspectedAt = fromMS(inspected)
	return &g, nil
}
