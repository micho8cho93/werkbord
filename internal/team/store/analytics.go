package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"devboard/internal/team/domain"
)

type AnalyticsData struct {
	Tickets     []domain.Ticket
	Completions []domain.Activity
	Truncated   bool
}

// AnalyticsData reads only authorised projects supplied by the service, scoped
// again to the workspace. No member identities, Git reports or descriptions.
func (t *sqlTx) AnalyticsData(ctx context.Context, ws string, projects []string, start, end time.Time, limit int) (out AnalyticsData, err error) {
	if len(projects) == 0 {
		return out, nil
	}
	if limit <= 0 || limit > 20000 {
		limit = 20000
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(projects)), ",")
	args := []any{ws}
	for _, p := range projects {
		args = append(args, p)
	}
	args = append(args, ms(end), limit+1)
	rows, err := t.q.QueryContext(ctx, `SELECT id,project_id,created_at,
 (SELECT json_group_array(label_id) FROM ticket_labels WHERE ticket_id=tickets.id)
 FROM tickets WHERE workspace_id=? AND project_id IN (`+marks+`) AND created_at<? ORDER BY created_at DESC,id LIMIT ?`, args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var k domain.Ticket
		var created int64
		var labels string
		if err = rows.Scan(&k.ID, &k.ProjectID, &created, &labels); err != nil {
			break
		}
		k.CreatedAt = fromMS(created)
		if err = json.Unmarshal([]byte(labels), &k.LabelIDs); err != nil {
			break
		}
		out.Tickets = append(out.Tickets, k)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(out.Tickets) > limit {
		out.Truncated = true
		out.Tickets = out.Tickets[:limit]
	}
	args = args[:len(args)-2]
	args = append(args, ms(start), ms(end), limit+1)
	rows, err = t.q.QueryContext(ctx, `SELECT id,project_id,ticket_id,created_at FROM activity WHERE workspace_id=?
 AND project_id IN (`+marks+`) AND kind='ticket.completed' AND created_at>=? AND created_at<? ORDER BY id LIMIT ?`, args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var a domain.Activity
		var created int64
		if err = rows.Scan(&a.ID, &a.ProjectID, &a.TicketID, &created); err != nil {
			break
		}
		a.CreatedAt = fromMS(created)
		out.Completions = append(out.Completions, a)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if len(out.Completions) > limit {
		out.Truncated = true
		out.Completions = out.Completions[:limit]
	}
	return out, err
}
