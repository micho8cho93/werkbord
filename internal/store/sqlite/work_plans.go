package sqlite

import (
	"context"
	"encoding/json"
	"werkbord/internal/domain"
)

type workPlanRepo struct{ q queryer }

func (r workPlanRepo) List(ctx context.Context, project string) ([]domain.WorkPlan, error) {
	rows, err := r.q.QueryContext(ctx, "SELECT document FROM work_plans WHERE project_id=? ORDER BY id", project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.WorkPlan{}
	for rows.Next() {
		var raw string
		var p domain.WorkPlan
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (r workPlanRepo) Save(ctx context.Context, p domain.WorkPlan, expected int64) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if expected == 0 {
		_, err = r.q.ExecContext(ctx, "INSERT INTO work_plans(id,project_id,version,document) VALUES(?,?,?,?)", p.ID, p.ProjectID, p.Version, string(raw))
		return err
	}
	result, err := r.q.ExecContext(ctx, "UPDATE work_plans SET version=?,document=? WHERE id=? AND project_id=? AND version=?", p.Version, string(raw), p.ID, p.ProjectID, expected)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return domain.ErrConflict
	}
	return err
}
