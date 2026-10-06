package store

import (
	"context"
	"strings"

	"devboard/internal/team/domain"
)

// Queries that span a whole workspace: what a member's "My Work", "Reviews" and
// the workspace overview are built from, and the cursor that keeps clients in step.

// WorkspaceRevision returns the revision of a workspace. It moves, inside the
// writing transaction (see migration 0003), whenever anything in it changes.
func (t *sqlTx) WorkspaceRevision(ctx context.Context, workspaceID string) (int64, error) {
	var rev int64
	err := t.q.QueryRowContext(ctx, `SELECT revision FROM workspaces WHERE id = ?`, workspaceID).Scan(&rev)
	return rev, notFound(err, "workspace")
}

// ProjectRoles returns a member's role on each project they are on.
func (t *sqlTx) ProjectRoles(ctx context.Context, workspaceID, memberID string) (map[string]domain.ProjectRole, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT project_id, role FROM project_members WHERE workspace_id = ? AND member_id = ?`, workspaceID, memberID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]domain.ProjectRole{}
	for rows.Next() {
		var id, role string
		if err := rows.Scan(&id, &role); err != nil {
			return nil, err
		}
		out[id] = domain.ProjectRole(role)
	}
	return out, rows.Err()
}

// TicketCounts counts a workspace's tickets by project and status.
func (t *sqlTx) TicketCounts(ctx context.Context, workspaceID string) (map[string]map[domain.TicketStatus]int, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT project_id, status, COUNT(*) FROM tickets WHERE workspace_id = ? AND archived_at IS NULL GROUP BY project_id, status`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[domain.TicketStatus]int{}
	for rows.Next() {
		var pid, st string
		var n int
		if err := rows.Scan(&pid, &st, &n); err != nil {
			return nil, err
		}
		if out[pid] == nil {
			out[pid] = map[domain.TicketStatus]int{}
		}
		out[pid][domain.TicketStatus(st)] = n
	}
	return out, rows.Err()
}

// HeldTickets lists the tickets of a workspace that someone is working on or that
// wait for a review (in progress or in review), across all its projects; commits
// are not loaded. Finished and unclaimed tickets are never what a member's work
// views are about, and leaving them out keeps these views small as a board grows.
func (t *sqlTx) HeldTickets(ctx context.Context, workspaceID string) ([]domain.Ticket, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT `+ticketCols+` FROM tickets WHERE workspace_id = ? AND archived_at IS NULL AND status IN ('in_progress', 'review') ORDER BY updated_at DESC, number`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Ticket{}
	for rows.Next() {
		k, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// ActivityAfter lists a workspace's history after an entry id, oldest first, for
// the projects in only (nil: every project). It returns at most limit+1 entries,
// so the caller can tell whether there were more.
func (t *sqlTx) ActivityAfter(ctx context.Context, workspaceID string, after int64, only []string, limit int) ([]domain.Activity, error) {
	q := `SELECT id, project_id, ticket_id, ticket_key, actor_id, kind, detail, created_at FROM activity WHERE workspace_id = ? AND id > ?`
	args := []any{workspaceID, after}
	if only != nil {
		if len(only) == 0 {
			return []domain.Activity{}, nil
		}
		q += ` AND project_id IN (` + placeholders(len(only)) + `)`
		for _, id := range only {
			args = append(args, id)
		}
	}
	rows, err := t.q.QueryContext(ctx, q+` ORDER BY id LIMIT ?`, append(args, limit+1)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActivities(rows)
}

// LatestActivityID is the id of the newest entry in a workspace's history, or 0.
func (t *sqlTx) LatestActivityID(ctx context.Context, workspaceID string) (int64, error) {
	var id int64
	err := t.q.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM activity WHERE workspace_id = ?`, workspaceID).Scan(&id)
	return id, err
}

// LatestTicketActivity returns, for each ticket given, its newest entry among the
// kinds given. It is how "assigned to you by someone else" and "changes were
// requested" are told from a plain claim.
func (t *sqlTx) LatestTicketActivity(ctx context.Context, workspaceID string, ticketIDs []string, kinds []domain.ActivityKind) (map[string]domain.Activity, error) {
	out := map[string]domain.Activity{}
	if len(ticketIDs) == 0 || len(kinds) == 0 {
		return out, nil
	}
	args := []any{workspaceID}
	for _, id := range ticketIDs {
		args = append(args, id)
	}
	for _, k := range kinds {
		args = append(args, string(k))
	}
	rows, err := t.q.QueryContext(ctx, `SELECT id, project_id, ticket_id, ticket_key, actor_id, kind, detail, created_at FROM activity
		WHERE workspace_id = ? AND ticket_id IN (`+placeholders(len(ticketIDs))+`) AND kind IN (`+placeholders(len(kinds))+`) ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	acts, err := scanActivities(rows)
	if err != nil {
		return nil, err
	}
	for _, a := range acts { // oldest first, so the last one wins
		out[a.TicketID] = a
	}
	return out, nil
}

// TicketCommits loads the commits reported for each ticket given.
func (t *sqlTx) TicketCommits(ctx context.Context, ticketIDs []string) (map[string][]domain.Commit, error) {
	out := map[string][]domain.Commit{}
	if len(ticketIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(ticketIDs))
	for i, id := range ticketIDs {
		args[i] = id
	}
	rows, err := t.q.QueryContext(ctx, `SELECT ticket_id, sha, subject, author, committed_at FROM ticket_commits
		WHERE ticket_id IN (`+placeholders(len(ticketIDs))+`) ORDER BY committed_at, sha`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var c domain.Commit
		var at int64
		if err := rows.Scan(&id, &c.SHA, &c.Subject, &c.Author, &at); err != nil {
			return nil, err
		}
		c.CommittedAt = fromMS(at)
		out[id] = append(out[id], c)
	}
	return out, rows.Err()
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
