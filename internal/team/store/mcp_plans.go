package store

import (
	"context"
	"database/sql"
	"fmt"

	"devboard/internal/team/domain"
)

const mcpTaskPlanColumns = `id, workspace_id, member_id, grant_id, project_id, idempotency_key, request_digest,
	plan_digest, plan_json, state, result_json, created_at, updated_at, resolved_at`

func scanMCPTaskPlan(row interface{ Scan(...any) error }) (domain.MCPTaskPlanProposal, error) {
	var p domain.MCPTaskPlanProposal
	var created, updated int64
	var resolved sql.NullInt64
	err := row.Scan(&p.ID, &p.WorkspaceID, &p.MemberID, &p.GrantID, &p.ProjectID, &p.IdempotencyKey,
		&p.RequestDigest, &p.PlanDigest, &p.PlanJSON, &p.State, &p.ResultJSON, &created, &updated, &resolved)
	if err != nil {
		return p, err
	}
	p.CreatedAt, p.UpdatedAt = fromMS(created), fromMS(updated)
	p.ResolvedAt = optMS(resolved)
	return p, nil
}

func (t *sqlTx) InsertMCPTaskPlan(ctx context.Context, p domain.MCPTaskPlanProposal) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO mcp_task_plan_proposals (`+mcpTaskPlanColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.WorkspaceID, p.MemberID, p.GrantID, p.ProjectID, p.IdempotencyKey, p.RequestDigest, p.PlanDigest,
		p.PlanJSON, p.State, p.ResultJSON, ms(p.CreatedAt), ms(p.UpdatedAt), nullMS(p.ResolvedAt))
	if isUnique(err) {
		return fmt.Errorf("%w: MCP task plan already exists", domain.ErrConflict)
	}
	return err
}

func (t *sqlTx) MCPTaskPlan(ctx context.Context, workspaceID, id string) (domain.MCPTaskPlanProposal, error) {
	p, err := scanMCPTaskPlan(t.q.QueryRowContext(ctx, `SELECT `+mcpTaskPlanColumns+` FROM mcp_task_plan_proposals WHERE workspace_id = ? AND id = ?`, workspaceID, id))
	if err != nil {
		return p, notFound(err, "MCP task plan")
	}
	return p, nil
}

func (t *sqlTx) MCPTaskPlanByKey(ctx context.Context, workspaceID, grantID, projectID, idempotencyKey string) (domain.MCPTaskPlanProposal, error) {
	p, err := scanMCPTaskPlan(t.q.QueryRowContext(ctx, `SELECT `+mcpTaskPlanColumns+` FROM mcp_task_plan_proposals WHERE workspace_id = ? AND grant_id = ? AND project_id = ? AND idempotency_key = ?`, workspaceID, grantID, projectID, idempotencyKey))
	if err != nil {
		return p, notFound(err, "MCP task plan")
	}
	return p, nil
}

func (t *sqlTx) MCPTaskPlans(ctx context.Context, workspaceID, grantID string, pendingOnly bool) ([]domain.MCPTaskPlanProposal, error) {
	query := `SELECT ` + mcpTaskPlanColumns + ` FROM mcp_task_plan_proposals WHERE workspace_id = ?`
	args := []any{workspaceID}
	if grantID != "" {
		query += ` AND grant_id = ?`
		args = append(args, grantID)
	}
	if pendingOnly {
		query += ` AND state = 'pending'`
	}
	query += ` ORDER BY created_at DESC, id`
	rows, err := t.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.MCPTaskPlanProposal{}
	for rows.Next() {
		p, err := scanMCPTaskPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (t *sqlTx) SaveMCPTaskPlan(ctx context.Context, p domain.MCPTaskPlanProposal, pendingOnly bool) error {
	query := `UPDATE mcp_task_plan_proposals SET plan_digest = ?, plan_json = ?, state = ?, result_json = ?, updated_at = ?, resolved_at = ? WHERE workspace_id = ? AND id = ?`
	if pendingOnly {
		query += ` AND state = 'pending'`
	}
	res, err := t.q.ExecContext(ctx, query, p.PlanDigest, p.PlanJSON, p.State, p.ResultJSON, ms(p.UpdatedAt), nullMS(p.ResolvedAt), p.WorkspaceID, p.ID)
	if err != nil {
		if isUnique(err) {
			return fmt.Errorf("%w: MCP task plan conflicts with an existing record", domain.ErrConflict)
		}
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return domain.ErrConflict
	}
	return nil
}
