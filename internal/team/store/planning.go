package store

import (
	"context"
	"fmt"
	"time"

	"devboard/internal/planning"
	"devboard/internal/team/domain"
)

const labelCols = `id, workspace_id, name, color, description, version, created_at, updated_at`

func scanLabel(s interface{ Scan(...any) error }) (domain.Label, error) {
	var l domain.Label
	var created, updated int64
	err := s.Scan(&l.ID, &l.WorkspaceID, &l.Name, &l.Color, &l.Description, &l.Version, &created, &updated)
	l.CreatedAt, l.UpdatedAt = fromMS(created), fromMS(updated)
	return l, err
}

func (t *sqlTx) InsertLabel(ctx context.Context, workspaceID string, l domain.Label) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO labels (id, workspace_id, name, name_key, color, description, version, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		l.ID, workspaceID, l.Name, planning.LabelKey(l.Name), l.Color, l.Description, ms(l.CreatedAt), ms(l.UpdatedAt))
	if isUnique(err) {
		return fmt.Errorf("%w: there is already a label named %q", domain.ErrConflict, l.Name)
	}
	return err
}

func (t *sqlTx) Label(ctx context.Context, workspaceID, id string) (domain.Label, error) {
	l, err := scanLabel(t.q.QueryRowContext(ctx, `SELECT `+labelCols+` FROM labels WHERE workspace_id = ? AND id = ?`, workspaceID, id))
	return l, notFound(err, "label")
}

func (t *sqlTx) Labels(ctx context.Context, workspaceID string) ([]domain.Label, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT `+labelCols+` FROM labels WHERE workspace_id = ? ORDER BY name_key, id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Label{}
	for rows.Next() {
		l, err := scanLabel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (t *sqlTx) CountLabels(ctx context.Context, workspaceID string) (int, error) {
	var n int
	err := t.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM labels WHERE workspace_id = ?`, workspaceID).Scan(&n)
	return n, err
}

func (t *sqlTx) UpdateLabel(ctx context.Context, workspaceID string, l domain.Label) (domain.Label, error) {
	res, err := t.q.ExecContext(ctx, `UPDATE labels SET name = ?, name_key = ?, color = ?, description = ?, updated_at = ?, version = version + 1
		WHERE workspace_id = ? AND id = ? AND version = ?`,
		l.Name, planning.LabelKey(l.Name), l.Color, l.Description, ms(l.UpdatedAt), workspaceID, l.ID, l.Version)
	if isUnique(err) {
		return l, fmt.Errorf("%w: there is already a label named %q", domain.ErrConflict, l.Name)
	}
	if err != nil {
		return l, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists int
		if e := t.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM labels WHERE workspace_id = ? AND id = ?`, workspaceID, l.ID).Scan(&exists); e != nil {
			return l, e
		}
		if exists == 0 {
			return l, fmt.Errorf("%w: label", domain.ErrNotFound)
		}
		return l, fmt.Errorf("%w: the label was changed by someone else; reload it and try again", domain.ErrConflict)
	}
	l.Version++
	return l, nil
}

func (t *sqlTx) DeleteLabel(ctx context.Context, workspaceID, id string, now time.Time) ([]string, error) {
	rows, err := t.q.QueryContext(ctx, `SELECT DISTINCT k.project_id FROM ticket_labels tl JOIN tickets k ON k.id = tl.ticket_id AND k.workspace_id = tl.workspace_id
		WHERE tl.workspace_id = ? AND tl.label_id = ? ORDER BY k.project_id`, workspaceID, id)
	if err != nil {
		return nil, err
	}
	projects := []string{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return nil, err
		}
		projects = append(projects, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if _, err := t.q.ExecContext(ctx, `UPDATE tickets SET version = version + 1, updated_at = ?
		WHERE workspace_id = ? AND id IN (SELECT ticket_id FROM ticket_labels WHERE workspace_id = ? AND label_id = ?)`, ms(now), workspaceID, workspaceID, id); err != nil {
		return nil, err
	}
	res, err := t.q.ExecContext(ctx, `DELETE FROM labels WHERE workspace_id = ? AND id = ?`, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, fmt.Errorf("%w: label", domain.ErrNotFound)
	}
	return projects, nil
}

func (t *sqlTx) LabelUsage(ctx context.Context, workspaceID, onlyMemberID string) (map[string]int, error) {
	q := `SELECT tl.label_id, COUNT(*) FROM ticket_labels tl JOIN tickets k ON k.id = tl.ticket_id AND k.workspace_id = tl.workspace_id
		WHERE tl.workspace_id = ? AND k.archived_at IS NULL`
	args := []any{workspaceID}
	if onlyMemberID != "" {
		q += ` AND k.project_id IN (SELECT project_id FROM project_members WHERE workspace_id = ? AND member_id = ?)`
		args = append(args, workspaceID, onlyMemberID)
	}
	rows, err := t.q.QueryContext(ctx, q+` GROUP BY tl.label_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// fillPlanning loads the labels and the dependencies of tickets read by one query: two more queries for all of
// them. where selects the same tickets (aliased t) as the query that read them.
func (t *sqlTx) fillPlanning(ctx context.Context, workspaceID string, tickets []domain.Ticket, where string, args ...any) error {
	if len(tickets) == 0 {
		return nil
	}
	for _, table := range []struct{ name, col string }{{"ticket_labels", "label_id"}, {"ticket_dependencies", "depends_on_id"}} {
		rows, err := t.q.QueryContext(ctx, `SELECT x.ticket_id, x.`+table.col+` FROM `+table.name+` x JOIN tickets t ON t.id = x.ticket_id AND t.workspace_id = x.workspace_id
			WHERE x.workspace_id = ? AND `+where+` ORDER BY x.rowid`, append([]any{workspaceID}, args...)...)
		if err != nil {
			return err
		}
		by := map[string][]string{}
		for rows.Next() {
			var ticket, other string
			if err := rows.Scan(&ticket, &other); err != nil {
				rows.Close()
				return err
			}
			by[ticket] = append(by[ticket], other)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for i := range tickets {
			if ids := by[tickets[i].ID]; ids != nil {
				if table.name == "ticket_labels" {
					tickets[i].LabelIDs = ids
				} else {
					tickets[i].Dependencies = ids
				}
			}
		}
	}
	return nil
}

func (t *sqlTx) SetTicketLabels(ctx context.Context, workspaceID, ticketID string, labelIDs []string) error {
	return t.replaceLinks(ctx, workspaceID, ticketID, "ticket_labels", "label_id", labelIDs, "label")
}

func (t *sqlTx) SetTicketDependencies(ctx context.Context, workspaceID, ticketID string, dependsOn []string) error {
	return t.replaceLinks(ctx, workspaceID, ticketID, "ticket_dependencies", "depends_on_id", dependsOn, "ticket")
}

// replaceLinks makes a ticket's links in table exactly ids, in that order, and leaves the rows alone when they
// already are: most writes of a ticket (a claim, a submit) do not touch its labels or dependencies.
func (t *sqlTx) replaceLinks(ctx context.Context, workspaceID, ticketID, table, col string, ids []string, what string) error {
	rows, err := t.q.QueryContext(ctx, `SELECT `+col+` FROM `+table+` WHERE workspace_id = ? AND ticket_id = ? ORDER BY rowid`, workspaceID, ticketID)
	if err != nil {
		return err
	}
	var have []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		have = append(have, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(have) == len(ids) {
		same := true
		for i := range ids {
			same = same && have[i] == ids[i]
		}
		if same {
			return nil
		}
	}
	if _, err := t.q.ExecContext(ctx, `DELETE FROM `+table+` WHERE workspace_id = ? AND ticket_id = ?`, workspaceID, ticketID); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := t.q.ExecContext(ctx, `INSERT INTO `+table+` (ticket_id, `+col+`, workspace_id) VALUES (?, ?, ?)`, ticketID, id, workspaceID); err != nil {
			if err != nil && (isUnique(err) || containsFK(err)) {
				return fmt.Errorf("%w: %s %s", domain.ErrNotFound, what, id)
			}
			return err
		}
	}
	return nil
}
