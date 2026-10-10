package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"devboard/internal/domain"
)

type taskRepo struct{ q queryer }

func archiveMS(at *time.Time) any {
	if at == nil {
		return nil
	}
	return ms(*at)
}

const taskCols = `id, project_id, title, description, state, position, version, created_at, updated_at, execution, orchestration, source_ref, work_branch, base_branch, archived_at, work_mode, plan_start, plan_end, milestone`

func scanTask(s interface{ Scan(...any) error }) (*domain.Task, error) {
	var t domain.Task
	var created, updated int64
	var execution, orchestration string
	var archived sql.NullInt64
	var milestone int
	if err := s.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Description, &t.State, &t.Position, &t.Version, &created, &updated, &execution, &orchestration, &t.SourceRef, &t.WorkBranch, &t.BaseBranch, &archived, &t.WorkMode, &t.Plan.Start, &t.Plan.End, &milestone); err != nil {
		return nil, err
	}
	t.Plan.Milestone = milestone == 1
	t.LabelIDs = []string{}
	t.CreatedAt, t.UpdatedAt = fromMS(created), fromMS(updated)
	if archived.Valid {
		at := fromMS(archived.Int64)
		t.ArchivedAt = &at
	}
	var err error
	if t.Execution, err = decodeExecution(execution); err != nil {
		return nil, fmt.Errorf("task %s: %w", t.ID, err)
	}
	if err := json.Unmarshal([]byte(orchestration), &t.Orchestration); err != nil {
		return nil, err
	}
	t.Orchestration.Normalize()
	return &t, nil
}

func (r taskRepo) Create(ctx context.Context, t *domain.Task) error {
	if t.Version == 0 {
		t.Version = 1
	}
	if t.LabelIDs == nil {
		t.LabelIDs = []string{} // sent as [], never null
	}
	execution, err := encodeExecution(t.Execution)
	if err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx,
		`INSERT INTO tasks (`+taskCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.ProjectID, t.Title, t.Description, t.State, t.Position, t.Version, ms(t.CreatedAt), ms(t.UpdatedAt), execution, mustJSON(t.Orchestration), t.SourceRef, t.WorkBranch, t.BaseBranch, archiveMS(t.ArchivedAt),
		t.Mode(), t.Plan.Start, t.Plan.End, boolInt(t.Plan.Milestone))
	if isFKViolation(err) {
		return fmt.Errorf("project %s: %w", t.ProjectID, domain.ErrNotFound)
	}
	if err != nil {
		return err
	}
	return r.setLabels(ctx, t.ID, t.LabelIDs)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// setLabels replaces a task's labels, keeping the order given, and leaves the rows alone when they
// already say the same: most updates (a card moved, a run started) do not touch labels. A label that
// does not exist is ErrNotFound; the service checks first so that the message can name it.
func (r taskRepo) setLabels(ctx context.Context, taskID string, ids []string) error {
	current := []domain.Task{{ID: taskID, LabelIDs: []string{}}}
	if err := r.withLabels(ctx, current, `t.id = ?`, taskID); err != nil {
		return err
	}
	if len(current[0].LabelIDs) == len(ids) {
		same := true
		for i := range ids {
			same = same && current[0].LabelIDs[i] == ids[i]
		}
		if same {
			return nil
		}
	}
	if _, err := r.q.ExecContext(ctx, `DELETE FROM task_labels WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := r.q.ExecContext(ctx, `INSERT INTO task_labels (task_id, label_id) VALUES (?, ?)`, taskID, id); err != nil {
			if isFKViolation(err) {
				return fmt.Errorf("label %s: %w", id, domain.ErrNotFound)
			}
			return err
		}
	}
	return nil
}

// withLabels fills in the labels of tasks read from one query: one more query for all of them.
func (r taskRepo) withLabels(ctx context.Context, tasks []domain.Task, where string, args ...any) error {
	if len(tasks) == 0 {
		return nil
	}
	rows, err := r.q.QueryContext(ctx, `SELECT tl.task_id, tl.label_id FROM task_labels tl JOIN tasks t ON t.id = tl.task_id WHERE `+where+` ORDER BY tl.rowid`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	byTask := map[string][]string{}
	for rows.Next() {
		var task, label string
		if err := rows.Scan(&task, &label); err != nil {
			return err
		}
		byTask[task] = append(byTask[task], label)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range tasks {
		if ids := byTask[tasks[i].ID]; ids != nil {
			tasks[i].LabelIDs = ids
		}
	}
	return nil
}

func (r taskRepo) Get(ctx context.Context, id string) (*domain.Task, error) {
	t, err := scanTask(r.q.QueryRowContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE id = ?`, id))
	if err != nil {
		return nil, notFound(err, "task", id)
	}
	one := []domain.Task{*t}
	if err := r.withLabels(ctx, one, `t.id = ?`, id); err != nil {
		return nil, err
	}
	return &one[0], nil
}

func (r taskRepo) ListByProject(ctx context.Context, projectID string) ([]domain.Task, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+taskCols+` FROM tasks WHERE project_id = ? ORDER BY state, position, created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.withLabels(ctx, out, `t.project_id = ?`, projectID); err != nil {
		return nil, err
	}
	return out, nil
}

func (r taskRepo) Update(ctx context.Context, t *domain.Task) error {
	if t.LabelIDs == nil {
		t.LabelIDs = []string{}
	}
	execution, err := encodeExecution(t.Execution)
	if err != nil {
		return err
	}
	res, err := r.q.ExecContext(ctx, `
		UPDATE tasks SET title = ?, description = ?, state = ?, position = ?, execution = ?, orchestration = ?, updated_at = ?, archived_at = ?,
			work_mode = ?, plan_start = ?, plan_end = ?, milestone = ?, version = version + 1
		WHERE id = ? AND version = ?`,
		t.Title, t.Description, t.State, t.Position, execution, mustJSON(t.Orchestration), ms(t.UpdatedAt), archiveMS(t.ArchivedAt),
		t.Mode(), t.Plan.Start, t.Plan.End, boolInt(t.Plan.Milestone), t.ID, t.Version)
	if err != nil {
		return err
	}
	if err := checkCAS(ctx, res, r.q, "tasks", t.ID, t.Version); err != nil {
		return err
	}
	if err := r.setLabels(ctx, t.ID, t.LabelIDs); err != nil {
		return err
	}
	t.Version++
	return nil
}

func (r taskRepo) MaxPosition(ctx context.Context, projectID string, state domain.TaskState) (float64, error) {
	var max sql.NullFloat64
	err := r.q.QueryRowContext(ctx,
		`SELECT MAX(position) FROM tasks WHERE project_id = ? AND state = ?`, projectID, state).Scan(&max)
	return max.Float64, err
}
