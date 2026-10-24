package sqlite

import (
	"context"
	"encoding/json"
	"fmt"

	"devboard/internal/domain"
	"devboard/internal/store"
)

type personalRepo struct{ q queryer }

func (t *txn) Personal() store.PersonalRepo { return personalRepo{t.q} }

// Table names below are private constants chosen by code, never user input.
func (r personalRepo) create(ctx context.Context, table string, meta *domain.PersonalMeta, value any, collection string) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	cols, args, marks := "id, revision, created_at, updated_at, document", []any{meta.ID, 1, ms(meta.CreatedAt), ms(meta.UpdatedAt), string(raw)}, "?, ?, ?, ?, ?"
	if collection != "" {
		cols += ", collection_id"
		args = append(args, collection)
		marks += ", ?"
	}
	_, err = r.q.ExecContext(ctx, `INSERT INTO `+table+` (`+cols+`) VALUES (`+marks+`)`, args...)
	if isUniqueViolation(err) {
		return domain.ErrDuplicate
	}
	if err == nil {
		meta.Revision = 1
	}
	return err
}
func scanPersonal(s interface{ Scan(...any) error }, meta *domain.PersonalMeta, value any) error {
	var created, updated int64
	var raw string
	if err := s.Scan(&meta.ID, &meta.Revision, &created, &updated, &raw); err != nil {
		return err
	}
	meta.CreatedAt, meta.UpdatedAt = fromMS(created), fromMS(updated)
	return json.Unmarshal([]byte(raw), value)
}
func (r personalRepo) get(ctx context.Context, table, id, collection string, meta *domain.PersonalMeta, value any) error {
	where, args := "id = ?", []any{id}
	if collection != "" {
		where += " AND collection_id = ?"
		args = append(args, collection)
	}
	return notFound(scanPersonal(r.q.QueryRowContext(ctx, `SELECT id, revision, created_at, updated_at, document FROM `+table+` WHERE `+where, args...), meta, value), "personal object", id)
}
func (r personalRepo) update(ctx context.Context, table string, meta *domain.PersonalMeta, value any, collection string) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	args := []any{string(raw), ms(meta.UpdatedAt), meta.ID, meta.Revision}
	where := "id = ? AND revision = ?"
	if collection != "" {
		where += " AND collection_id = ?"
		args = append(args, collection)
	}
	result, err := r.q.ExecContext(ctx, `UPDATE `+table+` SET document = ?, updated_at = ?, revision = revision + 1 WHERE `+where, args...)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return domain.ErrConflict
	}
	meta.Revision++
	return nil
}
func (r personalRepo) delete(ctx context.Context, table, id, collection string, revision int64) error {
	where, args := "id = ? AND revision = ?", []any{id, revision}
	if collection != "" {
		where += " AND collection_id = ?"
		args = append(args, collection)
	}
	// Explicit bounded existence probes avoid accidental destructive cascading.
	if table == "personal_collections" {
		var used bool
		if err := r.q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM personal_records WHERE collection_id = ?) OR EXISTS(SELECT 1 FROM personal_workspace_collections WHERE collection_id = ?)`, id, id).Scan(&used); err != nil {
			return err
		}
		if used {
			return fmt.Errorf("%w: collection still has records or workspace references", domain.ErrConflict)
		}
	}
	result, err := r.q.ExecContext(ctx, `DELETE FROM `+table+` WHERE `+where, args...)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return domain.ErrConflict
	}
	return nil
}
func listPersonal[T any](ctx context.Context, r personalRepo, table, prefix, collection string, query domain.PersonalQuery, scan func(interface{ Scan(...any) error }) (*T, error)) ([]T, error) {
	if err := domain.ValidatePersonalQuery(query, prefix); err != nil {
		return nil, err
	}
	where, args := "id > ?", []any{query.After}
	if collection != "" {
		where += " AND collection_id = ?"
		args = append(args, collection)
	}
	args = append(args, query.Limit)
	rows, err := r.q.QueryContext(ctx, `SELECT id, revision, created_at, updated_at, document FROM `+table+` WHERE `+where+` ORDER BY id LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}
func (r personalRepo) workspaceRefs(ctx context.Context, w *domain.PersonalWorkspace) error {
	if _, err := r.q.ExecContext(ctx, `DELETE FROM personal_workspace_collections WHERE workspace_id = ?`, w.ID); err != nil {
		return err
	}
	for _, id := range w.CollectionIDs {
		if _, err := r.q.ExecContext(ctx, `INSERT INTO personal_workspace_collections(workspace_id, collection_id) VALUES (?, ?)`, w.ID, id); err != nil {
			return err
		}
	}
	return nil
}
func (r personalRepo) CreateWorkspace(ctx context.Context, w *domain.PersonalWorkspace) error {
	if err := r.create(ctx, "personal_workspaces", &w.PersonalMeta, w.PersonalWorkspaceSpec, ""); err != nil {
		return err
	}
	return r.workspaceRefs(ctx, w)
}
func (r personalRepo) GetWorkspace(ctx context.Context, id string) (*domain.PersonalWorkspace, error) {
	w := new(domain.PersonalWorkspace)
	err := r.get(ctx, "personal_workspaces", id, "", &w.PersonalMeta, &w.PersonalWorkspaceSpec)
	return w, err
}
func (r personalRepo) UpdateWorkspace(ctx context.Context, w *domain.PersonalWorkspace) error {
	if err := r.update(ctx, "personal_workspaces", &w.PersonalMeta, w.PersonalWorkspaceSpec, ""); err != nil {
		return err
	}
	return r.workspaceRefs(ctx, w)
}
func (r personalRepo) DeleteWorkspace(ctx context.Context, id string, revision int64) error {
	return r.delete(ctx, "personal_workspaces", id, "", revision)
}
func (r personalRepo) ListWorkspaces(ctx context.Context, q domain.PersonalQuery) ([]domain.PersonalWorkspace, error) {
	return listPersonal(ctx, r, "personal_workspaces", domain.PrefixPersonalWorkspace, "", q, func(s interface{ Scan(...any) error }) (*domain.PersonalWorkspace, error) {
		w := new(domain.PersonalWorkspace)
		err := scanPersonal(s, &w.PersonalMeta, &w.PersonalWorkspaceSpec)
		return w, err
	})
}
func (r personalRepo) CreateCollection(ctx context.Context, c *domain.PersonalCollection) error {
	return r.create(ctx, "personal_collections", &c.PersonalMeta, c.PersonalCollectionSpec, "")
}
func (r personalRepo) GetCollection(ctx context.Context, id string) (*domain.PersonalCollection, error) {
	c := new(domain.PersonalCollection)
	err := r.get(ctx, "personal_collections", id, "", &c.PersonalMeta, &c.PersonalCollectionSpec)
	return c, err
}
func (r personalRepo) UpdateCollection(ctx context.Context, c *domain.PersonalCollection) error {
	return r.update(ctx, "personal_collections", &c.PersonalMeta, c.PersonalCollectionSpec, "")
}
func (r personalRepo) DeleteCollection(ctx context.Context, id string, revision int64) error {
	return r.delete(ctx, "personal_collections", id, "", revision)
}
func (r personalRepo) ListCollections(ctx context.Context, q domain.PersonalQuery) ([]domain.PersonalCollection, error) {
	return listPersonal(ctx, r, "personal_collections", domain.PrefixPersonalCollection, "", q, func(s interface{ Scan(...any) error }) (*domain.PersonalCollection, error) {
		c := new(domain.PersonalCollection)
		err := scanPersonal(s, &c.PersonalMeta, &c.PersonalCollectionSpec)
		return c, err
	})
}
func (r personalRepo) CreateRecord(ctx context.Context, v *domain.PersonalRecord) error {
	if !domain.ValidPersonalID(v.CollectionID, domain.PrefixPersonalCollection) {
		return domain.ErrInvalid
	}
	return r.create(ctx, "personal_records", &v.PersonalMeta, v.Values, v.CollectionID)
}
func (r personalRepo) GetRecord(ctx context.Context, collection, id string) (*domain.PersonalRecord, error) {
	if !domain.ValidPersonalID(collection, domain.PrefixPersonalCollection) {
		return nil, domain.ErrInvalid
	}
	v := &domain.PersonalRecord{CollectionID: collection}
	err := r.get(ctx, "personal_records", id, collection, &v.PersonalMeta, &v.Values)
	return v, err
}
func (r personalRepo) UpdateRecord(ctx context.Context, v *domain.PersonalRecord) error {
	if !domain.ValidPersonalID(v.CollectionID, domain.PrefixPersonalCollection) {
		return domain.ErrInvalid
	}
	return r.update(ctx, "personal_records", &v.PersonalMeta, v.Values, v.CollectionID)
}
func (r personalRepo) DeleteRecord(ctx context.Context, collection, id string, revision int64) error {
	if !domain.ValidPersonalID(collection, domain.PrefixPersonalCollection) {
		return domain.ErrInvalid
	}
	return r.delete(ctx, "personal_records", id, collection, revision)
}
func (r personalRepo) ListRecords(ctx context.Context, collection string, q domain.PersonalQuery) ([]domain.PersonalRecord, error) {
	if !domain.ValidPersonalID(collection, domain.PrefixPersonalCollection) {
		return nil, domain.ErrInvalid
	}
	return listPersonal(ctx, r, "personal_records", domain.PrefixPersonalRecord, collection, q, func(s interface{ Scan(...any) error }) (*domain.PersonalRecord, error) {
		v := &domain.PersonalRecord{CollectionID: collection}
		err := scanPersonal(s, &v.PersonalMeta, &v.Values)
		return v, err
	})
}

func (r personalRepo) WorkspaceRevision(ctx context.Context, id string, revision int64) (*domain.PersonalWorkspace, error) {
	w := new(domain.PersonalWorkspace)
	err := notFound(scanPersonal(r.q.QueryRowContext(ctx, `SELECT workspace_id, revision, created_at, updated_at, document FROM personal_workspace_history WHERE workspace_id = ? AND revision = ?`, id, revision), &w.PersonalMeta, &w.PersonalWorkspaceSpec), "workspace revision", id)
	return w, err
}
func (r personalRepo) WorkspaceRequest(ctx context.Context, key string) (string, error) {
	var id string
	err := notFound(r.q.QueryRowContext(ctx, `SELECT workspace_id FROM personal_workspace_requests WHERE request_key = ?`, key).Scan(&id), "workspace request", key)
	return id, err
}
func (r personalRepo) SaveWorkspaceRequest(ctx context.Context, key, id string) error {
	_, err := r.q.ExecContext(ctx, `INSERT INTO personal_workspace_requests(request_key, workspace_id) VALUES (?, ?)`, key, id)
	return err
}

// CollectionUsage is bounded even when many workspaces depend on one collection.
func (r personalRepo) CollectionUsage(ctx context.Context, id string) (*domain.PersonalCollectionUsage, error) {
	u := &domain.PersonalCollectionUsage{CollectionID: id, Workspaces: []domain.PersonalWorkspaceReference{}}
	err := notFound(r.q.QueryRowContext(ctx, `SELECT json_extract(c.document, '$.name'), c.revision, COALESCE(s.owner_workspace_id, ''), s.generation,
 (SELECT COUNT(*) FROM personal_records WHERE collection_id = c.id),
 (SELECT COUNT(*) FROM personal_workspace_collections WHERE collection_id = c.id)
 FROM personal_collections c JOIN personal_collection_state s ON s.collection_id = c.id WHERE c.id = ?`, id).Scan(&u.Name, &u.Revision, &u.OwnerWorkspaceID, &u.Generation, &u.RecordCount, &u.ReferenceCount), "personal collection", id)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.QueryContext(ctx, `SELECT w.id, json_extract(w.document, '$.name') FROM personal_workspaces w JOIN personal_workspace_collections r ON r.workspace_id = w.id WHERE r.collection_id = ? ORDER BY w.id LIMIT 20`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref domain.PersonalWorkspaceReference
		if err := rows.Scan(&ref.ID, &ref.Name); err != nil {
			return nil, err
		}
		u.Workspaces = append(u.Workspaces, ref)
	}
	return u, rows.Err()
}

// Only the lifecycle service invokes this, after removing the authorised workspace.
// Recheck live references here as defence in depth. The caller owns the transaction.
func (r personalRepo) DeleteCollectionRecords(ctx context.Context, id string) error {
	var used bool
	if err := r.q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM personal_workspace_collections WHERE collection_id = ?)`, id).Scan(&used); err != nil {
		return err
	}
	if used {
		return domain.ErrConflict
	}
	_, err := r.q.ExecContext(ctx, `DELETE FROM personal_records WHERE collection_id = ?`, id)
	return err
}
func (r personalRepo) WorkspaceHistory(ctx context.Context, id string) ([]domain.PersonalMeta, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT workspace_id, revision, created_at, updated_at FROM personal_workspace_history WHERE workspace_id = ? ORDER BY revision DESC LIMIT 100`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.PersonalMeta{}
	for rows.Next() {
		var m domain.PersonalMeta
		var created, updated int64
		if err := rows.Scan(&m.ID, &m.Revision, &created, &updated); err != nil {
			return nil, err
		}
		m.CreatedAt, m.UpdatedAt = fromMS(created), fromMS(updated)
		out = append(out, m)
	}
	return out, rows.Err()
}

// WorkspaceIcons reads only the small identity projection, never full definitions or records.
func (r personalRepo) WorkspaceIcons(ctx context.Context) ([]domain.PersonalWorkspaceIcon, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT id, json_extract(document,'$.name'), json_extract(document,'$.icon') FROM personal_workspaces ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.PersonalWorkspaceIcon{}
	for rows.Next() {
		var icon domain.PersonalWorkspaceIcon
		if err := rows.Scan(&icon.ID, &icon.Name, &icon.Icon); err != nil {
			return nil, err
		}
		out = append(out, icon)
	}
	return out, rows.Err()
}
