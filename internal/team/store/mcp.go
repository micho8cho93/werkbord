package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"devboard/internal/team/domain"
)

const mcpGrantCols = `id, workspace_id, member_id, name, projects_json, operations_json, token_hash, created_at, revoked_at`

func scanMCPGrant(row interface{ Scan(...any) error }) (domain.MCPGrant, error) {
	var g domain.MCPGrant
	var projects, operations string
	var created int64
	var revoked sql.NullInt64
	if err := row.Scan(&g.ID, &g.WorkspaceID, &g.MemberID, &g.Name, &projects, &operations, &g.TokenHash, &created, &revoked); err != nil {
		return g, notFound(err, "MCP grant")
	}
	if err := json.Unmarshal([]byte(projects), &g.Projects); err != nil {
		return g, err
	}
	if err := json.Unmarshal([]byte(operations), &g.Operations); err != nil {
		return g, err
	}
	g.CreatedAt = fromMS(created)
	if revoked.Valid {
		at := fromMS(revoked.Int64)
		g.RevokedAt = &at
	}
	return g, nil
}

func (t *sqlTx) InsertMCPGrant(ctx context.Context, g domain.MCPGrant) error {
	projects, err := json.Marshal(g.Projects)
	if err != nil {
		return err
	}
	operations, err := json.Marshal(g.Operations)
	if err != nil {
		return err
	}
	_, err = t.q.ExecContext(ctx, `INSERT INTO mcp_grants (`+mcpGrantCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		g.ID, g.WorkspaceID, g.MemberID, g.Name, string(projects), string(operations), g.TokenHash, ms(g.CreatedAt))
	if isUnique(err) {
		return domain.ErrConflict
	}
	if containsFK(err) {
		return domain.ErrNotFound
	}
	return err
}

func (t *sqlTx) MCPGrantByTokenHash(ctx context.Context, tokenHash string) (domain.MCPGrant, error) {
	g, err := scanMCPGrant(t.q.QueryRowContext(ctx, `SELECT `+mcpGrantCols+` FROM mcp_grants WHERE token_hash = ?`, tokenHash))
	if errors.Is(err, domain.ErrNotFound) {
		return domain.MCPGrant{}, domain.ErrNotFound
	}
	return g, err
}

func (t *sqlTx) MCPGrant(ctx context.Context, workspaceID, memberID, id string) (domain.MCPGrant, error) {
	return scanMCPGrant(t.q.QueryRowContext(ctx, `SELECT `+mcpGrantCols+` FROM mcp_grants WHERE workspace_id = ? AND member_id = ? AND id = ?`, workspaceID, memberID, id))
}

func (t *sqlTx) MCPGrants(ctx context.Context, workspaceID, memberID string) ([]domain.MCPGrant, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT `+mcpGrantCols+` FROM mcp_grants WHERE workspace_id = ? AND member_id = ? ORDER BY created_at DESC, id`, workspaceID, memberID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.MCPGrant
	for rows.Next() {
		grant, err := scanMCPGrant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, grant)
	}
	return out, rows.Err()
}

func (t *sqlTx) RevokeMCPGrant(ctx context.Context, workspaceID, memberID, id string, at time.Time) (bool, error) {
	res, err := t.q.ExecContext(ctx, `UPDATE mcp_grants SET revoked_at = ? WHERE workspace_id = ? AND member_id = ? AND id = ? AND revoked_at IS NULL`, ms(at), workspaceID, memberID, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
