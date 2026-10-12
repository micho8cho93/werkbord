package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"time"

	"devboard/internal/domain"
	"devboard/internal/store"
)

type analyticsRepo struct{ q queryer }

// Read caps each input independently and projects only analytics fields. Historical
// state events establish transitions; detailed outputs are never loaded.
func (r analyticsRepo) Read(ctx context.Context, project string, start, end time.Time, limit int) (out store.AnalyticsData, err error) {
	if limit <= 0 || limit > 20000 {
		limit = 20000
	}
	scope, args := "", []any{ms(end)}
	if project != "" {
		scope = " AND project_id = ?"
		args = append(args, project)
	}
	args = append(args, limit+1)
	rows, err := r.q.QueryContext(ctx, `SELECT id,project_id,state,created_at,archived_at,
 (SELECT json_group_array(label_id) FROM task_labels WHERE task_id=tasks.id)
 FROM tasks WHERE created_at < ?`+scope+` ORDER BY created_at DESC,id LIMIT ?`, args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var t domain.Task
		var created int64
		var archived sql.NullInt64
		var labels string
		if err = rows.Scan(&t.ID, &t.ProjectID, &t.State, &created, &archived, &labels); err != nil {
			break
		}
		t.CreatedAt, t.ArchivedAt = fromMS(created), fromNullMS(archived)
		if err = json.Unmarshal([]byte(labels), &t.LabelIDs); err != nil {
			break
		}
		out.Tasks = append(out.Tasks, t)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(out.Tasks) > limit {
		out.Truncated = true
		out.Tasks = out.Tasks[:limit]
	}
	rows, err = r.q.QueryContext(ctx, `SELECT id,task_id,project_id,agent_id,model,state,attempt,created_at,ended_at,
 COALESCE(json_extract(distribution,'$.runnerId'),''),COALESCE(json_extract(distribution,'$.usage'),'{}')
 FROM runs WHERE created_at < ?`+scope+` ORDER BY created_at DESC,rowid DESC LIMIT ?`, args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var run domain.Run
		var created int64
		var ended sql.NullInt64
		var usage string
		if err = rows.Scan(&run.ID, &run.TaskID, &run.ProjectID, &run.AgentID, &run.Model, &run.State, &run.Attempt, &created, &ended, &run.RunnerID, &usage); err != nil {
			break
		}
		run.CreatedAt, run.EndedAt = fromMS(created), fromNullMS(ended)
		if err = json.Unmarshal([]byte(usage), &run.Usage); err != nil {
			break
		}
		out.Runs = append(out.Runs, run)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(out.Runs) > limit {
		out.Truncated = true
		out.Runs = out.Runs[:limit]
	}
	// Compact payloads keep task descriptions, handoffs, prompts and other evidence private.
	evs, err := (eventRepo{r.q}).list(ctx, `SELECT seq,type,project_id,task_id,run_id,
 CASE WHEN json_valid(payload) THEN json_object('state',json_extract(payload,'$.state'),
 'acceptance',json_extract(payload,'$.run.usage.acceptance'),'ok',json_extract(payload,'$.ok'),
 'outcome',json_extract(payload,'$.outcome')) ELSE '{}' END,created_at
 FROM events WHERE type IN ('task.created','task.updated','run.state_changed','agent.completed','git.merged')
 AND created_at < ?`+scope+` ORDER BY seq DESC LIMIT ?`, args...)
	if err != nil {
		return out, err
	}
	if len(evs) > limit {
		out.Truncated = true
		evs = evs[:limit]
	}
	out.Events = evs
	noticeArgs := []any{ms(start), ms(end)}
	if project != "" {
		noticeArgs = append(noticeArgs, project)
	}
	noticeArgs = append(noticeArgs, limit+1)
	notices, err := (eventRepo{r.q}).list(ctx, `SELECT seq,type,project_id,task_id,run_id,
 json_object('stream','system','text',json_extract(payload,'$.text')),created_at FROM events
 WHERE type='agent.output' AND json_valid(payload) AND json_extract(payload,'$.stream')='system'
 AND created_at>=? AND created_at<?`+scope+`
 AND (json_extract(payload,'$.text')='Rate limit: rejected'
 OR json_extract(payload,'$.text') LIKE 'The turn failed: %') ORDER BY seq DESC LIMIT ?`, noticeArgs...)
	if err != nil {
		return out, err
	}
	if len(notices) > limit {
		out.Truncated = true
		notices = notices[:limit]
	}
	out.Events = append(out.Events, notices...)
	sort.Slice(out.Events, func(i, j int) bool { return out.Events[i].Seq < out.Events[j].Seq })
	return out, nil
}
