package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
	"werkbord/internal/devicewire"
	"werkbord/internal/domain"
)

type deviceOperationRepo struct{ q queryer }

func (r deviceOperationRepo) Get(ctx context.Context, id string) (*devicewire.Record, error) {
	var op, result, hash string
	var at int64
	err := r.q.QueryRowContext(ctx, `SELECT operation,result,request_hash,updated_at FROM device_operations WHERE id=?`, id).Scan(&op, &result, &hash, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var out devicewire.Record
	out.Hash = hash
	out.UpdatedAt = fromMS(at)
	if err = json.Unmarshal([]byte(op), &out.Operation); err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(result), &out.Result); err != nil {
		return nil, err
	}
	return &out, nil
}
func (r deviceOperationRepo) Save(ctx context.Context, in *devicewire.Record) error {
	op, err := json.Marshal(in.Operation)
	if err != nil {
		return err
	}
	result, err := json.Marshal(in.Result)
	if err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx, `INSERT INTO device_operations(id,runner_id,actor,request_hash,operation,result,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET operation=CASE WHEN excluded.state!='pending' AND json_extract(device_operations.operation,'$.request.kind')='terminal.input' THEN json_remove(device_operations.operation,'$.request.data') ELSE device_operations.operation END,result=excluded.result,state=excluded.state,updated_at=excluded.updated_at`, in.Operation.ID, in.Operation.Request.RunnerID, in.Operation.Actor, in.Hash, string(op), string(result), in.Result.State, ms(in.Operation.CreatedAt), ms(in.UpdatedAt))
	return err
}
func (r deviceOperationRepo) Pending(ctx context.Context, runner string) ([]devicewire.Record, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT operation,result,request_hash,updated_at FROM device_operations WHERE runner_id=? AND state='pending' ORDER BY created_at,id LIMIT 65`, runner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []devicewire.Record{}
	for rows.Next() {
		var op, res string
		var at int64
		var item devicewire.Record
		if err = rows.Scan(&op, &res, &item.Hash, &at); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(op), &item.Operation); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(res), &item.Result); err != nil {
			return nil, err
		}
		item.UpdatedAt = fromMS(at)
		out = append(out, item)
	}
	return out, rows.Err()
}

// Only read/output and idempotent resize receipts expire. Other receipts remain so the
// same request identifier can never silently execute again after a reconnect.
func (r deviceOperationRepo) Prune(ctx context.Context, before time.Time) error {
	_, err := r.q.ExecContext(ctx, `DELETE FROM device_operations WHERE state!='pending' AND updated_at<? AND json_extract(operation,'$.request.kind') IN ('git.status','terminal.output','terminal.resize')`, ms(before))
	return err
}
