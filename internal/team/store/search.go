package store

import (
	"context"
	"database/sql"
	"strings"

	"devboard/internal/team/domain"
)

// TicketMatch carries only what the search list needs, without ticket bodies or
// reported Git data. Matching and limiting happen in storage, not in the browser.
type TicketMatch struct {
	ID, ProjectID, Title string
	Number               int
	Status               domain.TicketStatus
	Archived             bool
}

func (t *sqlTx) SearchTickets(ctx context.Context, workspaceID, onlyMemberID, query string, limit int) ([]TicketMatch, error) {
	out := []TicketMatch{}
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 || limit < 1 {
		return out, nil
	}
	if limit > 50 {
		limit = 50
	}
	clauses := []string{"k.workspace_id = ?"}
	args := []any{workspaceID}
	if onlyMemberID != "" {
		clauses = append(clauses, `EXISTS (SELECT 1 FROM project_members pm WHERE pm.project_id = k.project_id AND pm.member_id = ?)`)
		args = append(args, onlyMemberID)
	}
	for _, term := range terms {
		// instr treats %, _ and quotes literally; every term is a bound value.
		clauses = append(clauses, `instr(lower(k.title || char(10) || k.description || char(10) || k.requirements || char(10) || k.branch || char(10) || 'WB-' || k.number || char(10) || p.name), ?) > 0`)
		args = append(args, term)
	}
	args = append(args, strings.ToLower(query), strings.ToLower(query), limit)
	rows, err := t.q.QueryContext(ctx, `SELECT k.id, k.project_id, k.number, k.title, k.status, k.archived_at
		FROM tickets k JOIN projects p ON p.id = k.project_id AND p.workspace_id = k.workspace_id
		WHERE `+strings.Join(clauses, " AND ")+`
		ORDER BY CASE WHEN lower('WB-' || k.number) = ? THEN 0 WHEN lower(k.title) = ? THEN 1 ELSE 2 END,
			p.archived, k.archived_at IS NOT NULL, k.updated_at DESC, k.number DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m TicketMatch
		var archived sql.NullInt64
		if err := rows.Scan(&m.ID, &m.ProjectID, &m.Number, &m.Title, &m.Status, &archived); err != nil {
			return nil, err
		}
		m.Archived = archived.Valid
		out = append(out, m)
	}
	return out, rows.Err()
}
